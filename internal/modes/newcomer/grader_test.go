package newcomer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

func TestGradeAwardsCorrectnessWhenTestsPass(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0, Stdout: "ok"}}
	got, err := Grade(context.Background(), Task{FeatureDescription: "feature"}, Submission{
		UserDiff: "+func New() {}",
	}, GradeOptions{
		Coach:   stubGrader{score: 30},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.CorrectnessScore != 100 {
		t.Fatalf("CorrectnessScore = %d, want 100", got.CorrectnessScore)
	}
	if got.ApproachScore != 30 {
		t.Fatalf("ApproachScore = %d, want 30", got.ApproachScore)
	}
	if got.Score != 130 {
		t.Fatalf("Score = %d, want 130", got.Score)
	}
}

func TestGradeZeroCorrectnessWhenTestsFail(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 1, Stderr: "FAIL"}}
	got, err := Grade(context.Background(), Task{FeatureDescription: "feature"}, Submission{
		UserDiff: "+func New() {}",
	}, GradeOptions{
		Coach:   stubGrader{score: 25},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.CorrectnessScore != 0 {
		t.Fatalf("CorrectnessScore = %d, want 0", got.CorrectnessScore)
	}
	if got.Score != 25 {
		t.Fatalf("Score = %d, want 25", got.Score)
	}
}

func TestGradeAddsTestQualityBonus(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	got, err := Grade(context.Background(), Task{}, Submission{
		UserDiff:     "+",
		NewTestFuncs: 4,
	}, GradeOptions{
		Coach:   stubGrader{score: 0},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.TestQualityScore != 30 {
		t.Fatalf("TestQualityScore = %d, want 30 (cap)", got.TestQualityScore)
	}
	if got.Score != 130 {
		t.Fatalf("Score = %d, want 130", got.Score)
	}
}

func TestGradeAppliesHintDeductionsAndStreak(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	got, err := Grade(context.Background(), Task{}, Submission{
		UserDiff:  "+",
		HintCosts: []int{10, 20, -5},
		Streak:    3,
	}, GradeOptions{
		Coach:   stubGrader{score: 40},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.HintDeduction != 30 {
		t.Fatalf("HintDeduction = %d, want 30", got.HintDeduction)
	}
	base := 100 + 40 - 30
	if got.StreakBonus != base*3/20 {
		t.Fatalf("StreakBonus = %d, want %d", got.StreakBonus, base*3/20)
	}
	if got.Score != base+got.StreakBonus {
		t.Fatalf("Score = %d, want %d", got.Score, base+got.StreakBonus)
	}
}

func TestGradeSoftTimeCapDoesNotPenalizeUnderHour(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	started := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	got, err := Grade(context.Background(), Task{}, Submission{
		UserDiff:    "+",
		StartedAt:   started,
		SubmittedAt: started.Add(45 * time.Minute),
	}, GradeOptions{
		Coach:   stubGrader{score: 50},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.Score != 150 {
		t.Fatalf("Score = %d, want 150 (no time penalty under soft cap)", got.Score)
	}
}

func TestGradeSoftTimeCapPenalizesOverHour(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	started := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	got, err := Grade(context.Background(), Task{}, Submission{
		UserDiff:    "+",
		StartedAt:   started,
		SubmittedAt: started.Add(2 * time.Hour),
	}, GradeOptions{
		Coach:   stubGrader{score: 50},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.Score != 145 {
		t.Fatalf("Score = %d, want 145 (one-hour overflow -> -5)", got.Score)
	}
}

func TestGradeClampsApproachScore(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	got, err := Grade(context.Background(), Task{}, Submission{UserDiff: "+"}, GradeOptions{
		Coach:   stubGrader{score: 99},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.ApproachScore != 50 {
		t.Fatalf("ApproachScore = %d, want 50 (clamped)", got.ApproachScore)
	}
}

func TestGradeRejectsLeakyApproachFeedback(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	_, err := Grade(context.Background(), Task{
		BannedIdentifiers: []string{"Multiply"},
	}, Submission{UserDiff: "+"}, GradeOptions{
		Coach:   stubGrader{score: 30, feedback: "Use Multiply more directly."},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err == nil || !strings.Contains(err.Error(), "approach feedback failed validator") {
		t.Fatalf("Grade error = %v, want validator failure", err)
	}
}

func TestGradeReturnsCoachError(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	wantErr := errors.New("grader unavailable")
	_, err := Grade(context.Background(), Task{}, Submission{UserDiff: "+"}, GradeOptions{
		Coach:   stubGrader{err: wantErr},
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Grade error = %v, want %v", err, wantErr)
	}
}

func TestGradeRendersApproachRubric(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	grader := &capturingApproachGrader{score: 25}
	_, err := Grade(context.Background(), Task{
		FeatureDescription: "Add multiplication behavior covered by the original tests.",
		BannedIdentifiers:  []string{"Multiply", "TestMultiply"},
	}, Submission{
		SessionID: "sess-1",
		UserDiff:  "+func Mul(a, b int) int { return a * b }",
	}, GradeOptions{
		Coach:   grader,
		TestCmd: []string{"go", "test", "./..."},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if grader.request.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", grader.request.SessionID)
	}
	for _, want := range []string{
		"Add multiplication behavior",
		"Multiply, TestMultiply",
		"+func Mul(a, b int)",
	} {
		if !strings.Contains(grader.request.Rubric, want) {
			t.Fatalf("rubric does not contain %q:\n%s", want, grader.request.Rubric)
		}
	}
}

func TestGradeTruncatesLargeDiffs(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0}}
	grader := &capturingApproachGrader{score: 10}
	var b strings.Builder
	for i := 0; i < 600; i++ {
		b.WriteString("+ added line\n")
	}
	_, err := Grade(context.Background(), Task{}, Submission{
		UserDiff: b.String(),
	}, GradeOptions{
		Coach:       grader,
		TestCmd:     []string{"go", "test", "./..."},
		Sandbox:     sb,
		DiffLineCap: 100,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if !strings.Contains(grader.request.Rubric, "[...truncated") {
		t.Fatalf("rubric should contain truncation marker:\n%s", grader.request.Rubric)
	}
}

func TestCountNewTestFuncs(t *testing.T) {
	diff := `diff --git a/calc/calc_test.go b/calc/calc_test.go
+func TestAdd(t *testing.T) {
+func TestMultiply(t *testing.T) {
+	if got := 2; got != 2 {
+func helper(t *testing.T) {
-func TestRemoved(t *testing.T) {
`
	if got := CountNewTestFuncs(diff); got != 2 {
		t.Fatalf("CountNewTestFuncs() = %d, want 2", got)
	}
}

type fakeSandbox struct {
	result sandbox.ExecResult
	err    error
	cmds   [][]string
}

func (f *fakeSandbox) Exec(ctx context.Context, cmd []string) (sandbox.ExecResult, error) {
	f.cmds = append(f.cmds, cmd)
	return f.result, f.err
}

func (f *fakeSandbox) WriteFile(path string, data []byte) error { return nil }
func (f *fakeSandbox) ReadFile(path string) ([]byte, error)     { return nil, nil }
func (f *fakeSandbox) Diff() (string, error)                    { return "", nil }
func (f *fakeSandbox) Close() error                             { return nil }

type stubGrader struct {
	score    int
	feedback string
	err      error
}

func (s stubGrader) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (s stubGrader) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	if s.err != nil {
		return coach.Grade{}, s.err
	}
	return coach.Grade{Score: s.score, Feedback: s.feedback}, nil
}

type capturingApproachGrader struct {
	score   int
	request coach.GradeRequest
}

func (g *capturingApproachGrader) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (g *capturingApproachGrader) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	g.request = req
	return coach.Grade{Score: g.score, Feedback: "ok"}, nil
}
