package op

import (
	"fmt"
	"go/ast"
	"go/token"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type ErrorDrop struct{}

func (ErrorDrop) Name() string    { return "errordrop" }
func (ErrorDrop) Difficulty() int { return 3 }

func (ErrorDrop) Candidates(f *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(f, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for _, stmt := range block.List {
			ifStmt, ok := stmt.(*ast.IfStmt)
			if !ok || !isErrorReturnGuard(ifStmt) {
				continue
			}
			sites = append(sites, mutate.Site{
				Description: "drop error return guard",
				Metadata: map[string]string{
					"operator": "drop-error-check",
				},
				Node: ifStmt,
			})
		}
		return true
	})
	return sites
}

func (e ErrorDrop) Apply(f *ast.File, site mutate.Site) (mutate.Mutation, error) {
	target, ok := site.Node.(*ast.IfStmt)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("errordrop site node is %T, want *ast.IfStmt", site.Node)
	}
	if !isErrorReturnGuard(target) {
		return mutate.Mutation{}, fmt.Errorf("errordrop site is not an error return guard")
	}
	if !removeStatement(f, target) {
		return mutate.Mutation{}, fmt.Errorf("errordrop site was not found in file")
	}
	return mutate.Mutation{
		Operator:    e.Name(),
		Difficulty:  e.Difficulty(),
		Description: "removed error return guard",
		AppliedAt:   time.Now().UTC(),
	}, nil
}

func isErrorReturnGuard(stmt *ast.IfStmt) bool {
	if stmt.Init != nil || stmt.Else != nil {
		return false
	}
	checked, ok := checkedErrorIdent(stmt.Cond)
	if !ok {
		return false
	}
	if len(stmt.Body.List) != 1 {
		return false
	}
	ret, ok := stmt.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	for _, result := range ret.Results {
		ident, ok := result.(*ast.Ident)
		if ok && ident.Name == checked {
			return true
		}
	}
	return false
}

func checkedErrorIdent(cond ast.Expr) (string, bool) {
	expr, ok := cond.(*ast.BinaryExpr)
	if !ok || expr.Op != token.NEQ {
		return "", false
	}
	if isNilIdent(expr.X) {
		if ident, ok := expr.Y.(*ast.Ident); ok {
			return ident.Name, true
		}
	}
	if isNilIdent(expr.Y) {
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name, true
		}
	}
	return "", false
}

func removeStatement(f *ast.File, target ast.Stmt) bool {
	removed := false
	ast.Inspect(f, func(node ast.Node) bool {
		if removed {
			return false
		}
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, stmt := range block.List {
			if stmt != target {
				continue
			}
			block.List = append(block.List[:i], block.List[i+1:]...)
			removed = true
			return false
		}
		return true
	})
	return removed
}
