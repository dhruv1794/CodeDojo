package op

import (
	"fmt"
	"go/ast"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type SliceBounds struct{}

func (SliceBounds) Name() string    { return "slicebounds" }
func (SliceBounds) Difficulty() int { return 2 }

func (SliceBounds) Candidates(f *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(f, func(node ast.Node) bool {
		expr, ok := node.(*ast.SliceExpr)
		if !ok || expr.Slice3 || !isOneSidedSlice(expr) {
			return true
		}
		sites = append(sites, mutate.Site{
			Description: "swap one-sided slice bound",
			Metadata: map[string]string{
				"operator": "slice-bound-swap",
			},
			Node: expr,
		})
		return true
	})
	return sites
}

func (s SliceBounds) Apply(_ *ast.File, site mutate.Site) (mutate.Mutation, error) {
	expr, ok := site.Node.(*ast.SliceExpr)
	if !ok {
		return mutate.Mutation{}, fmt.Errorf("slicebounds site node is %T, want *ast.SliceExpr", site.Node)
	}
	if !isOneSidedSlice(expr) || expr.Slice3 {
		return mutate.Mutation{}, fmt.Errorf("unsupported slice bounds site")
	}
	expr.Low, expr.High = expr.High, expr.Low
	return mutate.Mutation{
		Operator:    s.Name(),
		Difficulty:  s.Difficulty(),
		Description: "swapped one-sided slice bound",
		AppliedAt:   time.Now().UTC(),
	}, nil
}

func isOneSidedSlice(expr *ast.SliceExpr) bool {
	return (expr.Low == nil && expr.High != nil) || (expr.Low != nil && expr.High == nil)
}
