package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/cli/repl"
	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
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

	store, err := sqlite.Open(ctx, cfg.StorePath)
	if err != nil {
		return err
	}
	defer store.Close()

	loaded, err := loadReviewRepo(ctx, opts.Repo)
	if err != nil {
		return err
	}
	task, err := reviewer.GenerateTask(ctx, loaded, opts.Difficulty)
	if err != nil {
		return err
	}
	if err := hideMutationLog(task.RepoPath); err != nil {
		return err
	}
	if err := stageReviewBaseline(ctx, task.RepoPath); err != nil {
		return err
	}
	lang, err := repo.DetectLanguage(task.RepoPath)
	if err != nil {
		return err
	}
	if len(lang.TestCmd) == 0 {
		return fmt.Errorf("no test command detected for repo")
	}

	sessionID := fmt.Sprintf("review-%d", time.Now().UnixNano())
	bannedIdents := []string{
		task.MutationLog.Mutation.Operator,
		task.MutationLog.Mutation.Original,
		task.MutationLog.Mutation.Mutated,
	}
	hintCoach, err := buildCoach(cfg, bannedIdents)
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
		Mode:       session.ModeReviewer,
		Repo:       opts.Repo,
		Task:       task.Instructions,
		HintBudget: opts.Budget,
		StartedAt:  time.Now(),
	}
	box, err := manager.New(ctx, sess, selected.spec(task.RepoPath))
	if err != nil {
		return err
	}
	defer box.Close()
	if err := store.SaveMutationLog(ctx, sessionID, task.MutationLog); err != nil {
		return err
	}
	streak, err := store.GetStreak(ctx)
	if err != nil {
		return err
	}

	state := &reviewREPL{
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
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Reviewer task ready. Difficulty %d. Streak %d. Type help for commands.\n", opts.Difficulty, streak.Current); err != nil {
		return err
	}
	return repl.Runner{
		In:      cmd.InOrStdin(),
		Out:     cmd.OutOrStdout(),
		Prompt:  "codedojo(review)> ",
		Handler: state.handle,
	}.Run(ctx)
}

type reviewREPL struct {
	cmd        *cobra.Command
	manager    session.Manager
	store      *sqlite.Store
	box        sandbox.Session
	testCmd    []string
	task       reviewer.Task
	sessionID  string
	startedAt  time.Time
	hintLimit  int
	hintCosts  []int
	hintsUsed  int
	streak     int
	done       bool
	gradeCoach coach.Coach
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
		if !r.done {
			_ = r.manager.Close(ctx, r.sessionID, r.box)
		}
		return repl.ErrExit
	default:
		_, err := fmt.Fprintf(r.cmd.OutOrStdout(), "unknown command %q; type help\n", name)
		return err
	}
}

func (r *reviewREPL) help() error {
	_, err := fmt.Fprint(r.cmd.OutOrStdout(), `commands:
  help
  tests
  cat <file>
  diff
  hint [nudge|question|pointer|concept]
  submit <file>:<lineRange> <diagnosis>
  quit
`)
	return err
}

func (r *reviewREPL) tests(ctx context.Context) error {
	result, err := r.box.Exec(ctx, r.testCmd)
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
	if _, err := fmt.Fprintf(out, "exit code: %d\n", result.ExitCode); err != nil {
		return err
	}
	return nil
}

func (r *reviewREPL) cat(path string) error {
	if path == "" {
		return fmt.Errorf("usage: cat <file>")
	}
	data, err := r.box.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(r.cmd.OutOrStdout(), string(data))
	return err
}

func (r *reviewREPL) diff() error {
	diff, err := r.box.Diff()
	if err != nil {
		return err
	}
	if diff == "" {
		diff = "(no local edits)\n"
	}
	_, err = fmt.Fprint(r.cmd.OutOrStdout(), diff)
	return err
}

func (r *reviewREPL) hint(ctx context.Context, name string) error {
	if r.hintsUsed >= r.hintLimit {
		_, err := fmt.Fprintln(r.cmd.OutOrStdout(), "hint budget exhausted")
		return err
	}
	level, err := parseHintLevel(name)
	if err != nil {
		return err
	}
	hint, err := r.manager.RequestHint(ctx, r.sessionID, level, r.task.Instructions)
	if err != nil {
		return err
	}
	r.hintsUsed++
	r.hintCosts = append(r.hintCosts, hint.Cost)
	_, err = fmt.Fprintf(r.cmd.OutOrStdout(), "%s\n", hint.Content)
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
	if err := r.manager.Submit(ctx, r.sessionID, text); err != nil {
		return err
	}
	result, err := reviewer.Grade(ctx, reviewer.Submission{
		SessionID:     r.sessionID,
		FilePath:      file,
		StartLine:     start,
		EndLine:       end,
		OperatorClass: operator,
		Diagnosis:     diagnosis,
		HintCosts:     r.hintCosts,
		StartedAt:     r.startedAt,
		SubmittedAt:   time.Now(),
		Streak:        r.streak,
	}, r.task.MutationLog, reviewer.GradeOptions{Coach: r.gradeCoach})
	if err != nil {
		return err
	}
	if err := r.store.UpsertScore(ctx, r.sessionID, result.Score); err != nil {
		return err
	}
	if err := r.store.UpdateState(ctx, r.sessionID, session.StateGraded); err != nil {
		return err
	}
	if err := r.store.AppendEvent(ctx, session.Event{SessionID: r.sessionID, Type: session.EventGrade, Payload: fmt.Sprintf("score=%d", result.Score)}); err != nil {
		return err
	}
	streak, err := r.store.RecordStreakResult(ctx, result.Score > 0)
	if err != nil {
		return err
	}
	if err := r.manager.Close(ctx, r.sessionID, r.box); err != nil {
		return err
	}
	r.done = true
	if _, err := fmt.Fprintf(r.cmd.OutOrStdout(), "score: %d\nfile: %d line: %d operator: %d diagnosis: %d hints: -%d time: %d streak: %d\n%s\n",
		result.Score,
		result.FileScore,
		result.LineScore,
		result.OperatorScore,
		result.DiagnosisScore,
		result.HintDeduction,
		result.TimeBonus,
		streak.Current,
		result.DiagnosisFeedback,
	); err != nil {
		return err
	}
	return repl.ErrExit
}

func loadReviewRepo(ctx context.Context, source string) (repo.Repo, error) {
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return repo.OpenLocal(source)
	}
	tmp, err := os.MkdirTemp("", "codedojo-clone-*")
	if err != nil {
		return repo.Repo{}, fmt.Errorf("create clone temp dir: %w", err)
	}
	loaded, err := repo.Clone(ctx, source, tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return repo.Repo{}, err
	}
	return loaded, nil
}

func hideMutationLog(repoPath string) error {
	logPath := filepath.Join(repoPath, mutate.DefaultLogPath)
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hide mutation log: %w", err)
	}
	_ = os.Remove(filepath.Dir(logPath))
	return nil
}

func stageReviewBaseline(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "add", "--", ".")
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stage reviewer baseline: %w: %s", err, out)
	}
	return nil
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
