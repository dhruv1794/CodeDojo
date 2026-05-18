// SPDX-License-Identifier: MIT

package onpr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

const ArtifactSchemaVersion = "codedojo.on-pr.challenge.v1"

type Options struct {
	Repo       string
	Base       string
	Head       string
	Output     string
	Difficulty int
	Now        func() time.Time
}

type Challenge struct {
	SchemaVersion string             `json:"schema_version"`
	Source        string             `json:"source"`
	Base          string             `json:"base"`
	Head          string             `json:"head"`
	Language      string             `json:"language"`
	Difficulty    int                `json:"difficulty"`
	ChangedFiles  []string           `json:"changed_files"`
	SelectedFile  string             `json:"selected_file"`
	Profile       mutate.Profile     `json:"profile"`
	MutationLog   mutate.MutationLog `json:"mutation_log"`
	CreatedAt     time.Time          `json:"created_at"`
}

func Generate(ctx context.Context, opts Options) (Challenge, error) {
	if strings.TrimSpace(opts.Repo) == "" {
		return Challenge{}, fmt.Errorf("repo is required")
	}
	if strings.TrimSpace(opts.Base) == "" {
		return Challenge{}, fmt.Errorf("base is required")
	}
	if strings.TrimSpace(opts.Head) == "" {
		return Challenge{}, fmt.Errorf("head is required")
	}
	if opts.Difficulty < 0 || opts.Difficulty > 5 {
		return Challenge{}, fmt.Errorf("difficulty must be from 1 to 5, or 0 to sample all difficulties")
	}
	difficulty := opts.Difficulty
	if difficulty == 0 {
		difficulty = 3
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}

	changed, err := changedFiles(ctx, opts.Repo, opts.Base, opts.Head)
	if err != nil {
		return Challenge{}, err
	}
	if len(changed) == 0 {
		return Challenge{}, fmt.Errorf("no changed files found in range %s...%s", opts.Base, opts.Head)
	}
	changedSet := make(map[string]struct{}, len(changed))
	for _, rel := range changed {
		changedSet[rel] = struct{}{}
	}

	loaded, err := repo.OpenLocal(opts.Repo)
	if err != nil {
		return Challenge{}, err
	}
	defer os.RemoveAll(loaded.Path)

	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return Challenge{}, err
	}
	engine, err := engineForLanguage(lang.Name, changedSet)
	if err != nil {
		return Challenge{}, err
	}
	task, err := reviewer.TaskGenerator{Engine: engine}.GenerateTask(ctx, loaded, difficulty)
	if err != nil {
		return Challenge{}, err
	}
	task.MutationLog.RepoPath = opts.Repo
	task.MutationLog.HeadSHA = headSHA(loaded)

	return Challenge{
		SchemaVersion: ArtifactSchemaVersion,
		Source:        opts.Repo,
		Base:          opts.Base,
		Head:          opts.Head,
		Language:      task.Language,
		Difficulty:    difficulty,
		ChangedFiles:  changed,
		SelectedFile:  task.MutationLog.Mutation.FilePath,
		Profile:       task.MutationLog.Profile,
		MutationLog:   task.MutationLog,
		CreatedAt:     now,
	}, nil
}

func WriteMarkdown(path string, challenge Challenge) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	data := []byte(RenderMarkdown(challenge))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write on-pr artifact: %w", err)
	}
	return nil
}

func RenderMarkdown(c Challenge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# CodeDojo Spotter Challenge\n\n")
	fmt.Fprintf(&b, "CodeDojo selected one reviewer kata from this PR range.\n\n")
	fmt.Fprintf(&b, "- Range: `%s...%s`\n", c.Base, c.Head)
	fmt.Fprintf(&b, "- Language: `%s`\n", c.Language)
	fmt.Fprintf(&b, "- Changed files scanned: %d\n", len(c.ChangedFiles))
	fmt.Fprintf(&b, "- Challenge file: `%s`\n", c.SelectedFile)
	if c.Profile.Summary != "" {
		fmt.Fprintf(&b, "- Difficulty profile: %s", c.Profile.Summary)
		if c.Profile.Locality.Score > 0 {
			fmt.Fprintf(&b, " (locality %d, subtlety %d, knowledge %d)", c.Profile.Locality.Score, c.Profile.Subtlety.Score, c.Profile.Knowledge.Score)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Your Task")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Review the PR diff and find the injected bug in the challenge file. Submit the smallest file and line range you can defend, the likely bug class, and a short diagnosis of the behavior change.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Changed Files")
	fmt.Fprintln(&b)
	for _, rel := range c.ChangedFiles {
		fmt.Fprintf(&b, "- `%s`\n", rel)
	}
	return b.String()
}

func changedFiles(ctx context.Context, repoPath, base, head string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=ACMRT", base+"..."+head, "--")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff changed files: %w", err)
	}
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		rel := filepath.ToSlash(filepath.Clean(strings.TrimSpace(line)))
		if rel == "." || rel == "" || seen[rel] || !filepath.IsLocal(rel) {
			continue
		}
		seen[rel] = true
		files = append(files, rel)
	}
	slices.Sort(files)
	return files, nil
}

func engineForLanguage(lang string, changed map[string]struct{}) (reviewer.MutationEngine, error) {
	switch lang {
	case "go", "":
		return mutate.Engine{Mutators: op.All(), ScanCfg: restrictScan(mutate.DefaultGoScanConfig(), changed)}, nil
	case "python":
		engine := reviewer.NewPythonEngine()
		engine.ScanCfg = restrictScan(engine.ScanCfg, changed)
		return engine, nil
	case "javascript":
		engine := mutate.NewJSASTEngine()
		engine.ScanCfg = restrictScan(engine.ScanCfg, changed)
		return engine, nil
	case "typescript":
		engine := mutate.NewTSASTEngine()
		engine.ScanCfg = restrictScan(engine.ScanCfg, changed)
		return engine, nil
	case "rust":
		engine := mutate.NewRustASTEngine()
		engine.ScanCfg = restrictScan(engine.ScanCfg, changed)
		return engine, nil
	default:
		return nil, fmt.Errorf("on-pr does not yet support language %q", lang)
	}
}

func restrictScan(cfg mutate.ScanConfig, changed map[string]struct{}) mutate.ScanConfig {
	baseEligible := cfg.IsEligible
	cfg.IsEligible = func(repoPath, relPath string) (bool, error) {
		rel := filepath.ToSlash(filepath.Clean(relPath))
		if _, ok := changed[rel]; !ok {
			return false, nil
		}
		return baseEligible(repoPath, relPath)
	}
	return cfg
}

func headSHA(r repo.Repo) string {
	if r.Git == nil {
		return ""
	}
	ref, err := r.Git.Head()
	if err != nil {
		return ""
	}
	return ref.Hash().String()
}
