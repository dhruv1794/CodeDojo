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
	MutationLog  mutate.MutationLog
	Instructions string
}

type TaskGenerator struct {
	Engine mutate.Engine
}

func GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	return TaskGenerator{
		Engine: mutate.Engine{Mutators: op.All()},
	}.GenerateTask(ctx, r, difficulty)
}

func (g TaskGenerator) GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	engine := g.Engine
	if len(engine.Mutators) == 0 {
		engine.Mutators = op.All()
	}
	log, err := engine.SelectAndApply(ctx, r, difficulty)
	if err != nil {
		return Task{}, err
	}
	return Task{
		RepoPath:     r.Path,
		Difficulty:   difficulty,
		MutationLog:  log,
		Instructions: "Find the injected reviewer bug, then submit a file, line range, operator class, and diagnosis.",
	}, nil
}
