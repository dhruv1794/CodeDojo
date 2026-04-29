//go:build integration

package anthropic

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/validator"
)

// TestLiveHint exercises the real Anthropic API. It is gated by the
// `integration` build tag and will skip unless ANTHROPIC_API_KEY is set.
//
//	go test -tags=integration ./internal/coach/anthropic/... -run TestLive
func TestLiveHint(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := New(apiKey)
	c.RepoSummary = "Tiny calculator package: add, subtract, divide. Tests live alongside source."

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
	if usage.Calls < 1 || usage.InputTokens == 0 {
		t.Errorf("expected usage to be recorded, got %+v", usage)
	}
	t.Logf("hint: %s", hint.Content)
	t.Logf("usage: %+v cost=$%.6f", usage, c.Cost())
}

func TestLiveGrade(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := New(apiKey)
	g, err := c.Grade(ctx, coach.GradeRequest{
		Rubric: "Score from 0 to 50 how well the user diagnosis names the off-by-one boundary.",
		Answer: "The loop ran one element short because the comparison used <= instead of <.",
	})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if g.Score < 0 || g.Score > 100 {
		t.Errorf("grade score out of range: %d", g.Score)
	}
	t.Logf("grade: %d feedback=%q", g.Score, g.Feedback)
}
