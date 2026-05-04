package mutate

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/astop"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

// ASTEngine is a Tree-sitter-backed MutationEngine for non-Go languages.
// It performs AST-localized mutations using tree-sitter for precise site
// identification, avoiding false matches in comments/strings that plague
// text/regex-based mutators.
type ASTEngine struct {
	Lang     string
	Mutators []astop.ASTMutator
	LogLimit int
	Now      func() time.Time
	ScanCfg  ScanConfig
	GateCfg  GateConfig
}

// NewRustASTEngine returns an ASTEngine pre-configured for Rust.
func NewRustASTEngine() ASTEngine {
	return ASTEngine{
		Lang:     "rust",
		Mutators: astop.AllRust(),
		ScanCfg:  DefaultRustScanConfig(),
		GateCfg:  DefaultRustGateConfig(),
	}
}

// NewJSASTEngine returns an ASTEngine pre-configured for JavaScript.
func NewJSASTEngine() ASTEngine {
	return ASTEngine{
		Lang:     "javascript",
		Mutators: astop.AllJS(),
		ScanCfg:  DefaultJSScanConfig(),
		GateCfg:  DefaultJSGateConfig(),
	}
}

// NewTSASTEngine returns an ASTEngine pre-configured for TypeScript.
func NewTSASTEngine() ASTEngine {
	return ASTEngine{
		Lang:     "typescript",
		Mutators: astop.AllTS(),
		ScanCfg:  DefaultTSScanConfig(),
		GateCfg:  DefaultJSGateConfig(),
	}
}

// Language satisfies MutationEngine.
func (e ASTEngine) Language() string { return e.Lang }

// SelectAndApply satisfies MutationEngine.
func (e ASTEngine) SelectAndApply(ctx context.Context, r repo.Repo, difficulty int) (MutationLog, error) {
	if len(e.Mutators) == 0 {
		return MutationLog{}, fmt.Errorf("no mutators configured for language %q", e.Lang)
	}
	files, err := CandidateFiles(ctx, r.Path, e.LogLimit, e.ScanCfg)
	if err != nil {
		return MutationLog{}, err
	}
	if len(files) == 0 {
		return MutationLog{}, fmt.Errorf("no %s source files with tests found in repo", e.Lang)
	}

	type candidate struct {
		file    string
		mutator astop.ASTMutator
		site    astop.ASTSite
	}
	var best candidate
	bestDistance := math.MaxInt
	for _, rel := range files {
		full := filepath.Join(r.Path, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		for _, m := range e.Mutators {
			for _, site := range m.Candidates(data) {
				dist := abs(m.Difficulty() - difficulty)
				if dist < bestDistance {
					best = candidate{file: rel, mutator: m, site: site}
					bestDistance = dist
				}
			}
		}
	}
	if best.mutator == nil {
		return MutationLog{}, fmt.Errorf("no mutation candidates found in %s files", e.Lang)
	}

	fullPath := filepath.Join(r.Path, best.file)
	before, err := os.ReadFile(fullPath)
	if err != nil {
		return MutationLog{}, fmt.Errorf("read %q: %w", best.file, err)
	}
	mutated, err := best.mutator.Apply(before, best.site)
	if err != nil {
		return MutationLog{}, fmt.Errorf("apply mutation to %q: %w", best.file, err)
	}
	if err := os.WriteFile(fullPath, mutated, 0o644); err != nil {
		return MutationLog{}, fmt.Errorf("write %q: %w", best.file, err)
	}
	if _, err := RunGates(ctx, r.Path, e.GateCfg); err != nil {
		os.WriteFile(fullPath, before, 0o644)
		return MutationLog{}, fmt.Errorf("gates rejected mutation: %w", err)
	}

	now := e.now()
	mutation := Mutation{
		Operator:    best.mutator.Name(),
		Difficulty:  best.mutator.Difficulty(),
		FilePath:    best.file,
		StartLine:   best.site.StartLine,
		EndLine:     best.site.EndLine,
		Description: best.site.Description,
		Original:    string(before),
		Mutated:     string(mutated),
		AppliedAt:   now,
	}
	log := MutationLog{
		ID:         fmt.Sprintf("%d", now.UnixNano()),
		RepoPath:   r.Path,
		Difficulty: difficulty,
		Mutation:   mutation,
		CreatedAt:  now,
	}
	if err := WriteMutationLog(r.Path, log); err != nil {
		return MutationLog{}, err
	}
	return log, nil
}

func (e ASTEngine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}
