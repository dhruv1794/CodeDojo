package reviewer

import (
	"context"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

type Task struct {
	RepoPath     string
	Difficulty   int
	Language     string
	MutationLog  mutate.MutationLog
	Instructions string
}

// TaskGenerator drives mutation task creation for a specific language.
// Set Engine to a MutationEngine implementation (mutate.Engine for Go,
// TextEngine for Python/JS/TS/Rust). When nil, defaults to a Go engine.
type TaskGenerator struct {
	Engine MutationEngine
}

func GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	return TaskGenerator{}.GenerateTask(ctx, r, difficulty)
}

func (g TaskGenerator) GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	engine := g.Engine
	if engine == nil {
		engine = mutate.Engine{Mutators: op.All()}
	}
	log, err := engine.SelectAndApply(ctx, r, difficulty)
	if err != nil {
		return Task{}, err
	}
	return Task{
		RepoPath:     r.Path,
		Difficulty:   difficulty,
		Language:     engine.Language(),
		MutationLog:  log,
		Instructions: "Find the injected reviewer bug, then submit a file, line range, operator class, and diagnosis.",
	}, nil
}
