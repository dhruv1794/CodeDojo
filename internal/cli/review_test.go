package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/spf13/cobra"
)

func TestRunReviewScriptedSubmission(t *testing.T) {
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	repoPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample-go-repo"))
	if err != nil {
		t.Fatalf("abs repo path: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("diff\nsubmit calculator/calculator.go:13 boundary comparison changed at the lower clamp check\n"))
	cmd.SetOut(&out)

	err = runReview(context.Background(), cmd, reviewOptions{
		Repo:       repoPath,
		Difficulty: 1,
		Budget:     1,
	})
	if err != nil {
		t.Fatalf("runReview returned error: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Reviewer task ready",
		"(no local edits)",
		"score: 164",
		"file: 50 line: 30 operator: 20 diagnosis: 40",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output does not contain %q:\n%s", want, got)
		}
	}
}

func TestRunReviewPythonScriptedSubmission(t *testing.T) {
	t.Setenv("CODEDOJO_SANDBOX", "local")
	reviewTestSetup(t)
	repoPath := newPythonReviewFixture(t)

	got := runReviewScript(t, repoPath, "submit calculator.py:2 py-boundary comparison includes equal values in the lower branch\n")
	assertReviewOutput(t, got, "calculator.py", "py-boundary")
}

func TestRunReviewJavaScriptScriptedSubmission(t *testing.T) {
	t.Setenv("CODEDOJO_SANDBOX", "local")
	reviewTestSetup(t)
	repoPath := newJavaScriptReviewFixture(t)

	got := runReviewScript(t, repoPath, "submit calculator/calculator.js:2 js-boundary comparison includes equal values in the lower branch\n")
	assertReviewOutput(t, got, "calculator/calculator.js", "js-boundary")
}

func TestRunReviewTypeScriptScriptedSubmission(t *testing.T) {
	t.Setenv("CODEDOJO_SANDBOX", "local")
	reviewTestSetup(t)
	repoPath := newTypeScriptReviewFixture(t)

	got := runReviewScript(t, repoPath, "submit calculator/calculator.ts:2 js-boundary comparison includes equal values in the lower branch\n")
	assertReviewOutput(t, got, "calculator/calculator.ts", "js-boundary")
}

func TestRunReviewRustScriptedSubmission(t *testing.T) {
	t.Setenv("CODEDOJO_SANDBOX", "local")
	reviewTestSetup(t)
	repoPath := newRustReviewFixture(t)

	got := runReviewScript(t, repoPath, "submit src/lib.rs:2 rs-boundary comparison includes equal values in the lower branch\n")
	assertReviewOutput(t, got, "src/lib.rs", "rs-boundary")
}

func reviewTestSetup(t *testing.T) {
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

func runReviewScript(t *testing.T, repoPath, script string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(script))
	cmd.SetOut(&out)

	err := runReview(context.Background(), cmd, reviewOptions{
		Repo:       repoPath,
		Difficulty: 1,
		Budget:     1,
	})
	if err != nil {
		t.Fatalf("runReview returned error: %v\noutput:\n%s", err, out.String())
	}
	return out.String()
}

func assertReviewOutput(t *testing.T, got, file, operator string) {
	t.Helper()
	for _, want := range []string{
		"Reviewer task ready",
		"score: ",
		"file: 50 line: 30 operator: 20 diagnosis: 40",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output does not contain %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, file) {
		t.Fatalf("output does not contain file %q:\n%s", file, got)
	}
	if !strings.Contains(got, operator) {
		t.Fatalf("output does not contain operator %q:\n%s", operator, got)
	}
}

func newPythonReviewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "pyproject.toml", "[project]\nname = \"py-review-fixture\"\nversion = \"0.1.0\"\n")
	writeLearnFile(t, dir, "calculator.py", strings.Join([]string{
		"def clamp_lower(value, minimum):",
		"    if value < minimum:",
		"        return minimum",
		"    return value",
	}, "\n")+"\n")
	writeLearnFile(t, dir, "test_calculator.py", strings.Join([]string{
		"from calculator import clamp_lower",
		"",
		"",
		"def test_clamp_lower():",
		"    assert clamp_lower(0, 1) == 1",
		"    assert clamp_lower(2, 1) == 2",
	}, "\n")+"\n")
	initReviewFixtureGit(t, dir)
	return dir
}

func newJavaScriptReviewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "package.json", "{\n  \"name\": \"js-review-fixture\",\n  \"version\": \"1.0.0\",\n  \"scripts\": { \"test\": \"node --test calculator/calculator.test.js\" }\n}\n")
	writeLearnFile(t, dir, "calculator/calculator.js", strings.Join([]string{
		"function clampLower(value, minimum) {",
		"  if (value < minimum) {",
		"    return minimum;",
		"  }",
		"  return value;",
		"}",
		"module.exports = { clampLower };",
	}, "\n")+"\n")
	writeLearnFile(t, dir, "calculator/calculator.test.js", strings.Join([]string{
		"const test = require('node:test');",
		"const assert = require('node:assert');",
		"const { clampLower } = require('./calculator');",
		"",
		"test('clampLower', () => {",
		"  assert.strictEqual(clampLower(0, 1), 1);",
		"  assert.strictEqual(clampLower(2, 1), 2);",
		"});",
	}, "\n")+"\n")
	initReviewFixtureGit(t, dir)
	return dir
}

func newTypeScriptReviewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, ".codedojo.yaml", "language: typescript\ntest_cmd: node --experimental-strip-types --test calculator/calculator.test.ts\n")
	writeLearnFile(t, dir, "tsconfig.json", "{\"compilerOptions\":{\"target\":\"es2020\",\"module\":\"commonjs\"}}\n")
	writeLearnFile(t, dir, "calculator/calculator.ts", strings.Join([]string{
		"function clampLower(value: number, minimum: number): number {",
		"  if (value < minimum) {",
		"    return minimum;",
		"  }",
		"  return value;",
		"}",
		"module.exports = { clampLower };",
	}, "\n")+"\n")
	writeLearnFile(t, dir, "calculator/calculator.test.ts", strings.Join([]string{
		"const test = require('node:test');",
		"const assert = require('node:assert');",
		"const { clampLower } = require('./calculator.ts');",
		"",
		"test('clampLower', () => {",
		"  assert.strictEqual(clampLower(0, 1), 1);",
		"  assert.strictEqual(clampLower(2, 1), 2);",
		"});",
	}, "\n")+"\n")
	initReviewFixtureGit(t, dir)
	return dir
}

func newRustReviewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "Cargo.toml", "[package]\nname = \"rust_review_fixture\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	writeLearnFile(t, dir, "src/lib.rs", strings.Join([]string{
		"pub fn clamp_lower(value: i32, minimum: i32) -> i32 {",
		"    if value < minimum {",
		"        return minimum;",
		"    }",
		"    value",
		"}",
		"",
		"#[cfg(test)]",
		"mod tests {",
		"    use super::clamp_lower;",
		"",
		"    #[test]",
		"    fn clamps_lower_bound() {",
		"        assert_eq!(clamp_lower(0, 1), 1);",
		"        assert_eq!(clamp_lower(2, 1), 2);",
		"    }",
		"}",
	}, "\n")+"\n")
	initReviewFixtureGit(t, dir)
	return dir
}

func initReviewFixtureGit(t *testing.T, dir string) {
	t.Helper()
	runLearnGit(t, dir, "init")
	runLearnGit(t, dir, "add", ".")
	runLearnGit(t, dir, "commit", "-m", "initial review fixture")
}
