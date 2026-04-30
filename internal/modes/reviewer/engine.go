package reviewer

import (
	"context"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

// MutationEngine abstracts language-specific mutation implementations.
// mutate.Engine (Go AST-based) and TextEngine (regex/text-based) both satisfy
// this interface, enabling the reviewer to work across languages.
type MutationEngine interface {
	Language() string
	SelectAndApply(ctx context.Context, r repo.Repo, difficulty int) (mutate.MutationLog, error)
}
