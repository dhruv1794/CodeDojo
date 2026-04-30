package textop

import (
	"fmt"
	"regexp"
	"strings"
)

// --- Python: comparison boundary ---

// PythonBoundary flips comparison operators in Python source: < ↔ <=, > ↔ >=.
type PythonBoundary struct{}

func (PythonBoundary) Name() string     { return "py-boundary" }
func (PythonBoundary) Language() string { return "python" }
func (PythonBoundary) Difficulty() int  { return 1 }

var pyBoundaryRe = regexp.MustCompile(`(?m)([^<>!])(<|>)([^=\n])`)

func (PythonBoundary) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "#") {
			continue
		}
		for _, m := range pyBoundaryRe.FindAllStringSubmatch(line, -1) {
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

func (PythonBoundary) Apply(content string, site Site) (string, error) {
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
	// Replace first occurrence of the exact operator that is not already compound.
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

// --- Python: boolean conditional flip ---

// PythonConditional inserts or removes `not` in a simple if-condition.
// Targets: `if expr:` → `if not expr:` and `if not expr:` → `if expr:`
type PythonConditional struct{}

func (PythonConditional) Name() string     { return "py-conditional" }
func (PythonConditional) Language() string { return "python" }
func (PythonConditional) Difficulty() int  { return 2 }

// Matches "if <simple-expr>:" where the expression is a single identifier or attribute.
var pyIfSimpleRe = regexp.MustCompile(`(?m)^(\s*if\s+)(not\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s*:`)

func (PythonConditional) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		if pyIfSimpleRe.MatchString(line) {
			hasNot := strings.Contains(line, " not ")
			desc := "insert not into condition"
			if hasNot {
				desc = "remove not from condition"
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

func (PythonConditional) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	var newLine string
	if strings.Contains(line, " not ") {
		// Remove the first "not "
		newLine = strings.Replace(line, " not ", " ", 1)
	} else {
		// Insert "not " after "if "
		insertRe := regexp.MustCompile(`(\bif\s+)`)
		newLine = insertRe.ReplaceAllString(line, "${1}not ")
	}
	lines[site.StartLine-1] = newLine
	return strings.Join(lines, "\n"), nil
}

// --- Python: exception swallow ---

// PythonExceptSwallow replaces `except SomeError as e: raise` with
// `except SomeError as e: pass`, effectively swallowing the exception.
type PythonExceptSwallow struct{}

func (PythonExceptSwallow) Name() string     { return "py-except-swallow" }
func (PythonExceptSwallow) Language() string { return "python" }
func (PythonExceptSwallow) Difficulty() int  { return 3 }

func (PythonExceptSwallow) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	inExcept := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "except") && strings.HasSuffix(trimmed, ":") {
			inExcept = true
			continue
		}
		if inExcept && trimmed == "raise" || (inExcept && strings.HasPrefix(trimmed, "raise ")) {
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("swallow exception: replace raise with pass on line %d", i+1),
			})
			inExcept = false
		} else if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			inExcept = false
		}
	}
	return sites
}

func (PythonExceptSwallow) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	lines[site.StartLine-1] = indent + "pass"
	return strings.Join(lines, "\n"), nil
}

// --- Python: slice bounds ---

// PythonSliceBounds flips `s[i:]` to `s[:i]` and vice versa.
type PythonSliceBounds struct{}

func (PythonSliceBounds) Name() string     { return "py-slice-bounds" }
func (PythonSliceBounds) Language() string { return "python" }
func (PythonSliceBounds) Difficulty() int  { return 2 }

var pySliceFromRe = regexp.MustCompile(`\[([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\]`)
var pySliceToRe = regexp.MustCompile(`\[\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\s*\]`)

func (PythonSliceBounds) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		if pySliceFromRe.MatchString(line) {
			m := pySliceFromRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("flip slice [%s:] to [:%s]", m[1], m[1]),
				Metadata:    map[string]string{"var": m[1], "direction": "from"},
			})
		} else if pySliceToRe.MatchString(line) {
			m := pySliceToRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("flip slice [:%s] to [%s:]", m[1], m[1]),
				Metadata:    map[string]string{"var": m[1], "direction": "to"},
			})
		}
	}
	return sites
}

func (PythonSliceBounds) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	v := site.Metadata["var"]
	direction := site.Metadata["direction"]
	var newLine string
	switch direction {
	case "from":
		newLine = pySliceFromRe.ReplaceAllString(line, "[:"+v+"]")
	case "to":
		newLine = pySliceToRe.ReplaceAllString(line, "["+v+":]")
	default:
		return "", fmt.Errorf("unknown slice direction %q", direction)
	}
	lines[site.StartLine-1] = newLine
	return strings.Join(lines, "\n"), nil
}

// AllPython returns all Python text mutators.
func AllPython() []TextMutator {
	return []TextMutator{
		PythonBoundary{},
		PythonConditional{},
		PythonExceptSwallow{},
		PythonSliceBounds{},
	}
}
