package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newReviewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "review",
		Short: "Start reviewer mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "review mode is planned for Week 2")
			return nil
		},
	}
}
