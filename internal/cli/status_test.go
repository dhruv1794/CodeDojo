package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func TestRunStatusListsSessions(t *testing.T) {
	path := seedStatusStore(t)
	withTestConfig(t, path)

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runStatus(context.Background(), cmd, 10); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"MODE", "reviewer", "newcomer", "sample-go-repo", "120"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
}

func TestRunStatsPrintsAggregates(t *testing.T) {
	path := seedStatusStore(t)
	withTestConfig(t, path)

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runStats(context.Background(), cmd); err != nil {
		t.Fatalf("runStats() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"Sessions: 2 total", "Streak: current 1, best 1", "By mode:", "By repo:", "By operator:", "boundary"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stats output missing %q:\n%s", want, got)
		}
	}
}

func seedStatusStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codedojo.db")
	ctx := context.Background()
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	started := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	for _, sess := range []session.Session{
		{ID: "review-1", Mode: session.ModeReviewer, Repo: "/tmp/sample-go-repo", Task: "review", HintBudget: 3, HintsUsed: 1, Score: 120, State: session.StateClosed, StartedAt: started},
		{ID: "learn-1", Mode: session.ModeNewcomer, Repo: "/tmp/sample-go-repo", Task: "learn", HintBudget: 3, Score: 0, State: session.StateRunning, StartedAt: started.Add(time.Minute)},
	} {
		if err := store.CreateSession(ctx, sess); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}
	if err := store.SaveMutationLog(ctx, "review-1", mutate.MutationLog{
		ID:         "mut-1",
		RepoPath:   "/tmp/sample-go-repo",
		Difficulty: 1,
		Mutation:   mutate.Mutation{Operator: "boundary", FilePath: "calculator.go", StartLine: 1, EndLine: 1},
		CreatedAt:  started,
	}); err != nil {
		t.Fatalf("save mutation log: %v", err)
	}
	if _, err := store.RecordStreakResult(ctx, true); err != nil {
		t.Fatalf("record streak: %v", err)
	}
	return path
}

func withTestConfig(t *testing.T, storePath string) {
	t.Helper()
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Default()
	cfg.StorePath = storePath
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	cfgFile = path
}
