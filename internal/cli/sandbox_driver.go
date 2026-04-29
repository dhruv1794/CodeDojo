package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/dhruvmishra/codedojo/internal/sandbox"
	dockersandbox "github.com/dhruvmishra/codedojo/internal/sandbox/docker"
	"github.com/dhruvmishra/codedojo/internal/sandbox/local"
)

const defaultDockerImage = "codedojo/go:1.23"

type selectedSandbox struct {
	driver sandbox.Driver
	image  string
}

func selectSandbox(ctx context.Context, errOut io.Writer) selectedSandbox {
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
	return selectedSandbox{
		driver: driver,
		image:  defaultDockerImage,
	}
}

func (s selectedSandbox) spec(repoPath string) sandbox.Spec {
	return sandbox.Spec{
		Image:       s.image,
		RepoMount:   repoPath,
		Network:     sandbox.NetworkNone,
		CPULimit:    2,
		MemoryLimit: 2 * 1024 * 1024 * 1024,
		Timeout:     2 * time.Minute,
	}
}

func warnLocalFallback(out io.Writer, err error) {
	if out == nil {
		return
	}
	ui := themeForWriter(out)
	_, _ = fmt.Fprintf(out, "%s docker sandbox unavailable; falling back to local sandbox: %v\n", ui.Warning("WARN"), err)
}
