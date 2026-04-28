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

	"github.com/dhruvmishra/codedojo/internal/session"
)

func TestOpenAppliesSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	for _, table := range []string{"sessions", "events", "scores"} {
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
		HintsUsed:  1,
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
	if err := store.UpsertScore(ctx, "sess-1", 125); err != nil {
		t.Fatalf("UpsertScore() error = %v", err)
	}

	wantSession.State = session.StateRunning
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
