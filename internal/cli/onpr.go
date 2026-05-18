// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/modes/onpr"
	"github.com/spf13/cobra"
)

func newOnPRCommand() *cobra.Command {
	var opts onPROptions
	cmd := &cobra.Command{
		Use:   "on-pr",
		Short: "Create a spotter challenge from a PR diff",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnPR(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "local git repo to inspect")
	cmd.Flags().StringVar(&opts.Base, "base", "origin/main", "base ref for the PR diff")
	cmd.Flags().StringVar(&opts.Head, "head", "HEAD", "head ref for the PR diff")
	cmd.Flags().StringVar(&opts.Output, "output", "", "path to write the spotter challenge Markdown")
	cmd.Flags().IntVar(&opts.Difficulty, "difficulty", 3, "difficulty from 1 to 5")
	return cmd
}

type onPROptions struct {
	Repo       string
	Base       string
	Head       string
	Output     string
	Difficulty int
}

func runOnPR(ctx context.Context, cmd *cobra.Command, opts onPROptions) error {
	if opts.Repo == "" {
		return fmt.Errorf("--repo is required")
	}
	if opts.Output == "" {
		return fmt.Errorf("--output is required")
	}
	challenge, err := onpr.Generate(ctx, onpr.Options{
		Repo:       opts.Repo,
		Base:       opts.Base,
		Head:       opts.Head,
		Difficulty: opts.Difficulty,
	})
	if err != nil {
		return err
	}
	if err := onpr.WriteMarkdown(opts.Output, challenge); err != nil {
		return err
	}
	ui := themeFor(cmd)
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s wrote spotter challenge for %s -> %s\n", ui.Success("on-pr"), challenge.SelectedFile, opts.Output)
	return err
}
