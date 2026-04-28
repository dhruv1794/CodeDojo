package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/spf13/cobra"
)

func TestRunLearnScriptedReimplementation(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	repoPath := newLearnFixture(t)

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
	}, "\n")

	script := strings.Join([]string{
		"diff",
		"write calculator/calculator.go",
		implementation,
		"EOF",
		"submit",
	}, "\n") + "\n"

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(script))
	cmd.SetOut(&out)

	err := runLearn(context.Background(), cmd, learnOptions{
		Repo:       repoPath,
		Difficulty: 2,
		Budget:     1,
	})
	if err != nil {
		t.Fatalf("runLearn returned error: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Newcomer task ready",
		"Feature: ",
		"wrote 13 lines to calculator/calculator.go",
		"correctness: 100",
		"approach: 40",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output does not contain %q:\n%s", want, got)
		}
	}
}

func TestRunLearnRejectsBadDifficulty(t *testing.T) {
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	err := runLearn(context.Background(), cmd, learnOptions{Repo: tmp, Difficulty: 7, Budget: 1})
	if err == nil || !strings.Contains(err.Error(), "difficulty") {
		t.Fatalf("runLearn error = %v, want difficulty validation", err)
	}
}

func newLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "go.mod", "module example.com/learn\n\ngo 1.23\n")
	writeLearnFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeLearnFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n")
	runLearnGit(t, dir, "init")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "initial")

	writeLearnFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeLearnFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}

func writeLearnFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runLearnGit(t *testing.T, dir string, args ...string) string {
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
