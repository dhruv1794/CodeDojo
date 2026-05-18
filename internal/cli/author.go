// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dhruvmishra/codedojo/internal/modes/author"
	"github.com/spf13/cobra"
)

func newAuthorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "author",
		Short: "Create curated CodeDojo kata packs",
	}
	cmd.AddCommand(newAuthorPackCommand())
	return cmd
}

func newAuthorPackCommand() *cobra.Command {
	var opts authorPackOptions
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Generate a curated reviewer mutation pack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthorPack(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "local git repo to sample mutations from")
	cmd.Flags().StringVar(&opts.Output, "output", "", "path to write the pack JSON")
	cmd.Flags().StringVar(&opts.Title, "title", "", "pack title")
	cmd.Flags().IntVar(&opts.Count, "count", 1, "number of unique mutation tasks to include")
	cmd.Flags().IntVar(&opts.Difficulty, "difficulty", 0, "difficulty from 1 to 5, or 0 to sample all difficulties")
	cmd.Flags().BoolVar(&opts.AllowPartial, "allow-partial", false, "write a partial pack with a warning when fewer unique tasks than --count are found")
	return cmd
}

type authorPackOptions struct {
	Repo         string
	Output       string
	Title        string
	Count        int
	Difficulty   int
	AllowPartial bool
}

func runAuthorPack(ctx context.Context, cmd *cobra.Command, opts authorPackOptions) error {
	if opts.Repo == "" {
		return fmt.Errorf("--repo is required")
	}
	if opts.Output == "" {
		return fmt.Errorf("--output is required")
	}
	pack, err := author.GeneratePack(ctx, author.PackOptions{
		Repo:         opts.Repo,
		Title:        opts.Title,
		Count:        opts.Count,
		Difficulty:   opts.Difficulty,
		AllowPartial: opts.AllowPartial,
	})
	if err != nil {
		return err
	}
	if err := writeAuthorPack(opts.Output, pack); err != nil {
		return err
	}
	ui := themeFor(cmd)
	requested := opts.Count
	if requested <= 0 {
		requested = 1
	}
	if len(pack.Tasks) < requested {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s only %d of %d requested kata(s) were unique; wrote a partial pack\n",
			ui.Warning("author pack"), len(pack.Tasks), requested); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s wrote %d kata(s) to %s\n", ui.Success("author pack"), len(pack.Tasks), opts.Output)
	return err
}

func writeAuthorPack(path string, pack author.Pack) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create pack directory: %w", err)
	}
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal author pack: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write author pack: %w", err)
	}
	return nil
}
