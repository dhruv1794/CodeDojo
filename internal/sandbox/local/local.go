// SPDX-License-Identifier: MIT

package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/fsutil"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

// execWaitDelay bounds how long Exec blocks on the command's output pipes
// after the process exits or its context is cancelled. Without it, a test that
// leaks a background child process holding the stdout/stderr pipe hangs
// cmd.Run forever. It is a var so tests can shorten it.
var execWaitDelay = 10 * time.Second

// closeWatchdog bounds sandbox cleanup so a stuck filesystem operation
// surfaces as an error instead of an indefinite hang.
var closeWatchdog = 20 * time.Second

type Driver struct{}

type Session struct {
	workdir string
	closed  bool
}

func (Driver) Start(ctx context.Context, spec sandbox.Spec) (sandbox.Session, error) {
	if spec.Network != "" && spec.Network != sandbox.NetworkFull {
		slog.Warn("local sandbox does not enforce network policy", "policy", spec.Network)
	}
	if spec.CPULimit != 0 || spec.MemoryLimit != 0 {
		slog.Warn("local sandbox does not enforce resource limits")
	}
	if spec.RepoMount == "" {
		return nil, fmt.Errorf("repo mount is required")
	}
	tmp, err := os.MkdirTemp("", "codedojo-local-*")
	if err != nil {
		return nil, fmt.Errorf("create sandbox temp dir: %w", err)
	}
	if err := fsutil.CopyDir(spec.RepoMount, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	return &Session{workdir: tmp}, nil
}

func (s *Session) Exec(ctx context.Context, args []string) (sandbox.ExecResult, error) {
	if s.closed {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox is closed")
	}
	if len(args) == 0 {
		return sandbox.ExecResult{}, fmt.Errorf("command is required")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = s.workdir
	cmd.WaitDelay = execWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := sandbox.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		// A leaked background child can hold the output pipe open past the
		// process exit; WaitDelay caps the wait. The command itself still
		// finished, so report its real exit status with captured output.
		if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil {
			result.ExitCode = cmd.ProcessState.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("run %q: %w", args[0], err)
	}
	return result, nil
}

func (s *Session) WriteFile(path string, data []byte) error {
	full, err := s.safePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func (s *Session) ReadFile(path string) ([]byte, error) {
	full, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return data, nil
}

func (s *Session) Diff() (string, error) {
	res, err := s.Exec(context.Background(), []string{"git", "diff", "--"})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git diff failed: %s", res.Stderr)
	}
	return res.Stdout, nil
}

func (s *Session) Close() error {
	s.closed = true
	done := make(chan error, 1)
	go func() {
		done <- os.RemoveAll(s.workdir)
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("remove sandbox temp dir: %w", err)
		}
		return nil
	case <-time.After(closeWatchdog):
		return fmt.Errorf("sandbox cleanup timed out after %s removing %s", closeWatchdog, s.workdir)
	}
}

func (s *Session) safePath(path string) (string, error) {
	if !filepath.IsLocal(path) {
		return "", fmt.Errorf("path escapes sandbox: %s", path)
	}
	return filepath.Join(s.workdir, filepath.Clean(path)), nil
}
