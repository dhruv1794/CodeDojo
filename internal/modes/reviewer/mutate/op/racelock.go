// SPDX-License-Identifier: MIT

package op

import (
	"fmt"
	"go/ast"
	"strconv"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type RaceLockDrop struct{}

func (RaceLockDrop) Name() string    { return "race-lock-drop" }
func (RaceLockDrop) Difficulty() int { return 4 }

func (RaceLockDrop) Candidates(f *ast.File) []mutate.Site {
	var sites []mutate.Site
	ast.Inspect(f, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		for lockIndex, stmt := range fn.Body.List {
			receiver, lockKind, ok := lockCall(stmt)
			if !ok {
				continue
			}
			unlockIndex := matchingUnlockIndex(fn.Body.List, receiver, lockKind, lockIndex+1)
			if unlockIndex < 0 {
				continue
			}
			sites = append(sites, mutate.Site{
				Description: fmt.Sprintf("remove %s/%s critical section around %s", lockKind, unlockName(lockKind), receiver),
				Metadata: map[string]string{
					"operator":     "race-lock-drop",
					"receiver":     receiver,
					"lock":         lockKind,
					"lock_index":   strconv.Itoa(lockIndex),
					"unlock_index": strconv.Itoa(unlockIndex),
				},
				Node: fn,
			})
		}
		return true
	})
	return sites
}

func (r RaceLockDrop) Apply(_ *ast.File, site mutate.Site) (mutate.Mutation, error) {
	fn, ok := site.Node.(*ast.FuncDecl)
	if !ok || fn.Body == nil {
		return mutate.Mutation{}, fmt.Errorf("race-lock-drop site node is %T, want *ast.FuncDecl", site.Node)
	}
	lockIndex, err := strconv.Atoi(site.Metadata["lock_index"])
	if err != nil {
		return mutate.Mutation{}, fmt.Errorf("invalid lock index: %w", err)
	}
	unlockIndex, err := strconv.Atoi(site.Metadata["unlock_index"])
	if err != nil {
		return mutate.Mutation{}, fmt.Errorf("invalid unlock index: %w", err)
	}
	if lockIndex < 0 || unlockIndex < 0 || lockIndex >= len(fn.Body.List) || unlockIndex >= len(fn.Body.List) || lockIndex == unlockIndex {
		return mutate.Mutation{}, fmt.Errorf("lock/unlock indices out of range")
	}
	if lockIndex > unlockIndex {
		lockIndex, unlockIndex = unlockIndex, lockIndex
	}
	fn.Body.List = append(fn.Body.List[:unlockIndex], fn.Body.List[unlockIndex+1:]...)
	fn.Body.List = append(fn.Body.List[:lockIndex], fn.Body.List[lockIndex+1:]...)
	return mutate.Mutation{
		Operator:    r.Name(),
		Difficulty:  r.Difficulty(),
		Description: "removed mutex lock/unlock critical section",
		AppliedAt:   time.Now().UTC(),
	}, nil
}

func matchingUnlockIndex(stmts []ast.Stmt, receiver, lockKind string, start int) int {
	wantUnlock := unlockName(lockKind)
	for i := start; i < len(stmts); i++ {
		gotReceiver, gotUnlock, ok := unlockCall(stmts[i])
		if ok && gotReceiver == receiver && gotUnlock == wantUnlock {
			return i
		}
	}
	return -1
}

func lockCall(stmt ast.Stmt) (receiver, lockKind string, ok bool) {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return "", "", false
	}
	return mutexCall(expr.X, "Lock", "RLock")
}

func unlockCall(stmt ast.Stmt) (receiver, unlockKind string, ok bool) {
	deferStmt, ok := stmt.(*ast.DeferStmt)
	if !ok {
		return "", "", false
	}
	return mutexCall(deferStmt.Call, "Unlock", "RUnlock")
}

func mutexCall(expr ast.Expr, names ...string) (receiver, method string, ok bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) != 0 {
		return "", "", false
	}
	for _, name := range names {
		if sel.Sel.Name == name {
			return exprKey(sel.X), name, exprKey(sel.X) != ""
		}
	}
	return "", "", false
}

func unlockName(lockKind string) string {
	if lockKind == "RLock" {
		return "RUnlock"
	}
	return "Unlock"
}

func exprKey(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		base := exprKey(e.X)
		if base == "" {
			return ""
		}
		return base + "." + e.Sel.Name
	default:
		return ""
	}
}
