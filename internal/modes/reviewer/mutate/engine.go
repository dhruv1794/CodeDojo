// SPDX-License-Identifier: MIT

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
	"sort"
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
	if err := os.WriteFile(full, formatted.Bytes(), 0o600); err != nil {
		return MutationLog{}, fmt.Errorf("write mutated file: %w", err)
	}
	if _, err := RunGates(ctx, r.Path, e.GateCfg); err != nil {
		_ = os.WriteFile(full, before, 0o600)
		return MutationLog{}, fmt.Errorf("gates rejected mutation: %w", err)
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
	mutation.Profile = ProfileDifficulty(mutation)
	log := MutationLog{
		ID:         fmt.Sprintf("%d", now.UnixNano()),
		RepoPath:   r.Path,
		Difficulty: difficulty,
		Profile:    mutation.Profile,
		Mutation:   mutation,
		CreatedAt:  now,
	}
	if err := WriteMutationLog(r.Path, log); err != nil {
		return MutationLog{}, err
	}
	return log, nil
}

func (e Engine) SelectAndApplySameFlow(ctx context.Context, r repo.Repo, difficulty, count int) ([]MutationLog, error) {
	if len(e.Mutators) == 0 {
		return nil, fmt.Errorf("at least one mutator is required")
	}
	if count <= 1 {
		count = 2
	}
	files, err := CandidateFiles(ctx, r.Path, e.LogLimit, e.ScanCfg)
	if err != nil {
		return nil, err
	}
	candidates, fileSet, astFile, err := e.selectSameFlowCandidates(files, r.Path, difficulty, count)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(r.Path, candidates[0].FilePath)
	before, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("read selected file: %w", err)
	}
	mutations := make([]Mutation, 0, len(candidates))
	for _, candidate := range candidates {
		mutation, err := candidate.Mutator.Apply(astFile, candidate.Site)
		if err != nil {
			return nil, err
		}
		mutation.Operator = candidate.Mutator.Name()
		mutation.Difficulty = candidate.Mutator.Difficulty()
		mutation.FilePath = candidate.FilePath
		mutation.StartLine = candidate.Site.StartLine
		mutation.StartColumn = candidate.Site.StartColumn
		mutation.EndLine = candidate.Site.EndLine
		mutation.EndColumn = candidate.Site.EndColumn
		mutations = append(mutations, mutation)
	}
	var formatted bytes.Buffer
	if err := printer.Fprint(&formatted, fileSet, astFile); err != nil {
		return nil, fmt.Errorf("format compound mutated source: %w", err)
	}
	if err := os.WriteFile(full, formatted.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write compound mutated file: %w", err)
	}
	if _, err := RunGates(ctx, r.Path, e.GateCfg); err != nil {
		_ = os.WriteFile(full, before, 0o600)
		return nil, fmt.Errorf("gates rejected compound mutation: %w", err)
	}
	now := e.now()
	logs := make([]MutationLog, 0, len(mutations))
	for i, mutation := range mutations {
		mutation.Original = string(before)
		mutation.Mutated = formatted.String()
		mutation.AppliedAt = now
		mutation.Profile = ProfileDifficulty(mutation)
		logs = append(logs, MutationLog{
			ID:         fmt.Sprintf("%d", now.UnixNano()+int64(i)),
			RepoPath:   r.Path,
			Difficulty: difficulty,
			Profile:    mutation.Profile,
			Mutation:   mutation,
			CreatedAt:  now,
		})
	}
	if err := WriteMutationLog(r.Path, logs[0]); err != nil {
		return nil, err
	}
	return logs, nil
}

func (e Engine) selectSameFlowCandidates(files []string, repoPath string, difficulty, count int) ([]Candidate, *token.FileSet, *ast.File, error) {
	var best []Candidate
	var bestFileSet *token.FileSet
	var bestAST *ast.File
	bestScore := math.MaxInt
	for _, rel := range files {
		fileSet := token.NewFileSet()
		astFile, err := parser.ParseFile(fileSet, filepath.Join(repoPath, rel), nil, parser.ParseComments)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse %q: %w", rel, err)
		}
		byFunc := candidatesByFunction(astFile, fileSet, rel, e.Mutators, difficulty)
		for _, candidates := range byFunc {
			if len(candidates) < count {
				continue
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				left := abs(candidates[i].Mutator.Difficulty() - difficulty)
				right := abs(candidates[j].Mutator.Difficulty() - difficulty)
				if left == right {
					return candidates[i].Site.StartLine < candidates[j].Site.StartLine
				}
				return left < right
			})
			picked := candidates[:count]
			score := 0
			for _, candidate := range picked {
				score += abs(candidate.Mutator.Difficulty() - difficulty)
			}
			if score < bestScore {
				best = picked
				bestFileSet = fileSet
				bestAST = astFile
				bestScore = score
			}
		}
	}
	if len(best) < count {
		return nil, nil, nil, fmt.Errorf("no same-flow compound mutation candidates found")
	}
	return best, bestFileSet, bestAST, nil
}

func candidatesByFunction(astFile *ast.File, fileSet *token.FileSet, rel string, mutators []Mutator, difficulty int) map[*ast.FuncDecl][]Candidate {
	_ = difficulty
	out := map[*ast.FuncDecl][]Candidate{}
	funcs := functionDecls(astFile)
	for _, mutator := range mutators {
		if !sameFlowCompatible(mutator) {
			continue
		}
		for _, site := range mutator.Candidates(astFile) {
			fn := enclosingFunc(funcs, site.Node)
			if fn == nil {
				continue
			}
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
			out[fn] = append(out[fn], Candidate{FilePath: rel, Mutator: mutator, Site: site})
		}
	}
	return out
}

func sameFlowCompatible(mutator Mutator) bool {
	switch mutator.Name() {
	case "errordrop", "race-lock-drop":
		return false
	default:
		return true
	}
}

func functionDecls(astFile *ast.File) []*ast.FuncDecl {
	var funcs []*ast.FuncDecl
	for _, decl := range astFile.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			funcs = append(funcs, fn)
		}
	}
	return funcs
}

func enclosingFunc(funcs []*ast.FuncDecl, node ast.Node) *ast.FuncDecl {
	if node == nil {
		return nil
	}
	for _, fn := range funcs {
		if fn.Pos() <= node.Pos() && node.End() <= fn.End() {
			return fn
		}
	}
	return nil
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
