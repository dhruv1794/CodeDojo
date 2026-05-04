package textop

import (
	"fmt"
	"regexp"
	"strings"
)

// --- Rust: comparison boundary ---

// RustBoundary flips comparison operators: < ↔ <=, > ↔ >=.
type RustBoundary struct{}

func (RustBoundary) Name() string     { return "rs-boundary" }
func (RustBoundary) Language() string { return "rust" }
func (RustBoundary) Difficulty() int  { return 1 }

var rsBoundaryRe = regexp.MustCompile(`([^<>!-])(<|>)([^=\n/])`)

func (RustBoundary) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		for _, m := range rsBoundaryRe.FindAllStringSubmatch(line, -1) {
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

func (RustBoundary) Apply(content string, site Site) (string, error) {
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

// --- Rust: Option/Result branch inversion ---

// RustOptionInvert inverts an `if let Some(x) = ...` to `if let None = ...` or
// flips `is_some()` to `is_none()`.
type RustOptionInvert struct{}

func (RustOptionInvert) Name() string     { return "rs-option-invert" }
func (RustOptionInvert) Language() string { return "rust" }
func (RustOptionInvert) Difficulty() int  { return 2 }

var rsSomeRe = regexp.MustCompile(`\bis_some\(\)`)
var rsNoneRe = regexp.MustCompile(`\bis_none\(\)`)
var rsIsOkRe = regexp.MustCompile(`\bis_ok\(\)`)
var rsIsErrRe = regexp.MustCompile(`\bis_err\(\)`)

func (RustOptionInvert) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		switch {
		case rsSomeRe.MatchString(line):
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("invert is_some() to is_none() on line %d", i+1),
				Metadata:    map[string]string{"op": "is_some"},
			})
		case rsNoneRe.MatchString(line):
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("invert is_none() to is_some() on line %d", i+1),
				Metadata:    map[string]string{"op": "is_none"},
			})
		case rsIsOkRe.MatchString(line):
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("invert is_ok() to is_err() on line %d", i+1),
				Metadata:    map[string]string{"op": "is_ok"},
			})
		case rsIsErrRe.MatchString(line):
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("invert is_err() to is_ok() on line %d", i+1),
				Metadata:    map[string]string{"op": "is_err"},
			})
		}
	}
	return sites
}

func (RustOptionInvert) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	switch site.Metadata["op"] {
	case "is_some":
		line = rsSomeRe.ReplaceAllString(line, "is_none()")
	case "is_none":
		line = rsNoneRe.ReplaceAllString(line, "is_some()")
	case "is_ok":
		line = rsIsOkRe.ReplaceAllString(line, "is_err()")
	case "is_err":
		line = rsIsErrRe.ReplaceAllString(line, "is_ok()")
	default:
		return "", fmt.Errorf("unknown op %q", site.Metadata["op"])
	}
	lines[site.StartLine-1] = line
	return strings.Join(lines, "\n"), nil
}

// --- Rust: range bound mutation ---

// RustRangeBound flips exclusive to inclusive range: `0..n` ↔ `0..=n`.
type RustRangeBound struct{}

func (RustRangeBound) Name() string     { return "rs-range-bound" }
func (RustRangeBound) Language() string { return "rust" }
func (RustRangeBound) Difficulty() int  { return 2 }

var rsRangeExclusiveRe = regexp.MustCompile(`(\d+|[A-Za-z_][A-Za-z0-9_]*)\.\.([A-Za-z_][A-Za-z0-9_]*|\d+)`)
var rsRangeInclusiveRe = regexp.MustCompile(`(\d+|[A-Za-z_][A-Za-z0-9_]*)\.\.=([A-Za-z_][A-Za-z0-9_]*|\d+)`)

func (RustRangeBound) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if rsRangeInclusiveRe.MatchString(line) {
			m := rsRangeInclusiveRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("flip inclusive range ..= to exclusive .. on line %d", i+1),
				Metadata:    map[string]string{"kind": "inclusive", "from": m[1], "to": m[2]},
			})
		} else if rsRangeExclusiveRe.MatchString(line) {
			m := rsRangeExclusiveRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("flip exclusive range .. to inclusive ..= on line %d", i+1),
				Metadata:    map[string]string{"kind": "exclusive", "from": m[1], "to": m[2]},
			})
		}
	}
	return sites
}

func (RustRangeBound) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	line := lines[site.StartLine-1]
	from := site.Metadata["from"]
	to := site.Metadata["to"]
	switch site.Metadata["kind"] {
	case "inclusive":
		old := from + "..=" + to
		line = strings.Replace(line, old, from+".."+to, 1)
	case "exclusive":
		old := from + ".." + to
		line = strings.Replace(line, old, from+"..="+to, 1)
	default:
		return "", fmt.Errorf("unknown kind %q", site.Metadata["kind"])
	}
	lines[site.StartLine-1] = line
	return strings.Join(lines, "\n"), nil
}

// --- Rust: error propagation weakening ---

// RustErrPropagation replaces `?` error propagation with `.unwrap()`.
type RustErrPropagation struct{}

func (RustErrPropagation) Name() string     { return "rs-err-propagation" }
func (RustErrPropagation) Language() string { return "rust" }
func (RustErrPropagation) Difficulty() int  { return 3 }

var rsQuestionRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_()]*)\?`)

func (RustErrPropagation) Candidates(content string) []Site {
	lines := strings.Split(content, "\n")
	var sites []Site
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if rsQuestionRe.MatchString(line) {
			m := rsQuestionRe.FindStringSubmatch(line)
			sites = append(sites, Site{
				StartLine:   i + 1,
				EndLine:     i + 1,
				Description: fmt.Sprintf("weaken error propagation: replace ? with .unwrap() for %s on line %d", m[1], i+1),
				Metadata:    map[string]string{"expr": m[1]},
			})
		}
	}
	return sites
}

func (RustErrPropagation) Apply(content string, site Site) (string, error) {
	lines := strings.Split(content, "\n")
	if site.StartLine < 1 || site.StartLine > len(lines) {
		return "", fmt.Errorf("line %d out of range", site.StartLine)
	}
	expr := site.Metadata["expr"]
	old := expr + "?"
	new_ := expr + ".unwrap()"
	lines[site.StartLine-1] = strings.Replace(lines[site.StartLine-1], old, new_, 1)
	return strings.Join(lines, "\n"), nil
}

// AllRust returns all Rust text mutators.
func AllRust() []TextMutator {
	return []TextMutator{
		RustBoundary{},
		RustOptionInvert{},
		RustRangeBound{},
		RustErrPropagation{},
	}
}
