// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/author"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/sandbox/local"
	"github.com/dhruvmishra/codedojo/internal/session"
)

func TestServiceReviewFlow(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceReviewFixture(t)

	live, err := service.StartReview(ctx, StartOptions{Repo: repoPath, Difficulty: 1, HintBudget: 1})
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if live.Mode != "reviewer" {
		t.Fatalf("mode = %s, want reviewer", live.Mode)
	}
	diff, err := service.Diff(live.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "(no local edits)\n" {
		t.Fatalf("diff = %q, want no local edits", diff)
	}
	hint, err := service.Hint(ctx, live.ID, 0)
	if err != nil {
		t.Fatalf("hint: %v", err)
	}
	if hint.HintsUsed != 1 || hint.Hint == "" {
		t.Fatalf("hint result = %+v, want one non-empty hint", hint)
	}
	if _, err := service.ReadFile(live.ID, "calculator/calculator.go"); err != nil {
		t.Fatalf("read file: %v", err)
	}
	if _, err := service.RunTests(ctx, live.ID); err != nil {
		t.Fatalf("run tests: %v", err)
	}

	result, err := service.SubmitReview(ctx, live.ID, ReviewSubmission{
		FilePath:      "calculator/calculator.go",
		StartLine:     13,
		EndLine:       13,
		OperatorClass: "boundary",
		Diagnosis:     "boundary comparison changed at the lower clamp check",
	})
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if result.Breakdown["file"] != 50 || result.Breakdown["line"] != 30 || result.Breakdown["operator"] != 20 {
		t.Fatalf("breakdown = %+v, want deterministic localization scores", result.Breakdown)
	}
	if result.Commentary == "" {
		t.Fatalf("commentary is empty")
	}
	if result.ReasoningTrace == "" {
		t.Fatalf("reasoning trace is empty")
	}
	if !live.Done {
		t.Fatalf("session was not marked done")
	}
	events, err := service.store.ListEvents(ctx, live.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	eventTypes := make([]session.EventType, 0, len(events))
	for _, event := range events {
		eventTypes = append(eventTypes, event.Type)
	}
	for _, want := range []session.EventType{session.EventDiff, session.EventHint, session.EventFile, session.EventTests, session.EventSubmit, session.EventGrade, session.EventCommentary, session.EventTrace} {
		if !containsEventType(eventTypes, want) {
			t.Fatalf("event types = %v, want %s", eventTypes, want)
		}
	}
}

func TestServiceReviewCandidateFiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceReviewCandidateFixture(t, 5)

	live, err := service.StartReview(ctx, StartOptions{Repo: repoPath, Difficulty: 1, HintBudget: 1, CandidateCount: 5})
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if len(live.TaskFiles) != 5 {
		t.Fatalf("TaskFiles = %#v, want five reviewer candidates", live.TaskFiles)
	}
	selected := live.reviewTask.MutationLog.Mutation.FilePath
	foundSelected := false
	for _, file := range live.TaskFiles {
		if file.Path == selected {
			foundSelected = true
		}
		if !strings.Contains(file.Reason, "Candidate source file") {
			t.Fatalf("candidate reason = %q, want candidate wording", file.Reason)
		}
	}
	if !foundSelected {
		t.Fatalf("TaskFiles = %#v, want selected mutation file %q included", live.TaskFiles, selected)
	}
	if err := service.CloseSession(ctx, live.ID); err != nil {
		t.Fatalf("close session: %v", err)
	}
}

func TestServiceReviewCompoundMutations(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceReviewCandidateFixture(t, 3)

	live, err := service.StartReview(ctx, StartOptions{Repo: repoPath, Difficulty: 1, HintBudget: 1, CandidateCount: 3, MutationCount: 2})
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if len(live.reviewTask.MutationLogs) != 2 {
		t.Fatalf("MutationLogs = %#v, want two mutations", live.reviewTask.MutationLogs)
	}
	logs, err := service.store.ListMutationLogs(ctx, live.ID)
	if err != nil {
		t.Fatalf("list mutation logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("stored mutation logs = %#v, want two", logs)
	}
	second := live.reviewTask.MutationLogs[1].Mutation
	result, err := service.SubmitReview(ctx, live.ID, ReviewSubmission{
		FilePath:      second.FilePath,
		StartLine:     second.StartLine,
		EndLine:       second.EndLine,
		OperatorClass: second.Operator,
		Diagnosis:     fmt.Sprintf("%s:%d boundary comparison changed in this clamp", second.FilePath, second.StartLine),
	})
	if err != nil {
		t.Fatalf("submit second mutation: %v", err)
	}
	if result.Breakdown["file"] != 50 || result.Breakdown["line"] != 30 || result.Breakdown["operator"] != 20 {
		t.Fatalf("breakdown = %+v, want localization credit for second mutation", result.Breakdown)
	}
	if result.Reveal["file"] != second.FilePath {
		t.Fatalf("reveal file = %q, want matched second mutation %q", result.Reveal["file"], second.FilePath)
	}
}

func TestServiceReviewSameFlowCompound(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceReviewSameFlowFixture(t)

	live, err := service.StartReview(ctx, StartOptions{Repo: repoPath, Difficulty: 3, HintBudget: 1, MutationCount: 2, CompoundMode: "same-flow"})
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if live.reviewTask.CompoundMode != "same-flow" {
		t.Fatalf("CompoundMode = %q, want same-flow", live.reviewTask.CompoundMode)
	}
	if len(live.reviewTask.MutationLogs) != 2 {
		t.Fatalf("MutationLogs = %#v, want two same-flow mutations", live.reviewTask.MutationLogs)
	}
	firstFile := live.reviewTask.MutationLogs[0].Mutation.FilePath
	for _, log := range live.reviewTask.MutationLogs {
		if log.Mutation.FilePath != firstFile {
			t.Fatalf("mutation log file = %q, want same-flow file %q", log.Mutation.FilePath, firstFile)
		}
	}
	if !strings.Contains(live.Task, "same code path") {
		t.Fatalf("Task = %q, want same-flow instructions", live.Task)
	}
	if err := service.CloseSession(ctx, live.ID); err != nil {
		t.Fatalf("close session: %v", err)
	}
}

func TestServiceStartSenseiUsesAuthoredPack(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceReviewFixture(t)
	pack, err := author.GeneratePack(ctx, author.PackOptions{
		Repo:       repoPath,
		Title:      "Clamp sensei kata",
		Brief:      "A lower-bound cleanup changed calculator behavior. Review the kata and identify the bug.",
		Count:      1,
		Difficulty: 1,
	})
	if err != nil {
		t.Fatalf("generate pack: %v", err)
	}
	packPath := filepath.Join(t.TempDir(), "sensei.json")
	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal pack: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	live, err := service.StartSensei(ctx, SenseiStartOptions{PackPath: packPath})
	if err != nil {
		t.Fatalf("start sensei: %v", err)
	}
	if live.Mode != session.ModeReviewer {
		t.Fatalf("mode = %s, want reviewer", live.Mode)
	}
	if !strings.Contains(live.Task, "lower-bound cleanup") {
		t.Fatalf("task = %q, want authored brief", live.Task)
	}
	if len(live.TaskFiles) != 1 || live.TaskFiles[0].Path != pack.Tasks[0].FilePath {
		t.Fatalf("task files = %#v, want authored mutation file", live.TaskFiles)
	}
	diff, err := service.Diff(live.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "(no local edits)\n" {
		t.Fatalf("diff = %q, want hidden baseline", diff)
	}
	logs, err := service.store.ListMutationLogs(ctx, live.ID)
	if err != nil {
		t.Fatalf("list mutation logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Mutation.FilePath != pack.Tasks[0].FilePath {
		t.Fatalf("logs = %#v, want authored mutation log", logs)
	}
	if err := service.CloseSession(ctx, live.ID); err != nil {
		t.Fatalf("close session: %v", err)
	}
}

func TestServiceLearnFlow(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable in PATH")
	}
	ctx := context.Background()
	service := newTestService(t)
	repoPath := newServiceLearnFixture(t)

	live, err := service.StartLearn(ctx, StartOptions{Repo: repoPath, Difficulty: 2, HintBudget: 1, CommitRange: "HEAD~1..HEAD"})
	if err != nil {
		t.Fatalf("start learn: %v", err)
	}
	if live.CommitRange != "HEAD~1..HEAD" {
		t.Fatalf("CommitRange = %q, want HEAD~1..HEAD", live.CommitRange)
	}
	if live.Task == "" {
		t.Fatalf("task description is empty")
	}
	if len(live.TaskFiles) != 2 {
		t.Fatalf("TaskFiles = %#v, want suggested source and test files", live.TaskFiles)
	}
	if live.TaskFiles[0].Path != "calculator/calculator.go" || live.TaskFiles[0].Test {
		t.Fatalf("TaskFiles[0] = %#v, want source suggestion", live.TaskFiles[0])
	}
	implementation := strings.Join([]string{
		"package calculator",
		"",
		"func Add(a, b int) int {",
		"\treturn a + b",
		"}",
		"",
		"func Multiply(a, b int) int {",
		"\tresult := 0",
		"\tfor i := 0; i < b; i++ {",
		"\t\tresult = Add(result, a)",
		"\t}",
		"\treturn result",
		"}",
		"",
	}, "\n")
	if err := service.WriteFile(live.ID, "calculator/calculator.go", implementation); err != nil {
		t.Fatalf("write file: %v", err)
	}
	result, err := service.SubmitLearn(ctx, live.ID)
	if err != nil {
		t.Fatalf("submit learn: %v", err)
	}
	if result.Breakdown["correctness"] != 100 {
		t.Fatalf("correctness = %d, want 100; result = %+v", result.Breakdown["correctness"], result)
	}
	if result.Reveal["reference_diff"] == "" {
		t.Fatalf("reference diff was not exposed")
	}
}

func containsEventType(types []session.EventType, want session.EventType) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

func TestHideMutationLogRemovesOnlyCodeDojoLog(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	codeDojoDir := filepath.Join(repoPath, ".codedojo")
	if err := os.MkdirAll(codeDojoDir, 0o755); err != nil {
		t.Fatalf("mkdir .codedojo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codeDojoDir, "mutation-log.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write mutation log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codeDojoDir, "user-state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write user state: %v", err)
	}

	if err := hideMutationLog(repoPath); err != nil {
		t.Fatalf("hideMutationLog() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(codeDojoDir, "mutation-log.json")); !os.IsNotExist(err) {
		t.Fatalf("mutation log stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(codeDojoDir, "user-state.json")); err != nil {
		t.Fatalf("user state was removed: %v", err)
	}
}

func TestHideMutationLogRemovesEmptyCodeDojoDir(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	codeDojoDir := filepath.Join(repoPath, ".codedojo")
	if err := os.MkdirAll(codeDojoDir, 0o755); err != nil {
		t.Fatalf("mkdir .codedojo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codeDojoDir, "mutation-log.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write mutation log: %v", err)
	}

	if err := hideMutationLog(repoPath); err != nil {
		t.Fatalf("hideMutationLog() error = %v", err)
	}
	if _, err := os.Stat(codeDojoDir); !os.IsNotExist(err) {
		t.Fatalf(".codedojo stat err = %v, want not exist", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.StorePath = filepath.Join(t.TempDir(), "codedojo.db")
	service, err := NewService(context.Background(), cfg, local.Driver{}, func(repoPath string) sandbox.Spec {
		return sandbox.Spec{
			RepoMount: repoPath,
			Network:   sandbox.NetworkNone,
			Timeout:   2 * time.Minute,
		}
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func newServiceLearnFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeServiceFile(t, dir, "go.mod", "module example.com/learn\n\ngo 1.23\n")
	writeServiceFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeServiceFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n")
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial")

	writeServiceFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeServiceFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "add multiplication")
	return dir
}

func newServiceReviewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	copyServiceSampleFile(t, dir, "go.mod")
	copyServiceSampleFile(t, dir, "calculator/calculator.go")
	copyServiceSampleFile(t, dir, "calculator/calculator_test.go")
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial review fixture")
	return dir
}

func newServiceReviewCandidateFixture(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	writeServiceFile(t, dir, "go.mod", "module example.com/candidates\n\ngo 1.23\n")
	for i := 1; i <= count; i++ {
		pkg := fmt.Sprintf("pkg%d", i)
		writeServiceFile(t, dir, filepath.Join(pkg, pkg+".go"), fmt.Sprintf("package %s\n\nfunc Clamp%d(value, min int) int {\n\tif value < min {\n\t\treturn min\n\t}\n\treturn value\n}\n", pkg, i))
		writeServiceFile(t, dir, filepath.Join(pkg, pkg+"_test.go"), fmt.Sprintf("package %s\n\nimport \"testing\"\n\nfunc TestClamp%d(t *testing.T) {}\n", pkg, i))
	}
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial review candidate fixture")
	return dir
}

func newServiceReviewSameFlowFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeServiceFile(t, dir, "go.mod", "module example.com/sameflow\n\ngo 1.23\n")
	writeServiceFile(t, dir, "pager/pager.go", strings.Join([]string{
		"package pager",
		"",
		"func Page(values []int, offset, limit int) []int {",
		"\tif limit < 0 {",
		"\t\treturn nil",
		"\t}",
		"\treturn values[offset : offset+limit]",
		"}",
	}, "\n")+"\n")
	writeServiceFile(t, dir, "pager/pager_test.go", "package pager\n\nimport \"testing\"\n\nfunc TestPage(t *testing.T) {}\n")
	runServiceGit(t, dir, "init")
	runServiceGit(t, dir, "add", ".")
	runServiceGit(t, dir, "commit", "-m", "initial same-flow fixture")
	return dir
}

func copyServiceSampleFile(t *testing.T, root, rel string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sample-go-repo", rel))
	if err != nil {
		t.Fatalf("read sample fixture file: %v", err)
	}
	writeServiceFile(t, root, rel, string(data))
}

func writeServiceFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runServiceGit(t *testing.T, dir string, args ...string) string {
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
