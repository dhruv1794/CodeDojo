// SPDX-License-Identifier: MIT

package author

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/fsutil"
	"github.com/dhruvmishra/codedojo/internal/repo"
)

type VetOptions struct {
	PackPath string
	TaskID   string
	Timeout  time.Duration
}

type VetReport struct {
	PackTitle   string
	PackPath    string
	Source      string
	HeadSHA     string
	Language    string
	TestCommand []string
	Total       int
	Passed      int
	Failed      int
	Checks      []VetCheck
}

type VetCheck struct {
	TaskID       string
	FilePath     string
	Operator     string
	BaselinePass bool
	MutationFail bool
	SnapshotsOK  bool
	Passed       bool
	Error        string
}

func VetPack(ctx context.Context, opts VetOptions) (VetReport, error) {
	if opts.PackPath == "" {
		return VetReport{}, fmt.Errorf("pack path is required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	pack, err := ReadPack(opts.PackPath)
	if err != nil {
		return VetReport{}, err
	}
	if pack.SchemaVersion != PackSchemaVersion {
		return VetReport{}, fmt.Errorf("unsupported pack schema %q", pack.SchemaVersion)
	}
	return VetGeneratedPack(ctx, pack, opts)
}

func VetGeneratedPack(ctx context.Context, pack Pack, opts VetOptions) (VetReport, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	report := VetReport{
		PackTitle: pack.Title,
		PackPath:  opts.PackPath,
		Source:    pack.Source,
		HeadSHA:   pack.HeadSHA,
		Language:  pack.Language,
		Total:     len(pack.Tasks),
		Checks:    make([]VetCheck, 0, len(pack.Tasks)),
	}
	report.TestCommand = detectPackTestCommand(pack)
	tasks, err := vetTasks(pack.Tasks, opts.TaskID)
	if err != nil {
		return VetReport{}, err
	}
	report.Total = len(tasks)
	baseline, err := prepareVetBaseline(ctx, pack, opts.Timeout)
	if err != nil {
		report.Failed = len(tasks)
		for _, task := range tasks {
			report.Checks = append(report.Checks, failedBaselineCheck(task, err))
		}
		return report, nil
	}
	defer os.RemoveAll(baseline.Path)
	report.Language = baseline.Language
	report.TestCommand = baseline.TestCommand
	for _, task := range tasks {
		check := vetTask(ctx, baseline, task, opts.Timeout)
		report.Checks = append(report.Checks, check)
		if check.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report, nil
}

type vetBaseline struct {
	Path        string
	Language    string
	TestCommand []string
}

func prepareVetBaseline(ctx context.Context, pack Pack, timeout time.Duration) (vetBaseline, error) {
	loaded, err := repo.OpenLocal(pack.Source)
	if err != nil {
		return vetBaseline{}, fmt.Errorf("open source repo: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(loaded.Path)
		}
	}()
	if err := checkoutCommit(loaded, pack.HeadSHA); err != nil {
		return vetBaseline{}, err
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return vetBaseline{}, err
	}
	if len(lang.TestCmd) == 0 {
		return vetBaseline{}, fmt.Errorf("no test command detected")
	}
	baseline, err := runTestCommand(ctx, loaded.Path, lang.TestCmd, timeout)
	if err != nil {
		return vetBaseline{}, err
	}
	if baseline != 0 {
		return vetBaseline{}, fmt.Errorf("baseline tests failed with exit code %d", baseline)
	}
	cleanup = false
	return vetBaseline{Path: loaded.Path, Language: lang.Name, TestCommand: lang.TestCmd}, nil
}

func failedBaselineCheck(task PackTask, err error) VetCheck {
	mutation := task.MutationLog.Mutation
	return VetCheck{
		TaskID:       task.ID,
		FilePath:     task.FilePath,
		Operator:     task.Operator,
		BaselinePass: false,
		MutationFail: false,
		SnapshotsOK:  mutation.Original != "" && mutation.Mutated != "",
		Error:        err.Error(),
	}
}

func vetTasks(tasks []PackTask, taskID string) ([]PackTask, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return tasks, nil
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return []PackTask{task}, nil
		}
	}
	return nil, fmt.Errorf("sensei task %q not found", taskID)
}

func vetTask(ctx context.Context, baseline vetBaseline, task PackTask, timeout time.Duration) VetCheck {
	check := VetCheck{
		TaskID:   task.ID,
		FilePath: task.FilePath,
		Operator: task.Operator,
	}
	mutation := task.MutationLog.Mutation
	check.SnapshotsOK = mutation.Original != "" && mutation.Mutated != ""
	if !check.SnapshotsOK {
		check.Error = "missing original or mutated source snapshot"
		return check
	}
	workdir, err := os.MkdirTemp("", "codedojo-sensei-vet-*")
	if err != nil {
		check.Error = fmt.Sprintf("create vet temp dir: %v", err)
		return check
	}
	defer os.RemoveAll(workdir)
	if err := fsutil.CopyDir(baseline.Path, workdir); err != nil {
		check.Error = fmt.Sprintf("copy vet baseline: %v", err)
		return check
	}
	check.BaselinePass = true
	target := filepath.Join(workdir, filepath.FromSlash(task.FilePath))
	if err := os.WriteFile(target, []byte(mutation.Mutated), 0o644); err != nil {
		check.Error = fmt.Sprintf("write mutated source: %v", err)
		return check
	}
	mutatedExit, err := runTestCommand(ctx, workdir, baseline.TestCommand, timeout)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	check.MutationFail = mutatedExit != 0
	if !check.MutationFail {
		check.Error = "mutated tests still passed"
		return check
	}
	check.Passed = true
	return check
}

func detectPackTestCommand(pack Pack) []string {
	loaded, err := repo.OpenLocal(pack.Source)
	if err != nil {
		return nil
	}
	defer os.RemoveAll(loaded.Path)
	if err := checkoutCommit(loaded, pack.HeadSHA); err != nil {
		return nil
	}
	lang, err := repo.DetectLanguage(loaded.Path)
	if err != nil {
		return nil
	}
	return lang.TestCmd
}

func runTestCommand(ctx context.Context, dir string, args []string, timeout time.Duration) (int, error) {
	if len(args) == 0 {
		return -1, fmt.Errorf("test command is required")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return -1, runCtx.Err()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}
