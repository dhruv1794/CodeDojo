// SPDX-License-Identifier: MIT

package reviewer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

func TestGradeDetectionPartialCredit(t *testing.T) {
	log := reviewerLog()
	tests := []struct {
		name          string
		submission    Submission
		wantFile      int
		wantLine      int
		wantOperator  int
		wantDiagnosis int
		wantScore     int
	}{
		{
			name: "full detection",
			submission: Submission{
				FilePath:      "./calc/calc.go",
				StartLine:     10,
				EndLine:       10,
				OperatorClass: "boundary",
				Diagnosis:     "calc/calc.go line 10 has a boundary comparison change so the minimum case returns too early.",
			},
			wantFile:      50,
			wantLine:      30,
			wantOperator:  20,
			wantDiagnosis: 50,
			wantScore:     150,
		},
		{
			name: "line tolerance",
			submission: Submission{
				FilePath:      "calc/calc.go",
				StartLine:     12,
				OperatorClass: "boundary",
				Diagnosis:     "calc.go line 12 has a comparison boundary change.",
			},
			wantFile:      50,
			wantLine:      30,
			wantOperator:  20,
			wantDiagnosis: 50,
			wantScore:     150,
		},
		{
			name: "wrong file but right line and operator",
			submission: Submission{
				FilePath:      "calc/other.go",
				StartLine:     10,
				OperatorClass: "boundary",
				Diagnosis:     "calc/calc.go line 10 has a comparison boundary change.",
			},
			wantLine:      30,
			wantOperator:  20,
			wantDiagnosis: 50,
			wantScore:     100,
		},
		{
			name: "wrong line and operator",
			submission: Submission{
				FilePath:      "calc/calc.go",
				StartLine:     20,
				OperatorClass: "conditional",
				Diagnosis:     "calc/calc.go line 10 has a comparison boundary change.",
			},
			wantFile:      50,
			wantDiagnosis: 50,
			wantScore:     100,
		},
		{
			name: "empty diagnosis",
			submission: Submission{
				FilePath:      "calc/calc.go",
				StartLine:     10,
				OperatorClass: "boundary",
			},
			wantFile:     50,
			wantLine:     30,
			wantOperator: 20,
			wantScore:    100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Grade(context.Background(), tt.submission, log, GradeOptions{Coach: scriptedGrader{score: 40}})
			if err != nil {
				t.Fatalf("Grade returned error: %v", err)
			}
			if got.FileScore != tt.wantFile || got.LineScore != tt.wantLine || got.OperatorScore != tt.wantOperator || got.DiagnosisScore != tt.wantDiagnosis || got.Score != tt.wantScore {
				t.Fatalf("Grade = %+v, want file=%d line=%d operator=%d diagnosis=%d score=%d", got, tt.wantFile, tt.wantLine, tt.wantOperator, tt.wantDiagnosis, tt.wantScore)
			}
		})
	}
}

func TestGradeAppliesDeductionsTimeBonusAndStreak(t *testing.T) {
	started := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	got, err := Grade(context.Background(), Submission{
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "calc/calc.go line 10 has a boundary comparison changed at the lower guard branch.",
		HintCosts:     []int{10, 20, -5},
		StartedAt:     started,
		SubmittedAt:   started.Add(6 * time.Minute),
		Streak:        3,
	}, reviewerLog(), GradeOptions{
		Coach:          scriptedGrader{score: 20},
		TimeBonusLimit: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.HintDeduction != 30 {
		t.Fatalf("HintDeduction = %d, want 30", got.HintDeduction)
	}
	if got.TimeBonus != 10 {
		t.Fatalf("TimeBonus = %d, want 10", got.TimeBonus)
	}
	if got.StreakBonus != 19 {
		t.Fatalf("StreakBonus = %d, want 19", got.StreakBonus)
	}
	if got.Score != 149 {
		t.Fatalf("Score = %d, want 149", got.Score)
	}
}

func TestGradeClampsDiagnosisScore(t *testing.T) {
	got, err := Grade(context.Background(), Submission{
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "calc/calc.go line 10 has a boundary comparison changed at the lower guard branch.",
	}, reviewerLog(), GradeOptions{Coach: scriptedGrader{score: 99}})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.DiagnosisScore != 50 {
		t.Fatalf("DiagnosisScore = %d, want 50", got.DiagnosisScore)
	}
}

func TestGradeReturnsDiagnosisError(t *testing.T) {
	wantErr := errors.New("grader unavailable")
	_, err := Grade(context.Background(), Submission{
		FilePath:  "calc/calc.go",
		StartLine: 10,
		Diagnosis: "The boundary was widened.",
	}, reviewerLog(), GradeOptions{Coach: scriptedGrader{err: wantErr}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Grade error = %v, want %v", err, wantErr)
	}
}

func TestGradeRejectsDiagnosisFeedbackWithBannedIdentifiers(t *testing.T) {
	log := reviewerLog()
	banned := log.Mutation.Operator

	_, err := Grade(context.Background(), Submission{
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "The boundary was widened.",
	}, log, GradeOptions{Coach: scriptedGrader{score: 40, feedback: "The operator " + banned + " was used to change the comparison."}})
	if err == nil {
		t.Fatalf("expected error for feedback containing banned identifier %q", banned)
	}
	if !strings.Contains(err.Error(), "banned identifier") {
		t.Fatalf("error %q does not mention banned identifier", err)
	}
}

func TestGradeBuildsDiagnosisRubric(t *testing.T) {
	diagnosis := "calc/calc.go line 10 has a boundary comparison changed at the lower guard branch during validation."
	grader := &capturingGrader{score: 37}
	got, err := Grade(context.Background(), Submission{
		SessionID:     "sess-1",
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     diagnosis,
	}, reviewerLog(), GradeOptions{Coach: grader})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.DiagnosisScore != 50 {
		t.Fatalf("DiagnosisScore = %d, want 50", got.DiagnosisScore)
	}
	if grader.request.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", grader.request.SessionID)
	}
	for _, want := range []string{"calc/calc.go", "10", "boundary"} {
		if !strings.Contains(grader.request.Rubric, want) {
			t.Fatalf("rubric %q does not contain %q", grader.request.Rubric, want)
		}
	}
	if grader.request.Answer != diagnosis {
		t.Fatalf("Answer = %q, want submitted diagnosis", grader.request.Answer)
	}
}

func TestGradeDiagnosisDeterministicWithoutCoach(t *testing.T) {
	got, err := Grade(context.Background(), Submission{
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "calc/calc.go line 10 has a boundary comparison change.",
	}, reviewerLog(), GradeOptions{})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.DiagnosisScore != 50 {
		t.Fatalf("DiagnosisScore = %d, want deterministic full score", got.DiagnosisScore)
	}
}

func TestGradeDiagnosisMechanismIsCached(t *testing.T) {
	grader := &countingGrader{score: 12}
	sub := Submission{
		SessionID:     "sess-cache",
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "calc/calc.go line 10 has a boundary comparison change for minimum values.",
	}
	for i := 0; i < 2; i++ {
		if _, err := Grade(context.Background(), sub, reviewerLog(), GradeOptions{Coach: grader}); err != nil {
			t.Fatalf("Grade returned error: %v", err)
		}
	}
	if grader.calls != 1 {
		t.Fatalf("grader calls = %d, want 1 cached mechanism grade", grader.calls)
	}
}

func TestGradeBuildsDeterministicCommentary(t *testing.T) {
	log := reviewerLog()
	log.Profile = mutate.ProfileDifficulty(log.Mutation)
	got, err := Grade(context.Background(), Submission{
		SessionID:     "sess-commentary",
		FilePath:      "calculator/calculator.go",
		StartLine:     13,
		EndLine:       13,
		OperatorClass: "boundary",
		Diagnosis:     "boundary comparison changed at line 13",
	}, log, GradeOptions{})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	for _, want := range []string{
		"The hidden mutation was `boundary`",
		"difficulty profile is",
		"Your diagnosis scored",
		"final kata score",
	} {
		if !strings.Contains(got.Commentary, want) {
			t.Fatalf("commentary missing %q:\n%s", want, got.Commentary)
		}
	}
}

func TestGradeBuildsReasoningTrace(t *testing.T) {
	log := reviewerLog()
	log.Profile = mutate.ProfileDifficulty(log.Mutation)
	got, err := Grade(context.Background(), Submission{
		SessionID:     "sess-trace",
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "calc/calc.go line 10 has a boundary comparison change.",
		ForecastFile:  "calc/other.go",
	}, log, GradeOptions{})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	for _, want := range []string{
		"1. Start from the failing behavior",
		"Compare the first forecast `calc/other.go`",
		"actual mutation file `calc/calc.go`",
		"Before editing, verify the diagnosis",
	} {
		if !strings.Contains(got.ReasoningTrace, want) {
			t.Fatalf("reasoning trace missing %q:\n%s", want, got.ReasoningTrace)
		}
	}
}

func reviewerLog() mutate.MutationLog {
	return mutate.MutationLog{
		Mutation: mutate.Mutation{
			FilePath:    "calc/calc.go",
			StartLine:   10,
			Operator:    "boundary",
			Description: "changed boundary operator < to <=",
		},
	}
}

type scriptedGrader struct {
	score    int
	err      error
	feedback string
}

func (g scriptedGrader) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (g scriptedGrader) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	if g.err != nil {
		return coach.Grade{}, g.err
	}
	if req.Answer == "" {
		return coach.Grade{Score: 0, Feedback: "No diagnosis was provided."}, nil
	}
	fb := g.feedback
	if fb == "" {
		fb = "feedback"
	}
	return coach.Grade{Score: g.score, Feedback: fb}, nil
}

type capturingGrader struct {
	score   int
	request coach.GradeRequest
}

type countingGrader struct {
	score int
	calls int
}

func (g *countingGrader) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (g *countingGrader) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	g.calls++
	return coach.Grade{Score: g.score, Feedback: "feedback"}, nil
}

func (g *capturingGrader) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (g *capturingGrader) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	g.request = req
	return coach.Grade{Score: g.score, Feedback: "feedback"}, nil
}
