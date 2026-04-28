package op

import (
	"fmt"
	"go/ast"
	"go/token"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type Conditional struct{}

func (Conditional) Name() string    { return "conditional" }
func (Conditional) Difficulty() int { return 2 }

func (Conditional) Candidates(f *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(f, func(node ast.Node) bool {
		stmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		expr, ok := stmt.Cond.(*ast.BinaryExpr)
		if !ok || !isNilComparison(expr) || !isEqualityOperator(expr.Op) {
			return true
		}
		sites = append(sites, mutate.Site{
			Description: fmt.Sprintf("negate nil conditional %s", expr.Op),
			Metadata: map[string]string{
				"operator": expr.Op.String(),
			},
			Node: stmt,
		})
		return true
	})
	return sites
}

func (c Conditional) Apply(_ *ast.File, site mutate.Site) (mutate.Mutation, error) {
	stmt, ok := site.Node.(*ast.IfStmt)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("conditional site node is %T, want *ast.IfStmt", site.Node)
	}
	expr, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("conditional site does not contain a binary condition")
	}
	before := expr.Op.String()
	next, ok := equalityFlip(expr.Op)
	if !ok || !isNilComparison(expr) {
		return mutate.Mutation{}, fmt.Errorf("unsupported conditional %s", before)
	}
	expr.Op = next
	return mutate.Mutation{
		Operator:    c.Name(),
		Difficulty:  c.Difficulty(),
		Description: fmt.Sprintf("changed nil conditional %s to %s", before, next),
		AppliedAt:   time.Now().UTC(),
	}, nil
}

func isNilComparison(expr *ast.BinaryExpr) bool {
	return isNilIdent(expr.X) || isNilIdent(expr.Y)
}

func isNilIdent(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

func isEqualityOperator(op token.Token) bool {
	_, ok := equalityFlip(op)
	return ok
}

func equalityFlip(op token.Token) (token.Token, bool) {
	switch op {
	case token.EQL:
		return token.NEQ, true
	case token.NEQ:
		return token.EQL, true
	default:
		return token.ILLEGAL, false
	}
}
