package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dhruvmishra/codedojo/internal/session"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc/sqlite without WAL hits SQLITE_BUSY under concurrent writers; serialize at the pool layer.
	db.SetMaxOpenConns(1)
	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateSession(ctx context.Context, sess session.Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id, mode, repo, task, hint_budget, hints_used, score, state, started_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Mode, sess.Repo, sess.Task, sess.HintBudget, sess.HintsUsed, sess.Score, sess.State, sess.StartedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (session.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, mode, repo, task, hint_budget, hints_used, score, state, started_at FROM sessions WHERE id = ?`, id)
	return scanSession(row)
}

func (s *Store) ListSessions(ctx context.Context) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, mode, repo, task, hint_budget, hints_used, score, state, started_at FROM sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []session.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) AppendEvent(ctx context.Context, event session.Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO events(session_id, type, payload, created_at) VALUES(?, ?, ?, ?)`,
		event.SessionID, event.Type, event.Payload, event.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) ListEvents(ctx context.Context, sessionID string) ([]session.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, type, payload, created_at FROM events WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	var out []session.Event
	for rows.Next() {
		var event session.Event
		var created string
		if err := rows.Scan(&event.ID, &event.SessionID, &event.Type, &event.Payload, &created); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse event time: %w", err)
		}
		event.CreatedAt = t
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) UpdateState(ctx context.Context, id string, state session.State) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET state = ? WHERE id = ?`, state, id)
	if err != nil {
		return fmt.Errorf("update state: %w", err)
	}
	return nil
}

func (s *Store) UpsertScore(ctx context.Context, id string, score int) error {
	now := time.Now().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert score: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO scores(session_id, score, updated_at) VALUES(?, ?, ?) ON CONFLICT(session_id) DO UPDATE SET score = excluded.score, updated_at = excluded.updated_at`, id, score, now); err != nil {
		return fmt.Errorf("upsert score: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET score = ? WHERE id = ?`, score, id); err != nil {
		return fmt.Errorf("update session score: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert score: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (session.Session, error) {
	var sess session.Session
	var started string
	if err := row.Scan(&sess.ID, &sess.Mode, &sess.Repo, &sess.Task, &sess.HintBudget, &sess.HintsUsed, &sess.Score, &sess.State, &started); err != nil {
		return session.Session{}, fmt.Errorf("scan session: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return session.Session{}, fmt.Errorf("parse session time: %w", err)
	}
	sess.StartedAt = t
	return sess, nil
}
