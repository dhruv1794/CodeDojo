package validator

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		content           string
		bannedIdentifiers []string
		wantOK            bool
		wantReason        string
	}{
		{
			name:    "plain socratic nudge passes",
			content: "What assumption would you test next before changing anything?",
			wantOK:  true,
		},
		{
			name:    "short fenced block passes",
			content: "Try inspecting this shape:\n```go\nif condition {\n\treturn err\n}\n```",
			wantOK:  true,
		},
		{
			name:    "short fenced block ignores blank lines",
			content: "```python\na = 1\n\nb = 2\n\nc = 3\n```",
			wantOK:  true,
		},
		{
			name:       "long fenced block fails",
			content:    "```go\nline1\nline2\nline3\nline4\n```",
			wantOK:     false,
			wantReason: "fenced code block",
		},
		{
			name:       "unclosed long fenced block fails",
			content:    "```go\nline1\nline2\nline3\nline4",
			wantOK:     false,
			wantReason: "fenced code block",
		},
		{
			name:    "inline backticks pass",
			content: "Look at `CalculateTotal` only as a place to ask what invariant changed.",
			wantOK:  true,
		},
		{
			name:       "go function definition fails",
			content:    "func solve(input string) error {\n\treturn nil\n}",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "go method definition fails",
			content:    "func (s *service) solve() error {\n\treturn nil\n}",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "indented go function definition fails",
			content:    "  func answer() int {\n\treturn 42\n}",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "python function definition fails",
			content:    "def solve(value):\n    return value",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "indented python function definition fails",
			content:    "    def solve(value):\n        return value",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "javascript function definition fails",
			content:    "function solve(value) {\n  return value\n}",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "exported javascript function definition fails",
			content:    "export function solve(value) {\n  return value\n}",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "async javascript function definition fails",
			content:    "async function solve(value) {\n  return value\n}",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "const arrow function fails",
			content:    "const solve = (value) => value + 1",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "let async arrow function fails",
			content:    "let solve = async (value) => value + 1",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:       "var arrow function fails",
			content:    "var solve = (value) => value + 1",
			wantOK:     false,
			wantReason: "function definition",
		},
		{
			name:    "sentence mentioning function concept passes",
			content: "A function can preserve the invariant without exposing a full implementation.",
			wantOK:  true,
		},
		{
			name:              "banned identifier exact case fails",
			content:           "Look near CalculateTotal and ask what changed.",
			bannedIdentifiers: []string{"CalculateTotal"},
			wantOK:            false,
			wantReason:        "banned identifier",
		},
		{
			name:              "banned identifier case insensitive fails",
			content:           "The calculatetotal branch is where the symptom starts.",
			bannedIdentifiers: []string{"CalculateTotal"},
			wantOK:            false,
			wantReason:        "banned identifier",
		},
		{
			name:              "banned identifier substring fails",
			content:           "Focus on the subtotal helper.",
			bannedIdentifiers: []string{"total"},
			wantOK:            false,
			wantReason:        "banned identifier",
		},
		{
			name:              "empty banned identifiers are ignored",
			content:           "Name the invariant before inspecting the next branch.",
			bannedIdentifiers: []string{"", "   "},
			wantOK:            true,
		},
		{
			name:              "safe content with unrelated banned identifier passes",
			content:           "Check which error path is reachable in the failing test.",
			bannedIdentifiers: []string{"CalculateTotal"},
			wantOK:            true,
		},
		{
			name:       "fenced long code fails before function reason",
			content:    "```go\nfunc answer() int {\n\tvalue := 42\n\treturn value\n}\n```",
			wantOK:     false,
			wantReason: "fenced code block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Validate(tt.content, tt.bannedIdentifiers)
			if got.OK != tt.wantOK {
				t.Fatalf("Validate() OK = %v, want %v; reason = %q", got.OK, tt.wantOK, got.Reason)
			}
			if tt.wantReason != "" && !strings.Contains(got.Reason, tt.wantReason) {
				t.Fatalf("Validate() reason = %q, want substring %q", got.Reason, tt.wantReason)
			}
			if tt.wantOK && got.Reason != "" {
				t.Fatalf("Validate() reason = %q, want empty reason for passing content", got.Reason)
			}
		})
	}
}
