//go:build integration

package ollama

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/validator"
)

// TestLiveHint exercises a real local Ollama server. It is gated by the
// `integration` build tag and skips unless CODEDOJO_OLLAMA_INTEGRATION=1.
//
//	go test -tags=integration ./internal/coach/ollama/... -run TestLive
func TestLiveHint(t *testing.T) {
	if os.Getenv("CODEDOJO_OLLAMA_INTEGRATION") != "1" {
		t.Skip("CODEDOJO_OLLAMA_INTEGRATION=1 not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := New(os.Getenv("OLLAMA_MODEL"))
	if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
		c.BaseURL = baseURL
	}
	wrapped := coach.RetryWithStricterPrompt(c, []string{"DivideByZero"})
	hint, err := wrapped.Hint(ctx, coach.HintRequest{
		Level:   coach.LevelNudge,
		Context: "Tests for divide are failing on the zero-divisor case.",
	})
	if err != nil {
		t.Fatalf("Hint: %v", err)
	}
	if hint.Content == "" {
		t.Fatal("expected non-empty hint content")
	}
	if r := validator.Validate(hint.Content, []string{"DivideByZero"}); !r.OK {
		t.Errorf("validator rejected live hint after retry: %s; content=%q", r.Reason, hint.Content)
	}
	usage := c.Usage()
	if usage.Calls < 1 {
		t.Errorf("expected usage call count to be recorded, got %+v", usage)
	}
	t.Logf("hint: %s", hint.Content)
	t.Logf("usage: %+v", usage)
}

func TestLiveServerReachable(t *testing.T) {
	if os.Getenv("CODEDOJO_OLLAMA_INTEGRATION") != "1" {
		t.Skip("CODEDOJO_OLLAMA_INTEGRATION=1 not set")
	}
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		t.Fatalf("ollama not reachable at %s: %v", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("ollama /api/tags status: %d", resp.StatusCode)
	}
}
