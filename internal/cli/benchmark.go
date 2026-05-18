// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/benchmark"
	"github.com/spf13/cobra"
)

func newBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run CodeDojo benchmark packs",
	}
	cmd.AddCommand(newBenchmarkRunCommand())
	return cmd
}

func newBenchmarkRunCommand() *cobra.Command {
	var opts benchmarkRunOptions
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a curated mutation benchmark pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBenchmark(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.PackPath, "pack", "", "author pack JSON to run")
	cmd.Flags().StringVar(&opts.Output, "output", "", "path to write benchmark results JSON")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 2*time.Minute, "per-task test timeout")
	return cmd
}

type benchmarkRunOptions struct {
	PackPath string
	Output   string
	Timeout  time.Duration
}

func runBenchmark(ctx context.Context, cmd *cobra.Command, opts benchmarkRunOptions) error {
	if opts.PackPath == "" {
		return fmt.Errorf("--pack is required")
	}
	if opts.Output == "" {
		return fmt.Errorf("--output is required")
	}
	results, err := benchmark.Run(ctx, benchmark.RunOptions{
		PackPath: opts.PackPath,
		Timeout:  opts.Timeout,
	})
	if err != nil {
		return err
	}
	if err := writeBenchmarkResults(opts.Output, results); err != nil {
		return err
	}
	ui := themeFor(cmd)
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s ran %d kata(s): %d passed, %d failed -> %s\n",
		ui.Success("benchmark"),
		results.Total,
		results.Passed,
		results.Failed,
		opts.Output,
	)
	return err
}

func writeBenchmarkResults(path string, results benchmark.Results) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create results directory: %w", err)
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal benchmark results: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write benchmark results: %w", err)
	}
	return nil
}
