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
