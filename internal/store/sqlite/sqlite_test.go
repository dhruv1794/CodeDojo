package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/session"
)

func TestOpenAppliesSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	for _, table := range []string{"sessions", "events", "scores", "mutation_logs", "streaks"} {
		t.Run(table, func(t *testing.T) {
			var name string
			err := store.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
			if err != nil {
				t.Fatalf("schema table %q missing: %v", table, err)
			}
			if name != table {
				t.Fatalf("schema table = %q, want %q", name, table)
			}
		})
	}
}

func TestMutationLogRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	if err := store.CreateSession(ctx, session.Session{
		ID:         "sess-mutation",
		Mode:       session.ModeReviewer,
		Repo:       "/tmp/repo",
		Task:       "find the mutation",
		HintBudget: 3,
		State:      session.StateRunning,
		StartedAt:  time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	want := mutate.MutationLog{
		ID:         "mutation-1",
		RepoPath:   "/tmp/repo",
		HeadSHA:    "abc123",
		Difficulty: 3,
		Mutation: mutate.Mutation{
			Operator:    "boundary",
			Difficulty:  2,
			FilePath:    "calculator/calculator.go",
			StartLine:   14,
			StartColumn: 11,
			EndLine:     14,
			EndColumn:   12,
			Original:    "value < min",
			Mutated:     "value <= min",
			Description: "flipped less-than boundary",
			AppliedAt:   time.Date(2026, 4, 28, 9, 1, 0, 0, time.UTC),
		},
		CreatedAt: time.Date(2026, 4, 28, 9, 1, 0, 0, time.UTC),
	}

	if err := store.SaveMutationLog(ctx, "sess-mutation", want); err != nil {
		t.Fatalf("SaveMutationLog() error = %v", err)
	}
	got, err := store.GetMutationLog(ctx, "mutation-1")
	if err != nil {
		t.Fatalf("GetMutationLog() error = %v", err)
	}
	assertMutationLogEqual(t, got, want)

	logs, err := store.ListMutationLogs(ctx, "sess-mutation")
	if err != nil {
		t.Fatalf("ListMutationLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListMutationLogs() length = %d, want 1", len(logs))
	}
	assertMutationLogEqual(t, logs[0], want)

	want.Mutation.EndLine = 15
	if err := store.SaveMutationLog(ctx, "sess-mutation", want); err != nil {
		t.Fatalf("SaveMutationLog() update error = %v", err)
	}
	got, err = store.GetMutationLog(ctx, "mutation-1")
	if err != nil {
		t.Fatalf("GetMutationLog() after update error = %v", err)
	}
	assertMutationLogEqual(t, got, want)
}

func TestStoreCRUDRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	started := time.Date(2026, 4, 27, 10, 11, 12, 13, time.UTC)
	created := time.Date(2026, 4, 27, 10, 12, 13, 14, time.UTC)
	wantSession := session.Session{
		ID:         "sess-1",
		Mode:       session.ModeReviewer,
		Repo:       "/tmp/repo",
		Task:       "find the changed comparison",
		HintBudget: 3,
		Score:      42,
		State:      session.StateCreated,
		StartedAt:  started,
	}

	if err := store.CreateSession(ctx, wantSession); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	gotSession, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	assertSessionEqual(t, gotSession, wantSession)

	if err := store.AppendEvent(ctx, session.Event{
		SessionID: "sess-1",
		Type:      session.EventHint,
		Payload:   "look near the failing boundary",
		CreatedAt: created,
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	events, err := store.ListEvents(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListEvents() length = %d, want 1", len(events))
	}
	if events[0].ID == 0 {
		t.Fatal("event ID was not populated")
	}
	if events[0].SessionID != "sess-1" || events[0].Type != session.EventHint || events[0].Payload != "look near the failing boundary" || !events[0].CreatedAt.Equal(created) {
		t.Fatalf("event round trip = %+v, want session/type/payload/time preserved", events[0])
	}

	if err := store.UpdateState(ctx, "sess-1", session.StateRunning); err != nil {
		t.Fatalf("UpdateState() error = %v", err)
	}
	if err := store.IncrementHintsUsed(ctx, "sess-1"); err != nil {
		t.Fatalf("IncrementHintsUsed() error = %v", err)
	}
	if err := store.UpsertScore(ctx, "sess-1", 125); err != nil {
		t.Fatalf("UpsertScore() error = %v", err)
	}

	wantSession.State = session.StateRunning
	wantSession.HintsUsed = 1
	wantSession.Score = 125
	gotSession, err = store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession() after updates error = %v", err)
	}
	assertSessionEqual(t, gotSession, wantSession)

	var score int
	if err := store.db.QueryRowContext(ctx, `SELECT score FROM scores WHERE session_id = ?`, "sess-1").Scan(&score); err != nil {
		t.Fatalf("query score row error = %v", err)
	}
	if score != 125 {
		t.Fatalf("score row = %d, want 125", score)
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() length = %d, want 1", len(sessions))
	}
	assertSessionEqual(t, sessions[0], wantSession)
}

func TestStatsAndStreakRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	started := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	for _, sess := range []session.Session{
		{ID: "review-1", Mode: session.ModeReviewer, Repo: "/repos/api", Task: "review", Score: 100, State: session.StateClosed, StartedAt: started},
		{ID: "review-2", Mode: session.ModeReviewer, Repo: "/repos/api", Task: "review", Score: 50, State: session.StateClosed, StartedAt: started.Add(time.Minute)},
		{ID: "learn-1", Mode: session.ModeNewcomer, Repo: "/repos/web", Task: "learn", Score: 0, State: session.StateClosed, StartedAt: started.Add(2 * time.Minute)},
	} {
		if err := store.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession(%s) error = %v", sess.ID, err)
		}
	}
	if err := store.SaveMutationLog(ctx, "review-1", mutate.MutationLog{
		ID:         "mut-1",
		RepoPath:   "/repos/api",
		Difficulty: 2,
		Mutation:   mutate.Mutation{Operator: "boundary", FilePath: "a.go", StartLine: 1, EndLine: 1},
		CreatedAt:  started,
	}); err != nil {
		t.Fatalf("SaveMutationLog() error = %v", err)
	}
	if streak, err := store.RecordStreakResult(ctx, true); err != nil || streak.Current != 1 || streak.Best != 1 {
		t.Fatalf("RecordStreakResult(true) = %+v, %v; want 1/1", streak, err)
	}
	if streak, err := store.RecordStreakResult(ctx, true); err != nil || streak.Current != 2 || streak.Best != 2 {
		t.Fatalf("RecordStreakResult(true) = %+v, %v; want 2/2", streak, err)
	}
	if streak, err := store.RecordStreakResult(ctx, false); err != nil || streak.Current != 0 || streak.Best != 2 {
		t.Fatalf("RecordStreakResult(false) = %+v, %v; want 0/2", streak, err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Total != 3 || stats.Graded != 2 || stats.Best != 100 || stats.Streak.Current != 0 || stats.Streak.Best != 2 {
		t.Fatalf("Stats() = %+v, want totals 3/2 best 100 streak 0/2", stats)
	}
	if len(stats.ByOp) != 1 || stats.ByOp[0].Name != "boundary" || stats.ByOp[0].Count != 1 {
		t.Fatalf("ByOp = %+v, want one boundary row", stats.ByOp)
	}
}

func TestAppendEventConcurrent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	if err := store.CreateSession(ctx, session.Session{
		ID:         "sess-concurrent",
		Mode:       session.ModeReviewer,
		Repo:       "/tmp/repo",
		Task:       "find the bug",
		HintBudget: 3,
		State:      session.StateRunning,
		StartedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	const eventCount = 50
	var wg sync.WaitGroup
	errs := make(chan error, eventCount)
	for i := range eventCount {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- store.AppendEvent(ctx, session.Event{
				SessionID: "sess-concurrent",
				Type:      session.EventHint,
				Payload:   fmt.Sprintf("event-%02d", i),
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AppendEvent() concurrent error = %v", err)
		}
	}

	events, err := store.ListEvents(ctx, "sess-concurrent")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != eventCount {
		t.Fatalf("ListEvents() length = %d, want %d", len(events), eventCount)
	}
	seen := make(map[string]bool, eventCount)
	var lastID int64
	for _, event := range events {
		if event.ID <= lastID {
			t.Fatalf("events are not ordered by increasing ID: previous %d, current %d", lastID, event.ID)
		}
		lastID = event.ID
		seen[event.Payload] = true
		if event.CreatedAt.IsZero() {
			t.Fatalf("event %d CreatedAt is zero", event.ID)
		}
	}
	for i := range eventCount {
		payload := fmt.Sprintf("event-%02d", i)
		if !seen[payload] {
			t.Fatalf("missing appended event payload %q", payload)
		}
	}
}

func TestGetSessionMissingReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	_, err := store.GetSession(ctx, "missing")
	if err == nil {
		t.Fatal("GetSession() error = nil, want missing row error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSession() error = %v, want sql.ErrNoRows", err)
	}
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "codedojo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}

func assertSessionEqual(t *testing.T, got, want session.Session) {
	t.Helper()

	if got.ID != want.ID ||
		got.Mode != want.Mode ||
		got.Repo != want.Repo ||
		got.Task != want.Task ||
		got.HintBudget != want.HintBudget ||
		got.HintsUsed != want.HintsUsed ||
		got.Score != want.Score ||
		got.State != want.State ||
		!got.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("session = %+v, want %+v", got, want)
	}
}

func assertMutationLogEqual(t *testing.T, got, want mutate.MutationLog) {
	t.Helper()

	if got.ID != want.ID ||
		got.RepoPath != want.RepoPath ||
		got.HeadSHA != want.HeadSHA ||
		got.Difficulty != want.Difficulty ||
		got.Mutation.Operator != want.Mutation.Operator ||
		got.Mutation.FilePath != want.Mutation.FilePath ||
		got.Mutation.StartLine != want.Mutation.StartLine ||
		got.Mutation.EndLine != want.Mutation.EndLine ||
		got.Mutation.Original != want.Mutation.Original ||
		got.Mutation.Mutated != want.Mutation.Mutated ||
		!got.CreatedAt.Equal(want.CreatedAt) ||
		!got.Mutation.AppliedAt.Equal(want.Mutation.AppliedAt) {
		t.Fatalf("mutation log = %+v, want %+v", got, want)
	}
}
