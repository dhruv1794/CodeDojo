// Package anthropic implements the coach.Coach interface against the
// Anthropic Messages API. It uses prompt caching on the stable system
// prompt + repo summary, and tracks per-session token usage and cost so
// `codedojo stats` can report it.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
)

const (
	DefaultModel       = "claude-sonnet-4-6"
	DefaultEndpoint    = "https://api.anthropic.com/v1/messages"
	DefaultAPIVersion  = "2023-06-01"
	DefaultMaxTokens   = 512
	DefaultMaxTokensG  = 1024
	defaultHTTPTimeout = 60 * time.Second
)

// Pricing in USD per million tokens for the default Sonnet 4.6 model.
// Override Pricing on the Coach for other models.
var DefaultPricing = Pricing{
	InputPerM:        3.00,
	OutputPerM:       15.00,
	CacheWritePerM:   3.75,
	CacheReadPerM:    0.30,
}

type Pricing struct {
	InputPerM      float64
	OutputPerM     float64
	CacheWritePerM float64
	CacheReadPerM  float64
}

// Usage aggregates token counts across all calls a Coach has made.
type Usage struct {
	Calls                    int
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// Coach implements coach.Coach against the Anthropic Messages API.
//
// Coach is safe for concurrent use; usage accounting takes a mutex.
type Coach struct {
	APIKey      string
	Model       string
	Endpoint    string
	APIVersion  string
	HTTPClient  *http.Client
	RepoSummary string
	Pricing     Pricing
	MaxTokens   int

	mu    sync.Mutex
	usage Usage
}

// New returns a Coach with sensible defaults. APIKey is required.
func New(apiKey string) *Coach {
	return &Coach{
		APIKey:     apiKey,
		Model:      DefaultModel,
		Endpoint:   DefaultEndpoint,
		APIVersion: DefaultAPIVersion,
		HTTPClient: &http.Client{Timeout: defaultHTTPTimeout},
		Pricing:    DefaultPricing,
		MaxTokens:  DefaultMaxTokens,
	}
}

// Usage returns a snapshot of token + call counters.
func (c *Coach) Usage() Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usage
}

// Cost returns the running USD cost for accumulated usage at Pricing.
func (c *Coach) Cost() float64 {
	u := c.Usage()
	p := c.Pricing
	return (float64(u.InputTokens)*p.InputPerM +
		float64(u.OutputTokens)*p.OutputPerM +
		float64(u.CacheCreationInputTokens)*p.CacheWritePerM +
		float64(u.CacheReadInputTokens)*p.CacheReadPerM) / 1_000_000.0
}

// Hint asks the model for a Socratic hint at the requested level.
func (c *Coach) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	if c.APIKey == "" {
		return coach.Hint{}, errors.New("anthropic: APIKey is empty")
	}
	system, err := prompts.Render("reviewer/system.tmpl", map[string]any{
		"Difficulty": 0,
		"HintBudget": 0,
		"Level":      hintLevelName(req.Level),
		"Strict":     req.Strict,
		"Context":    "",
	})
	if err != nil {
		return coach.Hint{}, fmt.Errorf("render hint system prompt: %w", err)
	}
	userMsg := buildHintUserMessage(req)
	body, err := c.complete(ctx, completeRequest{
		System:    system,
		User:      userMsg,
		MaxTokens: c.maxTokens(false),
	})
	if err != nil {
		return coach.Hint{}, err
	}
	return coach.Hint{
		Level:   req.Level,
		Content: strings.TrimSpace(body),
		Cost:    coach.HintCost(req.Level),
	}, nil
}

// Grade scores a user submission against a rubric using a separate
// grading prompt. The model returns "<score>\n<feedback>" which we parse.
func (c *Coach) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	if c.APIKey == "" {
		return coach.Grade{}, errors.New("anthropic: APIKey is empty")
	}
	system := "You are a strict but fair grader. Respond with the integer score on the first line and a one-sentence justification on the second line. Never include code."
	userMsg := fmt.Sprintf("Rubric:\n%s\n\nAnswer:\n%s", req.Rubric, req.Answer)
	body, err := c.complete(ctx, completeRequest{
		System:    system,
		User:      userMsg,
		MaxTokens: c.maxTokens(true),
	})
	if err != nil {
		return coach.Grade{}, err
	}
	score, feedback := parseGrade(body)
	return coach.Grade{Score: score, Feedback: feedback}, nil
}

func (c *Coach) maxTokens(grading bool) int {
	if c.MaxTokens > 0 {
		if grading && c.MaxTokens < DefaultMaxTokensG {
			return DefaultMaxTokensG
		}
		return c.MaxTokens
	}
	if grading {
		return DefaultMaxTokensG
	}
	return DefaultMaxTokens
}

type completeRequest struct {
	System    string
	User      string
	MaxTokens int
}

type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    []systemBlock    `json:"system,omitempty"`
	Messages  []messageRequest `json:"messages"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type messageRequest struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type messagesResponse struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
	Usage   apiUsage       `json:"usage"`
	Error   *apiError      `json:"error,omitempty"`
}

type apiUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *Coach) complete(ctx context.Context, req completeRequest) (string, error) {
	systemBlocks := c.systemBlocks(req.System)
	body := messagesRequest{
		Model:     c.modelOrDefault(),
		MaxTokens: req.MaxTokens,
		System:    systemBlocks,
		Messages: []messageRequest{{
			Role:    "user",
			Content: []contentBlock{{Type: "text", Text: req.User}},
		}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	version := c.APIVersion
	if version == "" {
		version = DefaultAPIVersion
	}
	httpReq.Header.Set("anthropic-version", version)

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read anthropic response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("anthropic %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse anthropic response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic %s: %s", parsed.Error.Type, parsed.Error.Message)
	}
	c.recordUsage(parsed.Usage)
	var out strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	return out.String(), nil
}

func (c *Coach) systemBlocks(rendered string) []systemBlock {
	// Two cacheable prefix blocks: rules + repo summary. Both marked
	// ephemeral so a hot session reuses them across consecutive calls.
	blocks := []systemBlock{{
		Type:         "text",
		Text:         rendered,
		CacheControl: &cacheControl{Type: "ephemeral"},
	}}
	if c.RepoSummary != "" {
		blocks = append(blocks, systemBlock{
			Type:         "text",
			Text:         "Codebase summary:\n" + c.RepoSummary,
			CacheControl: &cacheControl{Type: "ephemeral"},
		})
	}
	return blocks
}

func (c *Coach) modelOrDefault() string {
	if c.Model != "" {
		return c.Model
	}
	return DefaultModel
}

func (c *Coach) recordUsage(u apiUsage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usage.Calls++
	c.usage.InputTokens += u.InputTokens
	c.usage.OutputTokens += u.OutputTokens
	c.usage.CacheCreationInputTokens += u.CacheCreationInputTokens
	c.usage.CacheReadInputTokens += u.CacheReadInputTokens
}

func buildHintUserMessage(req coach.HintRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Requested hint level: %s\n", hintLevelName(req.Level))
	if req.Strict {
		b.WriteString("Strict mode is on; avoid identifiers from the repository.\n")
	}
	if req.Context != "" {
		b.WriteString("\nWhat the user is working on:\n")
		b.WriteString(req.Context)
		b.WriteString("\n")
	}
	b.WriteString("\nRespond with one short Socratic hint at the requested shape. Do not include code.")
	return b.String()
}

func parseGrade(body string) (int, string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, ""
	}
	first, rest, _ := strings.Cut(body, "\n")
	first = strings.TrimSpace(first)
	score := 0
	for _, r := range first {
		if r >= '0' && r <= '9' {
			score = score*10 + int(r-'0')
			continue
		}
		break
	}
	feedback := strings.TrimSpace(rest)
	if feedback == "" {
		feedback = first
	}
	return score, feedback
}

func hintLevelName(level coach.HintLevel) string {
	switch level {
	case coach.LevelNudge:
		return "nudge"
	case coach.LevelQuestion:
		return "question"
	case coach.LevelPointer:
		return "pointer"
	case coach.LevelConcept:
		return "concept"
	default:
		return "unknown"
	}
}
