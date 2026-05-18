// SPDX-License-Identifier: MIT

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

func (RustBoundary) Name() string     { return rustBoundarySpec.name() }
func (RustBoundary) Language() string { return "rust" }
func (RustBoundary) Difficulty() int  { return BoundaryOperator.Difficulty }

var rustBoundarySpec = tokenReplacementSpec{
	Definition: BoundaryOperator,
	Prefix:     "rs",
	Language:   "rust",
	NodeType:   "binary_expression",
	TokenIndex: 1,
	Parser:     getRustLanguage,
	Describe: func(from, _ string, line int) string {
		return fmt.Sprintf("flip comparison operator %s on line %d", from, line)
	},
}

func (RustBoundary) Candidates(source []byte) []ASTSite {
	return rustBoundarySpec.candidates(source)
}

func (RustBoundary) Apply(source []byte, site ASTSite) ([]byte, error) {
	return applyReplacement(source, site)
}

// --- Rust: Option/Result branch inversion ---

// RustOptionInvert flips Option/Result check methods using tree-sitter AST.
// is_some() ↔ is_none(), is_ok() ↔ is_err()
type RustOptionInvert struct{}

func (RustOptionInvert) Name() string     { return "rs-option-invert" }
func (RustOptionInvert) Language() string { return "rust" }
func (RustOptionInvert) Difficulty() int  { return OptionPredicateOperator.Difficulty }

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
		flipped, ok := replacementFor(OptionPredicateOperator, name)
		if !ok {
			return true
		}
		sites = append(sites, ASTSite{
			StartByte:   int(nameNode.StartByte()),
			EndByte:     int(nameNode.EndByte()),
			StartLine:   pointToLine(nameNode.StartPoint()),
			EndLine:     pointToLine(nameNode.EndPoint()),
			Description: fmt.Sprintf("invert %s() to %s() on line %d", name, flipped, pointToLine(nameNode.StartPoint())),
			Metadata:    replacementMetadata(name, flipped),
		})
		return true
	})
	return sites
}

func (RustOptionInvert) Apply(source []byte, site ASTSite) ([]byte, error) {
	return applyReplacement(source, site)
}

// --- Rust: range bound mutation ---

// RustRangeBound flips exclusive to inclusive range: 0..n ↔ 0..=n.
type RustRangeBound struct{}

func (RustRangeBound) Name() string     { return "rs-range-bound" }
func (RustRangeBound) Language() string { return "rust" }
func (RustRangeBound) Difficulty() int  { return RangeBoundOperator.Difficulty }

var rustRangeBoundSpec = tokenReplacementSpec{
	Definition: RangeBoundOperator,
	Prefix:     "rs",
	Language:   "rust",
	NodeType:   "range_expression",
	TokenIndex: 1,
	Parser:     getRustLanguage,
	Describe: func(from, to string, line int) string {
		kind := "exclusive"
		if from == "..=" {
			kind = "inclusive"
		}
		return fmt.Sprintf("flip %s range to %s on line %d", kind, to, line)
	},
}

func (RustRangeBound) Candidates(source []byte) []ASTSite {
	return rustRangeBoundSpec.candidates(source)
}

func (RustRangeBound) Apply(source []byte, site ASTSite) ([]byte, error) {
	return applyReplacement(source, site)
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
	return replaceByteRange(source, site.StartByte, site.EndByte, replacement)
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
