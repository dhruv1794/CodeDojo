package history

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/dhruvmishra/codedojo/internal/repo"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

const DefaultScanLimit = 100

type ChangedFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Renamed   bool   `json:"renamed"`
	Test      bool   `json:"test"`
}

type CommitCandidate struct {
	SHA          string        `json:"sha"`
	Message      string        `json:"message"`
	Files        []ChangedFile `json:"files"`
	Additions    int           `json:"additions"`
	Deletions    int           `json:"deletions"`
	HasTests     bool          `json:"has_tests"`
	IsRevertable bool          `json:"is_revertable"`
	Score        int           `json:"score,omitempty"`
	Filtered     bool          `json:"filtered,omitempty"`
	FilterReason string        `json:"filter_reason,omitempty"`
}

func Scan(ctx context.Context, r repo.Repo, limit int) ([]CommitCandidate, error) {
	if limit <= 0 {
		limit = DefaultScanLimit
	}
	iter, err := r.Git.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("open commit log: %w", err)
	}
	defer iter.Close()

	var out []CommitCandidate
	err = iter.ForEach(func(commit *object.Commit) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(out) >= limit {
			return storer.ErrStop
		}
		candidate, err := candidateFromCommit(commit)
		if err != nil {
			return err
		}
		out = append(out, candidate)
		return nil
	})
	if err != nil && err != storer.ErrStop {
		return nil, fmt.Errorf("scan commits: %w", err)
	}
	return out, nil
}

func candidateFromCommit(commit *object.Commit) (CommitCandidate, error) {
	candidate := CommitCandidate{
		SHA:     commit.Hash.String(),
		Message: firstLine(commit.Message),
	}
	if commit.NumParents() != 1 {
		candidate.Filtered = true
		candidate.FilterReason = "merge or root commit"
		return candidate, nil
	}
	parent, err := commit.Parent(0)
	if err != nil {
		return CommitCandidate{}, fmt.Errorf("load parent for %s: %w", commit.Hash, err)
	}
	patch, err := parent.Patch(commit)
	if err != nil {
		return CommitCandidate{}, fmt.Errorf("diff commit %s: %w", commit.Hash, err)
	}
	candidate.Files = changedFiles(patch)
	for _, file := range candidate.Files {
		candidate.Additions += file.Additions
		candidate.Deletions += file.Deletions
		candidate.HasTests = candidate.HasTests || file.Test
	}
	candidate.IsRevertable = isRevertablePatch(candidate.Files)
	candidate.FilterReason = filterReason(candidate, patch)
	candidate.Filtered = candidate.FilterReason != ""
	return candidate, nil
}

func changedFiles(patch *object.Patch) []ChangedFile {
	stats := map[string]object.FileStat{}
	for _, stat := range patch.Stats() {
		stats[filepath.ToSlash(stat.Name)] = stat
	}
	files := make([]ChangedFile, 0, len(patch.FilePatches()))
	for _, filePatch := range patch.FilePatches() {
		from, to := filePatch.Files()
		path := patchPath(from, to)
		oldPath := ""
		if from != nil {
			oldPath = filepath.ToSlash(from.Path())
		}
		if to != nil {
			path = filepath.ToSlash(to.Path())
		}
		stat := stats[path]
		if stat.Name == "" && oldPath != "" {
			stat = stats[oldPath]
		}
		files = append(files, ChangedFile{
			Path:      path,
			OldPath:   oldPath,
			Additions: stat.Addition,
			Deletions: stat.Deletion,
			Renamed:   from != nil && to != nil && filepath.ToSlash(from.Path()) != filepath.ToSlash(to.Path()),
			Test:      isTestFile(path),
		})
	}
	slices.SortFunc(files, func(a, b ChangedFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	return files
}

func filterReason(candidate CommitCandidate, patch *object.Patch) string {
	if len(candidate.Files) == 0 {
		return "no file changes"
	}
	if !candidate.IsRevertable {
		return "not cleanly revertable"
	}
	if isDependencyOnly(candidate.Files) {
		return "dependency-only commit"
	}
	if isFormatOnly(patch) {
		return "format-only commit"
	}
	if len(candidate.Files) > 3 {
		return "touches too many files"
	}
	if candidate.Additions > 200 {
		return "too many additions"
	}
	if !candidate.HasTests {
		return "missing test changes"
	}
	return ""
}

func isRevertablePatch(files []ChangedFile) bool {
	for _, file := range files {
		if file.Renamed || file.Path == "" {
			return false
		}
	}
	return true
}

func isDependencyOnly(files []ChangedFile) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isDependencyFile(file.Path) {
			return false
		}
	}
	return true
}

func isFormatOnly(patch *object.Patch) bool {
	var removed, added strings.Builder
	hasRemoved := false
	hasAdded := false
	for _, filePatch := range patch.FilePatches() {
		if filePatch.IsBinary() {
			return false
		}
		for _, chunk := range filePatch.Chunks() {
			switch chunk.Type() {
			case diff.Delete:
				hasRemoved = true
				removed.WriteString(removeWhitespace(chunk.Content()))
			case diff.Add:
				hasAdded = true
				added.WriteString(removeWhitespace(chunk.Content()))
			}
		}
	}
	if !hasRemoved || !hasAdded {
		return false
	}
	return removed.String() == added.String()
}

func removeWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func patchPath(from, to diff.File) string {
	if to != nil {
		return filepath.ToSlash(to.Path())
	}
	if from != nil {
		return filepath.ToSlash(from.Path())
	}
	return ""
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	slashPath := filepath.ToSlash(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.js") ||
		strings.HasSuffix(base, ".test.jsx") ||
		strings.HasSuffix(base, ".spec.jsx") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.tsx") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasPrefix(base, "test_") ||
		strings.HasPrefix(slashPath, "tests/") ||
		strings.Contains(slashPath, "/tests/")
}

func isDependencyFile(path string) bool {
	switch filepath.ToSlash(path) {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "pyproject.toml", "poetry.lock", "requirements.txt":
		return true
	default:
		return false
	}
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return line
}
