// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func TestRunReplayPrintsSessionTimeline(t *testing.T) {
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	store, err := sqlite.Open(context.Background(), cfg.StorePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	if err := store.CreateSession(context.Background(), session.Session{
		ID:        "sess-replay",
		Mode:      session.ModeReviewer,
		Repo:      "testdata/sample-go-repo",
		Task:      "Find the hidden bug",
		Score:     154,
		State:     session.StateGraded,
		StartedAt: started,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SaveMutationLog(context.Background(), "sess-replay", mutate.MutationLog{
		ID:         "mut-replay",
		RepoPath:   "testdata/sample-go-repo",
		Difficulty: 2,
		Mutation: mutate.Mutation{
			Operator:    "boundary",
			Profile:     mutate.ProfileDifficulty(mutate.Mutation{Operator: "boundary", StartLine: 4, EndLine: 4}),
			FilePath:    "calculator/calculator.go",
			StartLine:   4,
			EndLine:     4,
			Description: "changed strict lower bound to inclusive lower bound",
			Original:    "package calculator\n\nfunc Clamp(value, min int) int {\n\tif value < min {\n\t\treturn min\n\t}\n\treturn value\n}\n",
			Mutated:     "package calculator\n\nfunc Clamp(value, min int) int {\n\tif value <= min {\n\t\treturn min\n\t}\n\treturn value\n}\n",
		},
		CreatedAt: started,
	}); err != nil {
		t.Fatalf("save mutation log: %v", err)
	}
	for _, event := range []session.Event{
		{SessionID: "sess-replay", Type: session.EventCreated, CreatedAt: started},
		{SessionID: "sess-replay", Type: session.EventStarted, CreatedAt: started.Add(time.Second)},
		{SessionID: "sess-replay", Type: session.EventFile, Payload: "calculator/calculator.go", CreatedAt: started.Add(2 * time.Second)},
		{SessionID: "sess-replay", Type: session.EventTests, Payload: "exit=1 command=go test ./...", CreatedAt: started.Add(3 * time.Second)},
		{SessionID: "sess-replay", Type: session.EventSubmit, Payload: "calculator/calculator.go:13 diagnosis", CreatedAt: started.Add(4 * time.Second)},
		{SessionID: "sess-replay", Type: session.EventGrade, Payload: "score=154", CreatedAt: started.Add(5 * time.Second)},
		{SessionID: "sess-replay", Type: session.EventCommentary, Payload: "After-action commentary", CreatedAt: started.Add(6 * time.Second)},
		{SessionID: "sess-replay", Type: session.EventTrace, Payload: "1. Start from the failing behavior", CreatedAt: started.Add(7 * time.Second)},
	} {
		if err := store.AppendEvent(context.Background(), event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := runReplay(context.Background(), cmd, "sess-replay", "text"); err != nil {
		t.Fatalf("runReplay returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Kata replay",
		"Session: sess-replay",
		"Mode: reviewer",
		"Score: 154",
		"Mutation",
		"file: calculator/calculator.go:4-4",
		"operator: boundary",
		"profile: white belt",
		"locality: 1/3 - single-line mutation",
		"knowledge: 1/3 - requires basic expression and comparison semantics",
		"Original snapshot",
		">    4 | \tif value < min {",
		"Mutated snapshot",
		">    4 | \tif value <= min {",
		"opened   calculator/calculator.go",
		"tests    exit=1 command=go test ./...",
		"submit   calculator/calculator.go:13 diagnosis",
		"grade    score=154",
		"comment  After-action commentary",
		"trace    1. Start from the failing behavior",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("replay output missing %q:\n%s", want, got)
		}
	}
}

func TestRunReplayPrintsJSONExport(t *testing.T) {
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	store, err := sqlite.Open(context.Background(), cfg.StorePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	if err := store.CreateSession(context.Background(), session.Session{
		ID:         "sess-json",
		Mode:       session.ModeReviewer,
		Repo:       "testdata/sample-go-repo",
		Task:       "Find the hidden bug",
		HintBudget: 3,
		HintsUsed:  1,
		Score:      154,
		State:      session.StateGraded,
		StartedAt:  started,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SaveMutationLog(context.Background(), "sess-json", mutate.MutationLog{
		ID:         "mut-json",
		RepoPath:   "testdata/sample-go-repo",
		Difficulty: 2,
		Mutation: mutate.Mutation{
			Operator:  "boundary",
			FilePath:  "calculator/calculator.go",
			StartLine: 4,
			EndLine:   4,
			Original:  "before",
			Mutated:   "after",
		},
		CreatedAt: started,
	}); err != nil {
		t.Fatalf("save mutation log: %v", err)
	}
	if err := store.AppendEvent(context.Background(), session.Event{
		SessionID: "sess-json",
		Type:      session.EventTests,
		Payload:   "exit=1 command=go test ./...",
		CreatedAt: started.Add(time.Second),
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := runReplay(context.Background(), cmd, "sess-json", "json"); err != nil {
		t.Fatalf("runReplay returned error: %v", err)
	}
	var got replayExport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal replay json: %v\n%s", err, out.String())
	}
	if got.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", got.SchemaVersion)
	}
	if got.Session.ID != "sess-json" || got.Session.Score != 154 || got.Session.HintsUsed != 1 {
		t.Fatalf("session export = %+v", got.Session)
	}
	if len(got.Mutations) != 1 || got.Mutations[0].Mutation.Original != "before" || got.Mutations[0].Mutation.Mutated != "after" {
		t.Fatalf("mutation export = %+v", got.Mutations)
	}
	if len(got.Events) != 1 || got.Events[0].Type != session.EventTests || got.Events[0].Label != "tests" {
		t.Fatalf("event export = %+v", got.Events)
	}
}

func TestRunReplayPrintsStepTimeline(t *testing.T) {
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tmp := t.TempDir()
	cfgFile = filepath.Join(tmp, "config.yaml")
	cfg := config.Default()
	cfg.StorePath = filepath.Join(tmp, "codedojo.db")
	if err := config.Save(cfgFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	store, err := sqlite.Open(context.Background(), cfg.StorePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	if err := store.CreateSession(context.Background(), session.Session{
		ID:        "sess-step",
		Mode:      session.ModeReviewer,
		Repo:      "testdata/sample-go-repo",
		Task:      "Find the hidden bug",
		Score:     154,
		State:     session.StateGraded,
		StartedAt: started,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, event := range []session.Event{
		{SessionID: "sess-step", Type: session.EventStarted, CreatedAt: started},
		{SessionID: "sess-step", Type: session.EventFile, Payload: "calculator/calculator.go", CreatedAt: started.Add(2 * time.Second)},
		{SessionID: "sess-step", Type: session.EventTests, Payload: "exit=1\ncommand=go test ./...", CreatedAt: started.Add(5 * time.Second)},
	} {
		if err := store.AppendEvent(context.Background(), event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := runReplayWithOptions(context.Background(), cmd, "sess-step", replayOptions{Format: "text", Step: true}); err != nil {
		t.Fatalf("runReplayWithOptions returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Playback steps",
		"Step 1/3  +00:00  gap +00:00",
		"started",
		"Step 2/3  +00:02  gap +00:02",
		"opened",
		"  calculator/calculator.go",
		"Step 3/3  +00:05  gap +00:03",
		"tests",
		"  exit=1",
		"  command=go test ./...",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("step replay output missing %q:\n%s", want, got)
		}
	}
}
