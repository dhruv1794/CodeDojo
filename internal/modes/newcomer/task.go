package newcomer

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

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
	BannedIdentifiers []string
}

type Summarizer interface {
	Summarize(ctx context.Context, req SummaryRequest) (string, error)
}

type TaskGenerator struct {
	Summarizer Summarizer
	ScanLimit  int
}

type PromptSummarizer struct{}

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
		summarizer = PromptSummarizer{}
	}
	description, err := summarizer.Summarize(ctx, SummaryRequest{
		CommitMessage:     state.CommitMessage,
		ReferenceDiff:     state.ReferenceDiff,
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
		GroundTruthSHA:     state.GroundTruthSHA,
		StartingSHA:        state.StartingSHA,
		ReferenceDiff:      state.ReferenceDiff,
		Candidate:          candidate,
		BannedIdentifiers:  banned,
		Instructions:       "Reimplement the described behavior, then submit when the original tests pass.",
	}, nil
}

func (PromptSummarizer) Summarize(ctx context.Context, req SummaryRequest) (string, error) {
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
	summary := summarizeCommitMessage(req.CommitMessage)
	summary = removeBannedIdentifiers(summary, req.BannedIdentifiers)
	summary = strings.TrimSpace(summary)
	if summary == "" || strings.Count(summary, "the feature") > 2 {
		summary = "Add the user-visible behavior covered by the original tests."
	}
	return summary, nil
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

func summarizeCommitMessage(message string) string {
	line := strings.TrimSpace(firstLine(message))
	line = strings.TrimSuffix(line, ".")
	if line == "" {
		return ""
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "feat:"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "feature:"))
	if line == "" {
		return ""
	}
	return upperFirst(line) + " behavior covered by the original tests."
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

func firstLine(value string) string {
	line, _, _ := strings.Cut(value, "\n")
	return strings.TrimSpace(line)
}

func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

var identifierPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)

var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

var commonWords = map[string]bool{
	"and": true, "are": true, "but": true, "can": true, "for": true, "from": true,
	"has": true, "have": true, "into": true, "not": true, "the": true, "this": true,
	"that": true, "then": true, "there": true, "these": true, "those": true, "with": true,
	"when": true, "where": true, "will": true, "your": true,
}
