package newcomer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/modes/newcomer/history"
	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
)

func TestGenerateTaskRevertsRankedCommitAndStripsIdentifiers(t *testing.T) {
	dir, featureSHA := newTaskFixture(t)
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}

	task, err := GenerateTask(context.Background(), repo.Repo{Path: dir, Git: gitRepo}, 2)
	if err != nil {
		t.Fatalf("GenerateTask() error = %v", err)
	}
	if task.GroundTruthSHA != featureSHA {
		t.Fatalf("GroundTruthSHA = %q, want %q", task.GroundTruthSHA, featureSHA)
	}
	if task.StartingSHA == "" || task.StartingSHA == featureSHA {
		t.Fatalf("StartingSHA = %q, want parent", task.StartingSHA)
	}
	if !strings.Contains(task.FeatureDescription, "Recreate the requested calculator behavior: add multiplication.") {
		t.Fatalf("FeatureDescription = %q, want snapshot summary", task.FeatureDescription)
	}
	if len(task.SuggestedFiles) != 2 {
		t.Fatalf("SuggestedFiles = %#v, want source and test suggestions", task.SuggestedFiles)
	}
	if task.SuggestedFiles[0].Path != "calculator/calculator.go" || task.SuggestedFiles[0].Test {
		t.Fatalf("SuggestedFiles[0] = %#v, want source file first", task.SuggestedFiles[0])
	}
	if task.SuggestedFiles[1].Path != "calculator/calculator_test.go" || !task.SuggestedFiles[1].Test {
		t.Fatalf("SuggestedFiles[1] = %#v, want test file second", task.SuggestedFiles[1])
	}
	for _, banned := range task.BannedIdentifiers {
		if strings.Contains(strings.ToLower(task.FeatureDescription), strings.ToLower(banned)) {
			t.Fatalf("FeatureDescription %q leaked banned identifier %q", task.FeatureDescription, banned)
		}
	}
	if !contains(task.BannedIdentifiers, "Multiply") || !contains(task.BannedIdentifiers, "TestMultiply") {
		t.Fatalf("BannedIdentifiers = %#v, want introduced identifiers", task.BannedIdentifiers)
	}
	if fileContainsTask(t, filepath.Join(dir, "calculator/calculator.go"), "Multiply") {
		t.Fatalf("GenerateTask() left feature implementation in working tree")
	}
	if task.Candidate.SHA != featureSHA {
		t.Fatalf("Candidate.SHA = %q, want %q", task.Candidate.SHA, featureSHA)
	}
}

func TestGenerateTaskRejectsLeakySummary(t *testing.T) {
	dir, _ := newTaskFixture(t)
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}

	_, err = TaskGenerator{Summarizer: leakySummarizer{summary: "Call Multiply to implement the behavior."}}.
		GenerateTask(context.Background(), repo.Repo{Path: dir, Git: gitRepo}, 2)
	if err == nil || !strings.Contains(err.Error(), "leaked implementation detail") {
		t.Fatalf("GenerateTask() error = %v, want leak rejection", err)
	}
}

func TestDeterministicSummarizerFallsBackWhenIdentifierStrippingDestroysSummary(t *testing.T) {
	summary, err := (DeterministicSummarizer{}).Summarize(context.Background(), SummaryRequest{
		CommitMessage:     "fix: update graph view renderer",
		ReferenceDiff:     "+placeholder",
		ChangedFiles:      []history.ChangedFile{{Path: "internal/dashboard/view.go"}, {Path: "internal/dashboard/view_test.go", Test: true}},
		BannedIdentifiers: []string{"update", "graph", "view", "renderer"},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary != "Recreate the internal dashboard behavior exercised by the changed tests." {
		t.Fatalf("Summarize() = %q, want fallback", summary)
	}
}

func TestDeterministicSummarizerUsesCommitMessageFilesAndTests(t *testing.T) {
	summary, err := (DeterministicSummarizer{}).Summarize(context.Background(), SummaryRequest{
		CommitMessage: "feat(auth): allow expired invitations to be refreshed",
		ChangedFiles: []history.ChangedFile{
			{Path: "internal/auth/invitations.go"},
			{Path: "internal/auth/invitations_test.go", Test: true},
		},
		BannedIdentifiers: []string{"RefreshInvitation"},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	want := "Recreate the requested internal auth behavior: allow expired invitations to be refreshed. Use the changed tests as the acceptance criteria."
	if summary != want {
		t.Fatalf("Summarize() = %q, want %q", summary, want)
	}
}

func TestDeterministicSummarizerQualityForRealishMessagesAndPythonDiffs(t *testing.T) {
	tests := []struct {
		name       string
		req        SummaryRequest
		want       string
		notContain []string
	}{
		{
			name: "scoped fix subject keeps behavior and area",
			req: SummaryRequest{
				CommitMessage: "fix(auth): allow expired invitations to be refreshed",
				ChangedFiles: []history.ChangedFile{
					{Path: "internal/auth/invitations.go"},
					{Path: "internal/auth/invitations_test.go", Test: true},
				},
				BannedIdentifiers: []string{"RefreshInvitation", "InvitationRefresher"},
			},
			want:       "Recreate the requested internal auth behavior: allow expired invitations to be refreshed. Use the changed tests as the acceptance criteria.",
			notContain: []string{"RefreshInvitation", "InvitationRefresher", "fix(auth)"},
		},
		{
			name: "python diff avoids implementation identifiers",
			req: SummaryRequest{
				CommitMessage: "feat(billing): retry failed invoice charges after provider timeouts",
				ReferenceDiff: `diff --git a/billing/retries.py b/billing/retries.py
--- a/billing/retries.py
+++ b/billing/retries.py
@@ -1,2 +1,9 @@
+class StripeRetryPolicy:
+    def retry_failed_invoice_charge(invoice_id):
+        return calculate_retry_window(invoice_id)
diff --git a/tests/test_retries.py b/tests/test_retries.py
--- a/tests/test_retries.py
+++ b/tests/test_retries.py
@@ -1,2 +1,6 @@
+def test_retry_failed_invoice_charge():
+    assert retry_failed_invoice_charge("inv_123")
`,
				ChangedFiles: []history.ChangedFile{
					{Path: "billing/retries.py"},
					{Path: "tests/test_retries.py", Test: true},
				},
				BannedIdentifiers: []string{"StripeRetryPolicy", "retry_failed_invoice_charge", "calculate_retry_window"},
			},
			want:       "Recreate the requested billing behavior: retry failed invoice charges after provider timeouts. Use the changed tests as the acceptance criteria.",
			notContain: []string{"StripeRetryPolicy", "retry_failed_invoice_charge", "calculate_retry_window", "feat(billing)"},
		},
		{
			name: "fallback remains specific when subject is mostly stripped",
			req: SummaryRequest{
				CommitMessage: "feat: WidgetRenderer GraphWidget",
				ChangedFiles: []history.ChangedFile{
					{Path: "web/widgets/graph.py"},
					{Path: "tests/test_graph_widget.py", Test: true},
				},
				BannedIdentifiers: []string{"WidgetRenderer", "GraphWidget"},
			},
			want:       "Recreate the web widgets behavior exercised by the changed tests.",
			notContain: []string{"WidgetRenderer", "GraphWidget"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := (DeterministicSummarizer{}).Summarize(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Summarize() error = %v", err)
			}
			if summary != tt.want {
				t.Fatalf("Summarize() = %q, want %q", summary, tt.want)
			}
			for _, banned := range tt.notContain {
				if strings.Contains(strings.ToLower(summary), strings.ToLower(banned)) {
					t.Fatalf("Summarize() = %q leaked %q", summary, banned)
				}
			}
		})
	}
}

func TestIntroducedIdentifiersOnlyUsesAddedLines(t *testing.T) {
	diff := `diff --git a/calc.go b/calc.go
--- a/calc.go
+++ b/calc.go
@@ -1,3 +1,7 @@
 package calc
-func Add(a, b int) int { return a + b }
+func Multiply(a, b int) int { return a * b }
+func TestMultiply(t *testing.T) { t.Fatal("bad multiply") }
`
	got := IntroducedIdentifiers(diff)
	if !contains(got, "Multiply") || !contains(got, "TestMultiply") {
		t.Fatalf("IntroducedIdentifiers() = %#v, want added identifiers", got)
	}
	if contains(got, "Add") {
		t.Fatalf("IntroducedIdentifiers() = %#v, should not include deleted identifiers", got)
	}
}

func TestIntroducedIdentifiersIgnoresCommonWords(t *testing.T) {
	diff := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
+This should be visible when the graph has data.
+def build_graph():
+    return Graph()
`
	got := IntroducedIdentifiers(diff)
	for _, ignored := range []string{"be", "This", "when", "the", "has"} {
		if contains(got, ignored) {
			t.Fatalf("IntroducedIdentifiers() = %#v, should ignore common word %q", got, ignored)
		}
	}
	if !contains(got, "build_graph") || !contains(got, "Graph") {
		t.Fatalf("IntroducedIdentifiers() = %#v, want real identifiers", got)
	}
}

func TestAISummarizerParsesValidJSONFeedback(t *testing.T) {
	s := AISummarizer{
		Coach: aiSummarizerCoach{feedback: `0
{"summary": "Add a retry mechanism for failed invoice charges.", "behavior_signals": ["tests fail with timeout error"], "non_signals": ["unrelated logging change"]}`},
	}
	summary, err := s.Summarize(context.Background(), SummaryRequest{
		CommitMessage: "feat(billing): retry failed invoice charges",
		ChangedFiles:  []history.ChangedFile{{Path: "billing/retries.py"}},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	want := "Add a retry mechanism for failed invoice charges."
	if summary != want {
		t.Fatalf("Summarize() = %q, want %q", summary, want)
	}
}

func TestAISummarizerFallsBackWhenCoachReturnsError(t *testing.T) {
	s := AISummarizer{
		Coach:    aiSummarizerCoach{err: errors.New("backend unavailable")},
		Fallback: DeterministicSummarizer{},
	}
	summary, err := s.Summarize(context.Background(), SummaryRequest{
		CommitMessage: "feat(auth): allow expired invitations to be refreshed",
		ChangedFiles: []history.ChangedFile{
			{Path: "internal/auth/invitations.go"},
			{Path: "internal/auth/invitations_test.go", Test: true},
		},
		BannedIdentifiers: []string{"RefreshInvitation"},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v, want fallback success", err)
	}
	want := "Recreate the requested internal auth behavior: allow expired invitations to be refreshed. Use the changed tests as the acceptance criteria."
	if summary != want {
		t.Fatalf("Summarize() = %q, want %q", summary, want)
	}
}

func TestAISummarizerFallsBackWhenCoachIsNil(t *testing.T) {
	s := AISummarizer{
		Coach:    nil,
		Fallback: DeterministicSummarizer{},
	}
	summary, err := s.Summarize(context.Background(), SummaryRequest{
		CommitMessage: "fix(auth): allow expired invitations to be refreshed",
		ChangedFiles: []history.ChangedFile{
			{Path: "internal/auth/invitations.go"},
			{Path: "internal/auth/invitations_test.go", Test: true},
		},
		BannedIdentifiers: []string{"RefreshInvitation"},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v, want fallback success", err)
	}
	want := "Recreate the requested internal auth behavior: allow expired invitations to be refreshed. Use the changed tests as the acceptance criteria."
	if summary != want {
		t.Fatalf("Summarize() = %q, want %q", summary, want)
	}
}

func TestAISummarizerFallsBackWhenFeedbackIsNotJSON(t *testing.T) {
	s := AISummarizer{
		Coach:    aiSummarizerCoach{feedback: "0\nSure, here's a nice summary: The feature adds retry logic."},
		Fallback: DeterministicSummarizer{},
	}
	summary, err := s.Summarize(context.Background(), SummaryRequest{
		CommitMessage: "feat(billing): retry failed invoice charges",
		ChangedFiles:  []history.ChangedFile{{Path: "billing/retries.go"}, {Path: "billing/retries_test.go", Test: true}},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v, want fallback success", err)
	}
	expected := "Recreate the requested billing behavior: retry failed invoice charges. Use the changed tests as the acceptance criteria."
	if summary != expected {
		t.Fatalf("Summarize() = %q, want %q", summary, expected)
	}
}

func TestAISummarizerFallsBackWhenSummaryIsEmpty(t *testing.T) {
	s := AISummarizer{
		Coach:    aiSummarizerCoach{feedback: "0\n{\"summary\": \"\", \"behavior_signals\": [], \"non_signals\": []}"},
		Fallback: DeterministicSummarizer{},
	}
	summary, err := s.Summarize(context.Background(), SummaryRequest{
		CommitMessage: "feat(billing): retry failed invoice charges",
		ChangedFiles:  []history.ChangedFile{{Path: "billing/retries.go"}, {Path: "billing/retries_test.go", Test: true}},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v, want fallback success", err)
	}
	expected := "Recreate the requested billing behavior: retry failed invoice charges. Use the changed tests as the acceptance criteria."
	if summary != expected {
		t.Fatalf("Summarize() = %q, want %q", summary, expected)
	}
}

func TestParseAISummary(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name:    "valid full JSON",
			raw:     `{"summary":"Add retry logic.","behavior_signals":["timeout"],"non_signals":["logging"]}`,
			want:    "Add retry logic.",
			wantErr: false,
		},
		{
			name:    "JSON after score line",
			raw:     "0\n{\"summary\":\"Fix the boundary.\",\"behavior_signals\":[],\"non_signals\":[]}",
			want:    "Fix the boundary.",
			wantErr: false,
		},
		{
			name:    "empty string",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "no JSON object",
			raw:     "Here is a summary: add retry logic",
			wantErr: true,
		},
		{
			name:    "JSON missing summary key",
			raw:     `{"behavior_signals":[],"non_signals":[]}`,
			want:    "",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAISummary(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAISummary() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Summary != tt.want {
				t.Fatalf("parseAISummary().Summary = %q, want %q", got.Summary, tt.want)
			}
		})
	}
}

type leakySummarizer struct {
	summary string
}

func (s leakySummarizer) Summarize(context.Context, SummaryRequest) (string, error) {
	return s.summary, nil
}

func newTaskFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	writeTaskFile(t, dir, "go.mod", "module example.com/newcomer\n\ngo 1.23\n")
	writeTaskFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeTaskFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n")
	runTaskGit(t, dir, "init")
	runTaskGit(t, dir, "add", ".")
	runTaskGit(t, dir, "commit", "-m", "initial")

	writeTaskFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeTaskFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runTaskGit(t, dir, "add", ".")
	runTaskGit(t, dir, "commit", "-m", "add multiplication")
	return dir, runTaskGit(t, dir, "rev-parse", "HEAD")
}

func writeTaskFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runTaskGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=CodeDojo",
		"GIT_AUTHOR_EMAIL=codedojo@example.test",
		"GIT_COMMITTER_NAME=CodeDojo",
		"GIT_COMMITTER_EMAIL=codedojo@example.test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func fileContainsTask(t *testing.T, path, want string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Contains(string(data), want)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type aiSummarizerCoach struct {
	feedback string
	err      error
}

func (c aiSummarizerCoach) Hint(ctx context.Context, req coach.HintRequest) (coach.Hint, error) {
	return coach.Hint{}, nil
}

func (c aiSummarizerCoach) Grade(ctx context.Context, req coach.GradeRequest) (coach.Grade, error) {
	if c.err != nil {
		return coach.Grade{}, c.err
	}
	return coach.Grade{Score: 0, Feedback: c.feedback}, nil
}
