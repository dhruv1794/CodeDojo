package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show recent CodeDojo sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), cmd, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum sessions to list")
	return cmd
}

func runStatus(ctx context.Context, cmd *cobra.Command, limit int) error {
	if limit < 0 {
		return fmt.Errorf("--limit must be non-negative")
	}
	store, err := openConfiguredStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(sessions) == 0 || limit == 0 {
		_, err := fmt.Fprintln(out, "No sessions yet.")
		return err
	}
	if _, err := fmt.Fprintln(out, "STARTED              MODE      SCORE  STATE   HINTS  REPO"); err != nil {
		return err
	}
	for i, sess := range sessions {
		if i >= limit {
			break
		}
		if _, err := fmt.Fprintf(out, "%-20s %-9s %-6d %-7s %-6d %s\n",
			formatStarted(sess.StartedAt),
			sess.Mode,
			sess.Score,
			sess.State,
			sess.HintsUsed,
			shortRepo(sess),
		); err != nil {
			return err
		}
	}
	return nil
}

func openConfiguredStore(ctx context.Context) (*sqlite.Store, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, err
	}
	return sqlite.Open(ctx, cfg.StorePath)
}

func formatStarted(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func shortRepo(sess session.Session) string {
	if sess.Repo == "" {
		return "-"
	}
	base := filepath.Base(sess.Repo)
	if base == "." || base == string(filepath.Separator) {
		return sess.Repo
	}
	return base
}
