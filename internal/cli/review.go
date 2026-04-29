package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dhruvmishra/codedojo/internal/app"
	"github.com/dhruvmishra/codedojo/internal/cli/repl"
	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/spf13/cobra"
)

func newReviewCommand() *cobra.Command {
	var repoPath string
	var difficulty int
	var budget int
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Start reviewer mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := reviewOptions{
				Repo:       repoPath,
				Difficulty: difficulty,
				Budget:     budget,
			}
			return runReview(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", "", "local path or remote URL for the repo to review")
	cmd.Flags().IntVar(&difficulty, "difficulty", 0, "mutation difficulty from 1 to 5")
	cmd.Flags().IntVar(&budget, "budget", 0, "hint count budget")
	return cmd
}

type reviewOptions struct {
	Repo       string
	Difficulty int
	Budget     int
}

func runReview(ctx context.Context, cmd *cobra.Command, opts reviewOptions) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	if opts.Repo == "" {
		return fmt.Errorf("--repo is required")
	}
	if opts.Difficulty == 0 {
		opts.Difficulty = cfg.Defaults.Difficulty
	}
	if opts.Difficulty < 1 || opts.Difficulty > 5 {
		return fmt.Errorf("--difficulty must be from 1 to 5")
	}
	if opts.Budget == 0 {
		opts.Budget = cfg.Defaults.HintBudget
	}
	if opts.Budget < 0 {
		return fmt.Errorf("--budget must be non-negative")
	}

	selected := selectSandbox(ctx, cmd.ErrOrStderr())
	service, err := app.NewService(ctx, cfg, selected.driver, selected.spec)
	if err != nil {
		return err
	}
	defer service.Close()

	live, err := service.StartReview(ctx, app.StartOptions{
		Repo:       opts.Repo,
		Difficulty: opts.Difficulty,
		HintBudget: opts.Budget,
	})
	if err != nil {
		return err
	}

	state := &reviewREPL{
		cmd:     cmd,
		service: service,
		live:    live,
	}
	ui := themeFor(cmd)
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\nReviewer task ready. Difficulty %d. Streak %d. Type help for commands.\n",
		ui.Banner("Reviewer mode"),
		opts.Difficulty,
		live.Streak,
	); err != nil {
		return err
	}
	return repl.Runner{
		In:      cmd.InOrStdin(),
		Out:     cmd.OutOrStdout(),
		Prompt:  ui.Prompt("review"),
		Handler: state.handle,
	}.Run(ctx)
}

type reviewREPL struct {
	cmd     *cobra.Command
	service *app.Service
	live    *app.LiveSession
}

func (r *reviewREPL) handle(ctx context.Context, line string) error {
	if line == "" {
		return nil
	}
	name, rest, _ := strings.Cut(line, " ")
	switch name {
	case "help":
		return r.help()
	case "tests":
		return r.tests(ctx)
	case "cat":
		return r.cat(strings.TrimSpace(rest))
	case "diff":
		return r.diff()
	case "hint":
		return r.hint(ctx, strings.TrimSpace(rest))
	case "submit":
		return r.submit(ctx, strings.TrimSpace(rest))
	case "quit", "exit":
		if !r.live.Done {
			_ = r.service.CloseSession(ctx, r.live.ID)
		}
		return repl.ErrExit
	default:
		_, err := fmt.Fprintf(r.cmd.OutOrStdout(), "unknown command %q; type help\n", name)
		return err
	}
}

func (r *reviewREPL) help() error {
	ui := themeFor(r.cmd)
	_, err := fmt.Fprint(r.cmd.OutOrStdout(), ui.CommandList(
		"help",
		"tests",
		"cat <file>",
		"diff",
		"hint [nudge|question|pointer|concept]",
		"submit <file>:<lineRange> <diagnosis>",
		"quit",
	))
	return err
}

func (r *reviewREPL) tests(ctx context.Context) error {
	result, err := r.service.RunTests(ctx, r.live.ID)
	if err != nil {
		return err
	}
	out := r.cmd.OutOrStdout()
	if result.Stdout != "" {
		if _, err := fmt.Fprint(out, result.Stdout); err != nil {
			return err
		}
	}
	if result.Stderr != "" {
		if _, err := fmt.Fprint(out, result.Stderr); err != nil {
			return err
		}
	}
	ui := themeFor(r.cmd)
	label := ui.Success("tests passed")
	if result.ExitCode != 0 {
		label = ui.Warning("tests failed")
	}
	if _, err := fmt.Fprintf(out, "%s\nexit code: %d\n", label, result.ExitCode); err != nil {
		return err
	}
	return nil
}

func (r *reviewREPL) cat(path string) error {
	if path == "" {
		return fmt.Errorf("usage: cat <file>")
	}
	data, err := r.service.ReadFile(r.live.ID, path)
	if err != nil {
		return err
	}
	ui := themeFor(r.cmd)
	_, err = fmt.Fprint(r.cmd.OutOrStdout(), ui.Box(path, data))
	return err
}

func (r *reviewREPL) diff() error {
	diff, err := r.service.Diff(r.live.ID)
	if err != nil {
		return err
	}
	ui := themeFor(r.cmd)
	_, err = fmt.Fprint(r.cmd.OutOrStdout(), ui.Box("diff", diff))
	return err
}

func (r *reviewREPL) hint(ctx context.Context, name string) error {
	level, err := parseHintLevel(name)
	if err != nil {
		return err
	}
	hint, err := r.service.Hint(ctx, r.live.ID, level)
	if err != nil {
		if strings.Contains(err.Error(), "hint budget exhausted") {
			ui := themeFor(r.cmd)
			_, writeErr := fmt.Fprintln(r.cmd.OutOrStdout(), ui.Warning("hint budget exhausted"))
			return writeErr
		}
		return err
	}
	ui := themeFor(r.cmd)
	_, err = fmt.Fprintf(r.cmd.OutOrStdout(), "%s %s\n", ui.Label("hint"), ui.Hint(hint.Hint))
	return err
}

func (r *reviewREPL) submit(ctx context.Context, text string) error {
	if text == "" {
		return fmt.Errorf("usage: submit <file>:<lineRange> <diagnosis>")
	}
	target, diagnosis, ok := strings.Cut(text, " ")
	if !ok || strings.TrimSpace(diagnosis) == "" {
		return fmt.Errorf("usage: submit <file>:<lineRange> <diagnosis>")
	}
	file, start, end, err := parseSubmissionTarget(target)
	if err != nil {
		return err
	}
	operator := inferOperatorClass(diagnosis)
	result, err := r.service.SubmitReview(ctx, r.live.ID, app.ReviewSubmission{
		FilePath:      file,
		StartLine:     start,
		EndLine:       end,
		OperatorClass: operator,
		Diagnosis:     diagnosis,
	})
	if err != nil {
		return err
	}
	ui := themeFor(r.cmd)
	if _, err := fmt.Fprintf(r.cmd.OutOrStdout(), "%s\nscore: %s\nfile: %d line: %d operator: %d diagnosis: %d hints: -%d time: %d streak: %d\n%s\n",
		ui.Banner("Result"),
		ui.Score(strconv.Itoa(result.Score)),
		result.Breakdown["file"],
		result.Breakdown["line"],
		result.Breakdown["operator"],
		result.Breakdown["diagnosis"],
		-result.Breakdown["hints"],
		result.Breakdown["time"],
		result.CurrentStreak,
		result.Feedback,
	); err != nil {
		return err
	}
	return repl.ErrExit
}

func parseHintLevel(name string) (coach.HintLevel, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "nudge":
		return coach.LevelNudge, nil
	case "question":
		return coach.LevelQuestion, nil
	case "pointer":
		return coach.LevelPointer, nil
	case "concept":
		return coach.LevelConcept, nil
	default:
		return coach.LevelNudge, fmt.Errorf("unknown hint level %q", name)
	}
}

func parseSubmissionTarget(target string) (string, int, int, error) {
	file, lineRange, ok := strings.Cut(target, ":")
	if !ok || strings.TrimSpace(file) == "" || strings.TrimSpace(lineRange) == "" {
		return "", 0, 0, fmt.Errorf("submission target must be <file>:<lineRange>")
	}
	startText, endText, hasEnd := strings.Cut(lineRange, "-")
	start, err := strconv.Atoi(startText)
	if err != nil || start <= 0 {
		return "", 0, 0, fmt.Errorf("invalid start line %q", startText)
	}
	end := start
	if hasEnd {
		end, err = strconv.Atoi(endText)
		if err != nil || end <= 0 {
			return "", 0, 0, fmt.Errorf("invalid end line %q", endText)
		}
	}
	return file, start, end, nil
}

func inferOperatorClass(diagnosis string) string {
	diagnosis = strings.ToLower(diagnosis)
	for _, mutator := range op.All() {
		if strings.Contains(diagnosis, strings.ToLower(mutator.Name())) {
			return mutator.Name()
		}
	}
	if strings.Contains(diagnosis, "comparison") || strings.Contains(diagnosis, "operator") {
		return "boundary"
	}
	if strings.Contains(diagnosis, "nil") || strings.Contains(diagnosis, "condition") {
		return "conditional"
	}
	if strings.Contains(diagnosis, "error") {
		return "errordrop"
	}
	if strings.Contains(diagnosis, "slice") {
		return "slicebounds"
	}
	return ""
}
