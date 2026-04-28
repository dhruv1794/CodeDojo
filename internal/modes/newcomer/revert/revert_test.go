package revert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
)

func TestRevertRestoreRoundTrip(t *testing.T) {
	dir, featureSHA := newRevertFixture(t)
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	r := repo.Repo{Path: dir, Git: gitRepo}

	state, err := Revert(context.Background(), r, featureSHA)
	if err != nil {
		t.Fatalf("Revert() error = %v", err)
	}
	if state.GroundTruthSHA != featureSHA {
		t.Fatalf("GroundTruthSHA = %q, want %q", state.GroundTruthSHA, featureSHA)
	}
	if state.StartingSHA == "" || state.StartingSHA == featureSHA {
		t.Fatalf("StartingSHA = %q, want parent sha", state.StartingSHA)
	}
	if !strings.Contains(state.ReferenceDiff, "+func Multiply") {
		t.Fatalf("ReferenceDiff missing feature implementation:\n%s", state.ReferenceDiff)
	}
	if !strings.Contains(state.ReverseDiff, "-func Multiply") {
		t.Fatalf("ReverseDiff missing removed feature implementation:\n%s", state.ReverseDiff)
	}
	if fileContains(t, filepath.Join(dir, "calculator/calculator.go"), "Multiply") {
		t.Fatalf("Revert() left feature implementation in working tree")
	}

	writeRevertFile(t, dir, "calculator/notes.txt", "user scratch\n")
	restored, err := Restore(context.Background(), r, state)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if !restored.RestoredToTruth {
		t.Fatalf("RestoredToTruth = false, want true")
	}
	if !fileContains(t, filepath.Join(dir, "calculator/calculator.go"), "Multiply") {
		t.Fatalf("Restore() did not restore feature implementation")
	}
	if _, err := os.Stat(filepath.Join(dir, "calculator/notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("Restore() did not discard scratch file, stat err = %v", err)
	}
}

func TestRevertRejectsRootCommit(t *testing.T) {
	dir, _ := newRevertFixture(t)
	rootSHA := runRevertGit(t, dir, "rev-list", "--max-parents=0", "HEAD")
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}

	_, err = Revert(context.Background(), repo.Repo{Path: dir, Git: gitRepo}, rootSHA)
	if err == nil || !strings.Contains(err.Error(), "has 0 parents") {
		t.Fatalf("Revert(root) error = %v, want root parent error", err)
	}
}

func newRevertFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	writeRevertFile(t, dir, "go.mod", "module example.com/revert\n\ngo 1.23\n")
	writeRevertFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeRevertFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n")
	runRevertGit(t, dir, "init")
	runRevertGit(t, dir, "add", ".")
	runRevertGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "initial")

	writeRevertFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeRevertFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runRevertGit(t, dir, "add", ".")
	runRevertGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "add multiplication")
	return dir, runRevertGit(t, dir, "rev-parse", "HEAD")
}

func writeRevertFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func fileContains(t *testing.T, path, want string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Contains(string(data), want)
}

func runRevertGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
