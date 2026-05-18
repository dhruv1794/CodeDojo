// SPDX-License-Identifier: MIT

package mutate

import (
	"context"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/repo"
)

func TestEngineSelectAndApply(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/sample\n\ngo 1.23\n")
	writeFile(t, dir, "calc/calc.go", "package calc\n\nfunc Clamp(value, min int) int {\n\tif value < min {\n\t\treturn min\n\t}\n\treturn value\n}\n")
	writeFile(t, dir, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestClamp(t *testing.T) {}\n")
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	engine := Engine{
		Mutators: []Mutator{comparisonMutator{}},
		Now:      func() time.Time { return now },
	}
	log, err := engine.SelectAndApply(context.Background(), repo.Repo{Path: dir}, 1)
	if err != nil {
		t.Fatalf("SelectAndApply returned error: %v", err)
	}
	if log.Mutation.Operator != "comparison" {
		t.Fatalf("operator = %q, want comparison", log.Mutation.Operator)
	}
	if log.Mutation.FilePath != "calc/calc.go" {
		t.Fatalf("file = %q, want calc/calc.go", log.Mutation.FilePath)
	}
	if log.Mutation.StartLine != 4 {
		t.Fatalf("start line = %d, want 4", log.Mutation.StartLine)
	}
	data, err := os.ReadFile(filepath.Join(dir, "calc/calc.go"))
	if err != nil {
		t.Fatalf("read mutated file: %v", err)
	}
	if !strings.Contains(string(data), "value <= min") {
		t.Fatalf("mutated source did not contain boundary flip:\n%s", data)
	}
	if !strings.Contains(log.Mutation.Original, "value < min") || !strings.Contains(log.Mutation.Mutated, "value <= min") {
		t.Fatalf("mutation log did not capture original and mutated source")
	}
}

func TestEngineSelectAndApplySameFlow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/sample\n\ngo 1.23\n")
	writeFile(t, dir, "pager/pager.go", "package pager\n\nfunc Page(values []int, offset, limit int) []int {\n\tif limit < 0 {\n\t\treturn nil\n\t}\n\treturn values[offset : offset+limit]\n}\n")
	writeFile(t, dir, "pager/pager_test.go", "package pager\n\nimport \"testing\"\n\nfunc TestPage(t *testing.T) {}\n")
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	engine := Engine{
		Mutators: []Mutator{comparisonMutator{}, sliceHighMutator{}},
		Now:      func() time.Time { return now },
	}
	logs, err := engine.SelectAndApplySameFlow(context.Background(), repo.Repo{Path: dir}, 2, 2)
	if err != nil {
		t.Fatalf("SelectAndApplySameFlow returned error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs len = %d, want 2", len(logs))
	}
	if logs[0].Mutation.FilePath != "pager/pager.go" || logs[1].Mutation.FilePath != "pager/pager.go" {
		t.Fatalf("logs = %+v, want same file", logs)
	}
	data, err := os.ReadFile(filepath.Join(dir, "pager/pager.go"))
	if err != nil {
		t.Fatalf("read mutated file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "limit <= 0") || !strings.Contains(got, "(offset+limit)-1") {
		t.Fatalf("compound mutation did not apply same-flow changes:\n%s", got)
	}
}

type comparisonMutator struct{}

func (comparisonMutator) Name() string    { return "comparison" }
func (comparisonMutator) Difficulty() int { return 1 }
func (comparisonMutator) Candidates(f *ast.File) []Site {
	var sites []Site
	ast.Inspect(f, func(node ast.Node) bool {
		expr, ok := node.(*ast.BinaryExpr)
		if ok && expr.Op.String() == "<" {
			sites = append(sites, Site{Description: "flip less-than", Node: expr})
		}
		return true
	})
	return sites
}

func (comparisonMutator) Apply(_ *ast.File, site Site) (Mutation, error) {
	expr := site.Node.(*ast.BinaryExpr)
	expr.Op = token.LEQ
	return Mutation{Description: site.Description}, nil
}

type sliceHighMutator struct{}

func (sliceHighMutator) Name() string    { return "slice-high" }
func (sliceHighMutator) Difficulty() int { return 2 }
func (sliceHighMutator) Candidates(f *ast.File) []Site {
	var sites []Site
	ast.Inspect(f, func(node ast.Node) bool {
		expr, ok := node.(*ast.SliceExpr)
		if ok && expr.Low != nil && expr.High != nil && !expr.Slice3 {
			sites = append(sites, Site{Description: "decrement slice high", Node: expr})
		}
		return true
	})
	return sites
}

func (sliceHighMutator) Apply(_ *ast.File, site Site) (Mutation, error) {
	expr := site.Node.(*ast.SliceExpr)
	expr.High = &ast.BinaryExpr{
		X:  &ast.ParenExpr{X: expr.High},
		Op: token.SUB,
		Y:  &ast.BasicLit{Kind: token.INT, Value: "1"},
	}
	return Mutation{Description: site.Description}, nil
}
