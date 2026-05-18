// SPDX-License-Identifier: MIT

package op

import (
	"fmt"
	"go/ast"
	"go/token"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type PaginationWindow struct{}

func (PaginationWindow) Name() string    { return "pagination-window" }
func (PaginationWindow) Difficulty() int { return 3 }

func (PaginationWindow) Candidates(f *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(f, func(node ast.Node) bool {
		expr, ok := node.(*ast.SliceExpr)
		if !ok || expr.Slice3 || expr.Low == nil || expr.High == nil {
			return true
		}
		if isLiteralZero(expr.Low) || isLenCall(expr.High) {
			return true
		}
		sites = append(sites, mutate.Site{
			Description: "shrink pagination window by one element",
			Metadata: map[string]string{
				"operator": "window-off-by-one",
			},
			Node: expr,
		})
		return true
	})
	return sites
}

func (p PaginationWindow) Apply(_ *ast.File, site mutate.Site) (mutate.Mutation, error) {
	expr, ok := site.Node.(*ast.SliceExpr)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("pagination site node is %T, want *ast.SliceExpr", site.Node)
	}
	if expr.Slice3 || expr.Low == nil || expr.High == nil {
		return mutate.Mutation{}, fmt.Errorf("unsupported pagination window site")
	}
	expr.High = &ast.BinaryExpr{
		X:  &ast.ParenExpr{X: expr.High},
		Op: token.SUB,
		Y:  &ast.BasicLit{Kind: token.INT, Value: "1"},
	}
	return mutate.Mutation{
		Operator:    p.Name(),
		Difficulty:  p.Difficulty(),
		Description: "decremented the upper bound of a two-sided slice window",
		AppliedAt:   time.Now().UTC(),
	}, nil
}

func isLiteralZero(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

func isLenCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "len"
}
