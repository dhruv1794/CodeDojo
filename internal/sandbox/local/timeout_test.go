// SPDX-License-Identifier: MIT

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

// TestExecDoesNotHangOnLeakedChild reproduces the dogfood hang: a command that
// exits promptly but leaves a background child holding the stdout pipe. Without
// WaitDelay, cmd.Run would block until the leaked child finishes.
func TestExecDoesNotHangOnLeakedChild(t *testing.T) {
	prev := execWaitDelay
	execWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { execWaitDelay = prev })

	repo := newGitFixture(t)
	box := startLocalSession(t, repo)
	defer closeSession(t, box)

	start := time.Now()
	// The shell exits immediately; the backgrounded sleep inherits the pipe.
	_, err := box.Exec(context.Background(), []string{"/bin/sh", "-c", "sleep 30 & exit 0"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Exec took %s with a leaked child, want WaitDelay to bound it", elapsed)
	}
}
