package sqlite

import (
	"context"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/modes/newcomer/history"
)

func TestNewcomerHistoryCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	candidates := []history.CommitCandidate{
		{
			SHA:          "abc123",
			Message:      "add feature",
			Files:        []history.ChangedFile{{Path: "calculator/calculator.go", Additions: 4}},
			Additions:    4,
			HasTests:     true,
			IsRevertable: true,
			Score:        150,
		},
	}
	if err := store.SaveNewcomerHistoryScan(ctx, "file:///repo", "head", candidates); err != nil {
		t.Fatalf("SaveNewcomerHistoryScan() error = %v", err)
	}
	got, err := store.GetNewcomerHistoryScan(ctx, "file:///repo", "head")
	if err != nil {
		t.Fatalf("GetNewcomerHistoryScan() error = %v", err)
	}
	if len(got) != 1 || got[0].SHA != "abc123" || got[0].Files[0].Path != "calculator/calculator.go" {
		t.Fatalf("GetNewcomerHistoryScan() = %#v, want cached candidate", got)
	}

	candidates[0].SHA = "def456"
	if err := store.SaveNewcomerHistoryScan(ctx, "file:///repo", "head", candidates); err != nil {
		t.Fatalf("SaveNewcomerHistoryScan() update error = %v", err)
	}
	got, err = store.GetNewcomerHistoryScan(ctx, "file:///repo", "head")
	if err != nil {
		t.Fatalf("GetNewcomerHistoryScan() after update error = %v", err)
	}
	if got[0].SHA != "def456" {
		t.Fatalf("updated SHA = %q, want def456", got[0].SHA)
	}
}
