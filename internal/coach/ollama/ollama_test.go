package ollama

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
	var captured chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path: got %q want /api/chat", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		respond(w, chatResponse{
			Message:      chatMessage{Role: "assistant", Content: "What invariant changes between the passing and failing tests?"},
			PromptEval:   100,
			ResponseEval: 20,
		})
	}))
	defer srv.Close()

	c := New("codellama")
	c.BaseURL = srv.URL

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
	if captured.Model != "codellama" {
		t.Errorf("model: got %q want codellama", captured.Model)
	}
	if captured.Stream {
		t.Error("expected non-streaming request")
	}
	if captured.Options.NumPredict != DefaultMaxTokens {
		t.Errorf("num_predict: got %d want %d", captured.Options.NumPredict, DefaultMaxTokens)
	}
	if len(captured.Messages) != 2 || captured.Messages[0].Role != "system" || captured.Messages[1].Role != "user" {
		t.Fatalf("unexpected messages: %+v", captured.Messages)
	}
	if !strings.Contains(captured.Messages[1].Content, "tests are red") {
		t.Errorf("user message missing context: %q", captured.Messages[1].Content)
	}
	usage := c.Usage()
	if usage.Calls != 1 || usage.PromptEval != 100 || usage.ResponseEval != 20 {
		t.Errorf("usage not recorded: %+v", usage)
	}
}

func TestHintAPIErrorBubblesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"model is loading"}`))
	}))
	defer srv.Close()
	c := New("m")
	c.BaseURL = srv.URL
	if _, err := c.Hint(context.Background(), coach.HintRequest{Level: coach.LevelNudge}); err == nil {
		t.Fatal("expected non-2xx to surface as error")
	}
}

func TestGradeParsesScoreAndFeedback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(w, chatResponse{
			Message: chatMessage{Role: "assistant", Content: "42\nThe diagnosis names the right boundary class but misses the off-by-one."},
		})
	}))
	defer srv.Close()
	c := New("m")
	c.BaseURL = srv.URL
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

func TestRetryWrapperValidatesOllamaHints(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := "```go\nfunc leaked() int {\n\treturn 1\n}\n```"
		if calls == 2 {
			content = "Which assumption can you falsify with the smallest failing test?"
		}
		respond(w, chatResponse{Message: chatMessage{Role: "assistant", Content: content}})
	}))
	defer srv.Close()
	c := New("m")
	c.BaseURL = srv.URL
	wrapped := coach.RetryWithStricterPrompt(c, nil)
	hint, err := wrapped.Hint(context.Background(), coach.HintRequest{Level: coach.LevelNudge})
	if err != nil {
		t.Fatalf("Hint: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls: got %d want 2", calls)
	}
	if strings.Contains(hint.Content, "func leaked") {
		t.Fatalf("retry returned leaked code: %q", hint.Content)
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
		{"score=12: text", 12},
		{"no score", 0},
	}
	for _, tc := range cases {
		got, _ := parseGrade(tc.body)
		if got != tc.score {
			t.Errorf("parseGrade(%q) score=%d want %d", tc.body, got, tc.score)
		}
	}
}

func respond(w http.ResponseWriter, body chatResponse) {
	w.Header().Set("content-type", "application/json")
	if body.Message.Role == "" {
		body.Message.Role = "assistant"
	}
	_ = json.NewEncoder(w).Encode(body)
}
