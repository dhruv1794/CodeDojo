package coach

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/coach/validator"
)

type ValidatingCoach struct {
	Inner             Coach
	BannedIdentifiers []string
}

func RetryWithStricterPrompt(inner Coach, bannedIdentifiers []string) Coach {
	return ValidatingCoach{Inner: inner, BannedIdentifiers: bannedIdentifiers}
}

func (c ValidatingCoach) Hint(ctx context.Context, req HintRequest) (Hint, error) {
	var last validator.Result
	for attempt := 0; attempt < 3; attempt++ {
		req.Strict = attempt > 0
		hint, err := c.Inner.Hint(ctx, req)
		if err != nil {
			return Hint{}, err
		}
		last = validator.Validate(hint.Content, c.BannedIdentifiers)
		if last.OK {
			return hint, nil
		}
	}
	return Hint{
		Level:   req.Level,
		Content: fmt.Sprintf("I cannot give a more specific hint without revealing the implementation. Re-check one assumption from the failing behavior. (%s)", last.Reason),
		Cost:    0,
	}, nil
}

func (c ValidatingCoach) Grade(ctx context.Context, req GradeRequest) (Grade, error) {
	return c.Inner.Grade(ctx, req)
}
