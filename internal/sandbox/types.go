package sandbox

import (
	"context"
	"time"
)

type NetworkPolicy string

const (
	NetworkNone       NetworkPolicy = "none"
	NetworkRestricted NetworkPolicy = "restricted"
	NetworkFull       NetworkPolicy = "full"
)

type Spec struct {
	Image       string
	RepoMount   string
	Network     NetworkPolicy
	CPULimit    float64
	MemoryLimit int64
	Timeout     time.Duration
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Driver interface {
	Start(ctx context.Context, spec Spec) (Session, error)
}

type Session interface {
	Exec(ctx context.Context, cmd []string) (ExecResult, error)
	WriteFile(path string, data []byte) error
	ReadFile(path string) ([]byte, error)
	Diff() (string, error)
	Close() error
}
