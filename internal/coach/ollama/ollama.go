// Package ollama implements the coach.Coach interface against a local
// Ollama HTTP server.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
)

const (
	DefaultBaseURL     = "http://localhost:11434"
	DefaultModel       = "llama3.1"
	DefaultMaxTokens   = 512
	DefaultMaxTokensG  = 1024
	defaultHTTPTimeout = 120 * time.Second
)

// Usage aggregates token counts reported by Ollama across calls.
type Usage struct {
	Calls        int
	PromptEval   int
	ResponseEval int
}

// Coach implements coach.Coach against Ollama's /api/chat endpoint.
//
// Coach is safe for concurrent use; usage accounting takes a mutex.
type Coach struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
	MaxTokens  int

	mu    sync.Mutex
	usage Usage
}

// New returns a Coach configured for a local Ollama server.
func New(model string) *Coach {
	if model == "" {
		model = DefaultModel
	}
	return &Coach{
		BaseURL:    DefaultBaseURL,
		Model:      model,
		HTTPClient: &http.Client{Timeout: defaultHTTPTimeout},
		MaxTokens:  DefaultMaxTokens,
	}
}

// Usage returns a snapshot of token counters reported by Ollama.
func (c *Coach) Usage() Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usage
}

// Hint asks the local model for a short Socratic hint.
func (c *Coach) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
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
	body, err := c.complete(ctx, completeRequest{
		System:    system,
		User:      buildHintUserMessage(req),
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

// Grade scores a user submission against a rubric. The expected model format
// is "<score>\n<feedback>", but parsing is intentionally forgiving.
func (c *Coach) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
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

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options,omitempty"`
}

type chatOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message      chatMessage `json:"message"`
	PromptEval   int         `json:"prompt_eval_count"`
	ResponseEval int         `json:"eval_count"`
	Error        string      `json:"error,omitempty"`
}

func (c *Coach) complete(ctx context.Context, req completeRequest) (string, error) {
	body := chatRequest{
		Model: c.modelOrDefault(),
		Messages: []chatMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
		Stream:  false,
		Options: chatOptions{NumPredict: req.MaxTokens},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	endpoint := strings.TrimRight(c.baseURLOrDefault(), "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ollama response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("ollama %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse ollama response: %w", err)
	}
	if parsed.Error != "" {
		return "", errors.New("ollama: " + parsed.Error)
	}
	c.recordUsage(parsed)
	return parsed.Message.Content, nil
}

func (c *Coach) baseURLOrDefault() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c *Coach) modelOrDefault() string {
	if c.Model != "" {
		return c.Model
	}
	return DefaultModel
}

func (c *Coach) recordUsage(resp chatResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usage.Calls++
	c.usage.PromptEval += resp.PromptEval
	c.usage.ResponseEval += resp.ResponseEval
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
	scoreText := first
	if fields := strings.FieldsFunc(first, func(r rune) bool { return r < '0' || r > '9' }); len(fields) > 0 {
		scoreText = fields[0]
	}
	score, err := strconv.Atoi(scoreText)
	if err != nil {
		score = 0
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
