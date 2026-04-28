package revert

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func Restore(ctx context.Context, r repo.Repo, state Reversion) (Reversion, error) {
	if err := ctx.Err(); err != nil {
		return Reversion{}, err
	}
	if state.GroundTruthSHA == "" {
		return Reversion{}, fmt.Errorf("ground truth sha is required")
	}
	worktree, err := r.Git.Worktree()
	if err != nil {
		return Reversion{}, fmt.Errorf("open worktree: %w", err)
	}
	if err := worktree.Checkout(&gogit.CheckoutOptions{Hash: plumbing.NewHash(state.GroundTruthSHA), Force: true}); err != nil {
		return Reversion{}, fmt.Errorf("restore ground truth %s: %w", state.GroundTruthSHA, err)
	}
	state.RestoredToTruth = true
	return state, nil
}
