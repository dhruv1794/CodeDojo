// SPDX-License-Identifier: MIT

package reviewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/prompts"
	"github.com/dhruvmishra/codedojo/internal/coach/validator"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/session"
)

const (
	fileScore       = 50
	lineScore       = 30
	operatorScore   = 20
	maxDiagnosis    = 50
	diagnosisFile   = 10
	diagnosisLine   = 10
	diagnosisOp     = 10
	diagnosisMech   = 20
	forecastScore   = 15
	defaultTimeGoal = 15 * time.Minute
)

var (
	lineMentionPattern = regexp.MustCompile(`(?i)\b(?:line|lines|:|#)\s*(\d{1,6})(?:\s*[-–]\s*(\d{1,6}))?\b`)
	pathMentionPattern = regexp.MustCompile(`[\w./-]+\.[A-Za-z][\w]*`)
	mechanismCache     sync.Map
)

type Submission struct {
	FilePath      string
	StartLine     int
	EndLine       int
	OperatorClass string
	Diagnosis     string
	ForecastFile  string
	HintCosts     []int
	StartedAt     time.Time
	SubmittedAt   time.Time
	Streak        int
	SessionID     string
}

type GradeResult struct {
	Score int

	FileScore      int
	LineScore      int
	OperatorScore  int
	DiagnosisScore int
	ForecastScore  int
	HintDeduction  int
	TimeBonus      int
	StreakBonus    int

	DiagnosisFeedback string
	Commentary        string
	ReasoningTrace    string
}

type GradeOptions struct {
	Coach           coach.Coach
	CoachCommentary bool
	TimeBonusLimit  time.Duration
}

func Grade(ctx context.Context, submission Submission, log mutate.MutationLog, opts GradeOptions) (GradeResult, error) {
	result := GradeResult{}
	if samePath(submission.FilePath, log.Mutation.FilePath) {
		result.FileScore = fileScore
	}
	if lineMatches(submission.StartLine, submission.EndLine, log.Mutation.StartLine) {
		result.LineScore = lineScore
	}
	if sameOperator(submission.OperatorClass, log.Mutation.Operator) {
		result.OperatorScore = operatorScore
	}

	if samePath(submission.ForecastFile, log.Mutation.FilePath) {
		result.ForecastScore = forecastScore
	}

	diagnosis, err := gradeDiagnosis(ctx, submission, log, opts.Coach)
	if err != nil {
		return GradeResult{}, err
	}
	result.DiagnosisScore = clamp(diagnosis.Score, 0, maxDiagnosis)
	result.DiagnosisFeedback = diagnosis.Feedback

	for _, cost := range submission.HintCosts {
		if cost > 0 {
			result.HintDeduction += cost
		}
	}
	if !submission.StartedAt.IsZero() && !submission.SubmittedAt.IsZero() && submission.SubmittedAt.After(submission.StartedAt) {
		limit := opts.TimeBonusLimit
		if limit <= 0 {
			limit = defaultTimeGoal
		}
		result.TimeBonus = session.TimeBonus(submission.SubmittedAt.Sub(submission.StartedAt), limit)
	}

	base := result.FileScore + result.LineScore + result.OperatorScore + result.DiagnosisScore + result.ForecastScore - result.HintDeduction + result.TimeBonus
	if base < 0 {
		base = 0
	}
	withStreak := session.ApplyStreak(base, submission.Streak)
	result.StreakBonus = withStreak - base
	result.Score = withStreak
	if opts.CoachCommentary {
		result.Commentary = buildCommentary(ctx, submission, log, result, opts.Coach)
	} else {
		result.Commentary = deterministicCommentary(submission, log, result)
	}
	result.ReasoningTrace = deterministicReasoningTrace(submission, log, result)
	return result, nil
}

func GradeAny(ctx context.Context, submission Submission, logs []mutate.MutationLog, opts GradeOptions) (GradeResult, mutate.MutationLog, error) {
	if len(logs) == 0 {
		return GradeResult{}, mutate.MutationLog{}, fmt.Errorf("at least one mutation log is required")
	}
	bestLog := logs[0]
	best, err := Grade(ctx, submission, bestLog, GradeOptions{})
	if err != nil {
		return GradeResult{}, mutate.MutationLog{}, err
	}
	for _, log := range logs[1:] {
		candidate, err := Grade(ctx, submission, log, GradeOptions{})
		if err != nil {
			return GradeResult{}, mutate.MutationLog{}, err
		}
		if candidate.Score > best.Score {
			best = candidate
			bestLog = log
		}
	}
	result, err := Grade(ctx, submission, bestLog, opts)
	if err != nil {
		return GradeResult{}, mutate.MutationLog{}, err
	}
	return result, bestLog, nil
}

func buildCommentary(ctx context.Context, submission Submission, log mutate.MutationLog, result GradeResult, grader coach.Coach) string {
	fallback := deterministicCommentary(submission, log, result)
	if grader == nil {
		return fallback
	}
	grade, err := grader.Grade(ctx, coach.GradeRequest{
		SessionID: submission.SessionID,
		Rubric: strings.Join([]string{
			"Write a concise after-action commentary for a completed CodeDojo reviewer kata.",
			"Use two short paragraphs.",
			"Paragraph one: explain what the mutation did and why it affected behavior.",
			"Paragraph two: describe the real-world bug pattern and one investigation habit to carry forward.",
			"This is after grading, so revealing the mutation is allowed.",
			fmt.Sprintf("Mutation: %s at %s:%d. %s", log.Mutation.Operator, log.Mutation.FilePath, log.Mutation.StartLine, log.Mutation.Description),
			fmt.Sprintf("Difficulty profile: %s", commentaryProfile(log).Summary),
			fmt.Sprintf("Score: %d. Diagnosis: %s", result.Score, submission.Diagnosis),
		}, "\n"),
		Answer: submission.Diagnosis,
	})
	if err != nil || strings.TrimSpace(grade.Feedback) == "" {
		return fallback
	}
	return strings.TrimSpace(grade.Feedback)
}

func deterministicCommentary(submission Submission, log mutate.MutationLog, result GradeResult) string {
	mutation := log.Mutation
	profile := commentaryProfile(log)
	description := strings.TrimSpace(mutation.Description)
	if description == "" {
		description = "changed the selected expression"
	}
	first := fmt.Sprintf("The hidden mutation was `%s` in `%s:%d`: %s. Its difficulty profile is %s: locality %d/3, subtlety %d/3, knowledge %d/3.",
		mutation.Operator,
		mutation.FilePath,
		mutation.StartLine,
		description,
		profile.Summary,
		profile.Locality.Score,
		profile.Subtlety.Score,
		profile.Knowledge.Score,
	)
	second := fmt.Sprintf("Your diagnosis scored %d/%d and the final kata score was %d. The transferable pattern is to tie the failing behavior back to one invariant, then verify the exact file, line, and operator before changing code.",
		result.DiagnosisScore,
		maxDiagnosis,
		result.Score,
	)
	if strings.TrimSpace(submission.Diagnosis) == "" {
		second = fmt.Sprintf("The final kata score was %d. The transferable pattern is to write down the invariant first, then verify the exact file, line, and operator before changing code.", result.Score)
	}
	return first + "\n\n" + second
}

func commentaryProfile(log mutate.MutationLog) mutate.Profile {
	if log.Profile.Summary != "" {
		return log.Profile
	}
	if log.Mutation.Profile.Summary != "" {
		return log.Mutation.Profile
	}
	return mutate.ProfileDifficulty(log.Mutation)
}

func deterministicReasoningTrace(submission Submission, log mutate.MutationLog, result GradeResult) string {
	mutation := log.Mutation
	profile := commentaryProfile(log)
	steps := []string{
		"1. Start from the failing behavior and state the invariant it violates before reading broadly.",
	}
	if strings.TrimSpace(submission.ForecastFile) != "" {
		steps = append(steps, fmt.Sprintf("2. Compare the first forecast `%s` with the actual mutation file `%s` to calibrate the search path.", submission.ForecastFile, mutation.FilePath))
	} else {
		steps = append(steps, fmt.Sprintf("2. Make an early file forecast, then check it against the actual mutation file `%s` after grading.", mutation.FilePath))
	}
	steps = append(steps,
		fmt.Sprintf("3. Inspect `%s` around line %d and ask whether the `%s` change alters the invariant only at an edge case.", mutation.FilePath, mutation.StartLine, mutation.Operator),
		fmt.Sprintf("4. Let the profile guide attention: locality %d/3, subtlety %d/3, knowledge %d/3 means this kata was primarily a %s practice target.", profile.Locality.Score, profile.Subtlety.Score, profile.Knowledge.Score, profile.Summary),
		fmt.Sprintf("5. Before editing, verify the diagnosis with one focused test or diff check; this diagnosis scored %d/%d.", result.DiagnosisScore, maxDiagnosis),
	)
	return strings.Join(steps, "\n")
}

func gradeDiagnosis(ctx context.Context, submission Submission, log mutate.MutationLog, grader coach.Coach) (coach.Grade, error) {
	extracted := extractDiagnosisEntities(submission.Diagnosis, log)
	score := 0
	parts := make([]string, 0, 4)
	if extracted.File {
		score += diagnosisFile
		parts = append(parts, "file")
	}
	if extracted.Line {
		score += diagnosisLine
		parts = append(parts, "line")
	}
	if extracted.Operator {
		score += diagnosisOp
		parts = append(parts, "operator")
	}
	mechanism, err := gradeMechanism(ctx, submission, log, grader, extracted.Mechanism)
	if err != nil {
		return coach.Grade{}, err
	}
	if mechanism.Score > 0 {
		parts = append(parts, "mechanism")
	}
	score += clamp(mechanism.Score, 0, diagnosisMech)

	feedback := mechanism.Feedback
	if feedback == "" {
		if len(parts) == 0 {
			feedback = "No concrete diagnosis evidence was found."
		} else {
			feedback = "Detected diagnosis evidence: " + strings.Join(parts, ", ") + "."
		}
	}
	return coach.Grade{Score: score, Feedback: feedback}, nil
}

type diagnosisEntities struct {
	File      bool
	Line      bool
	Operator  bool
	Mechanism int
}

func extractDiagnosisEntities(text string, log mutate.MutationLog) diagnosisEntities {
	normalized := strings.ToLower(text)
	return diagnosisEntities{
		File:      diagnosisMentionsFile(text, log.Mutation.FilePath),
		Line:      diagnosisMentionsLine(text, log.Mutation.StartLine),
		Operator:  diagnosisMentionsOperator(normalized, log.Mutation.Operator),
		Mechanism: deterministicMechanismScore(normalized, log),
	}
}

func diagnosisMentionsFile(text, want string) bool {
	want = cleanPath(want)
	if want == "." || want == "" {
		return false
	}
	lower := strings.ToLower(filepath.ToSlash(text))
	if strings.Contains(lower, strings.ToLower(want)) {
		return true
	}
	base := strings.ToLower(filepath.Base(want))
	if base == "." || base == "" {
		return false
	}
	for _, mention := range pathMentionPattern.FindAllString(lower, -1) {
		if filepath.Base(filepath.ToSlash(mention)) == base {
			return true
		}
	}
	return false
}

func diagnosisMentionsLine(text string, actual int) bool {
	if actual <= 0 {
		return false
	}
	for _, match := range lineMentionPattern.FindAllStringSubmatch(text, -1) {
		start, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		end := start
		if match[2] != "" {
			if parsed, err := strconv.Atoi(match[2]); err == nil {
				end = parsed
			}
		}
		if lineMatches(start, end, actual) {
			return true
		}
	}
	return false
}

func diagnosisMentionsOperator(normalized, operator string) bool {
	operator = strings.ToLower(strings.TrimSpace(operator))
	if operator == "" {
		return false
	}
	for _, alias := range operatorAliases(operator) {
		if strings.Contains(normalized, alias) {
			return true
		}
	}
	return false
}

func operatorAliases(operator string) []string {
	switch operator {
	case "boundary":
		return []string{"boundary", "comparison", "comparator", "off-by-one", "range check", "limit", "threshold"}
	case "conditional":
		return []string{"conditional", "condition", "branch", "predicate", "inverted", "negated"}
	case "error-drop":
		return []string{"error-drop", "error drop", "swallow", "ignored error", "dropped error", "exception"}
	case "slice-bounds":
		return []string{"slice-bounds", "slice bounds", "slice", "index", "range"}
	case "pagination-window":
		return []string{"pagination-window", "pagination window", "pagination", "window", "page", "off-by-one", "upper bound"}
	case "js-strict-equality":
		return []string{"js-strict-equality", "strict equality", "loose equality", "coercion", "coercive", "===", "==", "type coercion"}
	case "race-lock-drop":
		return []string{"race-lock-drop", "race", "lock", "mutex", "critical section", "concurrency", "data race", "unlock"}
	default:
		return []string{operator, strings.ReplaceAll(operator, "-", " ")}
	}
}

func deterministicMechanismScore(normalized string, log mutate.MutationLog) int {
	hits := 0
	for _, token := range mechanismTokens(log) {
		if mechanismTokenMatches(normalized, token) {
			hits++
		}
	}
	switch {
	case hits >= 3:
		return diagnosisMech
	case hits == 2:
		return diagnosisMech / 2
	case hits == 1:
		return diagnosisMech / 4
	default:
		return 0
	}
}

func mechanismTokenMatches(normalized, token string) bool {
	if strings.Contains(normalized, token) {
		return true
	}
	for _, suffix := range []string{"ed", "ing", "d"} {
		if strings.HasSuffix(token, suffix) {
			stem := strings.TrimSuffix(token, suffix)
			if len(stem) >= 4 && strings.Contains(normalized, stem) {
				return true
			}
		}
	}
	return false
}

func mechanismTokens(log mutate.MutationLog) []string {
	seen := map[string]bool{}
	var tokens []string
	add := func(values ...string) {
		for _, value := range values {
			for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
				return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
			}) {
				if len(token) < 4 || seen[token] {
					continue
				}
				seen[token] = true
				tokens = append(tokens, token)
			}
		}
	}
	add(log.Mutation.Description, log.Mutation.Operator)
	for _, alias := range operatorAliases(strings.ToLower(log.Mutation.Operator)) {
		add(alias)
	}
	return tokens
}

func gradeMechanism(ctx context.Context, submission Submission, log mutate.MutationLog, grader coach.Coach, fallback int) (coach.Grade, error) {
	if strings.TrimSpace(submission.Diagnosis) == "" {
		return coach.Grade{Feedback: "No diagnosis was provided."}, nil
	}
	if grader == nil {
		return coach.Grade{Score: fallback}, nil
	}
	key := mechanismCacheKey(log, submission.Diagnosis)
	if cached, ok := mechanismCache.Load(key); ok {
		return cached.(coach.Grade), nil
	}
	rubric, err := prompts.Render("reviewer/grade_diagnosis.tmpl", map[string]any{
		"MaxScore":     diagnosisMech,
		"MutationFile": log.Mutation.FilePath,
		"MutationLine": log.Mutation.StartLine,
		"Operator":     log.Mutation.Operator,
		"Description":  log.Mutation.Description,
		"Diagnosis":    submission.Diagnosis,
	})
	if err != nil {
		return coach.Grade{}, fmt.Errorf("render reviewer diagnosis prompt: %w", err)
	}
	grade, err := grader.Grade(ctx, coach.GradeRequest{
		SessionID: submission.SessionID,
		Rubric:    rubric,
		Answer:    submission.Diagnosis,
	})
	if err != nil {
		return coach.Grade{}, fmt.Errorf("grade reviewer diagnosis mechanism: %w", err)
	}
	grade.Score = clamp(grade.Score, 0, diagnosisMech)
	banned := []string{
		log.Mutation.Operator,
		log.Mutation.Original,
		log.Mutation.Mutated,
	}
	if grade.Feedback != "" {
		if r := validator.Validate(grade.Feedback, banned); !r.OK {
			return coach.Grade{}, fmt.Errorf("diagnosis feedback failed validator: %s", r.Reason)
		}
	}
	mechanismCache.Store(key, grade)
	return grade, nil
}

func mechanismCacheKey(log mutate.MutationLog, diagnosis string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(diagnosis))))
	return strings.Join([]string{
		cleanPath(log.Mutation.FilePath),
		strconv.Itoa(log.Mutation.StartLine),
		strings.ToLower(log.Mutation.Operator),
		log.Mutation.Description,
		hex.EncodeToString(hash[:]),
	}, "\x00")
}

func samePath(got, want string) bool {
	return cleanPath(got) == cleanPath(want)
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	return filepath.ToSlash(filepath.Clean(path))
}

func lineMatches(start, end, actual int) bool {
	if actual <= 0 || start <= 0 {
		return false
	}
	if end <= 0 {
		end = start
	}
	if end < start {
		start, end = end, start
	}
	return actual >= start-2 && actual <= end+2
}

func sameOperator(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func clamp(n, low, high int) int {
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
}
