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

func CandidateFiles(ctx context.Context, repoPath string, logLimit int) ([]string, error) {
	if logLimit <= 0 {
		logLimit = defaultLogLimit
	}
	fromLog, err := filesFromGitLog(ctx, repoPath, logLimit)
	if err == nil && len(fromLog) > 0 {
		return fromLog, nil
	}
	return filesFromWalk(repoPath)
}

func filesFromGitLog(ctx context.Context, repoPath string, logLimit int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", fmt.Sprintf("-%d", logLimit), "--name-only", "--pretty=format:", "--", "*.go")
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
		if ok, err := isEligibleGoFile(repoPath, rel); err != nil {
			return nil, err
		} else if ok {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	return files, nil
}

func filesFromWalk(repoPath string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}
		if ok, err := isEligibleGoFile(repoPath, rel); err != nil {
			return err
		} else if ok {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan go files: %w", err)
	}
	slices.Sort(files)
	return files, nil
}

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
