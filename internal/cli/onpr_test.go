// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunOnPRWritesSpotterChallenge(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	original, err := os.ReadFile(filepath.Join(repoPath, "calculator", "calculator.go"))
	if err != nil {
		t.Fatalf("read original source: %v", err)
	}
	writeLearnFile(t, repoPath, "calculator/calculator.go", string(original)+"\n// Clamp is intentionally part of this PR.\n")
	runLearnGit(t, repoPath, "add", ".")
	runLearnGit(t, repoPath, "commit", "-m", "touch clamp implementation")

	outPath := filepath.Join(t.TempDir(), "spotter-challenge.md")
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err = runOnPR(context.Background(), cmd, onPROptions{
		Repo:       repoPath,
		Base:       "HEAD~1",
		Head:       "HEAD",
		Output:     outPath,
		Difficulty: 1,
	})
	if err != nil {
		t.Fatalf("runOnPR returned error: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "wrote spotter challenge for calculator/calculator.go") {
		t.Fatalf("output = %q, want selected file", out.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"# CodeDojo Spotter Challenge",
		"Range: `HEAD~1...HEAD`",
		"Challenge file: `calculator/calculator.go`",
		"Review the PR diff and find the injected bug",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("artifact does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "boundary") {
		t.Fatalf("artifact leaked mutation operator:\n%s", got)
	}
	after, err := os.ReadFile(filepath.Join(repoPath, "calculator", "calculator.go"))
	if err != nil {
		t.Fatalf("read source after on-pr: %v", err)
	}
	if string(after) != string(original)+"\n// Clamp is intentionally part of this PR.\n" {
		t.Fatalf("source repo was mutated by on-pr")
	}
}

func TestRunOnPRRequiresOutput(t *testing.T) {
	cmd := &cobra.Command{}
	err := runOnPR(context.Background(), cmd, onPROptions{Repo: "repo"})
	if err == nil || !strings.Contains(err.Error(), "--output is required") {
		t.Fatalf("error = %v, want output requirement", err)
	}
}
