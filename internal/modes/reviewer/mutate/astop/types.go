package astop

import sitter "github.com/smacker/go-tree-sitter"

// ASTMutator is a Tree-sitter AST-based mutation operator for non-Go languages.
// Unlike textop.TextMutator, it uses a parse tree to identify mutation sites
// precisely, avoiding false matches inside comments/strings.
type ASTMutator interface {
	Name() string
	Language() string
	Difficulty() int
	Candidates(source []byte) []ASTSite
	Apply(source []byte, site ASTSite) ([]byte, error)
}

// ASTSite is a mutation location identified from a Tree-sitter parse tree.
type ASTSite struct {
	StartByte   int
	EndByte     int
	StartLine   int
	EndLine     int
	Description string
	Metadata    map[string]string
}

// walk traverses the parse tree depth-first, calling fn for each node.
// Return false from fn to stop traversal.
func walk(node *sitter.Node, fn func(*sitter.Node) bool) {
	if node == nil || node.IsNull() {
		return
	}
	if !fn(node) {
		return
	}
	for i := uint32(0); i < node.ChildCount(); i++ {
		walk(node.Child(int(i)), fn)
	}
}

// pointToLine converts a tree-sitter Point to a 1-based line number.
func pointToLine(p sitter.Point) int {
	return int(p.Row) + 1
}

// pointToColumn converts a tree-sitter Point to a 1-based column number.
func pointToColumn(p sitter.Point) int {
	return int(p.Column) + 1
}

// nodeText returns the source text within the node's byte range.
func nodeText(source []byte, node *sitter.Node) string {
	return string(source[node.StartByte():node.EndByte()])
}
