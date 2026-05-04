package reviewer

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/textop"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

// TextEngine is a language-neutral MutationEngine for non-Go languages.
// It performs text/regex-based mutations and uses configurable build/test
// commands for post-mutation gate checks.
type TextEngine struct {
	Lang     string
	Mutators []textop.TextMutator
	LogLimit int
	Now      func() time.Time
	ScanCfg  mutate.ScanConfig
	GateCfg  mutate.GateConfig
}

// NewPythonEngine returns a TextEngine pre-configured for Python repositories.
func NewPythonEngine() TextEngine {
	return TextEngine{
		Lang:     "python",
		Mutators: textop.AllPython(),
		ScanCfg:  mutate.DefaultPythonScanConfig(),
		GateCfg:  mutate.DefaultPythonGateConfig(),
	}
}

// NewJSEngine returns a TextEngine pre-configured for JavaScript repositories.
func NewJSEngine() TextEngine {
	return TextEngine{
		Lang:     "javascript",
		Mutators: textop.AllJS(),
		ScanCfg:  mutate.DefaultJSScanConfig(),
		GateCfg:  mutate.DefaultJSGateConfig(),
	}
}

// NewTSEngine returns a TextEngine pre-configured for TypeScript repositories.
func NewTSEngine() TextEngine {
	return TextEngine{
		Lang:     "typescript",
		Mutators: textop.AllTS(),
		ScanCfg:  mutate.DefaultTSScanConfig(),
		GateCfg:  mutate.DefaultJSGateConfig(),
	}
}

// NewRustEngine returns a TextEngine pre-configured for Rust repositories.
func NewRustEngine() TextEngine {
	return TextEngine{
		Lang:     "rust",
		Mutators: textop.AllRust(),
		ScanCfg:  mutate.DefaultRustScanConfig(),
		GateCfg:  mutate.DefaultRustGateConfig(),
	}
}

// Language satisfies MutationEngine.
func (e TextEngine) Language() string { return e.Lang }

// SelectAndApply satisfies MutationEngine.
func (e TextEngine) SelectAndApply(ctx context.Context, r repo.Repo, difficulty int) (mutate.MutationLog, error) {
	if len(e.Mutators) == 0 {
		return mutate.MutationLog{}, fmt.Errorf("no mutators configured for language %q", e.Lang)
	}
	files, err := mutate.CandidateFiles(ctx, r.Path, e.LogLimit, e.ScanCfg)
	if err != nil {
		return mutate.MutationLog{}, err
	}
	if len(files) == 0 {
		return mutate.MutationLog{}, fmt.Errorf("no %s source files with tests found in repo", e.Lang)
	}
	type candidate struct {
		file    string
		mutator textop.TextMutator
		site    textop.Site
	}
	var best candidate
	bestDistance := math.MaxInt
	for _, rel := range files {
		full := filepath.Join(r.Path, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		content := string(data)
		for _, m := range e.Mutators {
			for _, site := range m.Candidates(content) {
				dist := abs(m.Difficulty() - difficulty)
				if dist < bestDistance {
					best = candidate{file: rel, mutator: m, site: site}
					bestDistance = dist
				}
			}
		}
	}
	if best.mutator == nil {
		return mutate.MutationLog{}, fmt.Errorf("no mutation candidates found in %s files", e.Lang)
	}

	fullPath := filepath.Join(r.Path, best.file)
	before, err := os.ReadFile(fullPath)
	if err != nil {
		return mutate.MutationLog{}, fmt.Errorf("read %q: %w", best.file, err)
	}
	mutated, err := best.mutator.Apply(string(before), best.site)
	if err != nil {
		return mutate.MutationLog{}, fmt.Errorf("apply mutation to %q: %w", best.file, err)
	}
	if err := os.WriteFile(fullPath, []byte(mutated), 0o644); err != nil {
		return mutate.MutationLog{}, fmt.Errorf("write %q: %w", best.file, err)
	}
	if _, err := mutate.RunGates(ctx, r.Path, e.GateCfg); err != nil {
		os.WriteFile(fullPath, before, 0o644)
		return mutate.MutationLog{}, fmt.Errorf("gates rejected mutation: %w", err)
	}

	now := e.now()
	mutation := mutate.Mutation{
		Operator:    best.mutator.Name(),
		Difficulty:  best.mutator.Difficulty(),
		FilePath:    best.file,
		StartLine:   best.site.StartLine,
		EndLine:     best.site.EndLine,
		Description: best.site.Description,
		Original:    string(before),
		Mutated:     mutated,
		AppliedAt:   now,
	}
	log := mutate.MutationLog{
		ID:         fmt.Sprintf("%d", now.UnixNano()),
		RepoPath:   r.Path,
		Difficulty: difficulty,
		Mutation:   mutation,
		CreatedAt:  now,
	}
	if err := mutate.WriteMutationLog(r.Path, log); err != nil {
		return mutate.MutationLog{}, err
	}
	return log, nil
}

func (e TextEngine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
