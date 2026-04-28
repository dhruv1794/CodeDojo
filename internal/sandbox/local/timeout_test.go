package local

import (
	"context"
	"testing"
	"time"
)

func TestExecContextCancellationStopsCommand(t *testing.T) {
	t.Parallel()

	repo := newGitFixture(t)
	box := startLocalSession(t, repo)
	defer closeSession(t, box)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	result, err := box.Exec(ctx, []string{"/bin/sh", "-c", "sleep 5"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Exec took %s after context cancellation, want under 1s", elapsed)
	}
	if result.ExitCode == 0 {
		t.Fatal("cancelled command exit code = 0, want non-zero")
	}
}
