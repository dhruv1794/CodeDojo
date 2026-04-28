package mock

import (
	"context"

	"github.com/dhruvmishra/codedojo/internal/coach"
)

type Coach struct {
	LeakCode bool
}

func (c Coach) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
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
	if req.Answer == "" {
		return coach.Grade{Score: 0, Feedback: "No diagnosis was provided."}, nil
	}
	return coach.Grade{Score: 40, Feedback: "The diagnosis names a plausible cause and tradeoff."}, nil
}
