package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/anthropic"
	"github.com/dhruvmishra/codedojo/internal/coach/mock"
	"github.com/dhruvmishra/codedojo/internal/coach/ollama"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/repo"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
)

type SpecBuilder func(repoPath string) sandbox.Spec

type Service struct {
	cfg       config.Config
	store     *sqlite.Store
	driver    sandbox.Driver
	spec      SpecBuilder
	gradeBack coach.Coach

	mu       sync.Mutex
	sessions map[string]*LiveSession
}

type LiveSession struct {
	ID         string       `json:"id"`
	Mode       session.Mode `json:"mode"`
	Repo       string       `json:"repo"`
	RepoPath   string       `json:"repo_path"`
	Task       string       `json:"task"`
	Difficulty int          `json:"difficulty"`
	HintBudget int          `json:"hint_budget"`
	HintsUsed  int          `json:"hints_used"`
	Streak     int          `json:"streak"`
	StartedAt  time.Time    `json:"started_at"`
	Done       bool         `json:"done"`

	manager   session.Manager
	box       sandbox.Session
	testCmd   []string
	hintCosts []int

	reviewTask *reviewer.Task
	learnTask  *newcomer.Task
}

type StartOptions struct {
	Repo       string `json:"repo"`
	Difficulty int    `json:"difficulty"`
	HintBudget int    `json:"hint_budget"`
}

type HintResult struct {
	Hint      string `json:"hint"`
	Cost      int    `json:"cost"`
	HintsUsed int    `json:"hints_used"`
}

type TestResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type FileEntry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

type ReviewSubmission struct {
	FilePath      string `json:"file_path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	OperatorClass string `json:"operator_class"`
	Diagnosis     string `json:"diagnosis"`
}

type Result struct {
	Score     int               `json:"score"`
	Feedback  string            `json:"feedback"`
	Breakdown map[string]int    `json:"breakdown"`
	Reveal    map[string]string `json:"reveal,omitempty"`
}

func NewService(ctx context.Context, cfg config.Config, driver sandbox.Driver, spec SpecBuilder) (*Service, error) {
	store, err := sqlite.Open(ctx, cfg.StorePath)
	if err != nil {
		return nil, err
	}
	gradeBack, err := newBackendCoach(cfg)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &Service{
		cfg:       cfg,
		store:     store,
		driver:    driver,
		spec:      spec,
		gradeBack: gradeBack,
		sessions:  map[string]*LiveSession{},
	}, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, live := range s.sessions {
		if live.box != nil && !live.Done {
			_ = live.manager.Close(context.Background(), id, live.box)
		}
	}
	return s.store.Close()
}

func (s *Service) StartReview(ctx context.Context, opts StartOptions) (*LiveSession, error) {
	opts = s.withDefaults(opts)
	loaded, err := loadRepo(ctx, opts.Repo)
	if err != nil {
		return nil, err
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return nil, err
	}
	if lang.Name != "go" {
		return nil, fmt.Errorf("review mode currently supports Go repositories only; detected %s. Learn mode can use detected test commands for this repo", lang.Name)
	}
	task, err := reviewer.GenerateTask(ctx, loaded, opts.Difficulty)
	if err != nil {
		return nil, err
	}
	if err := hideMutationLog(task.RepoPath); err != nil {
		return nil, err
	}
	if err := stageReviewBaseline(ctx, task.RepoPath); err != nil {
		return nil, err
	}
	lang, err = repo.DetectLanguage(task.RepoPath)
	if err != nil {
		return nil, err
	}
	if len(lang.TestCmd) == 0 {
		return nil, fmt.Errorf("no test command detected for repo")
	}
	banned := []string{
		task.MutationLog.Mutation.Operator,
		task.MutationLog.Mutation.Original,
		task.MutationLog.Mutation.Mutated,
	}
	hintCoach, err := buildCoach(s.cfg, banned)
	if err != nil {
		return nil, err
	}
	live, err := s.start(ctx, session.ModeReviewer, opts, task.RepoPath, task.Instructions, lang.TestCmd, hintCoach)
	if err != nil {
		return nil, err
	}
	live.reviewTask = &task
	if err := s.store.SaveMutationLog(ctx, live.ID, task.MutationLog); err != nil {
		_ = s.CloseSession(ctx, live.ID)
		return nil, err
	}
	return live, nil
}

func (s *Service) StartLearn(ctx context.Context, opts StartOptions) (*LiveSession, error) {
	opts = s.withDefaults(opts)
	loaded, err := loadRepo(ctx, opts.Repo)
	if err != nil {
		return nil, err
	}
	task, err := newcomer.GenerateTask(ctx, loaded, opts.Difficulty)
	if err != nil {
		return nil, err
	}
	lang, err := repo.DetectLanguage(task.RepoPath)
	if err != nil {
		return nil, err
	}
	if len(lang.TestCmd) == 0 {
		return nil, fmt.Errorf("no test command detected for repo")
	}
	hintCoach, err := buildCoach(s.cfg, task.BannedIdentifiers)
	if err != nil {
		return nil, err
	}
	live, err := s.start(ctx, session.ModeNewcomer, opts, task.RepoPath, task.FeatureDescription, lang.TestCmd, hintCoach)
	if err != nil {
		return nil, err
	}
	live.learnTask = &task
	return live, nil
}

func (s *Service) start(ctx context.Context, mode session.Mode, opts StartOptions, repoPath, task string, testCmd []string, hintCoach coach.Coach) (*LiveSession, error) {
	id := fmt.Sprintf("%s-%d", mode, time.Now().UnixNano())
	manager := session.Manager{Coach: hintCoach, Store: s.store, Driver: s.driver}
	started := time.Now()
	row := session.Session{
		ID:         id,
		Mode:       mode,
		Repo:       opts.Repo,
		Task:       task,
		HintBudget: opts.HintBudget,
		StartedAt:  started,
	}
	box, err := manager.New(ctx, row, s.spec(repoPath))
	if err != nil {
		return nil, err
	}
	streak, err := s.store.GetStreak(ctx)
	if err != nil {
		_ = box.Close()
		return nil, err
	}
	live := &LiveSession{
		ID:         id,
		Mode:       mode,
		Repo:       opts.Repo,
		RepoPath:   repoPath,
		Task:       task,
		Difficulty: opts.Difficulty,
		HintBudget: opts.HintBudget,
		Streak:     streak.Current,
		StartedAt:  started,
		manager:    manager,
		box:        box,
		testCmd:    testCmd,
	}
	s.mu.Lock()
	s.sessions[id] = live
	s.mu.Unlock()
	return live, nil
}

func (s *Service) Session(id string) (*LiveSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return live, nil
}

func (s *Service) ListFiles(id string) ([]FileEntry, error) {
	live, err := s.Session(id)
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	err = filepath.WalkDir(live.RepoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(live.RepoPath, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == ".codedojo" || name == "node_modules" || name == "vendor") {
			return filepath.SkipDir
		}
		entries = append(entries, FileEntry{Path: rel, Dir: d.IsDir()})
		return nil
	})
	return entries, err
}

func (s *Service) ReadFile(id, path string) (string, error) {
	live, err := s.Session(id)
	if err != nil {
		return "", err
	}
	data, err := live.box.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Service) WriteFile(id, path, content string) error {
	live, err := s.Session(id)
	if err != nil {
		return err
	}
	return live.box.WriteFile(path, []byte(content))
}

func (s *Service) RunTests(ctx context.Context, id string) (TestResult, error) {
	live, err := s.Session(id)
	if err != nil {
		return TestResult{}, err
	}
	result, err := live.box.Exec(ctx, live.testCmd)
	if err != nil {
		return TestResult{}, err
	}
	return TestResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode}, nil
}

func (s *Service) Diff(id string) (string, error) {
	live, err := s.Session(id)
	if err != nil {
		return "", err
	}
	diff, err := live.box.Diff()
	if err != nil {
		return "", err
	}
	if diff == "" {
		return "(no local edits)\n", nil
	}
	return diff, nil
}

func (s *Service) Hint(ctx context.Context, id string, level coach.HintLevel) (HintResult, error) {
	live, err := s.Session(id)
	if err != nil {
		return HintResult{}, err
	}
	if live.HintsUsed >= live.HintBudget {
		return HintResult{}, fmt.Errorf("hint budget exhausted")
	}
	hint, err := live.manager.RequestHint(ctx, id, level, live.Task)
	if err != nil {
		return HintResult{}, err
	}
	live.HintsUsed++
	live.hintCosts = append(live.hintCosts, hint.Cost)
	return HintResult{Hint: hint.Content, Cost: hint.Cost, HintsUsed: live.HintsUsed}, nil
}

func (s *Service) SubmitLearn(ctx context.Context, id string) (Result, error) {
	live, err := s.Session(id)
	if err != nil {
		return Result{}, err
	}
	if live.learnTask == nil {
		return Result{}, fmt.Errorf("session %q is not a learn session", id)
	}
	if err := live.manager.Submit(ctx, id, "newcomer-submission"); err != nil {
		return Result{}, err
	}
	userDiff, err := live.box.Diff()
	if err != nil {
		return Result{}, err
	}
	grade, err := newcomer.Grade(ctx, *live.learnTask, newcomer.Submission{
		SessionID:    id,
		UserDiff:     userDiff,
		NewTestFuncs: newcomer.CountNewTestFuncs(userDiff),
		HintCosts:    live.hintCosts,
		StartedAt:    live.StartedAt,
		SubmittedAt:  time.Now(),
		Streak:       live.Streak,
	}, newcomer.GradeOptions{
		Coach:   s.gradeBack,
		TestCmd: live.testCmd,
		Sandbox: live.box,
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.persistResult(ctx, live, grade.Score, grade.Score > 0); err != nil {
		return Result{}, err
	}
	return Result{
		Score:    grade.Score,
		Feedback: grade.ApproachFeedback,
		Breakdown: map[string]int{
			"correctness": grade.CorrectnessScore,
			"approach":    grade.ApproachScore,
			"tests":       grade.TestQualityScore,
			"hints":       -grade.HintDeduction,
			"streak":      grade.StreakBonus,
		},
		Reveal: map[string]string{"reference_diff": live.learnTask.ReferenceDiff},
	}, nil
}

func (s *Service) SubmitReview(ctx context.Context, id string, sub ReviewSubmission) (Result, error) {
	live, err := s.Session(id)
	if err != nil {
		return Result{}, err
	}
	if live.reviewTask == nil {
		return Result{}, fmt.Errorf("session %q is not a review session", id)
	}
	if strings.TrimSpace(sub.Diagnosis) == "" {
		return Result{}, fmt.Errorf("diagnosis is required")
	}
	if err := live.manager.Submit(ctx, id, fmt.Sprintf("%s:%d-%d %s", sub.FilePath, sub.StartLine, sub.EndLine, sub.Diagnosis)); err != nil {
		return Result{}, err
	}
	grade, err := reviewer.Grade(ctx, reviewer.Submission{
		SessionID:     id,
		FilePath:      sub.FilePath,
		StartLine:     sub.StartLine,
		EndLine:       sub.EndLine,
		OperatorClass: sub.OperatorClass,
		Diagnosis:     sub.Diagnosis,
		HintCosts:     live.hintCosts,
		StartedAt:     live.StartedAt,
		SubmittedAt:   time.Now(),
		Streak:        live.Streak,
	}, live.reviewTask.MutationLog, reviewer.GradeOptions{Coach: s.gradeBack})
	if err != nil {
		return Result{}, err
	}
	if err := s.persistResult(ctx, live, grade.Score, grade.Score > 0); err != nil {
		return Result{}, err
	}
	mutation := live.reviewTask.MutationLog.Mutation
	return Result{
		Score:    grade.Score,
		Feedback: grade.DiagnosisFeedback,
		Breakdown: map[string]int{
			"file":      grade.FileScore,
			"line":      grade.LineScore,
			"operator":  grade.OperatorScore,
			"diagnosis": grade.DiagnosisScore,
			"hints":     -grade.HintDeduction,
			"time":      grade.TimeBonus,
			"streak":    grade.StreakBonus,
		},
		Reveal: map[string]string{
			"file":        mutation.FilePath,
			"line":        fmt.Sprintf("%d", mutation.StartLine),
			"operator":    mutation.Operator,
			"description": mutation.Description,
		},
	}, nil
}

func (s *Service) CloseSession(ctx context.Context, id string) error {
	live, err := s.Session(id)
	if err != nil {
		return err
	}
	if live.Done {
		return nil
	}
	if err := live.manager.Close(ctx, id, live.box); err != nil {
		return err
	}
	live.Done = true
	return nil
}

func (s *Service) persistResult(ctx context.Context, live *LiveSession, score int, success bool) error {
	if err := s.store.UpsertScore(ctx, live.ID, score); err != nil {
		return err
	}
	if err := s.store.UpdateState(ctx, live.ID, session.StateGraded); err != nil {
		return err
	}
	if err := s.store.AppendEvent(ctx, session.Event{SessionID: live.ID, Type: session.EventGrade, Payload: fmt.Sprintf("score=%d", score)}); err != nil {
		return err
	}
	if _, err := s.store.RecordStreakResult(ctx, success); err != nil {
		return err
	}
	if err := live.manager.Close(ctx, live.ID, live.box); err != nil {
		return err
	}
	live.Done = true
	return nil
}

func (s *Service) withDefaults(opts StartOptions) StartOptions {
	if opts.Difficulty == 0 {
		opts.Difficulty = s.cfg.Defaults.Difficulty
	}
	if opts.HintBudget == 0 {
		opts.HintBudget = s.cfg.Defaults.HintBudget
	}
	return opts
}

func loadRepo(ctx context.Context, source string) (repo.Repo, error) {
	if source == "" {
		return repo.Repo{}, fmt.Errorf("repo is required")
	}
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

func buildCoach(cfg config.Config, banned []string) (coach.Coach, error) {
	inner, err := newBackendCoach(cfg)
	if err != nil {
		return nil, err
	}
	return coach.RetryWithStricterPrompt(inner, banned), nil
}

func newBackendCoach(cfg config.Config) (coach.Coach, error) {
	switch cfg.Coach.Backend {
	case "", "mock":
		return mock.Coach{}, nil
	case "anthropic":
		key := cfg.Coach.APIKey
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		if key == "" {
			return nil, fmt.Errorf("anthropic backend selected but no API key (set ANTHROPIC_API_KEY or run codedojo init)")
		}
		return anthropic.New(key), nil
	case "ollama":
		c := ollama.New(os.Getenv("OLLAMA_MODEL"))
		if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
			c.BaseURL = baseURL
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown coach backend %q", cfg.Coach.Backend)
	}
}
