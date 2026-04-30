package mutate

import (
	"go/ast"
	"time"
)

// Mutator is the Go AST-based mutation interface. Language-specific
// text-based mutators use the TextMutator interface in the textop package.
type Mutator interface {
	Name() string
	Difficulty() int
	Candidates(*ast.File) []Site
	Apply(*ast.File, Site) (Mutation, error)
}

// ScanConfig controls which files are considered as mutation candidates.
type ScanConfig struct {
	// GlobPattern is the pattern passed to "git log -- <pattern>" (e.g. "*.go", "*.py").
	// Defaults to "*.go".
	GlobPattern string
	// IsEligible reports whether a repo-relative file path should be considered.
	// When nil, defaults to the Go-specific eligibility check.
	IsEligible func(repoPath, relPath string) (bool, error)
}

type Site struct {
	FilePath    string            `json:"file_path"`
	StartLine   int               `json:"start_line"`
	StartColumn int               `json:"start_column"`
	EndLine     int               `json:"end_line"`
	EndColumn   int               `json:"end_column"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Node        ast.Node          `json:"-"`
}

type Mutation struct {
	Operator    string    `json:"operator"`
	Difficulty  int       `json:"difficulty"`
	FilePath    string    `json:"file_path"`
	StartLine   int       `json:"start_line"`
	StartColumn int       `json:"start_column"`
	EndLine     int       `json:"end_line"`
	EndColumn   int       `json:"end_column"`
	Original    string    `json:"original,omitempty"`
	Mutated     string    `json:"mutated,omitempty"`
	Description string    `json:"description"`
	AppliedAt   time.Time `json:"applied_at"`
}

type MutationLog struct {
	ID         string    `json:"id"`
	RepoPath   string    `json:"repo_path"`
	HeadSHA    string    `json:"head_sha,omitempty"`
	Difficulty int       `json:"difficulty"`
	Mutation   Mutation  `json:"mutation"`
	CreatedAt  time.Time `json:"created_at"`
}

type Candidate struct {
	FilePath string
	Mutator  Mutator
	Site     Site
}
