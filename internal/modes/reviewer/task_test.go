package reviewer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

func TestGenerateTaskAppliesMutation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/reviewer\n\ngo 1.23\n")
	writeFile(t, dir, "calc/calc.go", "package calc\n\nfunc Clamp(value, min int) int {\n\tif value < min {\n\t\treturn min\n\t}\n\treturn value\n}\n")
	writeFile(t, dir, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestClamp(t *testing.T) {}\n")
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	task, err := TaskGenerator{
		Engine: mutate.Engine{
			Mutators: []mutate.Mutator{op.Boundary{}},
			Now:      func() time.Time { return now },
		},
	}.GenerateTask(context.Background(), repo.Repo{Path: dir}, 1)
	if err != nil {
		t.Fatalf("GenerateTask returned error: %v", err)
	}
	if task.RepoPath != dir {
		t.Fatalf("RepoPath = %q, want %q", task.RepoPath, dir)
	}
	if task.Difficulty != 1 {
		t.Fatalf("Difficulty = %d, want 1", task.Difficulty)
	}
	if task.MutationLog.Mutation.Operator != "boundary" {
		t.Fatalf("operator = %q, want boundary", task.MutationLog.Mutation.Operator)
	}
	if task.MutationLog.Mutation.FilePath != "calc/calc.go" {
		t.Fatalf("file = %q, want calc/calc.go", task.MutationLog.Mutation.FilePath)
	}
	data, err := os.ReadFile(filepath.Join(dir, "calc/calc.go"))
	if err != nil {
		t.Fatalf("read mutated file: %v", err)
	}
	if !strings.Contains(string(data), "value <= min") {
		t.Fatalf("mutated file does not contain boundary flip:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, mutate.DefaultLogPath)); err != nil {
		t.Fatalf("mutation log was not written: %v", err)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", rel, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=CodeDojo",
		"GIT_AUTHOR_EMAIL=codedojo@example.test",
		"GIT_COMMITTER_NAME=CodeDojo",
		"GIT_COMMITTER_EMAIL=codedojo@example.test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
