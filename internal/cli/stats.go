package cli

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func newStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate CodeDojo stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(cmd.Context(), cmd)
		},
	}
}

func runStats(ctx context.Context, cmd *cobra.Command) error {
	store, err := openConfiguredStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Sessions: %d total, %d scored, avg %.1f, best %d\n", stats.Total, stats.Graded, stats.Average, stats.Best); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Streak: current %d, best %d\n", stats.Streak.Current, stats.Streak.Best); err != nil {
		return err
	}
	if err := printGroupStats(out, "By mode", stats.ByMode); err != nil {
		return err
	}
	if err := printGroupStats(out, "By repo", stats.ByRepo); err != nil {
		return err
	}
	return printGroupStats(out, "By operator", stats.ByOp)
}

type statWriter interface {
	Write([]byte) (int, error)
}

func printGroupStats(out statWriter, title string, stats []sqlite.GroupStat) error {
	if _, err := fmt.Fprintf(out, "%s:\n", title); err != nil {
		return err
	}
	if len(stats) == 0 {
		_, err := fmt.Fprintln(out, "  none")
		return err
	}
	for _, stat := range stats {
		if _, err := fmt.Fprintf(out, "  %-24s count %-3d avg %5.1f best %d\n", stat.Name, stat.Count, stat.Average, stat.Best); err != nil {
			return err
		}
	}
	return nil
}
