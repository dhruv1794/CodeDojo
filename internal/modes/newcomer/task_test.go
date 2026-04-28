package newcomer

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

func TestGenerateTaskRevertsRankedCommitAndStripsIdentifiers(t *testing.T) {
	dir, featureSHA := newTaskFixture(t)
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}

	task, err := GenerateTask(context.Background(), repo.Repo{Path: dir, Git: gitRepo}, 2)
	if err != nil {
		t.Fatalf("GenerateTask() error = %v", err)
	}
	if task.GroundTruthSHA != featureSHA {
		t.Fatalf("GroundTruthSHA = %q, want %q", task.GroundTruthSHA, featureSHA)
	}
	if task.StartingSHA == "" || task.StartingSHA == featureSHA {
		t.Fatalf("StartingSHA = %q, want parent", task.StartingSHA)
	}
	if !strings.Contains(task.FeatureDescription, "Add multiplication behavior covered by the original tests.") {
		t.Fatalf("FeatureDescription = %q, want snapshot summary", task.FeatureDescription)
	}
	for _, banned := range task.BannedIdentifiers {
		if strings.Contains(strings.ToLower(task.FeatureDescription), strings.ToLower(banned)) {
			t.Fatalf("FeatureDescription %q leaked banned identifier %q", task.FeatureDescription, banned)
		}
	}
	if !contains(task.BannedIdentifiers, "Multiply") || !contains(task.BannedIdentifiers, "TestMultiply") {
		t.Fatalf("BannedIdentifiers = %#v, want introduced identifiers", task.BannedIdentifiers)
	}
	if fileContainsTask(t, filepath.Join(dir, "calculator/calculator.go"), "Multiply") {
		t.Fatalf("GenerateTask() left feature implementation in working tree")
	}
	if task.Candidate.SHA != featureSHA {
		t.Fatalf("Candidate.SHA = %q, want %q", task.Candidate.SHA, featureSHA)
	}
}

func TestGenerateTaskRejectsLeakySummary(t *testing.T) {
	dir, _ := newTaskFixture(t)
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}

	_, err = TaskGenerator{Summarizer: leakySummarizer{summary: "Call Multiply to implement the behavior."}}.
		GenerateTask(context.Background(), repo.Repo{Path: dir, Git: gitRepo}, 2)
	if err == nil || !strings.Contains(err.Error(), "leaked implementation detail") {
		t.Fatalf("GenerateTask() error = %v, want leak rejection", err)
	}
}

func TestIntroducedIdentifiersOnlyUsesAddedLines(t *testing.T) {
	diff := `diff --git a/calc.go b/calc.go
--- a/calc.go
+++ b/calc.go
@@ -1,3 +1,7 @@
 package calc
-func Add(a, b int) int { return a + b }
+func Multiply(a, b int) int { return a * b }
+func TestMultiply(t *testing.T) { t.Fatal("bad multiply") }
`
	got := IntroducedIdentifiers(diff)
	if !contains(got, "Multiply") || !contains(got, "TestMultiply") {
		t.Fatalf("IntroducedIdentifiers() = %#v, want added identifiers", got)
	}
	if contains(got, "Add") {
		t.Fatalf("IntroducedIdentifiers() = %#v, should not include deleted identifiers", got)
	}
}

type leakySummarizer struct {
	summary string
}

func (s leakySummarizer) Summarize(context.Context, SummaryRequest) (string, error) {
	return s.summary, nil
}

func newTaskFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	writeTaskFile(t, dir, "go.mod", "module example.com/newcomer\n\ngo 1.23\n")
	writeTaskFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeTaskFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n")
	runTaskGit(t, dir, "init")
	runTaskGit(t, dir, "add", ".")
	runTaskGit(t, dir, "commit", "-m", "initial")

	writeTaskFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeTaskFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runTaskGit(t, dir, "add", ".")
	runTaskGit(t, dir, "commit", "-m", "add multiplication")
	return dir, runTaskGit(t, dir, "rev-parse", "HEAD")
}

func writeTaskFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runTaskGit(t *testing.T, dir string, args ...string) string {
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

func fileContainsTask(t *testing.T, path, want string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Contains(string(data), want)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
