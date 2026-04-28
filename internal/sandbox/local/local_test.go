package local

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

func TestExecCapturesOutputAndExitCode(t *testing.T) {
	t.Parallel()

	repo := newGitFixture(t)
	box := startLocalSession(t, repo)
	defer closeSession(t, box)

	result, err := box.Exec(context.Background(), []string{"/bin/sh", "-c", "printf stdout; printf stderr >&2; exit 7"})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if result.Stdout != "stdout" {
		t.Fatalf("stdout = %q, want %q", result.Stdout, "stdout")
	}
	if result.Stderr != "stderr" {
		t.Fatalf("stderr = %q, want %q", result.Stderr, "stderr")
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", result.ExitCode)
	}
}

func TestWriteFileReadFileAndDiff(t *testing.T) {
	t.Parallel()

	repo := newGitFixture(t)
	box := startLocalSession(t, repo)
	defer closeSession(t, box)

	if err := box.WriteFile("nested/message.txt", []byte("updated\n")); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	got, err := box.ReadFile("nested/message.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(got, []byte("updated\n")) {
		t.Fatalf("ReadFile = %q, want updated file", string(got))
	}

	diff, err := box.Diff()
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	for _, want := range []string{"diff --git", "nested/message.txt", "-initial", "+updated"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("Diff missing %q:\n%s", want, diff)
		}
	}
}

func TestCloseCleansTempDirAndRejectsFurtherExec(t *testing.T) {
	t.Parallel()

	repo := newGitFixture(t)
	box := startLocalSession(t, repo)
	workdir := box.workdir

	if _, err := os.Stat(workdir); err != nil {
		t.Fatalf("sandbox workdir should exist before Close: %v", err)
	}
	if err := box.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("sandbox workdir still exists after Close, stat err = %v", err)
	}
	if _, err := box.Exec(context.Background(), []string{"git", "status"}); err == nil {
		t.Fatal("Exec after Close succeeded, want error")
	}
}

func startLocalSession(t *testing.T, repoPath string) *Session {
	t.Helper()

	got, err := (Driver{}).Start(context.Background(), sandbox.Spec{RepoMount: repoPath})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	box, ok := got.(*Session)
	if !ok {
		t.Fatalf("Start returned %T, want *Session", got)
	}
	return box
}

func closeSession(t *testing.T, box *Session) {
	t.Helper()

	if err := box.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func newGitFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "codedojo@example.com")
	runGit(t, dir, "config", "user.name", "CodeDojo Test")

	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "message.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
