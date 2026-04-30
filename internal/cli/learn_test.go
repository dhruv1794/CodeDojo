package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// nodeVersionAtLeast parses "v18.12.0" and returns whether the major version is >= major.
func nodeVersionAtLeast(versionStr string, major int) bool {
	s := strings.TrimPrefix(strings.TrimSpace(versionStr), "v")
	parts := strings.SplitN(s, ".", 2)
	if len(parts) == 0 {
		return false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	return n >= major
}

func learnTestSetup(t *testing.T) {
	t.Helper()
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })
	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

// ── Python ────────────────────────────────────────────────────────────────────

func TestRunLearnPython(t *testing.T) {
	if err := exec.Command("python", "-m", "pytest", "--version").Run(); err != nil {
		t.Skip("python -m pytest unavailable")
	}
	t.Setenv("CODEDOJO_SANDBOX", "local")
	learnTestSetup(t)
	repoPath := newPythonLearnFixture(t)

	implementation := strings.Join([]string{
		"def add(a, b):",
		"    return a + b",
		"",
		"",
		"def multiply(a, b):",
		"    return a * b",
	}, "\n")

	script := "diff\nwrite calculator.py\n" + implementation + "\nEOF\nsubmit\n"
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(script))
	cmd.SetOut(&out)

	if err := runLearn(context.Background(), cmd, learnOptions{Repo: repoPath, Difficulty: 2, Budget: 1}); err != nil {
		t.Fatalf("runLearn: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Newcomer task ready", "Feature: ", "correctness: 100", "approach: 40"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func newPythonLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "pyproject.toml", "[project]\nname = \"calculator\"\nversion = \"0.1.0\"\n")
	writeLearnFile(t, dir, "calculator.py", "def add(a, b):\n    return a + b\n")
	writeLearnFile(t, dir, "test_calculator.py", "from calculator import add\n\n\ndef test_add():\n    assert add(1, 2) == 3\n")
	runLearnGit(t, dir, "init")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "initial")

	writeLearnFile(t, dir, "calculator.py", "def add(a, b):\n    return a + b\n\n\ndef multiply(a, b):\n    return a * b\n")
	writeLearnFile(t, dir, "test_calculator.py", "from calculator import add, multiply\n\n\ndef test_add():\n    assert add(1, 2) == 3\n\n\ndef test_multiply():\n    assert multiply(2, 3) == 6\n")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}

// ── JavaScript ────────────────────────────────────────────────────────────────

func TestRunLearnJavaScript(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not in PATH")
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not in PATH")
	}
	vout, _ := exec.Command(nodePath, "--version").Output()
	if !nodeVersionAtLeast(string(vout), 18) {
		t.Skip("node 18+ required for --test runner")
	}
	t.Setenv("CODEDOJO_SANDBOX", "local")
	learnTestSetup(t)
	repoPath := newJavaScriptLearnFixture(t)

	implementation := strings.Join([]string{
		"function add(a, b) { return a + b; }",
		"function multiply(a, b) { return a * b; }",
		"module.exports = { add, multiply };",
	}, "\n")

	script := "diff\nwrite calculator/calculator.js\n" + implementation + "\nEOF\nsubmit\n"
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(script))
	cmd.SetOut(&out)

	if err := runLearn(context.Background(), cmd, learnOptions{Repo: repoPath, Difficulty: 2, Budget: 1}); err != nil {
		t.Fatalf("runLearn: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Newcomer task ready", "Feature: ", "correctness: 100", "approach: 40"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func newJavaScriptLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "package.json", "{\n  \"name\": \"js-learn-fixture\",\n  \"version\": \"1.0.0\",\n  \"scripts\": { \"test\": \"node --test calculator/calculator.test.js\" }\n}\n")
	writeLearnFile(t, dir, "calculator/calculator.js", "function add(a, b) { return a + b; }\nmodule.exports = { add };\n")
	writeLearnFile(t, dir, "calculator/calculator.test.js",
		"const test = require('node:test');\nconst assert = require('node:assert');\nconst { add } = require('./calculator');\n\ntest('add', () => { assert.strictEqual(add(1, 2), 3); });\n")
	runLearnGit(t, dir, "init")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "initial")

	writeLearnFile(t, dir, "calculator/calculator.js", "function add(a, b) { return a + b; }\nfunction multiply(a, b) { return a * b; }\nmodule.exports = { add, multiply };\n")
	writeLearnFile(t, dir, "calculator/calculator.test.js",
		"const test = require('node:test');\nconst assert = require('node:assert');\nconst { add, multiply } = require('./calculator');\n\ntest('add', () => { assert.strictEqual(add(1, 2), 3); });\ntest('multiply', () => { assert.strictEqual(multiply(2, 3), 6); });\n")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}

// ── TypeScript ────────────────────────────────────────────────────────────────

func TestRunLearnTypeScript(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not in PATH")
	}
	vout, _ := exec.Command(nodePath, "--version").Output()
	if !nodeVersionAtLeast(string(vout), 22) {
		t.Skip("node 22+ required for --experimental-strip-types")
	}
	t.Setenv("CODEDOJO_SANDBOX", "local")
	learnTestSetup(t)
	repoPath := newTypeScriptLearnFixture(t)

	implementation := strings.Join([]string{
		"function add(a, b) { return a + b; }",
		"function multiply(a, b) { return a * b; }",
		"module.exports = { add, multiply };",
	}, "\n")

	script := "diff\nwrite calculator/calculator.ts\n" + implementation + "\nEOF\nsubmit\n"
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(script))
	cmd.SetOut(&out)

	if err := runLearn(context.Background(), cmd, learnOptions{Repo: repoPath, Difficulty: 2, Budget: 1}); err != nil {
		t.Fatalf("runLearn: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Newcomer task ready", "Feature: ", "correctness: 100", "approach: 40"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func newTypeScriptLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// .codedojo.yaml overrides the test command so we don't need a build tool installed.
	writeLearnFile(t, dir, ".codedojo.yaml", "language: typescript\ntest_cmd: node --experimental-strip-types --test calculator/calculator.test.ts\n")
	writeLearnFile(t, dir, "tsconfig.json", "{\"compilerOptions\":{\"target\":\"es2020\",\"module\":\"commonjs\"}}\n")
	writeLearnFile(t, dir, "calculator/calculator.ts", "function add(a, b) { return a + b; }\nmodule.exports = { add };\n")
	writeLearnFile(t, dir, "calculator/calculator.test.ts",
		"const test = require('node:test');\nconst assert = require('node:assert');\nconst { add } = require('./calculator.ts');\n\ntest('add', () => { assert.strictEqual(add(1, 2), 3); });\n")
	runLearnGit(t, dir, "init")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "initial")

	writeLearnFile(t, dir, "calculator/calculator.ts", "function add(a, b) { return a + b; }\nfunction multiply(a, b) { return a * b; }\nmodule.exports = { add, multiply };\n")
	writeLearnFile(t, dir, "calculator/calculator.test.ts",
		"const test = require('node:test');\nconst assert = require('node:assert');\nconst { add, multiply } = require('./calculator.ts');\n\ntest('add', () => { assert.strictEqual(add(1, 2), 3); });\ntest('multiply', () => { assert.strictEqual(multiply(2, 3), 6); });\n")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}

// ── Rust ──────────────────────────────────────────────────────────────────────

func TestRunLearnRust(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo not in PATH")
	}
	t.Setenv("CODEDOJO_SANDBOX", "local")
	learnTestSetup(t)
	repoPath := newRustLearnFixture(t)

	implementation := strings.Join([]string{
		"pub fn add(a: i32, b: i32) -> i32 {",
		"    a + b",
		"}",
		"",
		"pub fn multiply(a: i32, b: i32) -> i32 {",
		"    a * b",
		"}",
	}, "\n")

	script := "diff\nwrite src/lib.rs\n" + implementation + "\nEOF\nsubmit\n"
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(script))
	cmd.SetOut(&out)

	if err := runLearn(context.Background(), cmd, learnOptions{Repo: repoPath, Difficulty: 2, Budget: 1}); err != nil {
		t.Fatalf("runLearn: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Newcomer task ready", "Feature: ", "correctness: 100", "approach: 40"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func newRustLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "Cargo.toml", "[package]\nname = \"calculator\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	writeLearnFile(t, dir, "src/lib.rs", "pub fn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n")
	writeLearnFile(t, dir, "tests/integration_test.rs", "use calculator::add;\n\n#[test]\nfn test_add() {\n    assert_eq!(add(1, 2), 3);\n}\n")
	runLearnGit(t, dir, "init")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "initial")

	writeLearnFile(t, dir, "src/lib.rs", "pub fn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n\npub fn multiply(a: i32, b: i32) -> i32 {\n    a * b\n}\n")
	writeLearnFile(t, dir, "tests/integration_test.rs", "use calculator::add;\nuse calculator::multiply;\n\n#[test]\nfn test_add() {\n    assert_eq!(add(1, 2), 3);\n}\n\n#[test]\nfn test_multiply() {\n    assert_eq!(multiply(2, 3), 6);\n}\n")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}
