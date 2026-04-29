package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/dhruvmishra/codedojo/internal/fsutil"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

const (
	defaultImage     = "codedojo/go:1.23"
	workspacePath    = "/workspace"
	defaultPidsLimit = int64(256)
)

type Driver struct {
	client *client.Client
}

func New() (Driver, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return Driver{}, fmt.Errorf("create docker client: %w", err)
	}
	return Driver{client: cli}, nil
}

func (d Driver) Ping(ctx context.Context) error {
	cli := d.client
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("create docker client: %w", err)
		}
	}
	if _, err := cli.Ping(ctx); err != nil {
		return fmt.Errorf("ping docker daemon: %w", err)
	}
	return nil
}

func (d Driver) Start(ctx context.Context, spec sandbox.Spec) (sandbox.Session, error) {
	if spec.RepoMount == "" {
		return nil, fmt.Errorf("repo mount is required")
	}
	cli := d.client
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, fmt.Errorf("create docker client: %w", err)
		}
	}
	image := spec.Image
	if image == "" {
		image = defaultImage
	}
	tmp, err := os.MkdirTemp("", "codedojo-docker-*")
	if err != nil {
		return nil, fmt.Errorf("create sandbox temp dir: %w", err)
	}
	if err := fsutil.CopyDir(spec.RepoMount, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	for _, dir := range []string{".tmp", filepath.Join(".cache", "go-build"), filepath.Join(".cache", "go-mod")} {
		if err := os.MkdirAll(filepath.Join(tmp, dir), 0o755); err != nil {
			_ = os.RemoveAll(tmp)
			return nil, fmt.Errorf("create workspace cache dir: %w", err)
		}
	}

	timeoutSeconds := 10
	cfg := &container.Config{
		Image:           image,
		Cmd:             []string{"sleep", "infinity"},
		WorkingDir:      workspacePath,
		Env:             workspaceEnv(),
		AttachStdout:    false,
		AttachStderr:    false,
		NetworkDisabled: spec.Network == "" || spec.Network == sandbox.NetworkNone,
		StopTimeout:     &timeoutSeconds,
	}
	hostCfg := &container.HostConfig{
		NetworkMode:    networkMode(spec.Network),
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: tmp,
			Target: workspacePath,
		}},
		Init: boolPtr(true),
		Resources: container.Resources{
			Memory:     spec.MemoryLimit,
			MemorySwap: spec.MemoryLimit,
			NanoCPUs:   nanoCPUs(spec.CPULimit),
			PidsLimit:  int64Ptr(defaultPidsLimit),
		},
	}

	created, err := cli.ContainerCreate(ctx, cfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("create docker container from %q: %w", image, err)
	}
	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("start docker container: %w", err)
	}

	return &Session{
		client:      cli,
		containerID: created.ID,
		workdir:     tmp,
		timeout:     spec.Timeout,
	}, nil
}

type Session struct {
	client      *client.Client
	containerID string
	workdir     string
	timeout     time.Duration
	closed      bool
}

func (s *Session) Exec(ctx context.Context, args []string) (sandbox.ExecResult, error) {
	if s.closed {
		return sandbox.ExecResult{}, fmt.Errorf("sandbox is closed")
	}
	if len(args) == 0 {
		return sandbox.ExecResult{}, fmt.Errorf("command is required")
	}
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	created, err := s.client.ContainerExecCreate(ctx, s.containerID, container.ExecOptions{
		Cmd:          args,
		WorkingDir:   workspacePath,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return sandbox.ExecResult{}, fmt.Errorf("create docker exec: %w", err)
	}
	attached, err := s.client.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return sandbox.ExecResult{}, fmt.Errorf("attach docker exec: %w", err)
	}
	defer attached.Close()

	var stdout, stderr bytes.Buffer
	copyErr := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader)
		if err == io.EOF {
			err = nil
		}
		copyErr <- err
	}()

	if err := <-copyErr; err != nil {
		return sandbox.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("read docker exec output: %w", err)
	}
	inspected, err := s.client.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return sandbox.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("inspect docker exec: %w", err)
	}
	return sandbox.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: inspected.ExitCode,
	}, nil
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
	if s.closed {
		return nil
	}
	s.closed = true
	var closeErr error
	if err := s.client.ContainerRemove(context.Background(), s.containerID, container.RemoveOptions{Force: true}); err != nil {
		closeErr = fmt.Errorf("remove docker container: %w", err)
	}
	if err := os.RemoveAll(s.workdir); err != nil && closeErr == nil {
		closeErr = fmt.Errorf("remove sandbox temp dir: %w", err)
	}
	return closeErr
}

func (s *Session) safePath(path string) (string, error) {
	if !filepath.IsLocal(path) {
		return "", fmt.Errorf("path escapes sandbox: %s", path)
	}
	return filepath.Join(s.workdir, filepath.Clean(path)), nil
}

func networkMode(policy sandbox.NetworkPolicy) container.NetworkMode {
	switch policy {
	case sandbox.NetworkFull:
		return "bridge"
	case sandbox.NetworkRestricted:
		return "none"
	default:
		return "none"
	}
}

func nanoCPUs(cpus float64) int64 {
	if cpus <= 0 {
		return 0
	}
	return int64(cpus * 1_000_000_000)
}

func workspaceEnv() []string {
	return []string{
		"HOME=" + workspacePath,
		"TMPDIR=" + workspacePath + "/.tmp",
		"GOCACHE=" + workspacePath + "/.cache/go-build",
		"GOMODCACHE=" + workspacePath + "/.cache/go-mod",
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}
