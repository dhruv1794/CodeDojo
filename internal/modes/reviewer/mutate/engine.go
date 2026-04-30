package mutate

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/repo"
)

// Engine selects a mutation site in a Go repository and applies it.
type Engine struct {
	Mutators []Mutator
	LogLimit int
	Now      func() time.Time
	// ScanCfg controls which files are considered. Defaults to Go scan config.
	ScanCfg ScanConfig
	// GateCfg controls post-mutation gates. Defaults to Go gate config.
	GateCfg GateConfig
}

// Language returns "go". Engine satisfies the reviewer.MutationEngine interface.
func (Engine) Language() string { return "go" }

func (e Engine) SelectAndApply(ctx context.Context, r repo.Repo, difficulty int) (MutationLog, error) {
	if len(e.Mutators) == 0 {
		return MutationLog{}, fmt.Errorf("at least one mutator is required")
	}
	files, err := CandidateFiles(ctx, r.Path, e.LogLimit, e.ScanCfg)
	if err != nil {
		return MutationLog{}, err
	}
	candidate, fileSet, astFile, err := e.selectCandidate(files, r.Path, difficulty)
	if err != nil {
		return MutationLog{}, err
	}
	full := filepath.Join(r.Path, candidate.FilePath)
	before, err := os.ReadFile(full)
	if err != nil {
		return MutationLog{}, fmt.Errorf("read selected file: %w", err)
	}
	mutation, err := candidate.Mutator.Apply(astFile, candidate.Site)
	if err != nil {
		return MutationLog{}, err
	}
	var formatted bytes.Buffer
	if err := printer.Fprint(&formatted, fileSet, astFile); err != nil {
		return MutationLog{}, fmt.Errorf("format mutated source: %w", err)
	}
	if err := os.WriteFile(full, formatted.Bytes(), 0o644); err != nil {
		return MutationLog{}, fmt.Errorf("write mutated file: %w", err)
	}
	now := e.now()
	mutation.Operator = candidate.Mutator.Name()
	mutation.Difficulty = candidate.Mutator.Difficulty()
	mutation.FilePath = candidate.FilePath
	mutation.StartLine = candidate.Site.StartLine
	mutation.StartColumn = candidate.Site.StartColumn
	mutation.EndLine = candidate.Site.EndLine
	mutation.EndColumn = candidate.Site.EndColumn
	mutation.Original = string(before)
	mutation.Mutated = formatted.String()
	mutation.AppliedAt = now
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

func (e Engine) selectCandidate(files []string, repoPath string, difficulty int) (Candidate, *token.FileSet, *ast.File, error) {
	var best Candidate
	var bestFileSet *token.FileSet
	var bestAST *ast.File
	bestDistance := math.MaxInt
	for _, rel := range files {
		fileSet := token.NewFileSet()
		astFile, err := parser.ParseFile(fileSet, filepath.Join(repoPath, rel), nil, parser.ParseComments)
		if err != nil {
			return Candidate{}, nil, nil, fmt.Errorf("parse %q: %w", rel, err)
		}
		for _, mutator := range e.Mutators {
			for _, site := range mutator.Candidates(astFile) {
				site.FilePath = rel
				start := fileSet.Position(site.Node.Pos())
				end := fileSet.Position(site.Node.End())
				if site.StartLine == 0 {
					site.StartLine = start.Line
					site.StartColumn = start.Column
				}
				if site.EndLine == 0 {
					site.EndLine = end.Line
					site.EndColumn = end.Column
				}
				distance := abs(mutator.Difficulty() - difficulty)
				if distance < bestDistance {
					best = Candidate{FilePath: rel, Mutator: mutator, Site: site}
					bestFileSet = fileSet
					bestAST = astFile
					bestDistance = distance
				}
			}
		}
	}
	if best.Mutator == nil {
		return Candidate{}, nil, nil, fmt.Errorf("no mutation candidates found")
	}
	return best, bestFileSet, bestAST, nil
}

func (e Engine) now() time.Time {
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
