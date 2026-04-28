package coach

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRetryWithStricterPromptAcceptsValidHintFirstTry(t *testing.T) {
	t.Parallel()

	inner := &scriptedCoach{
		hints: []Hint{
			{Level: LevelQuestion, Content: "Which branch would disprove your current assumption?", Cost: 10},
		},
	}
	wrapped := RetryWithStricterPrompt(inner, nil)

	got, err := wrapped.Hint(context.Background(), HintRequest{
		SessionID: "sess-1",
		Level:     LevelQuestion,
		Context:   "failing test",
	})
	if err != nil {
		t.Fatalf("Hint() error = %v", err)
	}
	if got.Content != "Which branch would disprove your current assumption?" {
		t.Fatalf("Hint() content = %q, want valid first hint", got.Content)
	}
	if len(inner.requests) != 1 {
		t.Fatalf("inner Hint calls = %d, want 1", len(inner.requests))
	}
	if inner.requests[0].Strict {
		t.Fatal("first attempt Strict = true, want false")
	}
}

func TestRetryWithStricterPromptRetriesInvalidHintWithStrictPrompt(t *testing.T) {
	t.Parallel()

	inner := &scriptedCoach{
		hints: []Hint{
			{Level: LevelPointer, Content: "Look at CalculateTotal.", Cost: 20},
			{Level: LevelPointer, Content: "Ask which invariant changed near the failing path.", Cost: 20},
		},
	}
	wrapped := RetryWithStricterPrompt(inner, []string{"CalculateTotal"})

	got, err := wrapped.Hint(context.Background(), HintRequest{
		SessionID: "sess-1",
		Level:     LevelPointer,
	})
	if err != nil {
		t.Fatalf("Hint() error = %v", err)
	}
	if got.Content != "Ask which invariant changed near the failing path." {
		t.Fatalf("Hint() content = %q, want second valid hint", got.Content)
	}
	if len(inner.requests) != 2 {
		t.Fatalf("inner Hint calls = %d, want 2", len(inner.requests))
	}
	if inner.requests[0].Strict {
		t.Fatal("first attempt Strict = true, want false")
	}
	if !inner.requests[1].Strict {
		t.Fatal("second attempt Strict = false, want true")
	}
}

func TestRetryWithStricterPromptFallsBackAfterThreeInvalidHints(t *testing.T) {
	t.Parallel()

	inner := &scriptedCoach{
		hints: []Hint{
			{Level: LevelConcept, Content: "func answer() int { return 42 }", Cost: 30},
			{Level: LevelConcept, Content: "func answer() int { return 42 }", Cost: 30},
			{Level: LevelConcept, Content: "func answer() int { return 42 }", Cost: 30},
		},
	}
	wrapped := RetryWithStricterPrompt(inner, nil)

	got, err := wrapped.Hint(context.Background(), HintRequest{
		SessionID: "sess-1",
		Level:     LevelConcept,
	})
	if err != nil {
		t.Fatalf("Hint() error = %v", err)
	}
	if got.Level != LevelConcept {
		t.Fatalf("fallback Level = %v, want %v", got.Level, LevelConcept)
	}
	if got.Cost != 0 {
		t.Fatalf("fallback Cost = %d, want 0", got.Cost)
	}
	if !strings.Contains(got.Content, "I cannot give a more specific hint") {
		t.Fatalf("fallback content = %q, want generic fallback", got.Content)
	}
	if len(inner.requests) != 3 {
		t.Fatalf("inner Hint calls = %d, want 3", len(inner.requests))
	}
	if inner.requests[0].Strict || !inner.requests[1].Strict || !inner.requests[2].Strict {
		t.Fatalf("Strict attempts = [%v %v %v], want [false true true]", inner.requests[0].Strict, inner.requests[1].Strict, inner.requests[2].Strict)
	}
}

func TestRetryWithStricterPromptReturnsInnerHintError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("coach unavailable")
	inner := &scriptedCoach{hintErr: wantErr}
	wrapped := RetryWithStricterPrompt(inner, nil)

	_, err := wrapped.Hint(context.Background(), HintRequest{SessionID: "sess-1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Hint() error = %v, want %v", err, wantErr)
	}
	if len(inner.requests) != 1 {
		t.Fatalf("inner Hint calls = %d, want 1", len(inner.requests))
	}
}

func TestRetryWithStricterPromptPassesGradeThrough(t *testing.T) {
	t.Parallel()

	inner := &scriptedCoach{
		grade: Grade{Score: 37, Feedback: "names the likely failure mode"},
	}
	wrapped := RetryWithStricterPrompt(inner, []string{"CalculateTotal"})

	got, err := wrapped.Grade(context.Background(), GradeRequest{
		SessionID: "sess-1",
		Rubric:    "diagnosis",
		Answer:    "boundary condition",
	})
	if err != nil {
		t.Fatalf("Grade() error = %v", err)
	}
	if got != inner.grade {
		t.Fatalf("Grade() = %+v, want %+v", got, inner.grade)
	}
	if len(inner.gradeRequests) != 1 {
		t.Fatalf("inner Grade calls = %d, want 1", len(inner.gradeRequests))
	}
}

type scriptedCoach struct {
	hints         []Hint
	hintErr       error
	grade         Grade
	gradeErr      error
	requests      []HintRequest
	gradeRequests []GradeRequest
}

func (c *scriptedCoach) Hint(ctx context.Context, req HintRequest) (Hint, error) {
	c.requests = append(c.requests, req)
	if c.hintErr != nil {
		return Hint{}, c.hintErr
	}
	if len(c.hints) == 0 {
		return Hint{Level: req.Level, Content: "What assumption can you test next?", Cost: 10}, nil
	}
	hint := c.hints[0]
	c.hints = c.hints[1:]
	return hint, nil
}

func (c *scriptedCoach) Grade(ctx context.Context, req GradeRequest) (Grade, error) {
	c.gradeRequests = append(c.gradeRequests, req)
	if c.gradeErr != nil {
		return Grade{}, c.gradeErr
	}
	return c.grade, nil
}
