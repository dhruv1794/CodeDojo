package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dhruvmishra/codedojo/internal/repo"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	dockersandbox "github.com/dhruvmishra/codedojo/internal/sandbox/docker"
	"github.com/dhruvmishra/codedojo/internal/sandbox/local"
)

type selectedSandbox struct {
	driver sandbox.Driver
	image  string
}

// selectSandbox picks a sandbox driver. Set CODEDOJO_SANDBOX=local to bypass
// Docker and force the local (host-process) driver.
func selectSandbox(ctx context.Context, errOut io.Writer) selectedSandbox {
	if os.Getenv("CODEDOJO_SANDBOX") == "local" {
		return selectedSandbox{driver: local.Driver{}}
	}
	driver, err := dockersandbox.New()
	if err != nil {
		warnLocalFallback(errOut, err)
		return selectedSandbox{driver: local.Driver{}}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := driver.Ping(pingCtx); err != nil {
		warnLocalFallback(errOut, err)
		return selectedSandbox{driver: local.Driver{}}
	}
	return selectedSandbox{driver: driver}
}

func (s selectedSandbox) spec(repoPath string) sandbox.Spec {
	image := s.image
	if image == "" {
		image = imageForRepoPath(repoPath)
	}
	return sandbox.Spec{
		Image:       image,
		RepoMount:   repoPath,
		Network:     sandbox.NetworkNone,
		CPULimit:    2,
		MemoryLimit: 2 * 1024 * 1024 * 1024,
		Timeout:     2 * time.Minute,
	}
}

func imageForRepoPath(repoPath string) string {
	lang, err := repo.DetectLanguage(repoPath)
	if err != nil {
		return "codedojo/go:1.23"
	}
	switch lang.Name {
	case "python":
		return "codedojo/python:3.12"
	case "javascript", "typescript":
		return "codedojo/node:20"
	case "rust":
		return "codedojo/rust:1.76"
	default:
		return "codedojo/go:1.23"
	}
}

func warnLocalFallback(out io.Writer, err error) {
	if out == nil {
		return
	}
	ui := themeForWriter(out)
	_, _ = fmt.Fprintf(out, "%s docker sandbox unavailable; falling back to local sandbox: %v\n", ui.Warning("WARN"), err)
}
