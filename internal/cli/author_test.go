// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/author"
	"github.com/spf13/cobra"
)

func TestRunAuthorPackWritesPack(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "idiomatic-go-bugs.json")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runAuthorPack(context.Background(), cmd, authorPackOptions{
		Repo:       repoPath,
		Output:     outPath,
		Title:      "10 idiomatic Go bugs",
		Count:      1,
		Difficulty: 1,
	})
	if err != nil {
		t.Fatalf("runAuthorPack returned error: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "wrote 1 kata(s)") {
		t.Fatalf("output = %q, want written count", out.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	var pack author.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("unmarshal pack: %v", err)
	}
	if pack.SchemaVersion != author.PackSchemaVersion {
		t.Fatalf("schema version = %q, want %q", pack.SchemaVersion, author.PackSchemaVersion)
	}
	if pack.Title != "10 idiomatic Go bugs" || pack.Language != "go" {
		t.Fatalf("pack metadata = %+v", pack)
	}
	if len(pack.Tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(pack.Tasks))
	}
	task := pack.Tasks[0]
	if task.Operator != "boundary" || task.FilePath != "calculator/calculator.go" || task.StartLine == 0 {
		t.Fatalf("task = %+v, want boundary mutation in calculator", task)
	}
	if task.MutationLog.Mutation.Original == "" || task.MutationLog.Mutation.Mutated == "" {
		t.Fatalf("pack task does not include source snapshots")
	}
	if task.MutationLog.RepoPath != repoPath || task.MutationLog.HeadSHA == "" {
		t.Fatalf("mutation log source metadata = %+v, want source path and head sha", task.MutationLog)
	}
}

func TestRunAuthorPackRequiresOutput(t *testing.T) {
	cmd := &cobra.Command{}
	err := runAuthorPack(context.Background(), cmd, authorPackOptions{Repo: "repo"})
	if err == nil || !strings.Contains(err.Error(), "--output is required") {
		t.Fatalf("error = %v, want output requirement", err)
	}
}

func TestRunAuthorPackRejectsPartialByDefault(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "rejected-pack.json")

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	err := runAuthorPack(context.Background(), cmd, authorPackOptions{
		Repo:       repoPath,
		Output:     outPath,
		Count:      8,
		Difficulty: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "requested 8") {
		t.Fatalf("error = %v, want partial-pack rejection", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected pack should not have been written, stat err = %v", statErr)
	}
}

func TestRunAuthorPackAllowsPartialPack(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "partial-pack.json")

	var out, errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := runAuthorPack(context.Background(), cmd, authorPackOptions{
		Repo:         repoPath,
		Output:       outPath,
		Count:        8,
		Difficulty:   1,
		AllowPartial: true,
	})
	if err != nil {
		t.Fatalf("runAuthorPack returned error: %v", err)
	}
	if !strings.Contains(errOut.String(), "wrote a partial pack") {
		t.Fatalf("stderr = %q, want partial-pack warning", errOut.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	var pack author.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("unmarshal pack: %v", err)
	}
	if len(pack.Tasks) == 0 || len(pack.Tasks) >= 8 {
		t.Fatalf("partial pack tasks = %d, want between 1 and 7", len(pack.Tasks))
	}
	if !strings.Contains(out.String(), fmt.Sprintf("wrote %d kata(s)", len(pack.Tasks))) {
		t.Fatalf("stdout = %q, want written count matching %d tasks", out.String(), len(pack.Tasks))
	}
}

func TestRunSenseiPublishWritesBriefedPack(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "sensei-kata.json")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runSenseiPublish(context.Background(), cmd, senseiPublishOptions{
		Repo:        repoPath,
		Output:      outPath,
		Title:       "Clamp review kata",
		Description: "The calculator lower-bound behavior regressed after a cleanup. Find the review bug.",
		Author:      "Platform Team",
		Difficulty:  1,
	})
	if err != nil {
		t.Fatalf("runSenseiPublish returned error: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "published") || !strings.Contains(out.String(), "?kata=") {
		t.Fatalf("output = %q, want publish summary and local open hint", out.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	var pack author.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("unmarshal pack: %v", err)
	}
	if pack.Author != "Platform Team" || len(pack.Tasks) != 1 {
		t.Fatalf("pack = %+v, want author and one task", pack)
	}
	if pack.Tasks[0].Brief == "" {
		t.Fatalf("task brief is empty")
	}
}

func TestRunSenseiPublishVetsBeforeWriting(t *testing.T) {
	repoPath := newSenseiVettedPublishFixture(t)
	outPath := filepath.Join(t.TempDir(), "sensei-kata.json")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runSenseiPublish(context.Background(), cmd, senseiPublishOptions{
		Repo:        repoPath,
		Output:      outPath,
		Title:       "Vetted boundary kata",
		Description: "A vetted boundary kata.",
		Difficulty:  1,
		Vet:         true,
		MaxAttempts: 8,
	})
	if err != nil {
		t.Fatalf("runSenseiPublish returned error: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "vet: 1 passed, 0 failed") {
		t.Fatalf("output = %q, want vet summary", out.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	var pack author.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("unmarshal pack: %v", err)
	}
	report, err := author.VetGeneratedPack(context.Background(), pack, author.VetOptions{})
	if err != nil {
		t.Fatalf("vet generated pack: %v", err)
	}
	if report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("vet report = %+v, want one passing vetted kata", report)
	}
}

func TestRunSenseiPublishVetFailsWithoutVettedCandidate(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "sensei-kata.json")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runSenseiPublish(context.Background(), cmd, senseiPublishOptions{
		Repo:        repoPath,
		Output:      outPath,
		Title:       "Unvetted kata",
		Description: "Should fail vetting.",
		Difficulty:  1,
		Vet:         true,
		MaxAttempts: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "no vetted authorable mutation tasks found") {
		t.Fatalf("error = %v, want no vetted task", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("output stat err = %v, want no written pack", statErr)
	}
}

func TestRunSenseiPlayScriptedSubmission(t *testing.T) {
	reviewTestSetup(t)
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "sensei-kata.json")

	var publishOut bytes.Buffer
	publishCmd := &cobra.Command{}
	publishCmd.SetOut(&publishOut)
	if err := runSenseiPublish(context.Background(), publishCmd, senseiPublishOptions{
		Repo:        repoPath,
		Output:      outPath,
		Title:       "Clamp review kata",
		Description: "A lower-bound cleanup changed calculator behavior. Find the review bug.",
		Difficulty:  1,
	}); err != nil {
		t.Fatalf("runSenseiPublish returned error: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("submit calculator/calculator.go:13 boundary comparison changed at the lower clamp check\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := runSenseiPlay(context.Background(), cmd, senseiPlayOptions{PackPath: outPath, Budget: 1}); err != nil {
		t.Fatalf("runSenseiPlay returned error: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Sensei kata ready",
		"A lower-bound cleanup changed calculator behavior",
		"calculator/calculator.go",
		"grading submission",
		"Result",
		"score:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunSenseiInspectShowsPackMetadata(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	outPath := filepath.Join(t.TempDir(), "sensei-kata.json")

	var publishOut bytes.Buffer
	publishCmd := &cobra.Command{}
	publishCmd.SetOut(&publishOut)
	if err := runSenseiPublish(context.Background(), publishCmd, senseiPublishOptions{
		Repo:        repoPath,
		Output:      outPath,
		Title:       "Clamp review kata",
		Description: "A lower-bound cleanup changed calculator behavior. Find the review bug.",
		Author:      "Platform Team",
		Difficulty:  1,
	}); err != nil {
		t.Fatalf("runSenseiPublish returned error: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	if err := runSenseiInspect(cmd, senseiInspectOptions{PackPath: outPath}); err != nil {
		t.Fatalf("runSenseiInspect returned error: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Sensei pack",
		"title: Clamp review kata",
		"author: Platform Team",
		"language: go",
		"tasks: 1",
		"kata-001",
		"file=calculator/calculator.go",
		"operator=boundary",
		"snapshots=ok",
		"Vet with: codedojo sensei vet --pack",
		"Play with: codedojo sensei play --pack",
		"Web link: http://localhost:8080/?kata=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunSenseiInspectRequiresPack(t *testing.T) {
	cmd := &cobra.Command{}
	err := runSenseiInspect(cmd, senseiInspectOptions{})
	if err == nil || !strings.Contains(err.Error(), "--pack is required") {
		t.Fatalf("error = %v, want pack requirement", err)
	}
}

func TestRunSenseiVetPassesPlayablePack(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	packPath := writeSenseiVetPack(t, repoPath, func(pack *author.Pack) {
		task := &pack.Tasks[0]
		original := task.MutationLog.Mutation.Original
		task.MutationLog.Mutation.Mutated = strings.Replace(original, "return value", "return min", 1)
	})

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	if err := runSenseiVet(context.Background(), cmd, senseiVetOptions{PackPath: packPath}); err != nil {
		t.Fatalf("runSenseiVet returned error: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Sensei vet",
		"summary: 1 passed, 0 failed, 1 total",
		"PASS kata-001",
		"baseline=true",
		"mutation_fail=true",
		"snapshots=true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunSenseiVetFailsWhenMutationDoesNotBreakTests(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	packPath := writeSenseiVetPack(t, repoPath, func(pack *author.Pack) {
		task := &pack.Tasks[0]
		task.MutationLog.Mutation.Mutated = task.MutationLog.Mutation.Original
	})

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runSenseiVet(context.Background(), cmd, senseiVetOptions{PackPath: packPath})
	if err == nil || !strings.Contains(err.Error(), "sensei vet failed") {
		t.Fatalf("error = %v, want vet failure; output:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"summary: 0 passed, 1 failed, 1 total",
		"FAIL kata-001",
		"mutation_fail=false",
		"mutated tests still passed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestSenseiVetCommandSuppressesUsageOnVetFailure(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	packPath := writeSenseiVetPack(t, repoPath, func(pack *author.Pack) {
		task := &pack.Tasks[0]
		task.MutationLog.Mutation.Mutated = task.MutationLog.Mutation.Original
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newSenseiVetCommand()
	cmd.SetArgs([]string{"--pack", packPath})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "sensei vet failed") {
		t.Fatalf("error = %v, want vet failure", err)
	}
	if strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("stderr included usage for runtime vet failure:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), "FAIL kata-001") {
		t.Fatalf("stdout missing vet report:\n%s", out.String())
	}
}

func TestRunSenseiVetReportsBaselineFailureForSelectedTasks(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	packPath := writeSenseiVetPack(t, repoPath, func(pack *author.Pack) {
		first := &pack.Tasks[0]
		first.MutationLog.Mutation.Mutated = strings.Replace(first.MutationLog.Mutation.Original, "return value", "return min", 1)
		second := pack.Tasks[0]
		second.ID = "kata-002"
		pack.Tasks = append(pack.Tasks, second)
	})
	writeLearnFile(t, repoPath, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestBrokenBaseline(t *testing.T) {\n\tt.Fatal(\"baseline broken\")\n}\n")
	runLearnGit(t, repoPath, "add", ".")
	runLearnGit(t, repoPath, "commit", "-m", "break baseline")
	repointPackHead(t, packPath, runLearnGit(t, repoPath, "rev-parse", "HEAD"))

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runSenseiVet(context.Background(), cmd, senseiVetOptions{PackPath: packPath, TaskID: "kata-002"})
	if err == nil || !strings.Contains(err.Error(), "sensei vet failed") {
		t.Fatalf("error = %v, want vet failure; output:\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "summary: 0 passed, 1 failed, 1 total") ||
		!strings.Contains(got, "FAIL kata-002") ||
		!strings.Contains(got, "baseline tests failed") {
		t.Fatalf("baseline failure output missing expected details:\n%s", got)
	}
	if strings.Contains(got, "kata-001") {
		t.Fatalf("baseline failure output included unselected task:\n%s", got)
	}
}

func TestRunSenseiVetFiltersTask(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	packPath := writeSenseiVetPack(t, repoPath, func(pack *author.Pack) {
		first := &pack.Tasks[0]
		first.MutationLog.Mutation.Mutated = strings.Replace(first.MutationLog.Mutation.Original, "return value", "return min", 1)
		second := pack.Tasks[0]
		second.ID = "kata-002"
		pack.Tasks = append(pack.Tasks, second)
	})

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	if err := runSenseiVet(context.Background(), cmd, senseiVetOptions{PackPath: packPath, TaskID: "kata-002"}); err != nil {
		t.Fatalf("runSenseiVet returned error: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "summary: 1 passed, 0 failed, 1 total") || !strings.Contains(got, "PASS kata-002") {
		t.Fatalf("filtered output missing selected kata:\n%s", got)
	}
	if strings.Contains(got, "kata-001") {
		t.Fatalf("filtered output included unselected task:\n%s", got)
	}
}

func TestRunSenseiVetReportsMissingTask(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	packPath := writeSenseiVetPack(t, repoPath, func(pack *author.Pack) {
		task := &pack.Tasks[0]
		task.MutationLog.Mutation.Mutated = strings.Replace(task.MutationLog.Mutation.Original, "return value", "return min", 1)
	})
	cmd := &cobra.Command{}
	err := runSenseiVet(context.Background(), cmd, senseiVetOptions{PackPath: packPath, TaskID: "missing"})
	if err == nil || !strings.Contains(err.Error(), `sensei task "missing" not found`) {
		t.Fatalf("error = %v, want missing task", err)
	}
}

func TestRunSenseiVetRequiresPack(t *testing.T) {
	cmd := &cobra.Command{}
	err := runSenseiVet(context.Background(), cmd, senseiVetOptions{})
	if err == nil || !strings.Contains(err.Error(), "--pack is required") {
		t.Fatalf("error = %v, want pack requirement", err)
	}
}

func TestRunSenseiPlayRequiresPack(t *testing.T) {
	reviewTestSetup(t)
	cmd := &cobra.Command{}
	err := runSenseiPlay(context.Background(), cmd, senseiPlayOptions{})
	if err == nil || !strings.Contains(err.Error(), "--pack is required") {
		t.Fatalf("error = %v, want pack requirement", err)
	}
}

func writeSenseiVetPack(t *testing.T, repoPath string, mutatePack func(*author.Pack)) string {
	t.Helper()
	return writeSenseiVetPackWithCount(t, repoPath, 1, mutatePack)
}

func writeSenseiVetPackWithCount(t *testing.T, repoPath string, count int, mutatePack func(*author.Pack)) string {
	t.Helper()
	pack, err := author.GeneratePack(context.Background(), author.PackOptions{
		Repo:       repoPath,
		Title:      "Clamp vet kata",
		Brief:      "Vet the lower-bound kata.",
		Count:      count,
		Difficulty: 1,
		Now:        fixedAuthorPackTime,
	})
	if err != nil {
		t.Fatalf("generate pack: %v", err)
	}
	mutatePack(&pack)
	packPath := filepath.Join(t.TempDir(), "sensei-vet.json")
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		t.Fatalf("marshal pack: %v", err)
	}
	if err := os.WriteFile(packPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}
	return packPath
}

func repointPackHead(t *testing.T, packPath, head string) {
	t.Helper()
	data, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	var pack author.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("unmarshal pack: %v", err)
	}
	pack.HeadSHA = strings.TrimSpace(head)
	data, err = json.MarshalIndent(pack, "", "  ")
	if err != nil {
		t.Fatalf("marshal pack: %v", err)
	}
	if err := os.WriteFile(packPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}
}

func fixedAuthorPackTime() time.Time {
	return time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
}

func newSenseiVettedPublishFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLearnFile(t, dir, "go.mod", "module example.com/senseivet\n\ngo 1.23\n")
	writeLearnFile(t, dir, "feature/feature.go", "package feature\n\nfunc OverLimit(value int) bool {\n\tif value > 10 {\n\t\treturn true\n\t}\n\treturn false\n}\n")
	writeLearnFile(t, dir, "feature/feature_test.go", "package feature\n\nimport \"testing\"\n\nfunc TestOverLimit(t *testing.T) {\n\tif !OverLimit(11) {\n\t\tt.Fatal(\"11 should be over the limit\")\n\t}\n\tif OverLimit(10) {\n\t\tt.Fatal(\"10 should not be over the limit\")\n\t}\n}\n")
	writeLearnFile(t, dir, "stable/stable.go", "package stable\n\nfunc OK() bool { return true }\n")
	writeLearnFile(t, dir, "stable/stable_test.go", "package stable\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {\n\tif !OK() {\n\t\tt.Fatal(\"not ok\")\n\t}\n}\n")
	initReviewFixtureGit(t, dir)
	return dir
}
