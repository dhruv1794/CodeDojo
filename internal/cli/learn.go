package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/cli/repl"
	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer"
	"github.com/dhruvmishra/codedojo/internal/repo"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func newLearnCommand() *cobra.Command {
	var repoPath string
	var difficulty int
	var budget int
	cmd := &cobra.Command{
		Use:   "learn",
		Short: "Start newcomer mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := learnOptions{
				Repo:       repoPath,
				Difficulty: difficulty,
				Budget:     budget,
			}
			return runLearn(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", "", "local path or remote URL for the repo to learn from")
	cmd.Flags().IntVar(&difficulty, "difficulty", 0, "task difficulty from 1 to 5")
	cmd.Flags().IntVar(&budget, "budget", 0, "hint count budget")
	return cmd
}

type learnOptions struct {
	Repo       string
	Difficulty int
	Budget     int
}

func runLearn(ctx context.Context, cmd *cobra.Command, opts learnOptions) error {
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

	store, err := sqlite.Open(ctx, cfg.StorePath)
	if err != nil {
		return err
	}
	defer store.Close()

	loaded, err := loadReviewRepo(ctx, opts.Repo)
	if err != nil {
		return err
	}
	task, err := newcomer.GenerateTask(ctx, loaded, opts.Difficulty)
	if err != nil {
		return err
	}
	lang, err := repo.DetectLanguage(task.RepoPath)
	if err != nil {
		return err
	}
	if len(lang.TestCmd) == 0 {
		return fmt.Errorf("no test command detected for repo")
	}

	sessionID := fmt.Sprintf("learn-%d", time.Now().UnixNano())
	hintCoach, err := buildCoach(cfg, task.BannedIdentifiers)
	if err != nil {
		return err
	}
	gradeCoach, err := newBackendCoach(cfg)
	if err != nil {
		return err
	}
	selected := selectSandbox(ctx, cmd.ErrOrStderr())
	manager := session.Manager{
		Coach:  hintCoach,
		Store:  store,
		Driver: selected.driver,
	}
	sess := session.Session{
		ID:         sessionID,
		Mode:       session.ModeNewcomer,
		Repo:       opts.Repo,
		Task:       task.FeatureDescription,
		HintBudget: opts.Budget,
		StartedAt:  time.Now(),
	}
	box, err := manager.New(ctx, sess, selected.spec(task.RepoPath))
	if err != nil {
		return err
	}
	defer box.Close()
	streak, err := store.GetStreak(ctx)
	if err != nil {
		return err
	}

	state := &learnREPL{
		cmd:        cmd,
		manager:    manager,
		store:      store,
		box:        box,
		testCmd:    lang.TestCmd,
		task:       task,
		sessionID:  sessionID,
		startedAt:  sess.StartedAt,
		hintLimit:  opts.Budget,
		streak:     streak.Current,
		gradeCoach: gradeCoach,
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Newcomer task ready. Difficulty %d. Streak %d.\nFeature: %s\nType help for commands.\n", opts.Difficulty, streak.Current, task.FeatureDescription); err != nil {
		return err
	}
	return repl.Runner{
		In:        cmd.InOrStdin(),
		Out:       cmd.OutOrStdout(),
		Prompt:    "codedojo(learn)> ",
		Multiline: state.handle,
	}.Run(ctx)
}

type learnREPL struct {
	cmd        *cobra.Command
	manager    session.Manager
	store      *sqlite.Store
	box        sandbox.Session
	testCmd    []string
	task       newcomer.Task
	sessionID  string
	startedAt  time.Time
	hintLimit  int
	hintCosts  []int
	hintsUsed  int
	streak     int
	done       bool
	gradeCoach coach.Coach
}

func (l *learnREPL) handle(ctx context.Context, line string, next repl.LineSource) error {
	if line == "" {
		return nil
	}
	name, rest, _ := strings.Cut(line, " ")
	switch name {
	case "help":
		return l.help()
	case "tests":
		return l.tests(ctx)
	case "cat":
		return l.cat(strings.TrimSpace(rest))
	case "diff":
		return l.diff()
	case "write":
		return l.write(strings.TrimSpace(rest), next)
	case "hint":
		return l.hint(ctx, strings.TrimSpace(rest))
	case "submit":
		return l.submit(ctx)
	case "quit", "exit":
		if !l.done {
			_ = l.manager.Close(ctx, l.sessionID, l.box)
		}
		return repl.ErrExit
	default:
		_, err := fmt.Fprintf(l.cmd.OutOrStdout(), "unknown command %q; type help\n", name)
		return err
	}
}

func (l *learnREPL) help() error {
	_, err := fmt.Fprint(l.cmd.OutOrStdout(), `commands:
  help
  tests
  cat <file>
  diff
  write <file>           (then enter content; finish with EOF on its own line)
  hint [nudge|question|pointer|concept]
  submit
  quit
`)
	return err
}

func (l *learnREPL) tests(ctx context.Context) error {
	result, err := l.box.Exec(ctx, l.testCmd)
	if err != nil {
		return err
	}
	out := l.cmd.OutOrStdout()
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
	if _, err := fmt.Fprintf(out, "exit code: %d\n", result.ExitCode); err != nil {
		return err
	}
	return nil
}

func (l *learnREPL) cat(path string) error {
	if path == "" {
		return fmt.Errorf("usage: cat <file>")
	}
	data, err := l.box.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(l.cmd.OutOrStdout(), string(data))
	return err
}

func (l *learnREPL) diff() error {
	diff, err := l.box.Diff()
	if err != nil {
		return err
	}
	if diff == "" {
		diff = "(no local edits)\n"
	}
	_, err = fmt.Fprint(l.cmd.OutOrStdout(), diff)
	return err
}

func (l *learnREPL) write(path string, next repl.LineSource) error {
	if path == "" {
		return fmt.Errorf("usage: write <file>")
	}
	var lines []string
	for {
		line, ok, err := next()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("write %s: unexpected end of input before EOF marker", path)
		}
		if line == "EOF" {
			break
		}
		lines = append(lines, line)
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := l.box.WriteFile(path, []byte(content)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(l.cmd.OutOrStdout(), "wrote %d lines to %s\n", len(lines), path)
	return err
}

func (l *learnREPL) hint(ctx context.Context, name string) error {
	if l.hintsUsed >= l.hintLimit {
		_, err := fmt.Fprintln(l.cmd.OutOrStdout(), "hint budget exhausted")
		return err
	}
	level, err := parseHintLevel(name)
	if err != nil {
		return err
	}
	hint, err := l.manager.RequestHint(ctx, l.sessionID, level, l.task.FeatureDescription)
	if err != nil {
		return err
	}
	l.hintsUsed++
	l.hintCosts = append(l.hintCosts, hint.Cost)
	_, err = fmt.Fprintf(l.cmd.OutOrStdout(), "%s\n", hint.Content)
	return err
}

func (l *learnREPL) submit(ctx context.Context) error {
	if err := l.manager.Submit(ctx, l.sessionID, "newcomer-submission"); err != nil {
		return err
	}
	userDiff, err := l.box.Diff()
	if err != nil {
		return err
	}
	result, err := newcomer.Grade(ctx, l.task, newcomer.Submission{
		SessionID:    l.sessionID,
		UserDiff:     userDiff,
		NewTestFuncs: newcomer.CountNewTestFuncs(userDiff),
		HintCosts:    l.hintCosts,
		StartedAt:    l.startedAt,
		SubmittedAt:  time.Now(),
		Streak:       l.streak,
	}, newcomer.GradeOptions{
		Coach:   l.gradeCoach,
		TestCmd: l.testCmd,
		Sandbox: l.box,
	})
	if err != nil {
		return err
	}
	if err := l.store.UpsertScore(ctx, l.sessionID, result.Score); err != nil {
		return err
	}
	if err := l.store.UpdateState(ctx, l.sessionID, session.StateGraded); err != nil {
		return err
	}
	if err := l.store.AppendEvent(ctx, session.Event{SessionID: l.sessionID, Type: session.EventGrade, Payload: fmt.Sprintf("score=%d", result.Score)}); err != nil {
		return err
	}
	streak, err := l.store.RecordStreakResult(ctx, result.Score > 0)
	if err != nil {
		return err
	}
	if err := l.manager.Close(ctx, l.sessionID, l.box); err != nil {
		return err
	}
	l.done = true
	out := l.cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "score: %d\ncorrectness: %d approach: %d tests: %d hints: -%d streak: %d\n%s\n",
		result.Score,
		result.CorrectnessScore,
		result.ApproachScore,
		result.TestQualityScore,
		result.HintDeduction,
		streak.Current,
		result.ApproachFeedback,
	); err != nil {
		return err
	}
	if result.CorrectnessScore == 0 {
		if _, err := fmt.Fprintf(out, "tests still failing (exit %d)\n", result.TestExitCode); err != nil {
			return err
		}
	}
	return repl.ErrExit
}
