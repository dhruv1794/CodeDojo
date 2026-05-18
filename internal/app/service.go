// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/anthropic"
	"github.com/dhruvmishra/codedojo/internal/coach/mock"
	"github.com/dhruvmishra/codedojo/internal/coach/ollama"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/author"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer/history"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
)

const serviceCloseTimeout = 30 * time.Second

type SpecBuilder func(repoPath string) sandbox.Spec

type Service struct {
	cfg       config.Config
	store     *sqlite.Store
	driver    sandbox.Driver
	spec      SpecBuilder
	gradeBack coach.Coach

	mu           sync.Mutex
	sessions     map[string]*LiveSession
	allowSSHAuth bool
}

type LiveSession struct {
	ID          string       `json:"id"`
	Mode        session.Mode `json:"mode"`
	Repo        string       `json:"repo"`
	RepoPath    string       `json:"repo_path"`
	Task        string       `json:"task"`
	TaskFiles   []TaskFile   `json:"task_files,omitempty"`
	Difficulty  int          `json:"difficulty"`
	HintBudget  int          `json:"hint_budget"`
	HintsUsed   int          `json:"hints_used"`
	Streak      int          `json:"streak"`
	Language    string       `json:"language"`
	CommitRange string       `json:"commit_range,omitempty"`
	StartedAt   time.Time    `json:"started_at"`
	Done        bool         `json:"done"`

	manager   session.Manager
	box       sandbox.Session
	testCmd   []string
	hintCosts []int

	reviewTask *reviewer.Task
	learnTask  *newcomer.Task
}

type TaskFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Test   bool   `json:"test,omitempty"`
}

type StartOptions struct {
	Repo           string `json:"repo"`
	Difficulty     int    `json:"difficulty"`
	HintBudget     int    `json:"hint_budget"`
	CommitRange    string `json:"commit_range,omitempty"`
	CandidateCount int    `json:"candidate_count,omitempty"`
	MutationCount  int    `json:"mutation_count,omitempty"`
	CompoundMode   string `json:"compound_mode,omitempty"`
}

type SenseiStartOptions struct {
	PackPath   string `json:"pack_path"`
	TaskID     string `json:"task_id,omitempty"`
	HintBudget int    `json:"hint_budget,omitempty"`
}

type Preflight struct {
	RepoPath       string           `json:"repo_path"`
	RepoName       string           `json:"repo_name"`
	Language       string           `json:"language"`
	TestCommand    []string         `json:"test_command,omitempty"`
	BuildCommand   []string         `json:"build_command,omitempty"`
	Learn          ModeAvailability `json:"learn"`
	Review         ModeAvailability `json:"review"`
	CandidateCount int              `json:"candidate_count,omitempty"`
}

type ModeAvailability struct {
	Available      bool     `json:"available"`
	Reason         string   `json:"reason,omitempty"`
	Actions        []string `json:"actions,omitempty"`
	CandidateCount int      `json:"candidate_count,omitempty"`
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
	ForecastFile  string `json:"forecast_file,omitempty"`
}

type Result struct {
	Score          int               `json:"score"`
	Feedback       string            `json:"feedback"`
	Commentary     string            `json:"commentary,omitempty"`
	ReasoningTrace string            `json:"reasoning_trace,omitempty"`
	Breakdown      map[string]int    `json:"breakdown"`
	CurrentStreak  int               `json:"current_streak"`
	TestExitCode   int               `json:"test_exit_code,omitempty"`
	Reveal         map[string]string `json:"reveal,omitempty"`
	MistakeIndex   []OpStat          `json:"mistake_index,omitempty"`
	Promotion      string            `json:"promotion,omitempty"`
}

type OpStat struct {
	Operator    string  `json:"operator"`
	Count       int     `json:"count"`
	SolveRate   float64 `json:"solve_rate"`
	AvgMinutes  float64 `json:"avg_minutes"`
	Recommended bool    `json:"recommended"`
}

func NewService(ctx context.Context, cfg config.Config, driver sandbox.Driver, spec SpecBuilder) (*Service, error) {
	store, err := sqlite.Open(ctx, cfg.StorePath)
	if err != nil {
		return nil, sessionStoreError(cfg.StorePath, err)
	}
	gradeBack, err := newBackendCoach(cfg)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &Service{
		cfg:          cfg,
		store:        store,
		driver:       driver,
		spec:         spec,
		gradeBack:    gradeBack,
		sessions:     map[string]*LiveSession{},
		allowSSHAuth: true,
	}, nil
}

func (s *Service) SetSSHAuthAllowed(allowed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowSSHAuth = allowed
}

func (s *Service) Preflight(ctx context.Context, source string) (Preflight, error) {
	loaded, err := s.loadRepo(ctx, source)
	if err != nil {
		return Preflight{}, err
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return Preflight{}, err
	}
	preflight := Preflight{
		RepoPath:     displayRepoPath(source, loaded.Path),
		RepoName:     repoName(source, loaded.Path),
		Language:     lang.Name,
		TestCommand:  lang.TestCmd,
		BuildCommand: lang.BuildCmd,
		Learn:        availability(false, "no suitable historical commits found", 0),
		Review:       availability(false, "review mode currently supports Go repositories only", 0),
	}
	if lang.Name == "" {
		preflight.Language = "unknown"
	}
	if preflight.Language == "unknown" {
		preflight.Learn = availabilityError(ProductError{
			Title:   "Repository language was not detected",
			Message: "CodeDojo could not find a supported project marker in this repository.",
			Actions: []string{"Add a .codedojo.yaml file with language and test_cmd.", "Use a Go, Python, JavaScript, TypeScript, or Rust repository."},
		}, 0)
		preflight.Review = preflight.Learn
		return preflight, nil
	}
	if len(lang.TestCmd) == 0 {
		preflight.Learn = availabilityError(noTestCommandError(nil), 0)
	} else {
		candidates, err := history.Scan(ctx, loaded, history.DefaultScanLimit)
		if err != nil {
			preflight.Learn = availability(false, fmt.Sprintf("could not scan commit history: %v", err), 0)
		} else {
			count := len(history.Rank(candidates))
			if count > 0 {
				preflight.Learn = availability(true, "", count)
			} else {
				preflight.Learn = availabilityError(noLearnCommitsError(nil), 0)
			}
		}
	}
	if preflight.Language != "" && preflight.Language != "unknown" {
		scanCfg := scanConfigForLanguage(preflight.Language)
		files, err := mutate.CandidateFiles(ctx, loaded.Path, 50, scanCfg)
		switch {
		case err != nil:
			preflight.Review = availabilityError(noReviewCandidatesError(err), 0)
		case len(files) == 0:
			preflight.Review = availabilityError(noReviewCandidatesError(nil), 0)
		default:
			preflight.Review = availability(true, "", len(files))
		}
	}
	preflight.CandidateCount = max(preflight.Learn.CandidateCount, preflight.Review.CandidateCount)
	return preflight, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, live := range s.sessions {
		if live.box != nil && !live.Done {
			ctx, cancel := context.WithTimeout(context.Background(), serviceCloseTimeout)
			_ = live.manager.Close(ctx, id, live.box)
			cancel()
		}
	}
	return s.store.Close()
}

func availability(ok bool, reason string, count int) ModeAvailability {
	if ok {
		reason = ""
	}
	return ModeAvailability{Available: ok, Reason: reason, CandidateCount: count}
}

func availabilityError(err ProductError, count int) ModeAvailability {
	return ModeAvailability{
		Available:      false,
		Reason:         err.Message,
		Actions:        err.Actions,
		CandidateCount: count,
	}
}

func displayRepoPath(source, loadedPath string) string {
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		if abs, err := filepath.Abs(source); err == nil {
			return abs
		}
		return source
	}
	return loadedPath
}

func repoName(source, loadedPath string) string {
	value := source
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		value = loadedPath
	}
	name := filepath.Base(filepath.Clean(value))
	if name == "." || name == string(filepath.Separator) {
		return value
	}
	return name
}

func (s *Service) StartReview(ctx context.Context, opts StartOptions) (*LiveSession, error) {
	opts = s.withDefaults(opts)
	if opts.CompoundMode != "" && opts.CompoundMode != "same-flow" {
		return nil, fmt.Errorf("unsupported review compound mode %q", opts.CompoundMode)
	}
	loaded, err := s.loadRepo(ctx, opts.Repo)
	if err != nil {
		return nil, err
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return nil, err
	}
	candidateFiles, err := mutate.CandidateFiles(ctx, loaded.Path, 50, scanConfigForLanguage(lang.Name))
	if err != nil {
		return nil, err
	}
	if opts.CandidateCount > 0 && len(candidateFiles) < opts.CandidateCount {
		return nil, fmt.Errorf("review candidate set requested %d files but only found %d", opts.CandidateCount, len(candidateFiles))
	}
	engine, err := reviewerEngineForLanguage(lang.Name)
	if err != nil {
		return nil, err
	}
	task, err := reviewer.TaskGenerator{Engine: engine, MutationCount: opts.MutationCount, CompoundMode: opts.CompoundMode}.GenerateTask(ctx, loaded, opts.Difficulty)
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
	banned := reviewBannedIdentifiers(task)
	hintCoach, err := buildCoach(s.cfg, banned)
	if err != nil {
		return nil, err
	}
	live, err := s.start(ctx, session.ModeReviewer, opts, task.RepoPath, task.Instructions, lang.TestCmd, hintCoach)
	if err != nil {
		return nil, err
	}
	live.Language = lang.Name
	live.reviewTask = &task
	live.TaskFiles = reviewTaskFiles(candidateFiles, task.MutationLog.Mutation.FilePath, opts.CandidateCount)
	for _, log := range taskMutationLogs(task) {
		if err := s.store.SaveMutationLog(ctx, live.ID, log); err != nil {
			_ = s.CloseSession(ctx, live.ID)
			return nil, err
		}
	}
	return live, nil
}

func (s *Service) StartLearn(ctx context.Context, opts StartOptions) (*LiveSession, error) {
	opts = s.withDefaults(opts)
	loaded, err := s.loadRepo(ctx, opts.Repo)
	if err != nil {
		return nil, err
	}
	commitRange, err := history.ParseRange(opts.CommitRange)
	if err != nil {
		return nil, err
	}
	gen := newcomer.TaskGenerator{Summarizer: s.newcomerSummarizer(), Range: commitRange}
	task, err := gen.GenerateTask(ctx, loaded, opts.Difficulty)
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
	live.Language = lang.Name
	live.CommitRange = task.CommitRange
	live.learnTask = &task
	live.TaskFiles = learnTaskFiles(task.SuggestedFiles)
	return live, nil
}

func (s *Service) StartSensei(ctx context.Context, opts SenseiStartOptions) (*LiveSession, error) {
	pack, task, err := loadSenseiTask(opts.PackPath, opts.TaskID)
	if err != nil {
		return nil, err
	}
	loaded, err := s.loadRepo(ctx, pack.Source)
	if err != nil {
		return nil, err
	}
	if err := checkoutLoadedRepo(ctx, loaded.Path, pack.HeadSHA); err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, err
	}
	mutation := task.MutationLog.Mutation
	if strings.TrimSpace(mutation.Mutated) == "" {
		_ = os.RemoveAll(loaded.Path)
		return nil, fmt.Errorf("sensei task %q is missing mutated source snapshot", task.ID)
	}
	target := filepath.Join(loaded.Path, filepath.FromSlash(mutation.FilePath))
	if err := os.WriteFile(target, []byte(mutation.Mutated), 0o600); err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, fmt.Errorf("write sensei mutation: %w", err)
	}
	if err := hideMutationLog(loaded.Path); err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, err
	}
	if err := stageReviewBaseline(ctx, loaded.Path); err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, err
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, err
	}
	if len(lang.TestCmd) == 0 {
		_ = os.RemoveAll(loaded.Path)
		return nil, fmt.Errorf("no test command detected for repo")
	}
	hintCoach, err := buildCoach(s.cfg, []string{mutation.Operator, mutation.Original, mutation.Mutated})
	if err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, err
	}
	difficulty := task.Difficulty
	if difficulty == 0 {
		difficulty = s.cfg.Defaults.Difficulty
	}
	hintBudget := opts.HintBudget
	if hintBudget == 0 {
		hintBudget = s.cfg.Defaults.HintBudget
	}
	instructions := strings.TrimSpace(task.Brief)
	if instructions == "" {
		instructions = task.Instructions
	}
	if instructions == "" {
		instructions = "Find the authored Sensei kata bug, then submit a file, line range, operator class, and diagnosis."
	}
	live, err := s.start(ctx, session.ModeReviewer, StartOptions{
		Repo:       pack.Source,
		Difficulty: difficulty,
		HintBudget: hintBudget,
	}, loaded.Path, instructions, lang.TestCmd, hintCoach)
	if err != nil {
		_ = os.RemoveAll(loaded.Path)
		return nil, err
	}
	live.Language = lang.Name
	reviewTask := reviewer.Task{
		RepoPath:     loaded.Path,
		Difficulty:   difficulty,
		Language:     lang.Name,
		MutationLog:  task.MutationLog,
		MutationLogs: []mutate.MutationLog{task.MutationLog},
		Instructions: instructions,
	}
	live.reviewTask = &reviewTask
	live.TaskFiles = []TaskFile{{
		Path:   mutation.FilePath,
		Reason: "Sensei-selected source file for this authored kata.",
	}}
	if err := s.store.SaveMutationLog(ctx, live.ID, task.MutationLog); err != nil {
		_ = s.CloseSession(ctx, live.ID)
		return nil, err
	}
	return live, nil
}

func loadSenseiTask(packPath, taskID string) (author.Pack, author.PackTask, error) {
	if strings.TrimSpace(packPath) == "" {
		return author.Pack{}, author.PackTask{}, fmt.Errorf("pack path is required")
	}
	pack, err := author.ReadPack(packPath)
	if err != nil {
		return author.Pack{}, author.PackTask{}, err
	}
	if pack.SchemaVersion != author.PackSchemaVersion {
		return author.Pack{}, author.PackTask{}, fmt.Errorf("unsupported pack schema %q", pack.SchemaVersion)
	}
	if len(pack.Tasks) == 0 {
		return author.Pack{}, author.PackTask{}, fmt.Errorf("sensei pack has no tasks")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return pack, pack.Tasks[0], nil
	}
	for _, task := range pack.Tasks {
		if task.ID == taskID {
			return pack, task, nil
		}
	}
	return author.Pack{}, author.PackTask{}, fmt.Errorf("sensei task %q not found", taskID)
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
		ID:          id,
		Mode:        mode,
		Repo:        opts.Repo,
		RepoPath:    repoPath,
		Task:        task,
		Difficulty:  opts.Difficulty,
		HintBudget:  opts.HintBudget,
		Streak:      streak.Current,
		CommitRange: opts.CommitRange,
		StartedAt:   started,
		manager:     manager,
		box:         box,
		testCmd:     testCmd,
	}
	s.mu.Lock()
	s.sessions[id] = live
	s.mu.Unlock()
	return live, nil
}

func learnTaskFiles(files []newcomer.SuggestedFile) []TaskFile {
	out := make([]TaskFile, 0, len(files))
	for _, file := range files {
		out = append(out, TaskFile{Path: file.Path, Reason: file.Reason, Test: file.Test})
	}
	return out
}

func taskMutationLogs(task reviewer.Task) []mutate.MutationLog {
	if len(task.MutationLogs) > 0 {
		return task.MutationLogs
	}
	return []mutate.MutationLog{task.MutationLog}
}

func reviewBannedIdentifiers(task reviewer.Task) []string {
	var banned []string
	for _, log := range taskMutationLogs(task) {
		banned = append(banned,
			log.Mutation.Operator,
			log.Mutation.Original,
			log.Mutation.Mutated,
		)
	}
	return banned
}

func reviewTaskFiles(files []string, selected string, requested int) []TaskFile {
	limit := requested
	if limit <= 0 {
		limit = 8
	}
	if limit > len(files) {
		limit = len(files)
	}
	picked := make([]string, 0, limit)
	seen := map[string]bool{}
	if selected != "" {
		picked = append(picked, selected)
		seen[selected] = true
	}
	for _, path := range files {
		if len(picked) >= limit {
			break
		}
		if seen[path] {
			continue
		}
		picked = append(picked, path)
		seen[path] = true
	}
	slices.Sort(picked)
	out := make([]TaskFile, 0, len(picked))
	for _, path := range picked {
		out = append(out, TaskFile{
			Path:   path,
			Reason: "Candidate source file with nearby tests and mutation sites.",
		})
	}
	return out
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
	s.appendReplayEvent(id, session.EventFile, path)
	return string(data), nil
}

func (s *Service) WriteFile(id, path, content string) error {
	live, err := s.Session(id)
	if err != nil {
		return err
	}
	if err := live.box.WriteFile(path, []byte(content)); err != nil {
		return err
	}
	s.appendReplayEvent(id, session.EventWrite, fmt.Sprintf("%s (%d bytes)", path, len(content)))
	return nil
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
	s.appendReplayEvent(id, session.EventTests, fmt.Sprintf("exit=%d command=%s", result.ExitCode, strings.Join(live.testCmd, " ")))
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
	s.appendReplayEvent(id, session.EventDiff, fmt.Sprintf("%d bytes", len(diff)))
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
	before := s.snapshotCoachUsage()
	hint, err := live.manager.RequestHint(ctx, id, level, live.Task, live.Language, live.Difficulty, live.HintBudget)
	s.recordCoachUsageDelta(ctx, id, "hint", before)
	if err != nil {
		return HintResult{}, err
	}
	if hint.Cost > 0 {
		live.HintsUsed++
	}
	live.hintCosts = append(live.hintCosts, hint.Cost)
	return HintResult{Hint: hint.Content, Cost: hint.Cost, HintsUsed: live.HintsUsed}, nil
}

func (s *Service) appendReplayEvent(id string, typ session.EventType, payload string) {
	if err := s.store.AppendEvent(context.Background(), session.Event{SessionID: id, Type: typ, Payload: payload}); err != nil {
		// Replay events are best-effort and must not interrupt practice flow.
		return
	}
}

type coachUsageSnapshot struct {
	Backend                  string
	Model                    string
	Calls                    int
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	CostUSD                  float64
}

func (s *Service) snapshotCoachUsage() coachUsageSnapshot {
	switch c := s.gradeBack.(type) {
	case *anthropic.Coach:
		u := c.Usage()
		return coachUsageSnapshot{
			Backend:                  "anthropic",
			Model:                    c.Model,
			Calls:                    u.Calls,
			InputTokens:              u.InputTokens,
			OutputTokens:             u.OutputTokens,
			CacheCreationInputTokens: u.CacheCreationInputTokens,
			CacheReadInputTokens:     u.CacheReadInputTokens,
			CostUSD:                  c.Cost(),
		}
	case *ollama.Coach:
		u := c.Usage()
		return coachUsageSnapshot{
			Backend:      "ollama",
			Model:        c.Model,
			Calls:        u.Calls,
			InputTokens:  u.PromptEval,
			OutputTokens: u.ResponseEval,
		}
	default:
		return coachUsageSnapshot{}
	}
}

func (s *Service) recordCoachUsageDelta(ctx context.Context, sessionID, operation string, before coachUsageSnapshot) {
	after := s.snapshotCoachUsage()
	if after.Backend == "" || after.Calls <= before.Calls {
		return
	}
	delta := sqlite.CoachUsage{
		SessionID:                sessionID,
		Backend:                  after.Backend,
		Model:                    after.Model,
		Operation:                operation,
		InputTokens:              after.InputTokens - before.InputTokens,
		OutputTokens:             after.OutputTokens - before.OutputTokens,
		CacheCreationInputTokens: after.CacheCreationInputTokens - before.CacheCreationInputTokens,
		CacheReadInputTokens:     after.CacheReadInputTokens - before.CacheReadInputTokens,
		CostUSD:                  after.CostUSD - before.CostUSD,
	}
	if err := s.store.SaveCoachUsage(ctx, delta); err != nil {
		return
	}
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
	before := s.snapshotCoachUsage()
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
	s.recordCoachUsageDelta(ctx, id, "grade", before)
	if err != nil {
		return Result{}, err
	}
	streak, err := s.persistResult(ctx, live, grade.Score, grade.Score > 0, "", "")
	if err != nil {
		return Result{}, err
	}
	mistakeIdx := s.mistakeIndex(ctx)
	promotion := s.beltPromotion(live.Difficulty, grade.Score, streak)
	return Result{
		Score:         grade.Score,
		Feedback:      grade.ApproachFeedback,
		CurrentStreak: streak,
		TestExitCode:  grade.TestExitCode,
		Breakdown: map[string]int{
			"correctness": grade.CorrectnessScore,
			"approach":    grade.ApproachScore,
			"tests":       grade.TestQualityScore,
			"hints":       -grade.HintDeduction,
			"streak":      grade.StreakBonus,
		},
		Reveal:       map[string]string{"reference_diff": live.learnTask.ReferenceDiff},
		MistakeIndex: mistakeIdx,
		Promotion:    promotion,
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
	before := s.snapshotCoachUsage()
	submission := reviewer.Submission{
		SessionID:     id,
		FilePath:      sub.FilePath,
		StartLine:     sub.StartLine,
		EndLine:       sub.EndLine,
		OperatorClass: sub.OperatorClass,
		Diagnosis:     sub.Diagnosis,
		ForecastFile:  sub.ForecastFile,
		HintCosts:     live.hintCosts,
		StartedAt:     live.StartedAt,
		SubmittedAt:   time.Now(),
		Streak:        live.Streak,
	}
	grade, matchedLog, err := reviewer.GradeAny(ctx, submission, taskMutationLogs(*live.reviewTask), reviewer.GradeOptions{Coach: s.gradeBack, CoachCommentary: true})
	s.recordCoachUsageDelta(ctx, id, "grade", before)
	if err != nil {
		return Result{}, err
	}
	streak, err := s.persistResult(ctx, live, grade.Score, grade.Score > 0, grade.Commentary, grade.ReasoningTrace)
	if err != nil {
		return Result{}, err
	}
	mistakeIdx := s.mistakeIndex(ctx)
	promotion := s.beltPromotion(live.Difficulty, grade.Score, streak)
	mutation := matchedLog.Mutation
	return Result{
		Score:          grade.Score,
		Feedback:       grade.DiagnosisFeedback,
		Commentary:     grade.Commentary,
		ReasoningTrace: grade.ReasoningTrace,
		CurrentStreak:  streak,
		Breakdown: map[string]int{
			"file":      grade.FileScore,
			"line":      grade.LineScore,
			"operator":  grade.OperatorScore,
			"diagnosis": grade.DiagnosisScore,
			"forecast":  grade.ForecastScore,
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
		MistakeIndex: mistakeIdx,
		Promotion:    promotion,
	}, nil
}

type SessionSummary struct {
	ID        string `json:"id"`
	Mode      string `json:"mode"`
	Repo      string `json:"repo"`
	Task      string `json:"task"`
	Score     int    `json:"score"`
	State     string `json:"state"`
	StartedAt string `json:"started_at"`
	Operator  string `json:"operator,omitempty"`
}

func (s *Service) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SessionSummary, 0, len(sessions))
	for _, sess := range sessions {
		if len(out) >= 50 {
			break
		}
		sum := SessionSummary{
			ID:        sess.ID,
			Mode:      string(sess.Mode),
			Repo:      sess.Repo,
			Task:      sess.Task,
			Score:     sess.Score,
			State:     string(sess.State),
			StartedAt: sess.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		out = append(out, sum)
	}
	return out, nil
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

func (s *Service) persistResult(ctx context.Context, live *LiveSession, score int, success bool, commentary, trace string) (int, error) {
	if err := s.store.UpsertScore(ctx, live.ID, score); err != nil {
		return 0, err
	}
	if err := s.store.UpdateState(ctx, live.ID, session.StateGraded); err != nil {
		return 0, err
	}
	if err := s.store.AppendEvent(ctx, session.Event{SessionID: live.ID, Type: session.EventGrade, Payload: fmt.Sprintf("score=%d", score)}); err != nil {
		return 0, err
	}
	if strings.TrimSpace(commentary) != "" {
		if err := s.store.AppendEvent(ctx, session.Event{SessionID: live.ID, Type: session.EventCommentary, Payload: commentary}); err != nil {
			return 0, err
		}
	}
	if strings.TrimSpace(trace) != "" {
		if err := s.store.AppendEvent(ctx, session.Event{SessionID: live.ID, Type: session.EventTrace, Payload: trace}); err != nil {
			return 0, err
		}
	}
	streak, err := s.store.RecordStreakResult(ctx, success)
	if err != nil {
		return 0, err
	}
	if err := live.manager.Close(ctx, live.ID, live.box); err != nil {
		return 0, err
	}
	live.Done = true
	return streak.Current, nil
}

func (s *Service) mistakeIndex(ctx context.Context) []OpStat {
	breakdown, err := s.store.OpBreakdown(ctx)
	if err != nil || len(breakdown) == 0 {
		return nil
	}
	out := make([]OpStat, 0, len(breakdown))
	lowestRate := 1.0
	lowestIdx := -1
	for i, b := range breakdown {
		if b.SolveRate < lowestRate {
			lowestRate = b.SolveRate
			lowestIdx = i
		}
	}
	for i, b := range breakdown {
		out = append(out, OpStat{
			Operator:    b.Operator,
			Count:       b.Count,
			SolveRate:   b.SolveRate,
			Recommended: i == lowestIdx && b.SolveRate < 0.7 && b.Count >= 2,
		})
	}
	return out
}

func (s *Service) beltPromotion(difficulty, score int, streak int) string {
	if score < 60 || difficulty >= 5 {
		return ""
	}
	if score >= 90 {
		return fmt.Sprintf("Belt earned: %s! Perfect score.", beltLabel(difficulty+1))
	}
	if score >= 75 && streak >= 3 {
		return fmt.Sprintf("Belt earned: %s! Streak of %d.", beltLabel(difficulty+1), streak)
	}
	return ""
}

func beltLabel(d int) string {
	switch d {
	case 1:
		return "White Belt"
	case 2:
		return "Yellow Belt"
	case 3:
		return "Green Belt"
	case 4:
		return "Brown Belt"
	case 5:
		return "Black Belt"
	default:
		return fmt.Sprintf("Level %d", d)
	}
}

func (s *Service) withDefaults(opts StartOptions) StartOptions {
	if opts.Difficulty == 0 {
		opts.Difficulty = s.cfg.Defaults.Difficulty
	}
	if opts.HintBudget == 0 {
		opts.HintBudget = s.cfg.Defaults.HintBudget
	}
	if opts.CandidateCount < 0 {
		opts.CandidateCount = 0
	}
	if opts.MutationCount < 0 {
		opts.MutationCount = 0
	}
	return opts
}

func (s *Service) loadRepo(ctx context.Context, source string) (repo.Repo, error) {
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
	hints := repo.EnvAuthHints()
	s.mu.Lock()
	allowSSHAuth := s.allowSSHAuth
	s.mu.Unlock()
	if !allowSSHAuth {
		hints.SSHKeys = nil
	}
	loaded, err := repo.CloneWithAuthHints(ctx, source, tmp, hints)
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
	logDir := filepath.Dir(logPath)
	entries, err := os.ReadDir(logDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect mutation log directory: %w", err)
	}
	if len(entries) == 0 {
		if err := os.Remove(logDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty mutation log directory: %w", err)
		}
	}
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

func checkoutLoadedRepo(ctx context.Context, repoPath, commit string) error {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "checkout", "--force", commit)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("checkout sensei source commit: %w: %s", err, out)
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

func (s *Service) newcomerSummarizer() newcomer.Summarizer {
	switch s.cfg.Coach.Backend {
	case "", "mock":
		return nil
	default:
		inner, err := newBackendCoach(s.cfg)
		if err != nil {
			return nil
		}
		return newcomer.AISummarizer{Coach: inner}
	}
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
		c := anthropic.New(key)
		if cfg.Coach.Model != "" {
			c.Model = cfg.Coach.Model
		}
		return c, nil
	case "ollama":
		model := cfg.Coach.Model
		if model == "" {
			model = os.Getenv("OLLAMA_MODEL")
		}
		c := ollama.New(model)
		if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
			c.BaseURL = baseURL
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown coach backend %q", cfg.Coach.Backend)
	}
}

// reviewerEngineForLanguage returns the appropriate MutationEngine for the
// detected repo language. Returns an error for unsupported languages.
func reviewerEngineForLanguage(lang string) (reviewer.MutationEngine, error) {
	switch lang {
	case "go", "":
		return mutate.Engine{Mutators: op.All()}, nil
	case "python":
		return reviewer.NewPythonEngine(), nil
	case "javascript":
		e := mutate.NewJSASTEngine()
		return e, nil
	case "typescript":
		e := mutate.NewTSASTEngine()
		return e, nil
	case "rust":
		e := mutate.NewRustASTEngine()
		return e, nil
	default:
		return nil, fmt.Errorf("review mode does not yet support language %q", lang)
	}
}

// scanConfigForLanguage returns the file scan configuration for the given language.
func scanConfigForLanguage(lang string) mutate.ScanConfig {
	switch lang {
	case "python":
		return mutate.DefaultPythonScanConfig()
	case "javascript":
		return mutate.DefaultJSScanConfig()
	case "typescript":
		return mutate.DefaultTSScanConfig()
	case "rust":
		return mutate.DefaultRustScanConfig()
	default:
		return mutate.DefaultGoScanConfig()
	}
}
