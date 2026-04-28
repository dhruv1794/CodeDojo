package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
	noColor bool
	version = "dev"
	commit  = "none"
)

func Execute() error {
	return newRootCommand().Execute()
}

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codedojo",
		Short: "Deliberate practice for developers in the AI era",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
		},
	}
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable color output")
	cmd.AddCommand(newInitCommand(), newReviewCommand(), newLearnCommand(), newStatusCommand(), newVersionCommand(), newSmokeCommand())
	return cmd
}
