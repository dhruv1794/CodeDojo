package mutate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const defaultLogLimit = 50

// CandidateFiles returns source files eligible for mutation in repoPath.
// When cfg is zero, Go defaults are used.
func CandidateFiles(ctx context.Context, repoPath string, logLimit int, cfgs ...ScanConfig) ([]string, error) {
	cfg := scanConfigWithDefaults(cfgs)
	if logLimit <= 0 {
		logLimit = defaultLogLimit
	}
	fromLog, err := filesFromGitLog(ctx, repoPath, logLimit, cfg)
	if err == nil && len(fromLog) > 0 {
		return fromLog, nil
	}
	return filesFromWalk(repoPath, cfg)
}

func scanConfigWithDefaults(cfgs []ScanConfig) ScanConfig {
	var cfg ScanConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.GlobPattern == "" {
		cfg.GlobPattern = "*.go"
	}
	if cfg.IsEligible == nil {
		cfg.IsEligible = isEligibleGoFile
	}
	return cfg
}

// DefaultGoScanConfig returns a ScanConfig for Go repositories.
func DefaultGoScanConfig() ScanConfig {
	return ScanConfig{GlobPattern: "*.go", IsEligible: isEligibleGoFile}
}

// DefaultPythonScanConfig returns a ScanConfig for Python repositories.
func DefaultPythonScanConfig() ScanConfig {
	return ScanConfig{GlobPattern: "*.py", IsEligible: isEligiblePythonFile}
}

// DefaultJSScanConfig returns a ScanConfig for JavaScript repositories.
func DefaultJSScanConfig() ScanConfig {
	return ScanConfig{GlobPattern: "*.js", IsEligible: isEligibleJSFile}
}

// DefaultTSScanConfig returns a ScanConfig for TypeScript repositories.
func DefaultTSScanConfig() ScanConfig {
	return ScanConfig{GlobPattern: "*.ts", IsEligible: isEligibleTSFile}
}

// DefaultRustScanConfig returns a ScanConfig for Rust repositories.
func DefaultRustScanConfig() ScanConfig {
	return ScanConfig{GlobPattern: "*.rs", IsEligible: isEligibleRustFile}
}

func filesFromGitLog(ctx context.Context, repoPath string, logLimit int, cfg ScanConfig) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", fmt.Sprintf("-%d", logLimit), "--name-only", "--pretty=format:", "--", cfg.GlobPattern)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log files: %w", err)
	}
	var files []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		rel := filepath.Clean(strings.TrimSpace(line))
		if rel == "." || rel == "" || seen[rel] {
			continue
		}
		if ok, err := cfg.IsEligible(repoPath, rel); err != nil {
			return nil, err
		} else if ok {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	return files, nil
}

func filesFromWalk(repoPath string, cfg ScanConfig) ([]string, error) {
	var files []string
	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "target":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}
		if ok, err := cfg.IsEligible(repoPath, rel); err != nil {
			return err
		} else if ok {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan source files: %w", err)
	}
	slices.Sort(files)
	return files, nil
}

// --- Go ---

func isEligibleGoFile(repoPath, rel string) (bool, error) {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return false, nil
	}
	full := filepath.Join(repoPath, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %q: %w", rel, err)
	}
	if bytes.Contains(data[:min(len(data), 2048)], []byte("Code generated")) {
		return false, nil
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(full), "*_test.go"))
	if err != nil {
		return false, fmt.Errorf("find neighbouring tests for %q: %w", rel, err)
	}
	return len(matches) > 0, nil
}

// --- Python ---

func isEligiblePythonFile(repoPath, rel string) (bool, error) {
	slashRel := filepath.ToSlash(rel)
	if !strings.HasSuffix(slashRel, ".py") {
		return false, nil
	}
	base := filepath.Base(rel)
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
		base == "__init__.py" || base == "conftest.py" || base == "setup.py" {
		return false, nil
	}
	dir := filepath.Dir(filepath.Join(repoPath, rel))
	matches, err := filepath.Glob(filepath.Join(dir, "test_*.py"))
	if err != nil {
		return false, err
	}
	if len(matches) > 0 {
		return true, nil
	}
	matches, err = filepath.Glob(filepath.Join(dir, "*_test.py"))
	if err != nil {
		return false, err
	}
	// Also accept if there is a sibling tests/ directory
	testDir := filepath.Join(filepath.Dir(dir), "tests")
	_, statErr := os.Stat(testDir)
	return len(matches) > 0 || statErr == nil, nil
}

// --- JavaScript ---

func isEligibleJSFile(repoPath, rel string) (bool, error) {
	slashRel := filepath.ToSlash(rel)
	if !strings.HasSuffix(slashRel, ".js") && !strings.HasSuffix(slashRel, ".jsx") {
		return false, nil
	}
	if isJSTestFile(slashRel) {
		return false, nil
	}
	return hasNeighbouringJSTest(filepath.Dir(filepath.Join(repoPath, rel))), nil
}

// --- TypeScript ---

func isEligibleTSFile(repoPath, rel string) (bool, error) {
	slashRel := filepath.ToSlash(rel)
	if !strings.HasSuffix(slashRel, ".ts") && !strings.HasSuffix(slashRel, ".tsx") {
		return false, nil
	}
	if isTSTestFile(slashRel) {
		return false, nil
	}
	return hasNeighbouringTSTest(filepath.Dir(filepath.Join(repoPath, rel))), nil
}

// --- Rust ---

func isEligibleRustFile(repoPath, rel string) (bool, error) {
	slashRel := filepath.ToSlash(rel)
	if !strings.HasSuffix(slashRel, ".rs") {
		return false, nil
	}
	// Rust integration tests live in tests/ — skip those.
	if strings.HasPrefix(slashRel, "tests/") || strings.Contains(slashRel, "/tests/") {
		return false, nil
	}
	full := filepath.Join(repoPath, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %q: %w", rel, err)
	}
	// Rust unit tests are inline; accept files that contain #[test].
	if bytes.Contains(data, []byte("#[test]")) || bytes.Contains(data, []byte("#[cfg(test)]")) {
		return true, nil
	}
	// Also accept if there is a neighbouring tests/ directory.
	testDir := filepath.Join(repoPath, "tests")
	_, statErr := os.Stat(testDir)
	return statErr == nil, nil
}

// --- JS/TS helpers ---

func isJSTestFile(slashRel string) bool {
	base := filepath.Base(slashRel)
	return strings.HasSuffix(base, ".test.js") || strings.HasSuffix(base, ".spec.js") ||
		strings.HasSuffix(base, ".test.jsx") || strings.HasSuffix(base, ".spec.jsx")
}

func isTSTestFile(slashRel string) bool {
	base := filepath.Base(slashRel)
	return strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".test.tsx") || strings.HasSuffix(base, ".spec.tsx")
}

func hasNeighbouringJSTest(dir string) bool {
	for _, pat := range []string{"*.test.js", "*.spec.js", "*.test.jsx", "*.spec.jsx"} {
		if m, _ := filepath.Glob(filepath.Join(dir, pat)); len(m) > 0 {
			return true
		}
	}
	return false
}

func hasNeighbouringTSTest(dir string) bool {
	for _, pat := range []string{"*.test.ts", "*.spec.ts", "*.test.tsx", "*.spec.tsx"} {
		if m, _ := filepath.Glob(filepath.Join(dir, pat)); len(m) > 0 {
			return true
		}
	}
	return hasNeighbouringJSTest(dir)
}
