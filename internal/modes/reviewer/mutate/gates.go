package mutate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type GateConfig struct {
	MinPassingRatio float64
}

type GateResult struct {
	VetStdout    string
	VetStderr    string
	TestStdout   string
	TestStderr   string
	PassedTests  int
	FailedTests  int
	PassingRatio float64
}

func RunGates(ctx context.Context, repoPath string, cfg GateConfig) (GateResult, error) {
	if cfg.MinPassingRatio == 0 {
		cfg.MinPassingRatio = 0.5
	}
	result, err := runCommand(ctx, repoPath, "go", "vet", "./...")
	out := GateResult{VetStdout: result.stdout, VetStderr: result.stderr}
	if err != nil {
		return out, err
	}
	if result.exitCode != 0 {
		return out, fmt.Errorf("go vet failed: %s", result.stderr)
	}

	result, err = runCommand(ctx, repoPath, "go", "test", "-json", "./...")
	out.TestStdout = result.stdout
	out.TestStderr = result.stderr
	if err != nil {
		return out, err
	}
	passed, failed, err := countTestResults([]byte(result.stdout))
	if err != nil {
		return out, err
	}
	out.PassedTests = passed
	out.FailedTests = failed
	total := passed + failed
	if total > 0 {
		out.PassingRatio = float64(passed) / float64(total)
	}
	if out.PassingRatio < cfg.MinPassingRatio {
		return out, fmt.Errorf("mutation rejected: %.2f passing ratio below %.2f", out.PassingRatio, cfg.MinPassingRatio)
	}
	return out, nil
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runCommand(ctx context.Context, dir string, name string, args ...string) (commandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.exitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("run %s: %w", name, err)
	}
	return result, nil
}

func countTestResults(data []byte) (int, int, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var passed, failed int
	for scanner.Scan() {
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return 0, 0, fmt.Errorf("parse go test json: %w", err)
		}
		if event.Test == "" {
			continue
		}
		switch event.Action {
		case "pass":
			passed++
		case "fail":
			failed++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("scan go test json: %w", err)
	}
	return passed, failed, nil
}
