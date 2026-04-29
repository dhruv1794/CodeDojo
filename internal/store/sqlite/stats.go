package sqlite

import (
	"context"
	"fmt"
	"time"
)

type Streak struct {
	Current int
	Best    int
}

type SessionStats struct {
	Total   int
	Graded  int
	Average float64
	Best    int
	Streak  Streak
	ByMode  []GroupStat
	ByRepo  []GroupStat
	ByOp    []GroupStat
}

type GroupStat struct {
	Name    string
	Count   int
	Average float64
	Best    int
}

func (s *Store) IncrementHintsUsed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET hints_used = hints_used + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("increment hints used: %w", err)
	}
	return nil
}

func (s *Store) GetStreak(ctx context.Context) (Streak, error) {
	var streak Streak
	err := s.db.QueryRowContext(ctx, `SELECT current_streak, best_streak FROM streaks WHERE id = 1`).Scan(&streak.Current, &streak.Best)
	if err != nil {
		return Streak{}, fmt.Errorf("get streak: %w", err)
	}
	return streak, nil
}

func (s *Store) RecordStreakResult(ctx context.Context, success bool) (Streak, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Streak{}, fmt.Errorf("begin streak update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current, best int
	if err := tx.QueryRowContext(ctx, `SELECT current_streak, best_streak FROM streaks WHERE id = 1`).Scan(&current, &best); err != nil {
		return Streak{}, fmt.Errorf("read streak: %w", err)
	}
	if success {
		current++
		if current > best {
			best = current
		}
	} else {
		current = 0
	}
	if _, err := tx.ExecContext(ctx, `UPDATE streaks SET current_streak = ?, best_streak = ?, updated_at = ? WHERE id = 1`, current, best, now); err != nil {
		return Streak{}, fmt.Errorf("update streak: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Streak{}, fmt.Errorf("commit streak update: %w", err)
	}
	return Streak{Current: current, Best: best}, nil
}

func (s *Store) Stats(ctx context.Context) (SessionStats, error) {
	var stats SessionStats
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN score > 0 THEN 1 ELSE 0 END), 0), COALESCE(AVG(NULLIF(score, 0)), 0), COALESCE(MAX(score), 0) FROM sessions`)
	if err := row.Scan(&stats.Total, &stats.Graded, &stats.Average, &stats.Best); err != nil {
		return SessionStats{}, fmt.Errorf("scan session stats: %w", err)
	}
	streak, err := s.GetStreak(ctx)
	if err != nil {
		return SessionStats{}, err
	}
	stats.Streak = streak
	if stats.ByMode, err = s.groupStats(ctx, `SELECT mode, COUNT(*), COALESCE(AVG(NULLIF(score, 0)), 0), COALESCE(MAX(score), 0) FROM sessions GROUP BY mode ORDER BY mode`); err != nil {
		return SessionStats{}, err
	}
	if stats.ByRepo, err = s.groupStats(ctx, `SELECT repo, COUNT(*), COALESCE(AVG(NULLIF(score, 0)), 0), COALESCE(MAX(score), 0) FROM sessions GROUP BY repo ORDER BY COUNT(*) DESC, repo LIMIT 10`); err != nil {
		return SessionStats{}, err
	}
	if stats.ByOp, err = s.groupStats(ctx, `SELECT m.operator, COUNT(*), COALESCE(AVG(NULLIF(s.score, 0)), 0), COALESCE(MAX(s.score), 0) FROM mutation_logs m LEFT JOIN sessions s ON s.id = m.session_id GROUP BY m.operator ORDER BY COUNT(*) DESC, m.operator`); err != nil {
		return SessionStats{}, err
	}
	return stats, nil
}

func (s *Store) groupStats(ctx context.Context, query string) ([]GroupStat, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query group stats: %w", err)
	}
	defer rows.Close()
	var out []GroupStat
	for rows.Next() {
		var stat GroupStat
		if err := rows.Scan(&stat.Name, &stat.Count, &stat.Average, &stat.Best); err != nil {
			return nil, fmt.Errorf("scan group stats: %w", err)
		}
		out = append(out, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group stats: %w", err)
	}
	return out, nil
}
