package newcomer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
	"github.com/dhruvmishra/codedojo/internal/coach/validator"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer/history"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer/revert"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

const defaultTaskScanLimit = 100

type Task struct {
	RepoPath           string
	Difficulty         int
	FeatureDescription string
	SuggestedFiles     []SuggestedFile
	GroundTruthSHA     string
	StartingSHA        string
	ReferenceDiff      string
	Candidate          history.CommitCandidate
	BannedIdentifiers  []string
	Instructions       string
}

type SummaryRequest struct {
	CommitMessage     string
	ReferenceDiff     string
	ChangedFiles      []history.ChangedFile
	BannedIdentifiers []string
}

type SuggestedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Test   bool   `json:"test,omitempty"`
}

type Summarizer interface {
	Summarize(ctx context.Context, req SummaryRequest) (string, error)
}

type TaskGenerator struct {
	Summarizer Summarizer
	ScanLimit  int
}

type DeterministicSummarizer struct{}

type AISummarizer struct {
	Coach    coach.Coach
	Fallback Summarizer
}

func GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	return TaskGenerator{}.GenerateTask(ctx, r, difficulty)
}

func (g TaskGenerator) GenerateTask(ctx context.Context, r repo.Repo, difficulty int) (Task, error) {
	limit := g.ScanLimit
	if limit <= 0 {
		limit = defaultTaskScanLimit
	}
	candidates, err := history.Scan(ctx, r, limit)
	if err != nil {
		return Task{}, err
	}
	ranked := history.Rank(candidates)
	if len(ranked) == 0 {
		return Task{}, fmt.Errorf("no suitable newcomer commits found")
	}
	candidate := selectCandidate(ranked, difficulty)
	state, err := revert.Revert(ctx, r, candidate.SHA)
	if err != nil {
		return Task{}, err
	}
	banned := IntroducedIdentifiers(state.ReferenceDiff)
	summarizer := g.Summarizer
	if summarizer == nil {
		summarizer = DeterministicSummarizer{}
	}
	description, err := summarizer.Summarize(ctx, SummaryRequest{
		CommitMessage:     state.CommitMessage,
		ReferenceDiff:     state.ReferenceDiff,
		ChangedFiles:      candidate.Files,
		BannedIdentifiers: banned,
	})
	if err != nil {
		return Task{}, err
	}
	if result := validator.Validate(description, banned); !result.OK {
		return Task{}, fmt.Errorf("newcomer task summary leaked implementation detail: %s", result.Reason)
	}
	return Task{
		RepoPath:           r.Path,
		Difficulty:         difficulty,
		FeatureDescription: description,
		SuggestedFiles:     SuggestedFiles(candidate.Files),
		GroundTruthSHA:     state.GroundTruthSHA,
		StartingSHA:        state.StartingSHA,
		ReferenceDiff:      state.ReferenceDiff,
		Candidate:          candidate,
		BannedIdentifiers:  banned,
		Instructions:       "Reimplement the described behavior, then submit when the original tests pass.",
	}, nil
}

func (DeterministicSummarizer) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := prompts.Render("newcomer/summarize.tmpl", map[string]any{
		"CommitMessage":     firstLine(req.CommitMessage),
		"ReferenceDiff":     req.ReferenceDiff,
		"BannedIdentifiers": strings.Join(req.BannedIdentifiers, ", "),
	}); err != nil {
		return "", fmt.Errorf("render newcomer summary prompt: %w", err)
	}
	summary := summarizeCommitMessage(req.CommitMessage, req.ChangedFiles)
	summary = removeBannedIdentifiers(summary, req.BannedIdentifiers)
	summary = strings.TrimSpace(summary)
	if needsFallbackSummary(summary) {
		summary = fallbackSummary(req.ChangedFiles)
		summary = removeBannedIdentifiers(summary, req.BannedIdentifiers)
	}
	if needsFallbackSummary(summary) {
		summary = "Recreate the behavior exercised by the changed tests."
	}
	return summary, nil
}

func (s AISummarizer) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Coach == nil {
		fallback := s.Fallback
		if fallback == nil {
			fallback = DeterministicSummarizer{}
		}
		return fallback.Summarize(ctx, req)
	}
	system, err := prompts.Render("newcomer/summarize.tmpl", map[string]any{
		"CommitMessage":     firstLine(req.CommitMessage),
		"ReferenceDiff":     req.ReferenceDiff,
		"BannedIdentifiers": strings.Join(req.BannedIdentifiers, ", "),
	})
	if err != nil {
		return "", fmt.Errorf("render newcomer summary prompt: %w", err)
	}
	system += "\n\nRespond with ONLY a JSON object (no markdown, no code fences) with these keys:\n" +
		"- \"summary\": a one-to-two-sentence human-readable task description\n" +
		"- \"behavior_signals\": observable behaviors the learner should look for in test output\n" +
		"- \"non_signals\": aspects of the diff that do NOT matter for this task\n\n" +
		"Example: {\"summary\":\"...\",\"behavior_signals\":[\"tests fail with...\"],\"non_signals\":[\"unrelated refactoring\"]}"
	grade, err := s.Coach.Grade(ctx, coach.GradeRequest{
		Rubric: system,
		Answer: "",
	})
	if err != nil {
		fallback := s.Fallback
		if fallback == nil {
			fallback = DeterministicSummarizer{}
		}
		return fallback.Summarize(ctx, req)
	}
	parsed, err := parseAISummary(grade.Feedback)
	if err != nil {
		fallback := s.Fallback
		if fallback == nil {
			fallback = DeterministicSummarizer{}
		}
		return fallback.Summarize(ctx, req)
	}
	summary := strings.TrimSpace(parsed.Summary)
	if summary == "" || needsFallbackSummary(summary) {
		fallback := s.Fallback
		if fallback == nil {
			fallback = DeterministicSummarizer{}
		}
		return fallback.Summarize(ctx, req)
	}
	return summary, nil
}

type aiSummarizeOutput struct {
	Summary         string   `json:"summary"`
	BehaviorSignals []string `json:"behavior_signals"`
	NonSignals      []string `json:"non_signals"`
}

func parseAISummary(feedback string) (aiSummarizeOutput, error) {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return aiSummarizeOutput{}, fmt.Errorf("empty feedback")
	}
	start := strings.Index(feedback, "{")
	if start < 0 {
		return aiSummarizeOutput{}, fmt.Errorf("no JSON object found in feedback")
	}
	decoder := json.NewDecoder(strings.NewReader(feedback[start:]))
	decoder.DisallowUnknownFields()
	var out aiSummarizeOutput
	if err := decoder.Decode(&out); err != nil {
		return aiSummarizeOutput{}, fmt.Errorf("parse AI summary JSON: %w", err)
	}
	return out, nil
}

func SuggestedFiles(files []history.ChangedFile) []SuggestedFile {
	out := make([]SuggestedFile, 0, len(files))
	for _, file := range files {
		if file.Path == "" || isDependencyOnlyFile(file.Path) {
			continue
		}
		reason := "Changed source file from the selected historical task."
		if file.Test {
			reason = "Changed test file that describes expected behavior."
		}
		out = append(out, SuggestedFile{Path: file.Path, Reason: reason, Test: file.Test})
	}
	slices.SortFunc(out, func(a, b SuggestedFile) int {
		if a.Test != b.Test {
			if a.Test {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

func IntroducedIdentifiers(diff string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		for _, ident := range identifierPattern.FindAllString(line[1:], -1) {
			if shouldIgnoreIdentifier(ident) {
				continue
			}
			seen[ident] = true
		}
	}
	out := make([]string, 0, len(seen))
	for ident := range seen {
		out = append(out, ident)
	}
	slices.Sort(out)
	return out
}

func shouldIgnoreIdentifier(ident string) bool {
	if len(ident) < 3 {
		return true
	}
	if goKeywords[ident] {
		return true
	}
	return commonWords[strings.ToLower(ident)]
}

func selectCandidate(ranked []history.CommitCandidate, difficulty int) history.CommitCandidate {
	if len(ranked) == 1 {
		return ranked[0]
	}
	switch {
	case difficulty <= 1:
		return ranked[len(ranked)-1]
	case difficulty >= 5:
		return ranked[0]
	default:
		index := (5 - difficulty) * (len(ranked) - 1) / 4
		return ranked[index]
	}
}

func summarizeCommitMessage(message string, files []history.ChangedFile) string {
	line := strings.TrimSpace(firstLine(message))
	line = strings.TrimSuffix(line, ".")
	if line == "" {
		return ""
	}
	line = stripCommitPrefix(line)
	if line == "" {
		return ""
	}
	area := taskArea(files)
	if area != "" {
		return fmt.Sprintf("Recreate the requested %s behavior: %s. Use the changed tests as the acceptance criteria.", area, line)
	}
	return fmt.Sprintf("Recreate the requested behavior: %s. Use the changed tests as the acceptance criteria.", line)
}

func removeBannedIdentifiers(summary string, banned []string) string {
	out := summary
	for _, ident := range banned {
		ident = strings.TrimSpace(ident)
		if ident == "" {
			continue
		}
		out = regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(ident)+`\b`).ReplaceAllString(out, "the feature")
	}
	return strings.Join(strings.Fields(out), " ")
}

func needsFallbackSummary(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return true
	}
	words := strings.Fields(summary)
	if len(words) < 6 {
		return true
	}
	return strings.Count(strings.ToLower(summary), "the feature") > 1
}

func fallbackSummary(files []history.ChangedFile) string {
	area := taskArea(files)
	if area == "" {
		return "Recreate the behavior exercised by the changed tests."
	}
	return fmt.Sprintf("Recreate the %s behavior exercised by the changed tests.", area)
}

func stripCommitPrefix(line string) string {
	lower := strings.ToLower(line)
	for _, prefix := range conventionalCommitPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	if close := strings.Index(line, ")"); close > 0 && close+1 < len(line) && line[close+1] == ':' {
		kind := lower[:strings.Index(lower, "(")+1]
		for _, prefix := range conventionalCommitPrefixes {
			if strings.TrimSuffix(prefix, ":")+"(" == kind {
				return strings.TrimSpace(line[close+2:])
			}
		}
	}
	return strings.TrimSpace(line)
}

func taskArea(files []history.ChangedFile) string {
	for _, file := range files {
		if file.Test || file.Path == "" || isDependencyOnlyFile(file.Path) {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(file.Path))
		switch dir {
		case ".", "":
			return ""
		default:
			return humanizePath(dir)
		}
	}
	return ""
}

func humanizePath(path string) string {
	path = strings.Trim(path, "/.")
	if path == "" {
		return ""
	}
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '-' || r == '_' || r == '.'
	})
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func isDependencyOnlyFile(path string) bool {
	switch filepath.ToSlash(path) {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "pyproject.toml", "poetry.lock", "requirements.txt":
		return true
	default:
		return false
	}
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(value, "\n")
	return strings.TrimSpace(line)
}

var identifierPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)

var conventionalCommitPrefixes = []string{"feat:", "feature:", "fix:", "bugfix:", "chore:", "refactor:", "test:", "tests:"}

var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

var commonWords = map[string]bool{
	// Articles, conjunctions, prepositions
	"and": true, "are": true, "but": true, "can": true, "for": true, "from": true,
	"has": true, "have": true, "into": true, "not": true, "the": true, "this": true,
	"that": true, "then": true, "there": true, "these": true, "those": true, "with": true,
	"when": true, "where": true, "will": true, "your": true,
	// Common English verbs that double as identifiers in code
	"add": true, "get": true, "set": true, "put": true, "run": true, "use": true,
	"let": true, "try": true, "see": true, "was": true, "had": true, "did": true,
	"via": true, "may": true, "got": true,
	// Common adjectives / determiners
	"all": true, "any": true, "new": true, "old": true, "big": true, "bad": true,
	"raw": true, "low": true, "top": true, "sub": true, "due": true, "per": true,
	// Common nouns / programming words used as identifiers
	"name": true, "file": true, "path": true, "data": true, "text": true,
	"code": true, "time": true, "size": true, "user": true, "item": true,
	"list": true, "node": true, "next": true, "done": true, "info": true,
	"args": true, "main": true, "make": true, "only": true, "some": true,
	// High-frequency identifier stems that block natural descriptions
	"error": true, "result": true, "value": true, "count": true, "index": true,
	"limit": true, "total": true, "input": true, "output": true, "check": true,
	"start": true, "stop":  true, "close": true, "open":  true, "read": true,
	"write": true, "send":  true, "handle": true, "process": true, "create": true,
	"update": true, "delete": true, "remove": true, "insert": true, "load": true,
	"save": true, "parse": true, "format": true, "print": true, "build": true,
	"fetch": true, "apply": true, "reset": true, "clear": true, "flush": true,
	"init": true, "setup": true, "clean": true, "move": true, "copy": true,
	"sort": true, "find": true, "filter": true, "map": true, "each": true,
	"step": true, "stage": true, "state": true, "event": true, "field": true,
	"type": true, "mode": true, "rate": true, "base": true, "body": true,
	"response": true, "request": true, "message": true, "status": true,
	// Common language builtins / keywords that leak into diffs across languages
	"test": true, "function": true, "require": true, "module": true, "exports": true,
	"assert": true, "expect": true, "describe": true, "spec": true,
	// Short variable names that are also real English words (3+ chars, common in code)
	"err": true, "res": true, "req": true, "ctx": true, "val": true, "key": true,
	"msg": true, "buf": true, "tmp": true, "idx": true, "ptr": true, "ref": true,
	"str": true, "num": true, "len": true, "cap": true, "sum": true, "avg": true,
	"now": true, "end": true, "log": true, "ok": true, "max": true, "min": true,
}
