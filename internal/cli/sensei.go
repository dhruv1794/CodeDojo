// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/app"
	"github.com/dhruvmishra/codedojo/internal/cli/repl"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/author"
	"github.com/spf13/cobra"
)

func newSenseiCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sensei",
		Short: "Publish and play authored CodeDojo katas",
	}
	cmd.AddCommand(newSenseiPublishCommand(), newSenseiInspectCommand(), newSenseiVetCommand(), newSenseiPlayCommand())
	return cmd
}

func newSenseiPublishCommand() *cobra.Command {
	var opts senseiPublishOptions
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a single authored reviewer kata pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSenseiPublish(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "local git repo to publish a kata from")
	cmd.Flags().StringVar(&opts.Output, "output", "", "path to write the sensei pack JSON")
	cmd.Flags().StringVar(&opts.Title, "title", "", "kata title")
	cmd.Flags().StringVar(&opts.Description, "description", "", "learner-facing kata brief")
	cmd.Flags().StringVar(&opts.Author, "author", "", "sensei or team name")
	cmd.Flags().StringVar(&opts.Commit, "commit", "", "source commit to sample from")
	cmd.Flags().IntVar(&opts.Difficulty, "difficulty", 3, "difficulty from 1 to 5")
	cmd.Flags().BoolVar(&opts.Vet, "vet", false, "only publish a kata whose mutation makes tests fail")
	cmd.Flags().IntVar(&opts.MaxAttempts, "max-attempts", 12, "maximum mutation candidates to try when --vet is set")
	cmd.Flags().DurationVar(&opts.VetTimeout, "vet-timeout", 2*time.Minute, "per-test-run timeout when --vet is set")
	return cmd
}

type senseiPublishOptions struct {
	Repo        string
	Output      string
	Title       string
	Description string
	Author      string
	Commit      string
	Difficulty  int
	Vet         bool
	MaxAttempts int
	VetTimeout  time.Duration
}

func newSenseiPlayCommand() *cobra.Command {
	var opts senseiPlayOptions
	cmd := &cobra.Command{
		Use:   "play",
		Short: "Play an authored Sensei kata pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runSenseiPlay(cmd.Context(), cmd, opts)
			silenceUsageForProductError(cmd, err)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.PackPath, "pack", "", "sensei pack JSON to play")
	cmd.Flags().StringVar(&opts.TaskID, "task", "", "task id inside the pack; defaults to the first task")
	cmd.Flags().IntVar(&opts.Budget, "budget", 0, "hint count budget")
	cmd.Flags().DurationVar(&opts.SubmitTimeout, "submit-timeout", defaultSubmitTimeout, "time limit for grading and session cleanup on submit")
	return cmd
}

func silenceUsageForProductError(cmd *cobra.Command, err error) {
	var product app.ProductError
	if errors.As(err, &product) {
		cmd.SilenceUsage = true
	}
}

type senseiPlayOptions struct {
	PackPath      string
	TaskID        string
	Budget        int
	SubmitTimeout time.Duration
}

func newSenseiInspectCommand() *cobra.Command {
	var opts senseiInspectOptions
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect an authored Sensei kata pack without running it",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSenseiInspect(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.PackPath, "pack", "", "sensei pack JSON to inspect")
	return cmd
}

type senseiInspectOptions struct {
	PackPath string
}

func newSenseiVetCommand() *cobra.Command {
	var opts senseiVetOptions
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "Validate that a Sensei kata pack is playable",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runSenseiVet(cmd.Context(), cmd, opts)
			var vetErr senseiVetFailedError
			if errors.As(err, &vetErr) {
				cmd.SilenceUsage = true
			}
			return err
		},
	}
	cmd.Flags().StringVar(&opts.PackPath, "pack", "", "sensei pack JSON to vet")
	cmd.Flags().StringVar(&opts.TaskID, "task", "", "task id inside the pack; defaults to all tasks")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 2*time.Minute, "per-test-run timeout")
	return cmd
}

type senseiVetOptions struct {
	PackPath string
	TaskID   string
	Timeout  time.Duration
}

type senseiVetFailedError struct {
	Failed int
	Total  int
}

func (e senseiVetFailedError) Error() string {
	return fmt.Sprintf("sensei vet failed: %d of %d kata(s) failed", e.Failed, e.Total)
}

func runSenseiPublish(ctx context.Context, cmd *cobra.Command, opts senseiPublishOptions) error {
	if opts.Repo == "" {
		return fmt.Errorf("--repo is required")
	}
	if opts.Output == "" {
		return fmt.Errorf("--output is required")
	}
	if opts.Description == "" {
		return fmt.Errorf("--description is required")
	}
	packOpts := author.PackOptions{
		Repo:        opts.Repo,
		Title:       opts.Title,
		Author:      opts.Author,
		Brief:       opts.Description,
		Commit:      opts.Commit,
		Count:       1,
		MaxAttempts: opts.MaxAttempts,
		Difficulty:  opts.Difficulty,
	}
	var pack author.Pack
	var vetReport author.VetReport
	var err error
	if opts.Vet {
		pack, vetReport, err = author.GenerateVettedPack(ctx, packOpts, author.VetOptions{Timeout: opts.VetTimeout})
	} else {
		pack, err = author.GeneratePack(ctx, packOpts)
	}
	if err != nil {
		return err
	}
	if err := writeAuthorPack(opts.Output, pack); err != nil {
		return err
	}
	absOutput, err := filepath.Abs(opts.Output)
	if err != nil {
		absOutput = opts.Output
	}
	ui := themeFor(cmd)
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s published %q to %s\n",
		ui.Success("sensei"),
		pack.Title,
		opts.Output,
	)
	if err != nil {
		return err
	}
	if opts.Vet {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "vet: %d passed, %d failed\n", vetReport.Passed, vetReport.Failed); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Open locally with: codedojo serve --repo %s, then http://localhost:8080/?kata=%s\n", opts.Repo, url.QueryEscape(absOutput))
	return err
}

func runSenseiInspect(cmd *cobra.Command, opts senseiInspectOptions) error {
	if strings.TrimSpace(opts.PackPath) == "" {
		return fmt.Errorf("--pack is required")
	}
	pack, err := author.ReadPack(opts.PackPath)
	if err != nil {
		return err
	}
	if pack.SchemaVersion != author.PackSchemaVersion {
		return fmt.Errorf("unsupported pack schema %q", pack.SchemaVersion)
	}
	absPath, err := filepath.Abs(opts.PackPath)
	if err != nil {
		absPath = opts.PackPath
	}
	ui := themeFor(cmd)
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "%s\n", ui.Banner("Sensei pack")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "title: %s\n", emptyLabel(pack.Title)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "author: %s\n", emptyLabel(pack.Author)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "source: %s\n", emptyLabel(pack.Source)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "head: %s\n", emptyLabel(pack.HeadSHA)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "language: %s\n", emptyLabel(pack.Language)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "created: %s\n", pack.CreatedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "tasks: %d\n", len(pack.Tasks)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "\nTask inventory:"); err != nil {
		return err
	}
	for _, task := range pack.Tasks {
		mutation := task.MutationLog.Mutation
		snapshots := "missing"
		if mutation.Original != "" && mutation.Mutated != "" {
			snapshots = "ok"
		}
		brief := task.Brief
		if brief == "" {
			brief = task.Description
		}
		if _, err := fmt.Fprintf(out, "- %s difficulty=%d file=%s:%d-%d operator=%s snapshots=%s\n",
			task.ID,
			task.Difficulty,
			emptyLabel(task.FilePath),
			task.StartLine,
			task.EndLine,
			emptyLabel(task.Operator),
			snapshots,
		); err != nil {
			return err
		}
		if strings.TrimSpace(brief) != "" {
			if _, err := fmt.Fprintf(out, "  brief: %s\n", brief); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(out, "\nVet with: codedojo sensei vet --pack %s\n", shellQuote(absPath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Play with: codedojo sensei play --pack %s\n", shellQuote(absPath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Web link: http://localhost:8080/?kata=%s\n", url.QueryEscape(absPath)); err != nil {
		return err
	}
	return nil
}

func runSenseiVet(ctx context.Context, cmd *cobra.Command, opts senseiVetOptions) error {
	if strings.TrimSpace(opts.PackPath) == "" {
		return fmt.Errorf("--pack is required")
	}
	report, err := author.VetPack(ctx, author.VetOptions{
		PackPath: opts.PackPath,
		TaskID:   opts.TaskID,
		Timeout:  opts.Timeout,
	})
	if err != nil {
		return err
	}
	ui := themeFor(cmd)
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "%s\n", ui.Banner("Sensei vet")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "pack: %s\nsource: %s\nhead: %s\nlanguage: %s\ntest: %s\n",
		emptyLabel(report.PackTitle),
		emptyLabel(report.Source),
		emptyLabel(report.HeadSHA),
		emptyLabel(report.Language),
		strings.Join(report.TestCommand, " "),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "summary: %d passed, %d failed, %d total\n\n", report.Passed, report.Failed, report.Total); err != nil {
		return err
	}
	for _, check := range report.Checks {
		status := "FAIL"
		if check.Passed {
			status = "PASS"
		}
		if _, err := fmt.Fprintf(out, "- %s %s file=%s operator=%s baseline=%t mutation_fail=%t snapshots=%t\n",
			status,
			check.TaskID,
			emptyLabel(check.FilePath),
			emptyLabel(check.Operator),
			check.BaselinePass,
			check.MutationFail,
			check.SnapshotsOK,
		); err != nil {
			return err
		}
		if strings.TrimSpace(check.Error) != "" {
			if _, err := fmt.Fprintf(out, "  error: %s\n", check.Error); err != nil {
				return err
			}
		}
	}
	if report.Failed > 0 {
		return senseiVetFailedError{Failed: report.Failed, Total: report.Total}
	}
	return nil
}

func runSenseiPlay(ctx context.Context, cmd *cobra.Command, opts senseiPlayOptions) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.PackPath) == "" {
		return fmt.Errorf("--pack is required")
	}
	if opts.Budget == 0 {
		opts.Budget = cfg.Defaults.HintBudget
	}
	if opts.Budget < 0 {
		return fmt.Errorf("--budget must be non-negative")
	}
	if opts.SubmitTimeout < 0 {
		return fmt.Errorf("--submit-timeout must be non-negative")
	}

	selected := selectSandbox(ctx, cmd.ErrOrStderr())
	service, err := app.NewService(ctx, cfg, selected.driver, selected.spec)
	if err != nil {
		return err
	}
	defer service.Close()

	live, err := service.StartSensei(ctx, app.SenseiStartOptions{
		PackPath:   opts.PackPath,
		TaskID:     opts.TaskID,
		HintBudget: opts.Budget,
	})
	if err != nil {
		return err
	}
	state := &reviewREPL{
		cmd:           cmd,
		service:       service,
		live:          live,
		submitTimeout: opts.SubmitTimeout,
	}
	ui := themeFor(cmd)
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\nSensei kata ready. Difficulty %d. Streak %d. Type help for commands.\n",
		ui.Banner("Sensei mode"),
		live.Difficulty,
		live.Streak,
	); err != nil {
		return err
	}
	if live.Task != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n%s\n", ui.Label("brief"), live.Task); err != nil {
			return err
		}
	}
	if len(live.TaskFiles) > 0 {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\nSensei files:\n"); err != nil {
			return err
		}
		for _, file := range live.TaskFiles {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", file.Path); err != nil {
				return err
			}
		}
	}
	return repl.Runner{
		In:      cmd.InOrStdin(),
		Out:     cmd.OutOrStdout(),
		Prompt:  ui.Prompt("sensei"),
		Handler: state.handle,
	}.Run(ctx)
}

func emptyLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(empty)"
	}
	return value
}

func shellQuote(path string) string {
	if path == "" {
		return "''"
	}
	if !strings.ContainsAny(path, " \t\n'\"\\$`!*?[]{}()&;|<>") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
