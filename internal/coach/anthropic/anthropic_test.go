package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/coach"
)

func TestHintHappyPath(t *testing.T) {
	var captured messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key: got %q want %q", got, "test-key")
		}
		if got := r.Header.Get("anthropic-version"); got != DefaultAPIVersion {
			t.Errorf("anthropic-version: got %q want %q", got, DefaultAPIVersion)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		respond(w, messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "What invariant changes between the passing and failing tests?"}},
			Usage: apiUsage{
				InputTokens:              100,
				OutputTokens:             20,
				CacheCreationInputTokens: 80,
				CacheReadInputTokens:     0,
			},
		})
	}))
	defer srv.Close()

	c := New("test-key")
	c.Endpoint = srv.URL
	c.RepoSummary = "tiny go module"

	hint, err := c.Hint(context.Background(), coach.HintRequest{Level: coach.LevelNudge, Context: "tests are red"})
	if err != nil {
		t.Fatalf("Hint: %v", err)
	}
	if hint.Content == "" {
		t.Fatal("expected non-empty hint content")
	}
	if hint.Cost != coach.HintCost(coach.LevelNudge) {
		t.Errorf("cost: got %d want %d", hint.Cost, coach.HintCost(coach.LevelNudge))
	}
	if captured.Model != DefaultModel {
		t.Errorf("model: got %q want %q", captured.Model, DefaultModel)
	}
	if len(captured.System) != 2 {
		t.Fatalf("expected 2 system blocks (rules + repo summary), got %d", len(captured.System))
	}
	for i, blk := range captured.System {
		if blk.CacheControl == nil || blk.CacheControl.Type != "ephemeral" {
			t.Errorf("system block %d missing cache_control ephemeral", i)
		}
	}
	usage := c.Usage()
	if usage.Calls != 1 || usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.CacheCreationInputTokens != 80 {
		t.Errorf("usage not recorded: %+v", usage)
	}
}

func TestHintRequiresAPIKey(t *testing.T) {
	c := &Coach{Model: DefaultModel, Endpoint: "http://unused"}
	if _, err := c.Hint(context.Background(), coach.HintRequest{Level: coach.LevelNudge}); err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
}

func TestHintAPIErrorBubblesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()
	c := New("k")
	c.Endpoint = srv.URL
	if _, err := c.Hint(context.Background(), coach.HintRequest{Level: coach.LevelNudge}); err == nil {
		t.Fatal("expected non-2xx to surface as error")
	}
}

func TestGradeParsesScoreAndFeedback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(w, messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "42\nThe diagnosis names the right boundary class but misses the off-by-one."}},
			Usage:   apiUsage{InputTokens: 200, OutputTokens: 40},
		})
	}))
	defer srv.Close()
	c := New("k")
	c.Endpoint = srv.URL
	g, err := c.Grade(context.Background(), coach.GradeRequest{Rubric: "rubric", Answer: "answer"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if g.Score != 42 {
		t.Errorf("score: got %d want %d", g.Score, 42)
	}
	if !strings.Contains(g.Feedback, "off-by-one") {
		t.Errorf("feedback: got %q", g.Feedback)
	}
}

func TestParseGradeEdgeCases(t *testing.T) {
	cases := []struct {
		body  string
		score int
	}{
		{"", 0},
		{"  100  ", 100},
		{"7\nReason here.", 7},
		{"score=12: text", 0},
	}
	for _, tc := range cases {
		got, _ := parseGrade(tc.body)
		if got != tc.score {
			t.Errorf("parseGrade(%q) score=%d want %d", tc.body, got, tc.score)
		}
	}
}

func TestCostMath(t *testing.T) {
	c := New("k")
	c.recordUsage(apiUsage{InputTokens: 1_000_000, OutputTokens: 0})
	got := c.Cost()
	want := DefaultPricing.InputPerM
	if got != want {
		t.Errorf("Cost: got %f want %f", got, want)
	}
	c.recordUsage(apiUsage{OutputTokens: 1_000_000})
	if c.Cost() != DefaultPricing.InputPerM+DefaultPricing.OutputPerM {
		t.Errorf("Cost after output: %f", c.Cost())
	}
}

func respond(w http.ResponseWriter, body messagesResponse) {
	w.Header().Set("content-type", "application/json")
	if body.Type == "" {
		body.Type = "message"
	}
	if body.Role == "" {
		body.Role = "assistant"
	}
	_ = json.NewEncoder(w).Encode(body)
}
