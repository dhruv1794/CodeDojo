package newcomer

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
	"github.com/dhruvmishra/codedojo/internal/coach/validator"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/session"
)

const (
	correctnessScore     = 100
	maxApproachScore     = 50
	maxTestQualityScore  = 30
	defaultDiffLineCap   = 400
	defaultTimeSoftCap   = time.Hour
	defaultTestQualityPP = 10
)

type Submission struct {
	SessionID    string
	UserDiff     string
	NewTestFuncs int
	HintCosts    []int
	StartedAt    time.Time
	SubmittedAt  time.Time
	Streak       int
}

type GradeOptions struct {
	Coach          coach.Coach
	TestCmd        []string
	Sandbox        sandbox.Session
	DiffLineCap    int
	TimeSoftCap    time.Duration
	TestQualityCap int
}

type GradeResult struct {
	Score int

	CorrectnessScore int
	ApproachScore    int
	TestQualityScore int
	HintDeduction    int
	StreakBonus      int

	TestStdout       string
	TestStderr       string
	TestExitCode     int
	ApproachFeedback string
}

func Grade(ctx context.Context, task Task, submission Submission, opts GradeOptions) (GradeResult, error) {
	result := GradeResult{}

	if opts.Sandbox == nil {
		return GradeResult{}, fmt.Errorf("sandbox session is required")
	}
	if len(opts.TestCmd) == 0 {
		return GradeResult{}, fmt.Errorf("test command is required")
	}
	exec, err := opts.Sandbox.Exec(ctx, opts.TestCmd)
	if err != nil {
		return GradeResult{}, fmt.Errorf("run original tests: %w", err)
	}
	result.TestStdout = exec.Stdout
	result.TestStderr = exec.Stderr
	result.TestExitCode = exec.ExitCode
	if exec.ExitCode == 0 {
		result.CorrectnessScore = correctnessScore
	}

	approach, err := gradeApproach(ctx, task, submission, opts)
	if err != nil {
		return GradeResult{}, err
	}
	result.ApproachScore = clamp(approach.Score, 0, maxApproachScore)
	result.ApproachFeedback = approach.Feedback

	testCap := opts.TestQualityCap
	if testCap <= 0 {
		testCap = maxTestQualityScore
	}
	if submission.NewTestFuncs > 0 {
		bonus := submission.NewTestFuncs * defaultTestQualityPP
		if bonus > testCap {
			bonus = testCap
		}
		result.TestQualityScore = bonus
	}

	for _, cost := range submission.HintCosts {
		if cost > 0 {
			result.HintDeduction += cost
		}
	}

	softCap := opts.TimeSoftCap
	if softCap <= 0 {
		softCap = defaultTimeSoftCap
	}
	timeOk := submission.StartedAt.IsZero() || submission.SubmittedAt.IsZero() || submission.SubmittedAt.Sub(submission.StartedAt) <= softCap

	base := result.CorrectnessScore + result.ApproachScore + result.TestQualityScore - result.HintDeduction
	if !timeOk {
		base -= timePenalty(submission.SubmittedAt.Sub(submission.StartedAt), softCap)
	}
	if base < 0 {
		base = 0
	}
	withStreak := session.ApplyStreak(base, submission.Streak)
	result.StreakBonus = withStreak - base
	result.Score = withStreak
	return result, nil
}

func gradeApproach(ctx context.Context, task Task, submission Submission, opts GradeOptions) (coach.Grade, error) {
	if opts.Coach == nil {
		return coach.Grade{}, nil
	}
	cap := opts.DiffLineCap
	if cap <= 0 {
		cap = defaultDiffLineCap
	}
	rubric, err := prompts.Render("newcomer/grade_approach.tmpl", map[string]any{
		"MaxScore":           maxApproachScore,
		"FeatureDescription": task.FeatureDescription,
		"UserDiff":           truncateDiff(submission.UserDiff, cap),
		"BannedIdentifiers":  strings.Join(task.BannedIdentifiers, ", "),
	})
	if err != nil {
		return coach.Grade{}, fmt.Errorf("render newcomer approach prompt: %w", err)
	}
	grade, err := opts.Coach.Grade(ctx, coach.GradeRequest{
		SessionID: submission.SessionID,
		Rubric:    rubric,
		Answer:    submission.UserDiff,
	})
	if err != nil {
		return coach.Grade{}, fmt.Errorf("grade newcomer approach: %w", err)
	}
	if grade.Feedback != "" {
		if r := validator.Validate(grade.Feedback, task.BannedIdentifiers); !r.OK {
			return coach.Grade{}, fmt.Errorf("approach feedback failed validator: %s", r.Reason)
		}
	}
	return grade, nil
}

func truncateDiff(diff string, lineCap int) string {
	if lineCap <= 0 {
		return diff
	}
	lines := strings.Split(diff, "\n")
	if len(lines) <= lineCap {
		return diff
	}
	keep := lineCap / 2
	if keep <= 0 {
		keep = 1
	}
	head := lines[:keep]
	tail := lines[len(lines)-keep:]
	return strings.Join(head, "\n") + "\n[...truncated " + fmt.Sprintf("%d", len(lines)-2*keep) + " lines...]\n" + strings.Join(tail, "\n")
}

func timePenalty(elapsed, softCap time.Duration) int {
	if elapsed <= softCap || softCap <= 0 {
		return 0
	}
	overflow := elapsed - softCap
	hours := int(overflow / time.Hour)
	if overflow%time.Hour > 0 {
		hours++
	}
	if hours <= 0 {
		hours = 1
	}
	return hours * 5
}

func CountNewTestFuncs(diff string) int {
	if diff == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		stripped := strings.TrimSpace(line[1:])
		if testFuncPattern.MatchString(stripped) {
			count++
		}
	}
	return count
}

var testFuncPattern = regexp.MustCompile(`^func\s+Test[A-Z][A-Za-z0-9_]*\s*\(`)

func clamp(n, low, high int) int {
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
}
