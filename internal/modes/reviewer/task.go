// SPDX-License-Identifier: MIT

package reviewer

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate/op"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

type Task struct {
	RepoPath     string
	Difficulty   int
	Language     string
	CompoundMode string
	MutationLog  mutate.MutationLog
	MutationLogs []mutate.MutationLog
	Instructions string
}

// TaskGenerator drives mutation task creation for a specific language.
// Set Engine to a MutationEngine implementation (mutate.Engine for Go,
// TextEngine for Python/JS/TS/Rust). When nil, defaults to a Go engine.
type TaskGenerator struct {
	Engine        MutationEngine
	MutationCount int
	CompoundMode  string
}

func GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	return TaskGenerator{}.GenerateTask(ctx, r, difficulty)
}

func (g TaskGenerator) GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	engine := g.Engine
	if engine == nil {
		engine = mutate.Engine{Mutators: op.All()}
	}
	count := g.MutationCount
	if count <= 0 {
		count = 1
	}
	logs, err := g.generateMutationLogs(ctx, r, engine, difficulty, count)
	if err != nil {
		return Task{}, err
	}
	instructions := "Find the injected reviewer bug, then submit a file, line range, operator class, and diagnosis."
	if count > 1 {
		instructions = fmt.Sprintf("Find any one of the %d injected reviewer bugs, then submit a file, line range, operator class, and diagnosis.", count)
	}
	if g.CompoundMode == "same-flow" {
		instructions = fmt.Sprintf("Find one of the %d interacting bugs in the same code path, then submit a file, line range, operator class, and diagnosis.", len(logs))
	}
	return Task{
		RepoPath:     r.Path,
		Difficulty:   difficulty,
		Language:     engine.Language(),
		CompoundMode: g.CompoundMode,
		MutationLog:  logs[0],
		MutationLogs: logs,
		Instructions: instructions,
	}, nil
}

func (g TaskGenerator) generateMutationLogs(ctx context.Context, r repo.Repo, engine MutationEngine, difficulty, count int) ([]mutate.MutationLog, error) {
	if g.CompoundMode == "same-flow" {
		goEngine, ok := engine.(mutate.Engine)
		if !ok {
			return nil, fmt.Errorf("same-flow compound mode currently supports Go repositories only")
		}
		return goEngine.SelectAndApplySameFlow(ctx, r, difficulty, count)
	}
	logs := make([]mutate.MutationLog, 0, count)
	usedFiles := map[string]struct{}{}
	for len(logs) < count {
		nextEngine := engineExcludingFiles(engine, usedFiles)
		log, err := nextEngine.SelectAndApply(ctx, r, difficulty)
		if err != nil {
			if len(logs) > 0 {
				return nil, fmt.Errorf("only generated %d mutation(s), requested %d: %w", len(logs), count, err)
			}
			return nil, err
		}
		logs = append(logs, log)
		usedFiles[filepath.ToSlash(log.Mutation.FilePath)] = struct{}{}
	}
	return logs, nil
}

func engineExcludingFiles(engine MutationEngine, excluded map[string]struct{}) MutationEngine {
	if len(excluded) == 0 {
		return engine
	}
	switch e := engine.(type) {
	case mutate.Engine:
		e.ScanCfg = excludeScanFiles(e.ScanCfg, e.Language(), excluded)
		return e
	case TextEngine:
		e.ScanCfg = excludeScanFiles(e.ScanCfg, e.Language(), excluded)
		return e
	case mutate.ASTEngine:
		e.ScanCfg = excludeScanFiles(e.ScanCfg, e.Language(), excluded)
		return e
	default:
		return engine
	}
}

func excludeScanFiles(cfg mutate.ScanConfig, lang string, excluded map[string]struct{}) mutate.ScanConfig {
	cfg = scanConfigWithLanguageDefaults(cfg, lang)
	baseEligible := cfg.IsEligible
	cfg.IsEligible = func(repoPath, relPath string) (bool, error) {
		if _, ok := excluded[filepath.ToSlash(filepath.Clean(relPath))]; ok {
			return false, nil
		}
		return baseEligible(repoPath, relPath)
	}
	return cfg
}

func scanConfigWithLanguageDefaults(cfg mutate.ScanConfig, lang string) mutate.ScanConfig {
	if cfg.IsEligible != nil {
		return cfg
	}
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
