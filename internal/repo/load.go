package repo

import (
	"context"
	"fmt"
	"os"

	"github.com/dhruvmishra/codedojo/internal/fsutil"
	gogit "github.com/go-git/go-git/v5"
)

func Clone(ctx context.Context, url, dest string) (Repo, error) {
	auth, err := authForURL(url)
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
	if err != nil {
		_ = os.RemoveAll(tmp)
		return Repo{}, fmt.Errorf("open git repo copy: %w", err)
	}
	return Repo{Path: tmp, Git: r}, nil
}
