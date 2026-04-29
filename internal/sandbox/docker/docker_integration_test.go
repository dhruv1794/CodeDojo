//go:build integration

package docker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

func TestDockerSessionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	driver, err := New()
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	if _, err := driver.client.Ping(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}

	repoPath := initGitRepo(t)
	box, err := driver.Start(ctx, sandbox.Spec{
		Image:       imageForTest(),
		RepoMount:   repoPath,
		Network:     sandbox.NetworkNone,
		CPULimit:    1,
		MemoryLimit: 512 * 1024 * 1024,
		Timeout:     30 * time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "No such image") || strings.Contains(err.Error(), "pull access denied") {
			t.Skipf("docker image unavailable; run make images first: %v", err)
		}
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := box.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	res, err := box.Exec(ctx, []string{"go", "test", "./..."})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("go test exit = %d, stdout = %q, stderr = %q", res.ExitCode, res.Stdout, res.Stderr)
	}

	changedSource := "package sample\n\nfunc Add(a, b int) int { return a + b + 0 }\n"
	if err := box.WriteFile("calc.go", []byte(changedSource)); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := box.ReadFile("calc.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != changedSource {
		t.Fatalf("ReadFile() = %q", got)
	}

	diff, err := box.Diff()
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if !strings.Contains(diff, "calc.go") {
		t.Fatalf("Diff() = %q, want calc.go change", diff)
	}
}

func imageForTest() string {
	if image := os.Getenv("CODEDOJO_DOCKER_IMAGE"); image != "" {
		return image
	}
	return defaultImage
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/sample\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "calc.go"), "package sample\n\nfunc Add(a, b int) int { return a + b }\n")
	writeFile(t, filepath.Join(dir, "calc_test.go"), "package sample\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad sum\")\n\t}\n}\n")
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "codedojo@example.test")
	run(t, dir, "git", "config", "user.name", "CodeDojo")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, out)
	}
}
