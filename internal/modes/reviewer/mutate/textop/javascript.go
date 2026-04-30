package textop

import (
	"fmt"
	"regexp"
	"strings"
)

// --- JavaScript: comparison boundary ---

// JSBoundary flips comparison operators in JavaScript/TypeScript source: < ↔ <=, > ↔ >=.
type JSBoundary struct{}

func (JSBoundary) Name() string     { return "js-boundary" }
func (JSBoundary) Language() string { return "javascript" }
func (JSBoundary) Difficulty() int  { return 1 }

var jsBoundaryRe = regexp.MustCompile(`([^<>!])(<|>)([^=\n/])`)

func (JSBoundary) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") || strings.HasPrefix(stripped, "*") {
			continue
		}
		for _, m := range jsBoundaryRe.FindAllStringSubmatch(line, -1) {
			op := m[2] // group 2 is the operator
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("flip comparison operator %s on line %d", op, i+1),
				Metadata:    map[string]string{"op": op},
			})
		}
	}
	return sites
}

func (JSBoundary) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	op := site.Metadata["op"]
	var flipped string
	switch op {
	case "<":
		flipped = "<="
	case ">":
		flipped = ">="
	case "<=":
		flipped = "<"
	case ">=":
		flipped = ">"
	default:
		return "", fmt.Errorf("unknown operator %q", op)
	}
	pattern := regexp.MustCompile(`([^<>!=])` + regexp.QuoteMeta(op) + `([^=])`)
	replaced := false
	newLine := pattern.ReplaceAllStringFunc(line, func(s string) string {
		if replaced {
			return s
		}
		replaced = true
		return string(s[0]) + flipped + string(s[len(s)-1])
	})
	lines[site.StartLine-1] = newLine
	return strings.Join(lines, "\n"), nil
}

// --- JavaScript: boolean conditional flip ---

// JSConditional negates the condition of a simple if-statement by adding/removing `!`.
// Targets: `if (expr)` → `if (!expr)` and `if (!expr)` → `if (expr)`
type JSConditional struct{}

func (JSConditional) Name() string     { return "js-conditional" }
func (JSConditional) Language() string { return "javascript" }
func (JSConditional) Difficulty() int  { return 2 }

// Matches "if (<simple-expr>)" where the expression is a single identifier or property access.
var jsIfSimpleRe = regexp.MustCompile(`(?m)\bif\s*\(\s*(!?)\s*([A-Za-z_$][A-Za-z0-9_$.]*)\s*\)`)

func (JSConditional) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		if jsIfSimpleRe.MatchString(line) {
			m := jsIfSimpleRe.FindStringSubmatch(line)
			hasNot := m[1] == "!"
			desc := "insert ! into condition"
			if hasNot {
				desc = "remove ! from condition"
			}
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: desc,
				Metadata:    map[string]string{"has_not": fmt.Sprintf("%v", hasNot)},
			})
		}
	}
	return sites
}

func (JSConditional) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	var newLine string
	if site.Metadata["has_not"] == "true" {
		// Remove the first ! after "if ("
		removeRe := regexp.MustCompile(`(\bif\s*\(\s*)!`)
		newLine = removeRe.ReplaceAllString(line, "$1")
	} else {
		// Insert ! after "if ("
		insertRe := regexp.MustCompile(`(\bif\s*\(\s*)`)
		newLine = insertRe.ReplaceAllString(line, "${1}!")
	}
	lines[site.StartLine-1] = newLine
	return strings.Join(lines, "\n"), nil
}

// --- JavaScript: async error swallow ---

// JSAsyncErrorSwallow replaces `throw` inside a catch block with a no-op comment.
type JSAsyncErrorSwallow struct{}

func (JSAsyncErrorSwallow) Name() string     { return "js-async-error-swallow" }
func (JSAsyncErrorSwallow) Language() string { return "javascript" }
func (JSAsyncErrorSwallow) Difficulty() int  { return 3 }

var jsCatchRe = regexp.MustCompile(`(?m)^\s*\}\s*catch\s*\(`)

func (JSAsyncErrorSwallow) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	inCatch := false
	depth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if jsCatchRe.MatchString(line) {
			inCatch = true
			depth = 0
		}
		if inCatch {
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if strings.HasPrefix(trimmed, "throw ") || trimmed == "throw;" {
				sites = append(sites, Site{
					StartLine:   i + 1,
					EndLine:     i + 1,
					Description: fmt.Sprintf("swallow error: replace throw with no-op on line %d", i+1),
				})
				inCatch = false
			}
			if depth < 0 {
				inCatch = false
			}
		}
	}
	return sites
}

func (JSAsyncErrorSwallow) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	lines[site.StartLine-1] = indent + "// error swallowed"
	return strings.Join(lines, "\n"), nil
}

// --- JavaScript: array index bounds ---

// JSArrayBounds flips `arr[i]` patterns to `arr[i-1]` or `arr[i+1]` (off-by-one).
type JSArrayBounds struct{}

func (JSArrayBounds) Name() string     { return "js-array-bounds" }
func (JSArrayBounds) Language() string { return "javascript" }
func (JSArrayBounds) Difficulty() int  { return 2 }

var jsIndexRe = regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$]*)\[([A-Za-z_$][A-Za-z0-9_$]*)\]`)

func (JSArrayBounds) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if jsIndexRe.MatchString(line) {
			m := jsIndexRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("shift array index %s[%s] to %s[%s-1]", m[1], m[2], m[1], m[2]),
				Metadata:    map[string]string{"arr": m[1], "idx": m[2]},
			})
		}
	}
	return sites
}

func (JSArrayBounds) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	arr := site.Metadata["arr"]
	idx := site.Metadata["idx"]
	old := fmt.Sprintf("%s[%s]", arr, idx)
	new_ := fmt.Sprintf("%s[%s-1]", arr, idx)
	replaced := false
	newLine := strings.Map(func(r rune) rune { return r }, line)
	if i := strings.Index(newLine, old); i >= 0 && !replaced {
		newLine = newLine[:i] + new_ + newLine[i+len(old):]
		replaced = true
	}
	_ = replaced
	lines[site.StartLine-1] = newLine
	return strings.Join(lines, "\n"), nil
}

// AllJS returns all JavaScript text mutators.
func AllJS() []TextMutator {
	return []TextMutator{
		JSBoundary{},
		JSConditional{},
		JSAsyncErrorSwallow{},
		JSArrayBounds{},
	}
}

// AllTS returns TypeScript mutators (superset of JS operators plus TS-specific ones).
func AllTS() []TextMutator {
	base := AllJS()
	ts := make([]TextMutator, len(base))
	copy(ts, base)
	// Add TypeScript-specific operators.
	ts = append(ts, TSOptionalChain{}, TSTypeGuardWeaken{})
	return ts
}

// --- TypeScript: optional chain weakening ---

// TSOptionalChain replaces `?.` optional chaining with `.` (removes the null guard).
type TSOptionalChain struct{}

func (TSOptionalChain) Name() string     { return "ts-optional-chain" }
func (TSOptionalChain) Language() string { return "typescript" }
func (TSOptionalChain) Difficulty() int  { return 2 }

var tsOptChainRe = regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$]*)\?\.`)

func (TSOptionalChain) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		if tsOptChainRe.MatchString(line) {
			m := tsOptChainRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("remove optional chain on %s on line %d", m[1], i+1),
				Metadata:    map[string]string{"obj": m[1]},
			})
		}
	}
	return sites
}

func (TSOptionalChain) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	obj := site.Metadata["obj"]
	old := obj + "?."
	new_ := obj + "."
	replaced := false
	line := lines[site.StartLine-1]
	if i := strings.Index(line, old); i >= 0 && !replaced {
		line = line[:i] + new_ + line[i+len(old):]
		replaced = true
	}
	_ = replaced
	lines[site.StartLine-1] = line
	return strings.Join(lines, "\n"), nil
}

// --- TypeScript: type guard weaken ---

// TSTypeGuardWeaken replaces `if (x instanceof T)` with `if (x)` (drops the type check).
type TSTypeGuardWeaken struct{}

func (TSTypeGuardWeaken) Name() string     { return "ts-type-guard-weaken" }
func (TSTypeGuardWeaken) Language() string { return "typescript" }
func (TSTypeGuardWeaken) Difficulty() int  { return 3 }

var tsInstanceofRe = regexp.MustCompile(`\bif\s*\(\s*([A-Za-z_$][A-Za-z0-9_$.]*)\s+instanceof\s+[A-Za-z_$][A-Za-z0-9_$.]*\s*\)`)

func (TSTypeGuardWeaken) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		if tsInstanceofRe.MatchString(line) {
			m := tsInstanceofRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("weaken instanceof type guard for %s on line %d", m[1], i+1),
				Metadata:    map[string]string{"obj": m[1]},
			})
		}
	}
	return sites
}

func (TSTypeGuardWeaken) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	obj := site.Metadata["obj"]
	newLine := tsInstanceofRe.ReplaceAllString(lines[site.StartLine-1], "if ("+obj+")")
	lines[site.StartLine-1] = newLine
	return strings.Join(lines, "\n"), nil
}
