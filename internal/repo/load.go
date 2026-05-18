// SPDX-License-Identifier: MIT

package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/dhruvmishra/codedojo/internal/fsutil"
	gogit "github.com/go-git/go-git/v5"
)

func Clone(ctx context.Context, url, dest string) (Repo, error) {
	return CloneWithAuthHints(ctx, url, dest, EnvAuthHints())
}

func CloneWithAuthHints(ctx context.Context, url, dest string, hints AuthHints) (Repo, error) {
	auth, err := authForURLWithHints(url, hints)
	if err != nil {
		return Repo{}, fmt.Errorf("resolve auth for %q: %w", url, err)
	}
	r, err := gogit.PlainCloneContext(ctx, dest, false, &gogit.CloneOptions{URL: url, Auth: auth})
	if err != nil {
		return Repo{}, fmt.Errorf("clone %q: %w", url, err)
	}
	return Repo{Path: dest, Git: r}, nil
}

func OpenLocal(srcPath string) (Repo, error) {
	tmp, err := os.MkdirTemp("", "codedojo-repo-*")
	if err != nil {
		return Repo{}, fmt.Errorf("create repo temp dir: %w", err)
	}
	if err := fsutil.CopyDir(srcPath, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return Repo{}, err
	}
	r, err := gogit.PlainOpen(tmp)
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		// The source is a plain directory rather than a git repository
		// (for example the bundled testdata fixture, which is tracked as
		// ordinary files). Initialize a repository in the working copy so
		// the rest of the pipeline can rely on git being present.
		if r, err = initRepoCopy(tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return Repo{}, err
		}
	} else if err != nil {
		_ = os.RemoveAll(tmp)
		return Repo{}, fmt.Errorf("open git repo copy: %w", err)
	}
	return Repo{Path: tmp, Git: r}, nil
}

// initRepoCopy initializes a git repository in dir and records a single
// import commit, then returns the opened repository.
func initRepoCopy(dir string) (*gogit.Repository, error) {
	steps := [][]string{
		{"init"},
		{"add", "-A"},
		{"-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "codedojo: import working copy"},
	}
	for _, args := range steps {
		// #nosec G204 -- fixed git subcommands with hardcoded arguments.
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git %v: %w\n%s", args, err, out)
		}
	}
	r, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("open initialized repo copy: %w", err)
	}
	return r, nil
}
