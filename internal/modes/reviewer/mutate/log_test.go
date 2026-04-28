package mutate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadMutationLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codedojo", "mutation-log.json")
	want := MutationLog{
		ID:         "log-1",
		RepoPath:   "/tmp/repo",
		Difficulty: 2,
		Mutation: Mutation{
			Operator:  "boundary",
			FilePath:  "calculator/calculator.go",
			StartLine: 10,
		},
		CreatedAt: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}
	if err := WriteMutationLogFile(path, want); err != nil {
		t.Fatalf("WriteMutationLogFile returned error: %v", err)
	}
	got, err := ReadMutationLogFile(path)
	if err != nil {
		t.Fatalf("ReadMutationLogFile returned error: %v", err)
	}
	if got.ID != want.ID || got.Mutation.Operator != want.Mutation.Operator || got.Mutation.FilePath != want.Mutation.FilePath {
		t.Fatalf("log = %#v, want %#v", got, want)
	}
}
