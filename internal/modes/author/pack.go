// SPDX-License-Identifier: MIT

package author

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const PackSchemaVersion = "codedojo.author.pack.v1"

type PackOptions struct {
	Repo         string
	Title        string
	Author       string
	Brief        string
	Commit       string
	Count        int
	MaxAttempts  int
	Difficulty   int
	AllowPartial bool
	Now          func() time.Time
}

type Pack struct {
	SchemaVersion string     `json:"schema_version"`
	Title         string     `json:"title"`
	Source        string     `json:"source"`
	Author        string     `json:"author,omitempty"`
	Language      string     `json:"language"`
	HeadSHA       string     `json:"head_sha,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	Tasks         []PackTask `json:"tasks"`
}

type PackTask struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	Difficulty   int                `json:"difficulty"`
	Operator     string             `json:"operator"`
	FilePath     string             `json:"file_path"`
	StartLine    int                `json:"start_line"`
	EndLine      int                `json:"end_line"`
	Brief        string             `json:"brief,omitempty"`
	Description  string             `json:"description"`
	Instructions string             `json:"instructions"`
	MutationLog  mutate.MutationLog `json:"mutation_log"`
}

func GeneratePack(ctx context.Context, opts PackOptions) (Pack, error) {
	return generatePack(ctx, opts, nil)
}

func GenerateVettedPack(ctx context.Context, opts PackOptions, vet VetOptions) (Pack, VetReport, error) {
	var finalReport VetReport
	pack, err := generatePack(ctx, opts, func(candidate Pack) (bool, error) {
		report, err := VetGeneratedPack(ctx, candidate, vet)
		finalReport = report
		if err != nil {
			return false, err
		}
		return report.Failed == 0 && report.Passed == report.Total && report.Total > 0, nil
	})
	if err != nil {
		return Pack{}, finalReport, err
	}
	report, err := VetGeneratedPack(ctx, pack, vet)
	return pack, report, err
}

func generatePack(ctx context.Context, opts PackOptions, accept func(Pack) (bool, error)) (Pack, error) {
	if strings.TrimSpace(opts.Repo) == "" {
		return Pack{}, fmt.Errorf("repo is required")
	}
	if opts.Count <= 0 {
		opts.Count = 1
	}
	if opts.Difficulty < 0 || opts.Difficulty > 5 {
		return Pack{}, fmt.Errorf("difficulty must be from 1 to 5, or 0 to sample all difficulties")
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = defaultPackTitle(opts.Repo)
	}

	probe, err := repo.OpenLocal(opts.Repo)
	if err != nil {
		return Pack{}, err
	}
	defer os.RemoveAll(probe.Path)
	if err := checkoutCommit(probe, opts.Commit); err != nil {
		return Pack{}, err
	}
	lang, err := repo.DetectLanguage(probe.Path)
	if err != nil {
		return Pack{}, err
	}
	engine, err := engineForLanguage(lang.Name)
	if err != nil {
		return Pack{}, err
	}
	headSHA := headSHA(probe)

	pack := Pack{
		SchemaVersion: PackSchemaVersion,
		Title:         title,
		Source:        opts.Repo,
		Author:        strings.TrimSpace(opts.Author),
		Language:      lang.Name,
		HeadSHA:       headSHA,
		CreatedAt:     now,
		Tasks:         make([]PackTask, 0, opts.Count),
	}
	seen := map[string]bool{}
	difficulties := difficultyPlan(opts.Difficulty)
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = opts.Count * len(difficulties) * 2
	}
	for attempts := 0; len(pack.Tasks) < opts.Count && attempts < maxAttempts; attempts++ {
		difficulty := difficulties[attempts%len(difficulties)]
		loaded, err := repo.OpenLocal(opts.Repo)
		if err != nil {
			return Pack{}, err
		}
		if err := checkoutCommit(loaded, opts.Commit); err != nil {
			_ = os.RemoveAll(loaded.Path)
			return Pack{}, err
		}
		task, taskErr := reviewer.TaskGenerator{Engine: engine}.GenerateTask(ctx, loaded, difficulty)
		_ = os.RemoveAll(loaded.Path)
		if taskErr != nil {
			return Pack{}, taskErr
		}
		mutation := task.MutationLog.Mutation
		key := fmt.Sprintf("%s:%s:%d:%d", mutation.Operator, mutation.FilePath, mutation.StartLine, mutation.EndLine)
		if seen[key] {
			continue
		}
		seen[key] = true
		task.MutationLog.RepoPath = opts.Repo
		task.MutationLog.HeadSHA = headSHA
		candidateTask := PackTask{
			ID:           fmt.Sprintf("kata-%03d", len(pack.Tasks)+1),
			Title:        taskTitle(mutation),
			Difficulty:   difficulty,
			Operator:     mutation.Operator,
			FilePath:     mutation.FilePath,
			StartLine:    mutation.StartLine,
			EndLine:      mutation.EndLine,
			Brief:        strings.TrimSpace(opts.Brief),
			Description:  mutation.Description,
			Instructions: task.Instructions,
			MutationLog:  task.MutationLog,
		}
		if accept != nil {
			candidatePack := pack
			candidatePack.Tasks = append(append([]PackTask{}, pack.Tasks...), candidateTask)
			ok, err := accept(candidatePack)
			if err != nil {
				return Pack{}, err
			}
			if !ok {
				continue
			}
		}
		pack.Tasks = append(pack.Tasks, candidateTask)
	}
	if len(pack.Tasks) == 0 {
		if accept != nil {
			return Pack{}, fmt.Errorf("no vetted authorable mutation tasks found")
		}
		return Pack{}, fmt.Errorf("no authorable mutation tasks found")
	}
	if len(pack.Tasks) < opts.Count && !opts.AllowPartial {
		if accept != nil {
			return Pack{}, fmt.Errorf("only generated %d vetted tasks, requested %d", len(pack.Tasks), opts.Count)
		}
		return Pack{}, fmt.Errorf("only generated %d unique tasks, requested %d (use AllowPartial to keep a partial pack)", len(pack.Tasks), opts.Count)
	}
	return pack, nil
}

func ReadPack(path string) (Pack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, fmt.Errorf("read author pack: %w", err)
	}
	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return Pack{}, fmt.Errorf("parse author pack: %w", err)
	}
	return pack, nil
}

func checkoutCommit(r repo.Repo, commit string) error {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return nil
	}
	worktree, err := r.Git.Worktree()
	if err != nil {
		return fmt.Errorf("open worktree: %w", err)
	}
	hash, err := r.Git.ResolveRevision(plumbing.Revision(commit))
	if err != nil {
		return fmt.Errorf("resolve commit %q: %w", commit, err)
	}
	if err := worktree.Checkout(&gogit.CheckoutOptions{Hash: *hash, Force: true}); err != nil {
		return fmt.Errorf("checkout commit %q: %w", commit, err)
	}
	return nil
}

func engineForLanguage(lang string) (reviewer.MutationEngine, error) {
	switch lang {
	case "go", "":
		return mutate.Engine{Mutators: op.All()}, nil
	case "python":
		return reviewer.NewPythonEngine(), nil
	case "javascript":
		return mutate.NewJSASTEngine(), nil
	case "typescript":
		return mutate.NewTSASTEngine(), nil
	case "rust":
		return mutate.NewRustASTEngine(), nil
	default:
		return nil, fmt.Errorf("author mode does not yet support language %q", lang)
	}
}

func difficultyPlan(difficulty int) []int {
	if difficulty > 0 {
		return []int{difficulty}
	}
	return []int{1, 2, 3, 4, 5}
}

func headSHA(r repo.Repo) string {
	if r.Git == nil {
		return ""
	}
	ref, err := r.Git.Head()
	if err != nil || ref.Hash() == plumbing.ZeroHash {
		return ""
	}
	return ref.Hash().String()
}

func defaultPackTitle(source string) string {
	base := filepath.Base(filepath.Clean(source))
	if base == "." || base == string(filepath.Separator) {
		return "CodeDojo mutation pack"
	}
	return base + " mutation pack"
}

func taskTitle(m mutate.Mutation) string {
	if strings.TrimSpace(m.Description) != "" {
		return m.Description
	}
	if m.Operator == "" {
		return "Reviewer mutation"
	}
	return m.Operator + " mutation"
}
