// SPDX-License-Identifier: MIT

package sqlite

import (
	"context"
	"fmt"
	"time"
)

type CoachUsage struct {
	SessionID                string
	Backend                  string
	Model                    string
	Operation                string
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	CostUSD                  float64
	CreatedAt                time.Time
}

func (s *Store) SaveCoachUsage(ctx context.Context, usage CoachUsage) error {
	if usage.SessionID == "" {
		return fmt.Errorf("coach usage session id is required")
	}
	if usage.Backend == "" {
		return fmt.Errorf("coach usage backend is required")
	}
	if usage.Operation == "" {
		return fmt.Errorf("coach usage operation is required")
	}
	if usage.CreatedAt.IsZero() {
		usage.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO coach_usage(
		session_id, backend, model, operation, input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens, cost_usd, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		usage.SessionID,
		usage.Backend,
		usage.Model,
		usage.Operation,
		usage.InputTokens,
		usage.OutputTokens,
		usage.CacheCreationInputTokens,
		usage.CacheReadInputTokens,
		usage.CostUSD,
		usage.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save coach usage: %w", err)
	}
	return nil
}
