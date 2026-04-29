package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/sandbox/local"
)

func TestServiceReviewFlow(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample-go-repo"))
	if err != nil {
		t.Fatalf("abs repo path: %v", err)
	}

	live, err := service.StartReview(ctx, StartOptions{Repo: repoPath, Difficulty: 1, HintBudget: 1})
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if live.Mode != "reviewer" {
		t.Fatalf("mode = %s, want reviewer", live.Mode)
	}
	diff, err := service.Diff(live.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "(no local edits)\n" {
		t.Fatalf("diff = %q, want no local edits", diff)
	}
	hint, err := service.Hint(ctx, live.ID, 0)
	if err != nil {
		t.Fatalf("hint: %v", err)
	}
	if hint.HintsUsed != 1 || hint.Hint == "" {
		t.Fatalf("hint result = %+v, want one non-empty hint", hint)
	}

	result, err := service.SubmitReview(ctx, live.ID, ReviewSubmission{
		FilePath:      "calculator/calculator.go",
		StartLine:     13,
		EndLine:       13,
		OperatorClass: "boundary",
		Diagnosis:     "boundary comparison changed at the lower clamp check",
	})
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if result.Breakdown["file"] != 50 || result.Breakdown["line"] != 30 || result.Breakdown["operator"] != 20 {
		t.Fatalf("breakdown = %+v, want deterministic localization scores", result.Breakdown)
	}
	if !live.Done {
		t.Fatalf("session was not marked done")
	}
}

func TestServiceLearnFlow(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceLearnFixture(t)

	live, err := service.StartLearn(ctx, StartOptions{Repo: repoPath, Difficulty: 2, HintBudget: 1})
	if err != nil {
		t.Fatalf("start learn: %v", err)
	}
	if live.Task == "" {
		t.Fatalf("task description is empty")
	}
	if len(live.TaskFiles) != 2 {
		t.Fatalf("TaskFiles = %#v, want suggested source and test files", live.TaskFiles)
	}
	if live.TaskFiles[0].Path != "calculator/calculator.go" || live.TaskFiles[0].Test {
		t.Fatalf("TaskFiles[0] = %#v, want source suggestion", live.TaskFiles[0])
	}
	implementation := strings.Join([]string{
		"package calculator",
		"",
		"func Add(a, b int) int {",
		"\treturn a + b",
		"}",
		"",
		"func Multiply(a, b int) int {",
		"\tresult := 0",
		"\tfor i := 0; i < b; i++ {",
		"\t\tresult = Add(result, a)",
		"\t}",
		"\treturn result",
		"}",
		"",
	}, "\n")
	if err := service.WriteFile(live.ID, "calculator/calculator.go", implementation); err != nil {
		t.Fatalf("write file: %v", err)
	}
	result, err := service.SubmitLearn(ctx, live.ID)
	if err != nil {
		t.Fatalf("submit learn: %v", err)
	}
	if result.Breakdown["correctness"] != 100 {
		t.Fatalf("correctness = %d, want 100; result = %+v", result.Breakdown["correctness"], result)
	}
	if result.Reveal["reference_diff"] == "" {
		t.Fatalf("reference diff was not exposed")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.StorePath = filepath.Join(t.TempDir(), "codedojo.db")
	service, err := NewService(context.Background(), cfg, local.Driver{}, func(repoPath string) sandbox.Spec {
		return sandbox.Spec{
			RepoMount: repoPath,
			Network:   sandbox.NetworkNone,
			Timeout:   2 * time.Minute,
		}
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func newServiceLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeServiceFile(t, dir, "go.mod", "module example.com/learn\n\ngo 1.23\n")
	writeServiceFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeServiceFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n")
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial")

	writeServiceFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeServiceFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}

func writeServiceFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runServiceGit(t *testing.T, dir string, args ...string) string {
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
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
