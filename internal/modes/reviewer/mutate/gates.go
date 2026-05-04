package mutate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GateConfig controls post-mutation gate checks.
type GateConfig struct {
	// MinPassingRatio is the minimum fraction of tests that must still pass.
	// Defaults to 0.5. For non-JSON runners this is applied as a binary pass/fail.
	MinPassingRatio float64
	// BuildCmd is the command used as a compile/vet check before running tests.
	// Defaults to ["go", "vet", "./..."] when nil.
	BuildCmd []string
	// TestCmd is the command used to count passing/failing tests.
	// Defaults to ["go", "test", "-json", "./..."] when nil.
	TestCmd []string
	// TestOutputJSON controls whether TestCmd output is parsed as Go test JSON.
	// When false, only the exit code is used to determine pass/fail ratio.
	TestOutputJSON bool
}

// GateResult holds the raw output and counts from gate runs.
type GateResult struct {
	VetStdout    string
	VetStderr    string
	TestStdout   string
	TestStderr   string
	PassedTests  int
	FailedTests  int
	PassingRatio float64
}

func (cfg *GateConfig) withDefaults() GateConfig {
	out := *cfg
	if out.MinPassingRatio == 0 {
		out.MinPassingRatio = 0.5
	}
	if len(out.TestCmd) == 0 {
		out.TestCmd = []string{"go", "test", "-json", "./..."}
		out.TestOutputJSON = true
	}
	return out
}

// DefaultGoGateConfig returns a GateConfig for Go repositories.
func DefaultGoGateConfig() GateConfig {
	return GateConfig{
		BuildCmd:       []string{"go", "vet", "./..."},
		TestCmd:        []string{"go", "test", "-json", "./..."},
		TestOutputJSON: true,
	}
}

// DefaultPythonGateConfig returns a GateConfig for Python repositories.
func DefaultPythonGateConfig() GateConfig {
	return GateConfig{
		BuildCmd: []string{"python", "-m", "compileall", "."},
		TestCmd:  []string{"python", "-m", "pytest", "--tb=no", "-q"},
	}
}

// DefaultJSGateConfig returns a GateConfig for JavaScript repositories.
func DefaultJSGateConfig() GateConfig {
	return GateConfig{
		TestCmd: []string{"npm", "test"},
	}
}

// DefaultRustGateConfig returns a GateConfig for Rust repositories.
func DefaultRustGateConfig() GateConfig {
	return GateConfig{
		BuildCmd: []string{"cargo", "build"},
		TestCmd:  []string{"cargo", "test"},
	}
}

// RunGates executes the build and test gate checks against repoPath.
func RunGates(ctx context.Context, repoPath string, cfg GateConfig) (GateResult, error) {
	cfg = cfg.withDefaults()
	var out GateResult

	if len(cfg.BuildCmd) > 0 {
		result, err := runCommand(ctx, repoPath, cfg.BuildCmd[0], cfg.BuildCmd[1:]...)
		out.VetStdout = result.stdout
		out.VetStderr = result.stderr
		if err != nil {
			return out, err
		}
		if result.exitCode != 0 {
			return out, fmt.Errorf("build check failed: %s", result.stderr)
		}
	}

	result, err := runCommand(ctx, repoPath, cfg.TestCmd[0], cfg.TestCmd[1:]...)
	out.TestStdout = result.stdout
	out.TestStderr = result.stderr
	if err != nil {
		return out, err
	}

	if cfg.TestOutputJSON {
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
	} else {
		out.PassedTests, out.FailedTests, out.PassingRatio = parseGenericTestOutput(result)
	}

	if out.PassingRatio < cfg.MinPassingRatio {
		return out, fmt.Errorf("mutation rejected: %.2f passing ratio below %.2f", out.PassingRatio, cfg.MinPassingRatio)
	}
	return out, nil
}

// parseGenericTestOutput derives a pass/fail estimate for non-JSON test runners.
// It tries to parse pytest-style and cargo-style summaries; falls back to exit code.
func parseGenericTestOutput(result commandResult) (passed, failed int, ratio float64) {
	// Try pytest summary: "X passed, Y failed in Zs"
	if p, f, ok := parsePytestSummary(result.stdout + result.stderr); ok {
		total := p + f
		if total == 0 {
			return 0, 0, 1.0
		}
		r := float64(p) / float64(total)
		return p, f, r
	}
	// Try cargo test summary: "test result: ... X passed; Y failed"
	if p, f, ok := parseCargoSummary(result.stdout + result.stderr); ok {
		total := p + f
		if total == 0 {
			return 0, 0, 1.0
		}
		r := float64(p) / float64(total)
		return p, f, r
	}
	// Fall back to exit code: 0 = all pass (ratio 1.0), non-zero = some fail (estimate 0.7).
	if result.exitCode == 0 {
		return 1, 0, 1.0
	}
	return 1, 1, 0.7
}

func parsePytestSummary(output string) (passed, failed int, ok bool) {
	scanner := bufio.NewScanner(bytes.NewBufferString(output))
	for scanner.Scan() {
		line := scanner.Text()
		var p, f int
		if n, _ := fmt.Sscanf(line, "%d passed", &p); n == 1 {
			fmt.Sscanf(line, "%d passed, %d failed", &p, &f)
			return p, f, true
		}
		if n, _ := fmt.Sscanf(line, "%d failed", &f); n == 1 {
			return 0, f, true
		}
	}
	return 0, 0, false
}

func parseCargoSummary(output string) (passed, failed int, ok bool) {
	scanner := bufio.NewScanner(bytes.NewBufferString(output))
	for scanner.Scan() {
		line := scanner.Text()
		var p, f int
		if strings.Contains(line, "test result:") {
			fmt.Sscanf(line, "test result: %*s. %d passed; %d failed", &p, &f)
			return p, f, true
		}
		_ = p
		_ = f
	}
	return 0, 0, false
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
