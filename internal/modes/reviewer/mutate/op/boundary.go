package op

import (
	"fmt"
	"go/ast"
	"go/token"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type Boundary struct{}

func (Boundary) Name() string    { return "boundary" }
func (Boundary) Difficulty() int { return 1 }

func (Boundary) Candidates(f *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(f, func(node ast.Node) bool {
		expr, ok := node.(*ast.BinaryExpr)
		if !ok || !isBoundaryOperator(expr.Op) {
			return true
		}
		sites = append(sites, mutate.Site{
			Description: fmt.Sprintf("flip boundary operator %s", expr.Op),
			Metadata: map[string]string{
				"operator": expr.Op.String(),
			},
			Node: expr,
		})
		return true
	})
	return sites
}

func (b Boundary) Apply(_ *ast.File, site mutate.Site) (mutate.Mutation, error) {
	expr, ok := site.Node.(*ast.BinaryExpr)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("boundary site node is %T, want *ast.BinaryExpr", site.Node)
	}
	before := expr.Op.String()
	next, ok := boundaryFlip(expr.Op)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("unsupported boundary operator %s", expr.Op)
	}
	expr.Op = next
	return mutate.Mutation{
		Operator:    b.Name(),
		Difficulty:  b.Difficulty(),
		Description: fmt.Sprintf("changed boundary operator %s to %s", before, next),
		AppliedAt:   time.Now().UTC(),
	}, nil
}

func isBoundaryOperator(op token.Token) bool {
	_, ok := boundaryFlip(op)
	return ok
}

func boundaryFlip(op token.Token) (token.Token, bool) {
	switch op {
	case token.LSS:
		return token.LEQ, true
	case token.LEQ:
		return token.LSS, true
	case token.GTR:
		return token.GEQ, true
	case token.GEQ:
		return token.GTR, true
	default:
		return token.ILLEGAL, false
	}
}
