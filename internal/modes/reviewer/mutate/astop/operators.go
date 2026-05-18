// SPDX-License-Identifier: MIT

package astop

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// OperatorDefinition describes the language-independent reviewer behavior.
// Language-specific mutators provide only the parser and node finder details.
type OperatorDefinition struct {
	Class       string
	Intent      string
	Difficulty  int
	Replacement map[string]string
}

var (
	BoundaryOperator = OperatorDefinition{
		Class:      "boundary",
		Intent:     "change relational operator at clamp/range check",
		Difficulty: 1,
		Replacement: map[string]string{
			"<":  "<=",
			">":  ">=",
			"<=": "<",
			">=": ">",
		},
	}

	OptionPredicateOperator = OperatorDefinition{
		Class:      "option-predicate",
		Intent:     "invert success/presence predicate",
		Difficulty: 2,
		Replacement: map[string]string{
			"is_some": "is_none",
			"is_none": "is_some",
			"is_ok":   "is_err",
			"is_err":  "is_ok",
		},
	}

	RangeBoundOperator = OperatorDefinition{
		Class:      "range-bound",
		Intent:     "change exclusive/inclusive range endpoint",
		Difficulty: 2,
		Replacement: map[string]string{
			"..":  "..=",
			"..=": "..",
		},
	}

	StrictEqualityOperator = OperatorDefinition{
		Class:      "strict-equality",
		Intent:     "weaken strict equality into coercive equality",
		Difficulty: 3,
		Replacement: map[string]string{
			"===": "==",
			"!==": "!=",
		},
	}
)

func languageOperatorName(prefix string, def OperatorDefinition) string {
	if prefix == "" {
		return def.Class
	}
	return prefix + "-" + def.Class
}

type tokenReplacementSpec struct {
	Definition OperatorDefinition
	Prefix     string
	Language   string
	NodeType   string
	TokenIndex int
	Parser     func() *sitter.Language
	Describe   func(from, to string, line int) string
}

func (s tokenReplacementSpec) name() string {
	return languageOperatorName(s.Prefix, s.Definition)
}

func (s tokenReplacementSpec) candidates(source []byte) []ASTSite {
	parser := sitter.NewParser()
	parser.SetLanguage(s.Parser())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil
	}
	var sites []ASTSite
	walk(tree.RootNode(), func(node *sitter.Node) bool {
		if node.Type() != s.NodeType || int(node.ChildCount()) <= s.TokenIndex {
			return true
		}
		token := node.Child(s.TokenIndex)
		from := nodeText(source, token)
		to, ok := s.Definition.Replacement[from]
		if !ok {
			return true
		}
		line := pointToLine(token.StartPoint())
		desc := fmt.Sprintf("%s: replace %s with %s on line %d", s.Definition.Intent, from, to, line)
		if s.Describe != nil {
			desc = s.Describe(from, to, line)
		}
		sites = append(sites, ASTSite{
			StartByte:   int(token.StartByte()),
			EndByte:     int(token.EndByte()),
			StartLine:   line,
			EndLine:     pointToLine(token.EndPoint()),
			Description: desc,
			Metadata:    replacementMetadata(from, to),
		})
		return true
	})
	return sites
}

func replacementMetadata(from, to string) map[string]string {
	return map[string]string{"from": from, "to": to, "op": from}
}

func applyReplacement(source []byte, site ASTSite) ([]byte, error) {
	from := site.Metadata["from"]
	to := site.Metadata["to"]
	if to == "" {
		return nil, fmt.Errorf("missing replacement for %q", from)
	}
	return replaceByteRange(source, site.StartByte, site.EndByte, []byte(to))
}

func replaceByteRange(source []byte, start, end int, replacement []byte) ([]byte, error) {
	if start < 0 || end < start || end > len(source) {
		return nil, fmt.Errorf("invalid replacement byte range %d:%d for source length %d", start, end, len(source))
	}
	result := make([]byte, len(source)+len(replacement)-(end-start))
	n := copy(result, source[:start])
	n += copy(result[n:], replacement)
	copy(result[n:], source[end:])
	return result, nil
}

func replacementFor(def OperatorDefinition, from string) (string, bool) {
	to, ok := def.Replacement[from]
	return to, ok
}

func ensureOperatorDefinitions(defs ...OperatorDefinition) error {
	for _, def := range defs {
		if strings.TrimSpace(def.Class) == "" {
			return fmt.Errorf("operator definition has empty class")
		}
		if def.Difficulty < 1 {
			return fmt.Errorf("%s difficulty = %d, want positive", def.Class, def.Difficulty)
		}
		if len(def.Replacement) == 0 {
			return fmt.Errorf("%s has no replacements", def.Class)
		}
	}
	return nil
}
