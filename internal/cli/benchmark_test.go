// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/benchmark"
	"github.com/spf13/cobra"
)

func TestRunBenchmarkWritesResults(t *testing.T) {
	repoPath := newGoReviewFixture(t)
	tmp := t.TempDir()
	packPath := filepath.Join(tmp, "pack.json")
	resultsPath := filepath.Join(tmp, "results.json")

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	if err := runAuthorPack(context.Background(), cmd, authorPackOptions{
		Repo:       repoPath,
		Output:     packPath,
		Title:      "benchmark fixture",
		Count:      1,
		Difficulty: 1,
	}); err != nil {
		t.Fatalf("runAuthorPack returned error: %v", err)
	}

	var out bytes.Buffer
	cmd = &cobra.Command{}
	cmd.SetOut(&out)
	err := runBenchmark(context.Background(), cmd, benchmarkRunOptions{
		PackPath: packPath,
		Output:   resultsPath,
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatalf("runBenchmark returned error: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ran 1 kata(s)") {
		t.Fatalf("output = %q, want benchmark count", out.String())
	}

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var results benchmark.Results
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if results.SchemaVersion != benchmark.ResultsSchemaVersion {
		t.Fatalf("schema = %q, want %q", results.SchemaVersion, benchmark.ResultsSchemaVersion)
	}
	if results.Total != 1 || len(results.Tasks) != 1 {
		t.Fatalf("results = %+v, want one task", results)
	}
	task := results.Tasks[0]
	if task.ID != "kata-001" || task.Operator != "boundary" || len(task.TestCommand) == 0 {
		t.Fatalf("task result = %+v, want boundary task with test command", task)
	}
	if task.ExitCode < 0 || task.Duration <= 0 {
		t.Fatalf("task execution = %+v, want recorded exit code and duration", task)
	}
}

func TestRunBenchmarkRequiresPackAndOutput(t *testing.T) {
	cmd := &cobra.Command{}
	err := runBenchmark(context.Background(), cmd, benchmarkRunOptions{})
	if err == nil || !strings.Contains(err.Error(), "--pack is required") {
		t.Fatalf("error = %v, want pack requirement", err)
	}
	err = runBenchmark(context.Background(), cmd, benchmarkRunOptions{PackPath: "pack.json"})
	if err == nil || !strings.Contains(err.Error(), "--output is required") {
		t.Fatalf("error = %v, want output requirement", err)
	}
}
