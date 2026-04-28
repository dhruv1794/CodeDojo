package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

func (s *Store) SaveMutationLog(ctx context.Context, sessionID string, log mutate.MutationLog) error {
	if log.ID == "" {
		return fmt.Errorf("mutation log id is required")
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	if log.Mutation.AppliedAt.IsZero() {
		log.Mutation.AppliedAt = log.CreatedAt
	}
	payload, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("marshal mutation log: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO mutation_logs(
		id, session_id, repo_path, head_sha, difficulty, operator, file_path, start_line, end_line, payload, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		session_id = excluded.session_id,
		repo_path = excluded.repo_path,
		head_sha = excluded.head_sha,
		difficulty = excluded.difficulty,
		operator = excluded.operator,
		file_path = excluded.file_path,
		start_line = excluded.start_line,
		end_line = excluded.end_line,
		payload = excluded.payload,
		created_at = excluded.created_at`,
		log.ID,
		nullableString(sessionID),
		log.RepoPath,
		log.HeadSHA,
		log.Difficulty,
		log.Mutation.Operator,
		log.Mutation.FilePath,
		log.Mutation.StartLine,
		log.Mutation.EndLine,
		string(payload),
		log.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save mutation log: %w", err)
	}
	return nil
}

func (s *Store) GetMutationLog(ctx context.Context, id string) (mutate.MutationLog, error) {
	row := s.db.QueryRowContext(ctx, `SELECT payload FROM mutation_logs WHERE id = ?`, id)
	var payload string
	if err := row.Scan(&payload); err != nil {
		return mutate.MutationLog{}, fmt.Errorf("get mutation log: %w", err)
	}
	var log mutate.MutationLog
	if err := json.Unmarshal([]byte(payload), &log); err != nil {
		return mutate.MutationLog{}, fmt.Errorf("unmarshal mutation log: %w", err)
	}
	return log, nil
}

func (s *Store) ListMutationLogs(ctx context.Context, sessionID string) ([]mutate.MutationLog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM mutation_logs WHERE session_id = ? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list mutation logs: %w", err)
	}
	defer rows.Close()
	var logs []mutate.MutationLog
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan mutation log: %w", err)
		}
		var log mutate.MutationLog
		if err := json.Unmarshal([]byte(payload), &log); err != nil {
			return nil, fmt.Errorf("unmarshal mutation log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mutation logs: %w", err)
	}
	return logs, nil
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
