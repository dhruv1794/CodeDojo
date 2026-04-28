package validator

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

type Result struct {
	OK     bool
	Reason string
}

var functionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*func\s+(\([^)]+\)\s*)?[A-Za-z_][A-Za-z0-9_]*\s*\(`),
	regexp.MustCompile(`(?m)^\s*def\s+[A-Za-z_][A-Za-z0-9_]*\s*\(`),
	regexp.MustCompile(`(?m)^\s*(export\s+)?(async\s+)?function\s+[A-Za-z_$][A-Za-z0-9_$]*\s*\(`),
	regexp.MustCompile(`(?m)^\s*(const|let|var)\s+[A-Za-z_$][A-Za-z0-9_$]*\s*=\s*(async\s*)?\([^)]*\)\s*=>`),
}

func Validate(content string, bannedIdentifiers []string) Result {
	if hasLongFence(content) {
		return Result{OK: false, Reason: "contains a fenced code block longer than 3 non-empty lines"}
	}
	for _, pattern := range functionPatterns {
		if pattern.MatchString(content) {
			return Result{OK: false, Reason: "contains what looks like a function definition"}
		}
	}
	lower := strings.ToLower(content)
	for _, ident := range bannedIdentifiers {
		ident = strings.TrimSpace(ident)
		if ident == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(ident)) {
			return Result{OK: false, Reason: fmt.Sprintf("contains banned identifier %q", ident)}
		}
	}
	return Result{OK: true}
}

func hasLongFence(content string) bool {
	inFence := false
	nonEmpty := 0
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(trimmed, "```") {
			if inFence && nonEmpty > 3 {
				return true
			}
			inFence = !inFence
			nonEmpty = 0
			continue
		}
		if inFence && trimmed != "" {
			nonEmpty++
		}
	}
	return inFence && nonEmpty > 3
}
