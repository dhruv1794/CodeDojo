package mock

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
)

type Coach struct {
	LeakCode bool
}

func (c Coach) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	if _, err := prompts.Render("reviewer/system.tmpl", map[string]any{
		"Difficulty": 0,
		"HintBudget": 0,
		"Level":      hintLevelName(req.Level),
		"Strict":     req.Strict,
		"Context":    req.Context,
	}); err != nil {
		return coach.Hint{}, fmt.Errorf("render reviewer hint prompt: %w", err)
	}
	if c.LeakCode {
		return coach.Hint{Level: req.Level, Content: "```go\nfunc answer() int {\n\treturn 42\n}\n```", Cost: coach.HintCost(req.Level)}, nil
	}
	content := map[coach.HintLevel]string{
		coach.LevelNudge:    "What have you already ruled out, and what result would disprove your current guess?",
		coach.LevelQuestion: "Which boundary or error path changes the observed behavior?",
		coach.LevelPointer:  "Look near the smallest function touched by the failing behavior.",
		coach.LevelConcept:  "Compare the failing path against the invariant the tests imply before changing code.",
	}[req.Level]
	if content == "" {
		content = "State the invariant you expect, then test one assumption at a time."
	}
	return coach.Hint{Level: req.Level, Content: content, Cost: coach.HintCost(req.Level)}, nil
}

func (c Coach) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	if _, err := prompts.Render("reviewer/grade_diagnosis.tmpl", map[string]any{
		"MaxScore":     50,
		"MutationFile": "unknown",
		"MutationLine": 0,
		"Operator":     "unknown",
		"Description":  req.Rubric,
		"Diagnosis":    req.Answer,
	}); err != nil {
		return coach.Grade{}, fmt.Errorf("render reviewer grade prompt: %w", err)
	}
	if req.Answer == "" {
		return coach.Grade{Score: 0, Feedback: "No diagnosis was provided."}, nil
	}
	return coach.Grade{Score: 40, Feedback: "The diagnosis names a plausible cause and tradeoff."}, nil
}

func hintLevelName(level coach.HintLevel) string {
	switch level {
	case coach.LevelNudge:
		return "nudge"
	case coach.LevelQuestion:
		return "question"
	case coach.LevelPointer:
		return "pointer"
	case coach.LevelConcept:
		return "concept"
	default:
		return "unknown"
	}
}
