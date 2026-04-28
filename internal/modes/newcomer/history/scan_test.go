package history

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
)

func TestScanFiltersAndRanksCommitCandidates(t *testing.T) {
	dir := newHistoryFixture(t)
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}

	candidates, err := Scan(context.Background(), repo.Repo{Path: dir, Git: gitRepo}, 20)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	byMessage := map[string]CommitCandidate{}
	for _, candidate := range candidates {
		byMessage[candidate.Message] = candidate
	}

	feature := byMessage["add multiplication"]
	if feature.Filtered {
		t.Fatalf("feature commit filtered: %s", feature.FilterReason)
	}
	if !feature.HasTests || !feature.IsRevertable {
		t.Fatalf("feature HasTests=%v IsRevertable=%v, want true true", feature.HasTests, feature.IsRevertable)
	}
	if len(feature.Files) != 2 {
		t.Fatalf("feature files = %d, want 2", len(feature.Files))
	}

	wantFiltered := map[string]string{
		"merge feature branch":      "merge or root commit",
		"rename calculator package": "not cleanly revertable",
		"format calculator":         "format-only commit",
		"bump module metadata":      "dependency-only commit",
		"initial":                   "merge or root commit",
	}
	for message, reason := range wantFiltered {
		candidate, ok := byMessage[message]
		if !ok {
			t.Fatalf("missing commit %q in scanned candidates", message)
		}
		if !candidate.Filtered || candidate.FilterReason != reason {
			t.Fatalf("%q filtered=%v reason=%q, want filtered reason %q", message, candidate.Filtered, candidate.FilterReason, reason)
		}
	}

	ranked := Rank(candidates)
	if len(ranked) != 1 {
		t.Fatalf("Rank() returned %d candidates, want 1", len(ranked))
	}
	if ranked[0].Message != "add multiplication" {
		t.Fatalf("top ranked = %q, want add multiplication", ranked[0].Message)
	}
	if ranked[0].Score == 0 {
		t.Fatalf("ranked score = 0, want positive")
	}
}

func TestRankPrefersSmallTestBackedCommits(t *testing.T) {
	candidates := []CommitCandidate{
		{
			SHA:          "large",
			Message:      "large",
			Files:        []ChangedFile{{Path: "a.go"}, {Path: "a_test.go", Test: true}, {Path: "b.go"}},
			Additions:    160,
			Deletions:    20,
			HasTests:     true,
			IsRevertable: true,
		},
		{
			SHA:          "small",
			Message:      "small",
			Files:        []ChangedFile{{Path: "a.go"}, {Path: "a_test.go", Test: true}},
			Additions:    20,
			Deletions:    2,
			HasTests:     true,
			IsRevertable: true,
		},
		{
			SHA:          "no-tests",
			Message:      "no tests",
			Files:        []ChangedFile{{Path: "a.go"}},
			Additions:    10,
			IsRevertable: true,
		},
	}

	ranked := Rank(candidates)
	if len(ranked) != 2 {
		t.Fatalf("Rank() len = %d, want 2", len(ranked))
	}
	if ranked[0].SHA != "small" {
		t.Fatalf("Rank()[0].SHA = %q, want small", ranked[0].SHA)
	}
}

func TestScanCachedUsesCacheUnlessRefreshRequested(t *testing.T) {
	ctx := context.Background()
	cache := &fakeScanCache{cached: []CommitCandidate{{SHA: "cached"}}}
	scanCalls := 0
	scan := func(context.Context) ([]CommitCandidate, error) {
		scanCalls++
		return []CommitCandidate{{SHA: "fresh"}}, nil
	}

	got, err := ScanCached(ctx, cache, "repo", "head", false, scan)
	if err != nil {
		t.Fatalf("ScanCached() cache hit error = %v", err)
	}
	if got[0].SHA != "cached" || scanCalls != 0 {
		t.Fatalf("cache hit got=%#v scanCalls=%d, want cached and no scan", got, scanCalls)
	}

	got, err = ScanCached(ctx, cache, "repo", "head", true, scan)
	if err != nil {
		t.Fatalf("ScanCached() refresh error = %v", err)
	}
	if got[0].SHA != "fresh" || scanCalls != 1 || cache.saved[0].SHA != "fresh" {
		t.Fatalf("refresh got=%#v scanCalls=%d saved=%#v, want fresh scan saved", got, scanCalls, cache.saved)
	}
}

func newHistoryFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeHistoryFile(t, dir, "go.mod", "module example.com/history\n\ngo 1.23\n")
	writeHistoryFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a,b int)int{return a+b}\n")
	writeHistoryFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T){if Add(1,2)!=3{t.Fatal(\"bad add\")}}\n")
	runHistoryGit(t, dir, "init")
	runHistoryGit(t, dir, "add", ".")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "initial")

	writeHistoryFile(t, dir, "go.mod", "module example.com/history\n\ngo 1.23\n\nrequire example.com/dep v1.2.3\n")
	runHistoryGit(t, dir, "add", "go.mod")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "bump module metadata")

	writeHistoryFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	runHistoryGit(t, dir, "add", "calculator/calculator.go")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "format calculator")

	runHistoryGit(t, dir, "mv", "calculator/calculator.go", "calculator/math.go")
	runHistoryGit(t, dir, "add", ".")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "rename calculator package")

	writeHistoryFile(t, dir, "calculator/math.go", "package calculator\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n")
	writeHistoryFile(t, dir, "calculator/calculator_test.go", "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad add\")\n\t}\n}\n\nfunc TestMultiply(t *testing.T) {\n\tif Multiply(2, 3) != 6 {\n\t\tt.Fatal(\"bad multiply\")\n\t}\n}\n")
	runHistoryGit(t, dir, "add", ".")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "add multiplication")

	runHistoryGit(t, dir, "checkout", "-b", "side")
	writeHistoryFile(t, dir, "calculator/side.go", "package calculator\n\nfunc Negate(v int) int { return -v }\n")
	runHistoryGit(t, dir, "add", ".")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "side branch change")
	runHistoryGit(t, dir, "checkout", "main")
	writeHistoryFile(t, dir, "README.md", "# history fixture\n")
	runHistoryGit(t, dir, "add", "README.md")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "main branch change")
	runHistoryGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "merge", "--no-ff", "side", "-m", "merge feature branch")

	return dir
}

func writeHistoryFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runHistoryGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

type fakeScanCache struct {
	cached []CommitCandidate
	saved  []CommitCandidate
	miss   bool
}

func (f *fakeScanCache) SaveNewcomerHistoryScan(_ context.Context, _, _ string, candidates []CommitCandidate) error {
	f.saved = candidates
	return nil
}

func (f *fakeScanCache) GetNewcomerHistoryScan(context.Context, string, string) ([]CommitCandidate, error) {
	if f.miss {
		return nil, errors.New("cache miss")
	}
	return f.cached, nil
}
