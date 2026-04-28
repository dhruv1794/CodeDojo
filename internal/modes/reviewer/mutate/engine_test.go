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
