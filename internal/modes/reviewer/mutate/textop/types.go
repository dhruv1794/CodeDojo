// Package textop provides text/regex-based mutation operators for non-Go languages.
// These are intentionally simpler than the Go AST operators — they work on raw source
// text and are designed to produce plausible bugs without requiring a language parser.
package textop

// TextMutator is a language-specific text-based mutation operator.
type TextMutator interface {
	Name() string
	Language() string
	Difficulty() int
	// Candidates scans the source text and returns candidate mutation sites.
	Candidates(content string) []Site
	// Apply rewrites content at the given site and returns the new content.
	Apply(content string, site Site) (string, error)
}

// Site is a text-level mutation location within a source file.
type Site struct {
	// StartLine and EndLine are 1-based.
	StartLine   int
	EndLine     int
	Description string
	// Metadata carries operator-specific context (e.g. "before" token).
	Metadata map[string]string
}
