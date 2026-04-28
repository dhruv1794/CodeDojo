package reviewer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/session"
)

const (
	fileScore       = 50
	lineScore       = 30
	operatorScore   = 20
	maxDiagnosis    = 50
	defaultTimeGoal = 15 * time.Minute
)

type Submission struct {
	FilePath      string
	StartLine     int
	EndLine       int
	OperatorClass string
	Diagnosis     string
	HintCosts     []int
	StartedAt     time.Time
	SubmittedAt   time.Time
	Streak        int
	SessionID     string
}

type GradeOptions struct {
	Coach          coach.Coach
	TimeBonusLimit time.Duration
}

type GradeResult struct {
	Score int

	FileScore      int
	LineScore      int
	OperatorScore  int
	DiagnosisScore int
	HintDeduction  int
	TimeBonus      int
	StreakBonus    int

	DiagnosisFeedback string
}

func Grade(ctx context.Context, submission Submission, log mutate.MutationLog, opts GradeOptions) (GradeResult, error) {
	result := GradeResult{}
	if samePath(submission.FilePath, log.Mutation.FilePath) {
		result.FileScore = fileScore
	}
	if lineMatches(submission.StartLine, submission.EndLine, log.Mutation.StartLine) {
		result.LineScore = lineScore
	}
	if sameOperator(submission.OperatorClass, log.Mutation.Operator) {
		result.OperatorScore = operatorScore
	}

	diagnosis, err := gradeDiagnosis(ctx, submission, log, opts.Coach)
	if err != nil {
		return GradeResult{}, err
	}
	result.DiagnosisScore = clamp(diagnosis.Score, 0, maxDiagnosis)
	result.DiagnosisFeedback = diagnosis.Feedback

	for _, cost := range submission.HintCosts {
		if cost > 0 {
			result.HintDeduction += cost
		}
	}
	if !submission.StartedAt.IsZero() && !submission.SubmittedAt.IsZero() && submission.SubmittedAt.After(submission.StartedAt) {
		limit := opts.TimeBonusLimit
		if limit <= 0 {
			limit = defaultTimeGoal
		}
		result.TimeBonus = session.TimeBonus(submission.SubmittedAt.Sub(submission.StartedAt), limit)
	}

	base := result.FileScore + result.LineScore + result.OperatorScore + result.DiagnosisScore - result.HintDeduction + result.TimeBonus
	if base < 0 {
		base = 0
	}
	withStreak := session.ApplyStreak(base, submission.Streak)
	result.StreakBonus = withStreak - base
	result.Score = withStreak
	return result, nil
}

func gradeDiagnosis(ctx context.Context, submission Submission, log mutate.MutationLog, grader coach.Coach) (coach.Grade, error) {
	if grader == nil {
		return coach.Grade{}, nil
	}
	rubric, err := prompts.Render("reviewer/grade_diagnosis.tmpl", map[string]any{
		"MaxScore":     maxDiagnosis,
		"MutationFile": log.Mutation.FilePath,
		"MutationLine": log.Mutation.StartLine,
		"Operator":     log.Mutation.Operator,
		"Description":  log.Mutation.Description,
		"Diagnosis":    submission.Diagnosis,
	})
	if err != nil {
		return coach.Grade{}, fmt.Errorf("render reviewer diagnosis prompt: %w", err)
	}
	grade, err := grader.Grade(ctx, coach.GradeRequest{
		SessionID: submission.SessionID,
		Rubric:    rubric,
		Answer:    submission.Diagnosis,
	})
	if err != nil {
		return coach.Grade{}, fmt.Errorf("grade reviewer diagnosis: %w", err)
	}
	return grade, nil
}

func samePath(got, want string) bool {
	return cleanPath(got) == cleanPath(want)
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	return filepath.ToSlash(filepath.Clean(path))
}

func lineMatches(start, end, actual int) bool {
	if actual <= 0 || start <= 0 {
		return false
	}
	if end <= 0 {
		end = start
	}
	if end < start {
		start, end = end, start
	}
	return actual >= start-2 && actual <= end+2
}

func sameOperator(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func clamp(n, low, high int) int {
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
}
