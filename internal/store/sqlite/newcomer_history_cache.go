package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/newcomer/history"
)

func (s *Store) SaveNewcomerHistoryScan(ctx context.Context, repoURL, headSHA string, candidates []history.CommitCandidate) error {
	if repoURL == "" {
		return fmt.Errorf("repo url is required")
	}
	if headSHA == "" {
		return fmt.Errorf("head sha is required")
	}
	payload, err := json.Marshal(candidates)
	if err != nil {
		return fmt.Errorf("marshal newcomer history scan: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO newcomer_history_cache(repo_url, head_sha, payload, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(repo_url, head_sha) DO UPDATE SET
			payload = excluded.payload,
			updated_at = excluded.updated_at`,
		repoURL, headSHA, string(payload), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save newcomer history scan: %w", err)
	}
	return nil
}

func (s *Store) GetNewcomerHistoryScan(ctx context.Context, repoURL, headSHA string) ([]history.CommitCandidate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT payload FROM newcomer_history_cache WHERE repo_url = ? AND head_sha = ?`, repoURL, headSHA)
	var payload string
	if err := row.Scan(&payload); err != nil {
		return nil, fmt.Errorf("get newcomer history scan: %w", err)
	}
	var candidates []history.CommitCandidate
	if err := json.Unmarshal([]byte(payload), &candidates); err != nil {
		return nil, fmt.Errorf("unmarshal newcomer history scan: %w", err)
	}
	return candidates, nil
}
