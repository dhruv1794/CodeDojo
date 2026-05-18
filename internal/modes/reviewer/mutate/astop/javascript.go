// SPDX-License-Identifier: MIT

package astop

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// --- JavaScript: comparison boundary flip ---

// JSBoundary flips comparison operators in JavaScript/TypeScript using tree-sitter.
type JSBoundary struct{}

func (JSBoundary) Name() string     { return jsBoundarySpec.name() }
func (JSBoundary) Language() string { return "javascript" }
func (JSBoundary) Difficulty() int  { return BoundaryOperator.Difficulty }

var jsBoundarySpec = tokenReplacementSpec{
	Definition: BoundaryOperator,
	Prefix:     "js",
	Language:   "javascript",
	NodeType:   "binary_expression",
	TokenIndex: 1,
	Parser:     getJSLanguage,
	Describe: func(from, _ string, line int) string {
		return fmt.Sprintf("flip comparison operator %s on line %d", from, line)
	},
}

func (JSBoundary) Candidates(source []byte) []ASTSite {
	return jsBoundarySpec.candidates(source)
}

func (JSBoundary) Apply(source []byte, site ASTSite) ([]byte, error) {
	return applyReplacement(source, site)
}

// --- JavaScript: strict equality coercion ---

// JSStrictEquality weakens strict equality so JavaScript coercion can change behavior.
type JSStrictEquality struct{}

func (JSStrictEquality) Name() string     { return jsStrictEqualitySpec.name() }
func (JSStrictEquality) Language() string { return "javascript" }
func (JSStrictEquality) Difficulty() int  { return StrictEqualityOperator.Difficulty }

var jsStrictEqualitySpec = tokenReplacementSpec{
	Definition: StrictEqualityOperator,
	Prefix:     "js",
	Language:   "javascript",
	NodeType:   "binary_expression",
	TokenIndex: 1,
	Parser:     getJSLanguage,
	Describe: func(from, to string, line int) string {
		return fmt.Sprintf("weaken strict equality %s to %s on line %d", from, to, line)
	},
}

func (JSStrictEquality) Candidates(source []byte) []ASTSite {
	return jsStrictEqualitySpec.candidates(source)
}

func (JSStrictEquality) Apply(source []byte, site ASTSite) ([]byte, error) {
	return applyReplacement(source, site)
}

// --- JavaScript: boolean conditional flip ---

// JSConditional negates the condition of an if-statement by adding/removing !.
type JSConditional struct{}

func (JSConditional) Name() string     { return "js-conditional" }
func (JSConditional) Language() string { return "javascript" }
func (JSConditional) Difficulty() int  { return 2 }

func (JSConditional) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getJSLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "if_statement" {
			return true
		}
		if node.ChildCount() < 3 {
			return true
		}
		parenNode := node.Child(1)
		if parenNode.Type() != "parenthesized_expression" || parenNode.ChildCount() < 3 {
			return true
		}
		innerNode := parenNode.Child(1)
		hasNot := false
		var target *sitter.Node
		switch innerNode.Type() {
		case "unary_expression":
			if innerNode.ChildCount() >= 2 && nodeText(source, innerNode.Child(0)) == "!" {
				hasNot = true
				target = innerNode.Child(1)
			} else {
				return true
			}
		default:
			target = innerNode
		}
		if target == nil || target.Type() != "identifier" {
			return true
		}
		desc := "insert ! into condition"
		var byteRange [2]int
		if hasNot {
			desc = "remove ! from condition"
			notNode := innerNode.Child(0)
			byteRange = [2]int{int(notNode.StartByte()), int(notNode.EndByte())}
		} else {
			byteRange = [2]int{int(target.StartByte()), int(target.StartByte())}
		}
		sites = append(sites, ASTSite{
			StartByte:   byteRange[0],
			EndByte:     byteRange[1],
			StartLine:   pointToLine(parenNode.StartPoint()),
			EndLine:     pointToLine(parenNode.EndPoint()),
			Description: desc,
			Metadata:    map[string]string{"has_not": fmt.Sprintf("%v", hasNot)},
		})
		return true
	})
	return sites
}

func (JSConditional) Apply(source []byte, site ASTSite) ([]byte, error) {
	if site.Metadata["has_not"] == "true" {
		return replaceByteRange(source, site.StartByte, site.EndByte, nil)
	}
	return replaceByteRange(source, site.StartByte, site.EndByte, []byte("!"))
}

// --- JavaScript: async error swallow ---

// JSAsyncErrorSwallow replaces throw inside a catch block with a no-op comment.
type JSAsyncErrorSwallow struct{}

func (JSAsyncErrorSwallow) Name() string     { return "js-async-error-swallow" }
func (JSAsyncErrorSwallow) Language() string { return "javascript" }
func (JSAsyncErrorSwallow) Difficulty() int  { return 3 }

func (JSAsyncErrorSwallow) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getJSLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "throw_statement" {
			return true
		}
		parent := node.Parent()
		for parent != nil && !parent.IsNull() {
			if parent.Type() == "catch_clause" {
				sites = append(sites, ASTSite{
					StartByte:   int(node.StartByte()),
					EndByte:     int(node.EndByte()),
					StartLine:   pointToLine(node.StartPoint()),
					EndLine:     pointToLine(node.EndPoint()),
					Description: fmt.Sprintf("swallow error: replace throw with no-op on line %d", pointToLine(node.StartPoint())),
				})
				return true
			}
			parent = parent.Parent()
		}
		return true
	})
	return sites
}

func (JSAsyncErrorSwallow) Apply(source []byte, site ASTSite) ([]byte, error) {
	indent := make([]byte, 0)
	for i := site.StartByte - 1; i >= 0; i-- {
		if source[i] == '\n' {
			break
		}
		if source[i] == ' ' || source[i] == '\t' {
			indent = append([]byte{source[i]}, indent...)
		} else {
			indent = nil
		}
	}
	replacement := append(indent, []byte("// error swallowed")...)
	return replaceByteRange(source, site.StartByte, site.EndByte, replacement)
}

// --- JavaScript: array index bounds ---

// JSArrayBounds shifts array index: arr[i] → arr[i-1] (off-by-one).
type JSArrayBounds struct{}

func (JSArrayBounds) Name() string     { return "js-array-bounds" }
func (JSArrayBounds) Language() string { return "javascript" }
func (JSArrayBounds) Difficulty() int  { return 2 }

func (JSArrayBounds) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getJSLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "subscript_expression" {
			return true
		}
		if node.ChildCount() < 4 {
			return true
		}
		bracketNode := node.Child(int(node.ChildCount()) - 1)
		if bracketNode.Type() != "]" {
			return true
		}
		indexNode := node.Child(2)
		idxText := nodeText(source, indexNode)
		arrNode := node.Child(0)
		arrText := nodeText(source, arrNode)
		sites = append(sites, ASTSite{
			StartByte:   int(indexNode.EndByte()),
			EndByte:     int(indexNode.EndByte()),
			StartLine:   pointToLine(indexNode.StartPoint()),
			EndLine:     pointToLine(indexNode.EndPoint()),
			Description: fmt.Sprintf("shift array index %s[%s] to %s[%s-1]", arrText, idxText, arrText, idxText),
			Metadata:    map[string]string{"arr": arrText, "idx": idxText},
		})
		return true
	})
	return sites
}

func (JSArrayBounds) Apply(source []byte, site ASTSite) ([]byte, error) {
	idx := site.Metadata["idx"]
	replacement := []byte("-1")
	_ = idx
	return replaceByteRange(source, site.StartByte, site.EndByte, replacement)
}

// AllJS returns all JavaScript AST-based mutators.
func AllJS() []ASTMutator {
	return []ASTMutator{
		JSBoundary{},
		JSStrictEquality{},
		JSConditional{},
		JSAsyncErrorSwallow{},
		JSArrayBounds{},
	}
}

// --- TypeScript: optional chain weakening ---

// TSOptionalChain replaces optional chaining ?. with . (removes null guard).
type TSOptionalChain struct{}

func (TSOptionalChain) Name() string     { return "ts-optional-chain" }
func (TSOptionalChain) Language() string { return "typescript" }
func (TSOptionalChain) Difficulty() int  { return 2 }

func (TSOptionalChain) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getTSLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "member_expression" {
			return true
		}
		var dotNode *sitter.Node
		for i := uint32(0); i < node.ChildCount(); i++ {
			child := node.Child(int(i))
			if child.Type() == "optional_chain" && child.ChildCount() > 0 {
				dotNode = child.Child(0)
				break
			}
		}
		if dotNode == nil {
			return true
		}
		sites = append(sites, ASTSite{
			StartByte:   int(dotNode.StartByte()),
			EndByte:     int(dotNode.EndByte()),
			StartLine:   pointToLine(dotNode.StartPoint()),
			EndLine:     pointToLine(dotNode.EndPoint()),
			Description: fmt.Sprintf("remove optional chain on line %d", pointToLine(dotNode.StartPoint())),
		})
		return true
	})
	return sites
}

func (TSOptionalChain) Apply(source []byte, site ASTSite) ([]byte, error) {
	return replaceByteRange(source, site.StartByte, site.EndByte, []byte("."))
}

// --- TypeScript: type guard weaken ---

// TSTypeGuardWeaken replaces if (x instanceof T) with if (x) — drops the type check.
type TSTypeGuardWeaken struct{}

func (TSTypeGuardWeaken) Name() string     { return "ts-type-guard-weaken" }
func (TSTypeGuardWeaken) Language() string { return "typescript" }
func (TSTypeGuardWeaken) Difficulty() int  { return 3 }

func (TSTypeGuardWeaken) Candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(getTSLanguage())
	tree := parser.Parse(nil, source)
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != "if_statement" {
			return true
		}
		if node.ChildCount() < 3 {
			return true
		}
		parenNode := node.Child(1)
		if parenNode.Type() != "parenthesized_expression" || parenNode.ChildCount() < 3 {
			return true
		}
		condNode := parenNode.Child(1)
		if condNode.Type() != "binary_expression" {
			return true
		}
		if condNode.ChildCount() < 3 {
			return true
		}
		opNode := condNode.Child(1)
		if nodeText(source, opNode) != "instanceof" {
			return true
		}
		leftNode := condNode.Child(0)
		leftName := nodeText(source, leftNode)
		sites = append(sites, ASTSite{
			StartByte:   int(condNode.StartByte()),
			EndByte:     int(condNode.EndByte()),
			StartLine:   pointToLine(condNode.StartPoint()),
			EndLine:     pointToLine(condNode.EndPoint()),
			Description: fmt.Sprintf("weaken instanceof type guard for %s on line %d", leftName, pointToLine(condNode.StartPoint())),
			Metadata:    map[string]string{"obj": leftName},
		})
		return true
	})
	return sites
}

func (TSTypeGuardWeaken) Apply(source []byte, site ASTSite) ([]byte, error) {
	obj := site.Metadata["obj"]
	return replaceByteRange(source, site.StartByte, site.EndByte, []byte(obj))
}

// AllTS returns TypeScript AST-based mutators (superset of JS).
func AllTS() []ASTMutator {
	return append(AllJS(), TSOptionalChain{}, TSTypeGuardWeaken{})
}
