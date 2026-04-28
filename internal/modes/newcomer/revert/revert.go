package revert

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type Reversion struct {
	RepoPath        string `json:"repo_path"`
	GroundTruthSHA  string `json:"ground_truth_sha"`
	StartingSHA     string `json:"starting_sha"`
	ReferenceDiff   string `json:"reference_diff"`
	ReverseDiff     string `json:"reverse_diff"`
	CommitMessage   string `json:"commit_message"`
	ParentMessage   string `json:"parent_message"`
	RestoredToTruth bool   `json:"restored_to_truth"`
}

func Revert(ctx context.Context, r repo.Repo, sha string) (Reversion, error) {
	if err := ctx.Err(); err != nil {
		return Reversion{}, err
	}
	if sha == "" {
		return Reversion{}, fmt.Errorf("ground truth sha is required")
	}
	commit, err := r.Git.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return Reversion{}, fmt.Errorf("load ground truth commit %q: %w", sha, err)
	}
	if commit.NumParents() != 1 {
		return Reversion{}, fmt.Errorf("ground truth commit %s has %d parents, want 1", commit.Hash, commit.NumParents())
	}
	parent, err := commit.Parent(0)
	if err != nil {
		return Reversion{}, fmt.Errorf("load parent for %s: %w", commit.Hash, err)
	}
	patch, err := parent.Patch(commit)
	if err != nil {
		return Reversion{}, fmt.Errorf("compute reference diff for %s: %w", commit.Hash, err)
	}
	reversePatch, err := commit.Patch(parent)
	if err != nil {
		return Reversion{}, fmt.Errorf("compute reverse diff for %s: %w", commit.Hash, err)
	}
	worktree, err := r.Git.Worktree()
	if err != nil {
		return Reversion{}, fmt.Errorf("open worktree: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Reversion{}, err
	}
	if err := worktree.Checkout(&gogit.CheckoutOptions{Hash: parent.Hash, Force: true}); err != nil {
		return Reversion{}, fmt.Errorf("checkout parent %s: %w", parent.Hash, err)
	}
	return Reversion{
		RepoPath:       r.Path,
		GroundTruthSHA: commit.Hash.String(),
		StartingSHA:    parent.Hash.String(),
		ReferenceDiff:  patch.String(),
		ReverseDiff:    reversePatch.String(),
		CommitMessage:  commit.Message,
		ParentMessage:  parent.Message,
	}, nil
}
