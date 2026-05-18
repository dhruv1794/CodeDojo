// SPDX-License-Identifier: MIT

package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
)

type Streak struct {
	Current int
	Best    int
}

type SessionStats struct {
	Total          int
	Graded         int
	Average        float64
	Best           int
	Streak         Streak
	ByMode         []GroupStat
	ByRepo         []GroupStat
	ByOp           []GroupStat
	Engagement     []EngagementStat
	Recommendation EngagementStat
	Cost           CostStats
}

type GroupStat struct {
	Name    string
	Count   int
	Average float64
	Best    int
}

type EngagementStat struct {
	Name        string
	Kind        string
	Count       int
	Average     float64
	Best        int
	SolveRate   float64
	AvgHints    float64
	AvgMinutes  float64
	Recommended bool
}

type CostStats struct {
	Calls             int
	InputTokens       int
	OutputTokens      int
	CacheTokens       int
	TotalUSD          float64
	AvgUSDPerSession  float64
	TokensPerHint     float64
	ProjectedMonthUSD float64
	ByBackend         []CostGroupStat
}

type CostGroupStat struct {
	Name         string
	Calls        int
	InputTokens  int
	OutputTokens int
	TotalUSD     float64
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
	if stats.Engagement, stats.Recommendation, err = s.engagementStats(ctx); err != nil {
		return SessionStats{}, err
	}
	if stats.Cost, err = s.costStats(ctx); err != nil {
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

type engagementAggregate struct {
	stat         EngagementStat
	scoreTotal   int
	hintTotal    int
	minuteTotal  float64
	successTotal int
}

func (s *Store) engagementStats(ctx context.Context) ([]EngagementStat, EngagementStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.operator, m.payload, s.score, s.hints_used, s.started_at, COALESCE(MAX(e.created_at), s.started_at)
		FROM mutation_logs m
		JOIN sessions s ON s.id = m.session_id
		LEFT JOIN events e ON e.session_id = s.id
		GROUP BY m.id, m.operator, m.payload, s.score, s.hints_used, s.started_at
	`)
	if err != nil {
		return nil, EngagementStat{}, fmt.Errorf("query engagement stats: %w", err)
	}
	defer rows.Close()

	aggregates := map[string]*engagementAggregate{}
	for rows.Next() {
		var operator, payload, startedRaw, endedRaw string
		var score, hints int
		if err := rows.Scan(&operator, &payload, &score, &hints, &startedRaw, &endedRaw); err != nil {
			return nil, EngagementStat{}, fmt.Errorf("scan engagement stat: %w", err)
		}
		started, _ := time.Parse(time.RFC3339Nano, startedRaw)
		ended, _ := time.Parse(time.RFC3339Nano, endedRaw)
		minutes := 0.0
		if !started.IsZero() && ended.After(started) {
			minutes = ended.Sub(started).Minutes()
		}
		var log mutate.MutationLog
		_ = json.Unmarshal([]byte(payload), &log)
		if operator == "" {
			operator = log.Mutation.Operator
		}
		addEngagementStat(aggregates, "operator", operator, score, hints, minutes)
		profile := log.Profile
		if profile.Summary == "" {
			profile = log.Mutation.Profile
		}
		if profile.Summary != "" {
			addEngagementStat(aggregates, "profile", "locality "+profileAxisName(profile.Locality.Score), score, hints, minutes)
			addEngagementStat(aggregates, "profile", "subtlety "+profileAxisName(profile.Subtlety.Score), score, hints, minutes)
			addEngagementStat(aggregates, "profile", "knowledge "+profileAxisName(profile.Knowledge.Score), score, hints, minutes)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, EngagementStat{}, fmt.Errorf("iterate engagement stats: %w", err)
	}

	stats := make([]EngagementStat, 0, len(aggregates))
	for _, aggregate := range aggregates {
		stat := aggregate.stat
		stat.Average = float64(aggregate.scoreTotal) / float64(stat.Count)
		stat.SolveRate = float64(aggregate.successTotal) / float64(stat.Count)
		stat.AvgHints = float64(aggregate.hintTotal) / float64(stat.Count)
		stat.AvgMinutes = aggregate.minuteTotal / float64(stat.Count)
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].SolveRate != stats[j].SolveRate {
			return stats[i].SolveRate < stats[j].SolveRate
		}
		if stats[i].Average != stats[j].Average {
			return stats[i].Average < stats[j].Average
		}
		if stats[i].AvgHints != stats[j].AvgHints {
			return stats[i].AvgHints > stats[j].AvgHints
		}
		return stats[i].Name < stats[j].Name
	})
	var recommendation EngagementStat
	if len(stats) > 0 {
		stats[0].Recommended = true
		recommendation = stats[0]
	}
	return stats, recommendation, nil
}

func addEngagementStat(aggregates map[string]*engagementAggregate, kind, name string, score, hints int, minutes float64) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := kind + "\x00" + name
	aggregate := aggregates[key]
	if aggregate == nil {
		aggregate = &engagementAggregate{stat: EngagementStat{Name: name, Kind: kind}}
		aggregates[key] = aggregate
	}
	aggregate.stat.Count++
	aggregate.scoreTotal += score
	aggregate.hintTotal += hints
	aggregate.minuteTotal += minutes
	if score > aggregate.stat.Best {
		aggregate.stat.Best = score
	}
	if score > 0 {
		aggregate.successTotal++
	}
}

func profileAxisName(score int) string {
	switch {
	case score >= 3:
		return "high"
	case score == 2:
		return "medium"
	default:
		return "low"
	}
}

func (s *Store) costStats(ctx context.Context) (CostStats, error) {
	var stats CostStats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_input_tokens + cache_read_input_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM coach_usage
	`).Scan(&stats.Calls, &stats.InputTokens, &stats.OutputTokens, &stats.CacheTokens, &stats.TotalUSD)
	if err != nil {
		return CostStats{}, fmt.Errorf("scan cost stats: %w", err)
	}
	var sessions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT session_id) FROM coach_usage`).Scan(&sessions); err != nil {
		return CostStats{}, fmt.Errorf("scan cost sessions: %w", err)
	}
	if sessions > 0 {
		stats.AvgUSDPerSession = stats.TotalUSD / float64(sessions)
	}
	var hintCalls, hintTokens int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(input_tokens + output_tokens + cache_creation_input_tokens + cache_read_input_tokens), 0) FROM coach_usage WHERE operation = 'hint'`).Scan(&hintCalls, &hintTokens); err != nil {
		return CostStats{}, fmt.Errorf("scan hint cost stats: %w", err)
	}
	if hintCalls > 0 {
		stats.TokensPerHint = float64(hintTokens) / float64(hintCalls)
	}
	stats.ProjectedMonthUSD, err = s.projectedMonthlyCost(ctx, stats.TotalUSD)
	if err != nil {
		return CostStats{}, err
	}
	groups, err := s.costGroupStats(ctx)
	if err != nil {
		return CostStats{}, err
	}
	stats.ByBackend = groups
	return stats, nil
}

func (s *Store) projectedMonthlyCost(ctx context.Context, total float64) (float64, error) {
	if total == 0 {
		return 0, nil
	}
	var firstRaw, lastRaw string
	err := s.db.QueryRowContext(ctx, `SELECT MIN(created_at), MAX(created_at) FROM coach_usage`).Scan(&firstRaw, &lastRaw)
	if err != nil {
		return 0, fmt.Errorf("scan cost date range: %w", err)
	}
	first, err := time.Parse(time.RFC3339Nano, firstRaw)
	if err != nil {
		return 0, fmt.Errorf("parse first cost date: %w", err)
	}
	last, err := time.Parse(time.RFC3339Nano, lastRaw)
	if err != nil {
		return 0, fmt.Errorf("parse last cost date: %w", err)
	}
	days := last.Sub(first).Hours() / 24
	if days < 1 {
		days = 1
	}
	return total / days * 30, nil
}

func (s *Store) costGroupStats(ctx context.Context) ([]CostGroupStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT backend,
			COUNT(*),
			COALESCE(SUM(input_tokens + cache_creation_input_tokens + cache_read_input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM coach_usage
		GROUP BY backend
		ORDER BY SUM(cost_usd) DESC, backend
	`)
	if err != nil {
		return nil, fmt.Errorf("query cost group stats: %w", err)
	}
	defer rows.Close()
	var out []CostGroupStat
	for rows.Next() {
		var stat CostGroupStat
		if err := rows.Scan(&stat.Name, &stat.Calls, &stat.InputTokens, &stat.OutputTokens, &stat.TotalUSD); err != nil {
			return nil, fmt.Errorf("scan cost group stats: %w", err)
		}
		out = append(out, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cost group stats: %w", err)
	}
	return out, nil
}

type OpBreakdown struct {
	Operator  string
	Count     int
	SolveRate float64
}

func (s *Store) OpBreakdown(ctx context.Context) ([]OpBreakdown, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.operator, COUNT(*), COALESCE(SUM(CASE WHEN s.score > 0 THEN 1 ELSE 0 END), 0)
		FROM mutation_logs m
		LEFT JOIN sessions s ON s.id = m.session_id
		GROUP BY m.operator
		ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query op breakdown: %w", err)
	}
	defer rows.Close()
	var out []OpBreakdown
	for rows.Next() {
		var ob OpBreakdown
		var successes int
		if err := rows.Scan(&ob.Operator, &ob.Count, &successes); err != nil {
			return nil, fmt.Errorf("scan op breakdown: %w", err)
		}
		if ob.Count > 0 {
			ob.SolveRate = float64(successes) / float64(ob.Count)
		}
		out = append(out, ob)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate op breakdown: %w", err)
	}
	return out, nil
}
