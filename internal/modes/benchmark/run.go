// SPDX-License-Identifier: MIT

package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/author"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

const ResultsSchemaVersion = "codedojo.benchmark.results.v1"

type RunOptions struct {
	PackPath string
	Timeout  time.Duration
	Now      func() time.Time
}

type Results struct {
	SchemaVersion string       `json:"schema_version"`
	PackTitle     string       `json:"pack_title"`
	PackPath      string       `json:"pack_path"`
	Source        string       `json:"source"`
	Language      string       `json:"language"`
	HeadSHA       string       `json:"head_sha,omitempty"`
	StartedAt     time.Time    `json:"started_at"`
	FinishedAt    time.Time    `json:"finished_at"`
	Total         int          `json:"total"`
	Passed        int          `json:"passed"`
	Failed        int          `json:"failed"`
	Tasks         []TaskResult `json:"tasks"`
}

type TaskResult struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Operator    string        `json:"operator"`
	Difficulty  int           `json:"difficulty"`
	FilePath    string        `json:"file_path"`
	StartLine   int           `json:"start_line"`
	EndLine     int           `json:"end_line"`
	TestCommand []string      `json:"test_command"`
	ExitCode    int           `json:"exit_code"`
	Duration    time.Duration `json:"duration"`
	Passed      bool          `json:"passed"`
	Error       string        `json:"error,omitempty"`
}

func Run(ctx context.Context, opts RunOptions) (Results, error) {
	if opts.PackPath == "" {
		return Results{}, fmt.Errorf("pack path is required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	started := time.Now().UTC()
	if opts.Now != nil {
		started = opts.Now().UTC()
	}
	pack, err := ReadPack(opts.PackPath)
	if err != nil {
		return Results{}, err
	}
	if pack.SchemaVersion != author.PackSchemaVersion {
		return Results{}, fmt.Errorf("unsupported pack schema %q", pack.SchemaVersion)
	}
	results := Results{
		SchemaVersion: ResultsSchemaVersion,
		PackTitle:     pack.Title,
		PackPath:      opts.PackPath,
		Source:        pack.Source,
		Language:      pack.Language,
		HeadSHA:       pack.HeadSHA,
		StartedAt:     started,
		Tasks:         make([]TaskResult, 0, len(pack.Tasks)),
	}
	for _, task := range pack.Tasks {
		result := runTask(ctx, pack, task, opts.Timeout)
		results.Tasks = append(results.Tasks, result)
		results.Total++
		if result.Passed {
			results.Passed++
		} else {
			results.Failed++
		}
	}
	results.FinishedAt = time.Now().UTC()
	if opts.Now != nil {
		results.FinishedAt = opts.Now().UTC()
	}
	return results, nil
}

func ReadPack(path string) (author.Pack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return author.Pack{}, fmt.Errorf("read benchmark pack: %w", err)
	}
	var pack author.Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return author.Pack{}, fmt.Errorf("parse benchmark pack: %w", err)
	}
	return pack, nil
}

func runTask(ctx context.Context, pack author.Pack, task author.PackTask, timeout time.Duration) (result TaskResult) {
	result = TaskResult{
		ID:         task.ID,
		Title:      task.Title,
		Operator:   task.Operator,
		Difficulty: task.Difficulty,
		FilePath:   task.FilePath,
		StartLine:  task.StartLine,
		EndLine:    task.EndLine,
		ExitCode:   -1,
	}
	start := time.Now()
	defer func() {
		result.Duration = time.Since(start).Round(time.Millisecond)
		if result.Duration == 0 {
			result.Duration = time.Millisecond
		}
	}()
	loaded, err := repo.OpenLocal(pack.Source)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer os.RemoveAll(loaded.Path)
	mutated := task.MutationLog.Mutation.Mutated
	if mutated == "" {
		result.Error = "pack task missing mutated source snapshot"
		return result
	}
	target := filepath.Join(loaded.Path, filepath.FromSlash(task.FilePath))
	if err := os.WriteFile(target, []byte(mutated), 0o600); err != nil {
		result.Error = fmt.Sprintf("write mutated source: %v", err)
		return result
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(lang.TestCmd) == 0 {
		result.Error = "no test command detected"
		return result
	}
	result.TestCommand = lang.TestCmd
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- runs the repository's detected test command by design.
	cmd := exec.CommandContext(runCtx, lang.TestCmd[0], lang.TestCmd[1:]...)
	cmd.Dir = loaded.Path
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			result.Error = runCtx.Err().Error()
			return result
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result
		}
		result.Error = err.Error()
		return result
	}
	result.ExitCode = 0
	result.Passed = true
	return result
}
