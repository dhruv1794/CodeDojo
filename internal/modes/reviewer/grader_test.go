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
				Diagnosis:     "The boundary was widened so the minimum case returns too early.",
			},
			wantFile:      50,
			wantLine:      30,
			wantOperator:  20,
			wantDiagnosis: 40,
			wantScore:     140,
		},
		{
			name: "line tolerance",
			submission: Submission{
				FilePath:      "calc/calc.go",
				StartLine:     12,
				OperatorClass: "boundary",
				Diagnosis:     "The comparison boundary changed.",
			},
			wantFile:      50,
			wantLine:      30,
			wantOperator:  20,
			wantDiagnosis: 40,
			wantScore:     140,
		},
		{
			name: "wrong file but right line and operator",
			submission: Submission{
				FilePath:      "calc/other.go",
				StartLine:     10,
				OperatorClass: "boundary",
				Diagnosis:     "The comparison boundary changed.",
			},
			wantLine:      30,
			wantOperator:  20,
			wantDiagnosis: 40,
			wantScore:     90,
		},
		{
			name: "wrong line and operator",
			submission: Submission{
				FilePath:      "calc/calc.go",
				StartLine:     20,
				OperatorClass: "conditional",
				Diagnosis:     "The comparison boundary changed.",
			},
			wantFile:      50,
			wantDiagnosis: 40,
			wantScore:     90,
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
		Diagnosis:     "The boundary was widened.",
		HintCosts:     []int{10, 20, -5},
		StartedAt:     started,
		SubmittedAt:   started.Add(6 * time.Minute),
		Streak:        3,
	}, reviewerLog(), GradeOptions{
		Coach:          scriptedGrader{score: 45},
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
	if got.StreakBonus != 18 {
		t.Fatalf("StreakBonus = %d, want 18", got.StreakBonus)
	}
	if got.Score != 143 {
		t.Fatalf("Score = %d, want 143", got.Score)
	}
}

func TestGradeClampsDiagnosisScore(t *testing.T) {
	got, err := Grade(context.Background(), Submission{
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "The boundary was widened.",
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

func TestGradeBuildsDiagnosisRubric(t *testing.T) {
	grader := &capturingGrader{score: 37}
	got, err := Grade(context.Background(), Submission{
		SessionID:     "sess-1",
		FilePath:      "calc/calc.go",
		StartLine:     10,
		OperatorClass: "boundary",
		Diagnosis:     "The boundary changed.",
	}, reviewerLog(), GradeOptions{Coach: grader})
	if err != nil {
		t.Fatalf("Grade returned error: %v", err)
	}
	if got.DiagnosisScore != 37 {
		t.Fatalf("DiagnosisScore = %d, want 37", got.DiagnosisScore)
	}
	if grader.request.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", grader.request.SessionID)
	}
	for _, want := range []string{"calc/calc.go", "10", "boundary"} {
		if !strings.Contains(grader.request.Rubric, want) {
			t.Fatalf("rubric %q does not contain %q", grader.request.Rubric, want)
		}
	}
	if grader.request.Answer != "The boundary changed." {
		t.Fatalf("Answer = %q, want submitted diagnosis", grader.request.Answer)
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
	score int
	err   error
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
	return coach.Grade{Score: g.score, Feedback: "feedback"}, nil
}

type capturingGrader struct {
	score   int
	request coach.GradeRequest
}

func (g *capturingGrader) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (g *capturingGrader) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	g.request = req
	return coach.Grade{Score: g.score, Feedback: "feedback"}, nil
}
