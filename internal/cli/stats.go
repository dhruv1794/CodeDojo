// SPDX-License-Identifier: MIT

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
		RunE: func(cmd *cobra.Command, _ []string) error {
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
	ui := themeFor(cmd)
	if _, err := fmt.Fprintf(out, "%s\n", ui.Banner("Dojo stats")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Katas: %d total, %d scored, avg %.1f, best %d\n", stats.Total, stats.Graded, stats.Average, stats.Best); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Streak: current %d, best %d\n", stats.Streak.Current, stats.Streak.Best); err != nil {
		return err
	}
	if stats.Recommendation.Name != "" {
		if _, err := fmt.Fprintf(out, "Next practice: %s %s (solve %.0f%%, avg %.1f, hints %.1f, time %.1fm)\n",
			stats.Recommendation.Kind,
			stats.Recommendation.Name,
			stats.Recommendation.SolveRate*100,
			stats.Recommendation.Average,
			stats.Recommendation.AvgHints,
			stats.Recommendation.AvgMinutes,
		); err != nil {
			return err
		}
	}
	if err := printGroupStats(out, ui, "By mode", stats.ByMode); err != nil {
		return err
	}
	if err := printGroupStats(out, ui, "By repo", stats.ByRepo); err != nil {
		return err
	}
	if err := printGroupStats(out, ui, "Mistake Index", stats.ByOp); err != nil {
		return err
	}
	if err := printEngagementStats(out, ui, "Engagement Signals", stats.Engagement); err != nil {
		return err
	}
	return printCostStats(out, ui, stats.Cost)
}

type statWriter interface {
	Write([]byte) (int, error)
}

func printGroupStats(out statWriter, ui cliTheme, title string, stats []sqlite.GroupStat) error {
	if _, err := fmt.Fprintf(out, "%s\n", ui.Label(title)); err != nil {
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

func printEngagementStats(out statWriter, ui cliTheme, title string, stats []sqlite.EngagementStat) error {
	if _, err := fmt.Fprintf(out, "%s\n", ui.Label(title)); err != nil {
		return err
	}
	if len(stats) == 0 {
		_, err := fmt.Fprintln(out, "  none")
		return err
	}
	for _, stat := range stats {
		marker := " "
		if stat.Recommended {
			marker = "*"
		}
		if _, err := fmt.Fprintf(out, "%s %-10s %-20s count %-3d solve %3.0f%% avg %5.1f hints %3.1f time %4.1fm\n",
			marker,
			stat.Kind,
			stat.Name,
			stat.Count,
			stat.SolveRate*100,
			stat.Average,
			stat.AvgHints,
			stat.AvgMinutes,
		); err != nil {
			return err
		}
	}
	return nil
}

func printCostStats(out statWriter, ui cliTheme, stats sqlite.CostStats) error {
	if _, err := fmt.Fprintf(out, "%s\n", ui.Label("Cost Dashboard")); err != nil {
		return err
	}
	if stats.Calls == 0 {
		_, err := fmt.Fprintln(out, "  none")
		return err
	}
	if _, err := fmt.Fprintf(out, "  calls %d input %d output %d cache %d total $%.4f avg/session $%.4f tokens/hint %.1f projected/month $%.2f\n",
		stats.Calls,
		stats.InputTokens,
		stats.OutputTokens,
		stats.CacheTokens,
		stats.TotalUSD,
		stats.AvgUSDPerSession,
		stats.TokensPerHint,
		stats.ProjectedMonthUSD,
	); err != nil {
		return err
	}
	for _, stat := range stats.ByBackend {
		if _, err := fmt.Fprintf(out, "  %-12s calls %-3d input %-6d output %-6d total $%.4f\n",
			stat.Name,
			stat.Calls,
			stat.InputTokens,
			stat.OutputTokens,
			stat.TotalUSD,
		); err != nil {
			return err
		}
	}
	return nil
}
