package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/mock"
	"github.com/dhruvmishra/codedojo/internal/repo"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/sandbox/local"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func newSmokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "smoke",
		Short:  "Run the Week 1 smoke test",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSmoke(cmd.Context(), cmd)
		},
	}
	return cmd
}

func runSmoke(ctx context.Context, cmd *cobra.Command) error {
	root, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	source := filepath.Join(root, "testdata", "sample-go-repo")
	loaded, err := repo.OpenLocal(source)
	if err != nil {
		return err
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return err
	}
	if lang.Name != "go" {
		return fmt.Errorf("expected go sample repo, got %s", lang.Name)
	}
	store, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		return err
	}
	defer store.Close()

	manager := session.Manager{
		Coach:  coach.RetryWithStricterPrompt(mock.Coach{}, nil),
		Store:  store,
		Driver: local.Driver{},
	}
	sess := session.Session{
		ID:         fmt.Sprintf("smoke-%d", time.Now().UnixNano()),
		Mode:       session.ModeReviewer,
		Repo:       loaded.Path,
		Task:       "smoke",
		HintBudget: 3,
	}
	box, err := manager.New(ctx, sess, sandbox.Spec{RepoMount: loaded.Path, Network: sandbox.NetworkNone})
	if err != nil {
		return err
	}
	defer box.Close()
	for _, run := range [][]string{lang.BuildCmd, lang.TestCmd} {
		result, err := box.Exec(ctx, run)
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("%v failed\nstdout:\n%s\nstderr:\n%s", run, result.Stdout, result.Stderr)
		}
	}
	hint, err := manager.RequestHint(ctx, sess.ID, coach.LevelNudge, "smoke")
	if err != nil {
		return err
	}
	if hint.Content == "" {
		return fmt.Errorf("empty hint")
	}
	if err := manager.Close(ctx, sess.ID, box); err != nil {
		return err
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return err
	}
	if len(sessions) != 1 {
		return fmt.Errorf("expected one session row, got %d", len(sessions))
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "smoke ok: %s\n", hint.Content)
	return nil
}
