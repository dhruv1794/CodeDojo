package astop

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// --- Rust: comparison boundary flip (using Tree-sitter AST) ---

// RustBoundary flips comparison operators: < ↔ <=, > ↔ >=.
// Uses tree-sitter to find binary_expression nodes, avoiding false matches
// in strings, comments, and generic type parameters.
type RustBoundary struct{}

func (RustBoundary) Name() string     { return "rs-boundary" }
func (RustBoundary) Language() string { return "rust" }
func (RustBoundary) Difficulty() int  { return 1 }

func (RustBoundary) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getRustLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "binary_expression" {
			return true
		}
		if node.ChildCount() < 3 {
			return true
		}
		opNode := node.Child(1)
		opText := nodeText(source, opNode)
		if opText != "<" && opText != ">" {
			return true
		}
		sites = append(sites, ASTSite{
			StartByte:   int(opNode.StartByte()),
			EndByte:     int(opNode.EndByte()),
			StartLine:   pointToLine(opNode.StartPoint()),
			EndLine:     pointToLine(opNode.EndPoint()),
			Description: fmt.Sprintf("flip comparison operator %s on line %d", opText, pointToLine(opNode.StartPoint())),
			Metadata:    map[string]string{"op": opText},
		})
		return true
	})
	return sites
}

func (RustBoundary) Apply(source []byte, site ASTSite) ([]byte, error) {
	op := site.Metadata["op"]
	flipped, ok := boundaryFlipMap[op]
	if !ok {
		return nil, fmt.Errorf("unknown operator %q", op)
	}
	result := make([]byte, len(source)+len(flipped)-len(op))
	n := copy(result, source[:site.StartByte])
	n += copy(result[n:], []byte(flipped))
	copy(result[n:], source[site.EndByte:])
	return result, nil
}

var boundaryFlipMap = map[string]string{
	"<":  "<=",
	">":  ">=",
	"<=": "<",
	">=": ">",
}

// --- Rust: Option/Result branch inversion ---

// RustOptionInvert flips Option/Result check methods using tree-sitter AST.
// is_some() ↔ is_none(), is_ok() ↔ is_err()
type RustOptionInvert struct{}

func (RustOptionInvert) Name() string     { return "rs-option-invert" }
func (RustOptionInvert) Language() string { return "rust" }
func (RustOptionInvert) Difficulty() int  { return 2 }

var rustOptionMethods = map[string]struct{}{
	"is_some": {}, "is_none": {}, "is_ok": {}, "is_err": {},
}

var rustOptionFlip = map[string]string{
	"is_some": "is_none",
	"is_none": "is_some",
	"is_ok":   "is_err",
	"is_err":  "is_ok",
}

func (RustOptionInvert) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getRustLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "call_expression" {
			return true
		}
		if node.ChildCount() < 2 {
			return true
		}
		funcNode := node.Child(0)
		var nameNode *sitter.Node
		if funcNode.Type() == "field_expression" && funcNode.ChildCount() >= 3 {
			nameNode = funcNode.Child(2)
		} else if funcNode.Type() == "identifier" {
			nameNode = funcNode
		}
		if nameNode == nil {
			return true
		}
		name := nodeText(source, nameNode)
		if _, ok := rustOptionMethods[name]; !ok {
			return true
		}
		flipped := rustOptionFlip[name]
		sites = append(sites, ASTSite{
			StartByte:   int(nameNode.StartByte()),
			EndByte:     int(nameNode.EndByte()),
			StartLine:   pointToLine(nameNode.StartPoint()),
			EndLine:     pointToLine(nameNode.EndPoint()),
			Description: fmt.Sprintf("invert %s() to %s() on line %d", name, flipped, pointToLine(nameNode.StartPoint())),
			Metadata:    map[string]string{"op": name},
		})
		return true
	})
	return sites
}

func (RustOptionInvert) Apply(source []byte, site ASTSite) ([]byte, error) {
	op := site.Metadata["op"]
	flipped, ok := rustOptionFlip[op]
	if !ok {
		return nil, fmt.Errorf("unknown option method %q", op)
	}
	result := make([]byte, len(source)+len(flipped)-len(op))
	n := copy(result, source[:site.StartByte])
	n += copy(result[n:], []byte(flipped))
	copy(result[n:], source[site.EndByte:])
	return result, nil
}

// --- Rust: range bound mutation ---

// RustRangeBound flips exclusive to inclusive range: 0..n ↔ 0..=n.
type RustRangeBound struct{}

func (RustRangeBound) Name() string     { return "rs-range-bound" }
func (RustRangeBound) Language() string { return "rust" }
func (RustRangeBound) Difficulty() int  { return 2 }

func (RustRangeBound) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getRustLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "range_expression" {
			return true
		}
		if node.ChildCount() < 3 {
			return true
		}
		opNode := node.Child(1)
		opText := nodeText(source, opNode)
		if opText != ".." && opText != "..=" {
			return true
		}
		flipped := "..="
		kind := "exclusive"
		if opText == "..=" {
			flipped = ".."
			kind = "inclusive"
		}
		sites = append(sites, ASTSite{
			StartByte:   int(opNode.StartByte()),
			EndByte:     int(opNode.EndByte()),
			StartLine:   pointToLine(opNode.StartPoint()),
			EndLine:     pointToLine(opNode.EndPoint()),
			Description: fmt.Sprintf("flip %s range to %s on line %d", kind, flipped, pointToLine(opNode.StartPoint())),
			Metadata:    map[string]string{"kind": kind, "from": opText, "to": flipped},
		})
		return true
	})
	return sites
}

func (RustRangeBound) Apply(source []byte, site ASTSite) ([]byte, error) {
	to := site.Metadata["to"]
	from := site.Metadata["from"]
	result := make([]byte, len(source)+len(to)-len(from))
	n := copy(result, source[:site.StartByte])
	n += copy(result[n:], []byte(to))
	copy(result[n:], source[site.EndByte:])
	return result, nil
}

// --- Rust: error propagation weakening ---

// RustErrPropagation replaces ? error propagation with .unwrap().
type RustErrPropagation struct{}

func (RustErrPropagation) Name() string     { return "rs-err-propagation" }
func (RustErrPropagation) Language() string { return "rust" }
func (RustErrPropagation) Difficulty() int  { return 3 }

func (RustErrPropagation) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getRustLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "try_expression" {
			return true
		}
		if node.ChildCount() < 2 {
			return true
		}
		qNode := node.Child(int(node.ChildCount()) - 1)
		if nodeText(source, qNode) != "?" {
			return true
		}
		inner := node.Child(0)
		innerText := nodeText(source, inner)
		sites = append(sites, ASTSite{
			StartByte:   int(qNode.StartByte()),
			EndByte:     int(qNode.EndByte()),
			StartLine:   pointToLine(qNode.StartPoint()),
			EndLine:     pointToLine(qNode.EndPoint()),
			Description: fmt.Sprintf("weaken error propagation: replace ? with .unwrap() for %s on line %d", innerText, pointToLine(qNode.StartPoint())),
			Metadata:    map[string]string{"expr": innerText},
		})
		return true
	})
	return sites
}

func (RustErrPropagation) Apply(source []byte, site ASTSite) ([]byte, error) {
	replacement := []byte(".unwrap()")
	result := make([]byte, len(source)-1+len(replacement))
	n := copy(result, source[:site.StartByte])
	n += copy(result[n:], replacement)
	copy(result[n:], source[site.EndByte:])
	return result, nil
}

// AllRust returns all Rust AST-based mutators.
func AllRust() []ASTMutator {
	return []ASTMutator{
		RustBoundary{},
		RustOptionInvert{},
		RustRangeBound{},
		RustErrPropagation{},
	}
}
