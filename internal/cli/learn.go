package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLearnCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "learn",
		Short: "Start newcomer mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "learn mode is planned for Week 3")
			return nil
		},
	}
}
