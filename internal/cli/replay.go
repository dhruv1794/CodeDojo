// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/spf13/cobra"
)

func newReplayCommand() *cobra.Command {
	var format string
	var step bool
	var delay time.Duration
	cmd := &cobra.Command{
		Use:   "replay <session-id>",
		Short: "Replay a completed or active kata timeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplayWithOptions(cmd.Context(), cmd, args[0], replayOptions{
				Format: format,
				Step:   step,
				Delay:  delay,
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	cmd.Flags().BoolVar(&step, "step", false, "render replay as numbered playback steps")
	cmd.Flags().DurationVar(&delay, "delay", 0, "wait between playback steps, for example 500ms or 1s")
	return cmd
}

type replayOptions struct {
	Format string
	Step   bool
	Delay  time.Duration
}

func runReplay(ctx context.Context, cmd *cobra.Command, sessionID, format string) error {
	return runReplayWithOptions(ctx, cmd, sessionID, replayOptions{Format: format})
}

func runReplayWithOptions(ctx context.Context, cmd *cobra.Command, sessionID string, opts replayOptions) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported replay format %q", format)
	}
	if opts.Delay < 0 {
		return fmt.Errorf("replay delay must be non-negative")
	}
	if format == "json" && (opts.Step || opts.Delay > 0) {
		return fmt.Errorf("replay pacing options require text format")
	}
	if opts.Delay > 0 {
		opts.Step = true
	}
	store, err := openConfiguredStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	events, err := store.ListEvents(ctx, sessionID)
	if err != nil {
		return err
	}
	mutationLogs, err := store.ListMutationLogs(ctx, sessionID)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if format == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(newReplayExport(sess, mutationLogs, events))
	}
	ui := themeFor(cmd)
	if _, err := fmt.Fprintf(out, "%s\n%s %s\n%s %s\n%s %s\n%s %d\n\n",
		ui.Banner("Kata replay"),
		ui.Label("Session"), sess.ID,
		ui.Label("Mode"), sess.Mode,
		ui.Label("Repo"), sess.Repo,
		ui.Label("Score"), sess.Score,
	); err != nil {
		return err
	}
	for _, log := range mutationLogs {
		if err := printReplayMutation(out, log); err != nil {
			return err
		}
	}
	if len(events) == 0 {
		_, err := fmt.Fprintln(out, ui.Muted("No replay events recorded."))
		return err
	}
	if opts.Step {
		return printReplayStepTimeline(ctx, out, events, opts.Delay)
	}
	for _, event := range events {
		if _, err := fmt.Fprintf(out, "%s  %-8s %s\n", formatReplayTime(event), replayLabel(event.Type), replayPayload(event)); err != nil {
			return err
		}
	}
	return nil
}

func printReplayStepTimeline(ctx context.Context, out replayWriter, events []session.Event, delay time.Duration) error {
	if _, err := fmt.Fprintln(out, "Playback steps"); err != nil {
		return err
	}
	startedAt := firstReplayEventTime(events)
	var previous time.Time
	for i, event := range events {
		if i > 0 && delay > 0 {
			if err := waitReplayDelay(ctx, delay); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(
			out,
			"\nStep %d/%d  %s  gap %s  %s  %s\n",
			i+1,
			len(events),
			formatReplayOffset(startedAt, event.CreatedAt),
			formatReplayGap(previous, event.CreatedAt),
			formatReplayTime(event),
			replayLabel(event.Type),
		); err != nil {
			return err
		}
		payload := replayPayload(event)
		for _, line := range strings.Split(payload, "\n") {
			if _, err := fmt.Fprintf(out, "  %s\n", line); err != nil {
				return err
			}
		}
		previous = event.CreatedAt
	}
	return nil
}

func waitReplayDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type replayExport struct {
	SchemaVersion int                  `json:"schema_version"`
	Session       replaySessionExport  `json:"session"`
	Mutations     []mutate.MutationLog `json:"mutations,omitempty"`
	Events        []replayEventExport  `json:"events"`
}

type replaySessionExport struct {
	ID         string        `json:"id"`
	Mode       session.Mode  `json:"mode"`
	Repo       string        `json:"repo"`
	Task       string        `json:"task"`
	HintBudget int           `json:"hint_budget"`
	HintsUsed  int           `json:"hints_used"`
	Score      int           `json:"score"`
	State      session.State `json:"state"`
	StartedAt  time.Time     `json:"started_at"`
}

type replayEventExport struct {
	ID        int64             `json:"id"`
	Type      session.EventType `json:"type"`
	Label     string            `json:"label"`
	Payload   string            `json:"payload"`
	CreatedAt time.Time         `json:"created_at"`
}

func newReplayExport(sess session.Session, mutations []mutate.MutationLog, events []session.Event) replayExport {
	out := replayExport{
		SchemaVersion: 1,
		Session: replaySessionExport{
			ID:         sess.ID,
			Mode:       sess.Mode,
			Repo:       sess.Repo,
			Task:       sess.Task,
			HintBudget: sess.HintBudget,
			HintsUsed:  sess.HintsUsed,
			Score:      sess.Score,
			State:      sess.State,
			StartedAt:  sess.StartedAt,
		},
		Mutations: mutations,
		Events:    make([]replayEventExport, 0, len(events)),
	}
	for _, event := range events {
		out.Events = append(out.Events, replayEventExport{
			ID:        event.ID,
			Type:      event.Type,
			Label:     replayLabel(event.Type),
			Payload:   event.Payload,
			CreatedAt: event.CreatedAt,
		})
	}
	return out
}

type replayWriter interface {
	Write([]byte) (int, error)
}

func printReplayMutation(out replayWriter, log mutate.MutationLog) error {
	mutation := log.Mutation
	if _, err := fmt.Fprintf(out, "Mutation\n  file: %s:%d-%d\n  operator: %s\n",
		mutation.FilePath,
		mutation.StartLine,
		mutation.EndLine,
		mutation.Operator,
	); err != nil {
		return err
	}
	if strings.TrimSpace(mutation.Description) != "" {
		if _, err := fmt.Fprintf(out, "  description: %s\n", mutation.Description); err != nil {
			return err
		}
	}
	profile := log.Profile
	if profile.Summary == "" {
		profile = mutation.Profile
	}
	if profile.Summary != "" {
		if _, err := fmt.Fprintf(out, "  profile: %s\n", profile.Summary); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    locality: %d/3 - %s\n", profile.Locality.Score, profile.Locality.Reason); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    subtlety: %d/3 - %s\n", profile.Subtlety.Score, profile.Subtlety.Reason); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    knowledge: %d/3 - %s\n", profile.Knowledge.Score, profile.Knowledge.Reason); err != nil {
			return err
		}
	}
	if strings.TrimSpace(mutation.Original) != "" || strings.TrimSpace(mutation.Mutated) != "" {
		if _, err := fmt.Fprintln(out, "\nOriginal snapshot"); err != nil {
			return err
		}
		if err := printReplaySnippet(out, mutation.Original, mutation.StartLine, mutation.EndLine); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, "Mutated snapshot"); err != nil {
			return err
		}
		if err := printReplaySnippet(out, mutation.Mutated, mutation.StartLine, mutation.EndLine); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func printReplaySnippet(out replayWriter, code string, startLine, endLine int) error {
	lines := strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		_, err := fmt.Fprintln(out, "  (snapshot unavailable)")
		return err
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	from := max(1, startLine-2)
	to := min(len(lines), endLine+2)
	for lineNo := from; lineNo <= to; lineNo++ {
		marker := " "
		if lineNo >= startLine && lineNo <= endLine {
			marker = ">"
		}
		if _, err := fmt.Fprintf(out, "%s %4d | %s\n", marker, lineNo, lines[lineNo-1]); err != nil {
			return err
		}
	}
	return nil
}

func formatReplayTime(event session.Event) string {
	if event.CreatedAt.IsZero() {
		return "--:--:--"
	}
	return event.CreatedAt.Local().Format("15:04:05")
}

func firstReplayEventTime(events []session.Event) time.Time {
	for _, event := range events {
		if !event.CreatedAt.IsZero() {
			return event.CreatedAt
		}
	}
	return time.Time{}
}

func formatReplayOffset(startedAt, eventAt time.Time) string {
	if startedAt.IsZero() || eventAt.IsZero() {
		return "+--:--"
	}
	return "+" + formatReplayDuration(eventAt.Sub(startedAt))
}

func formatReplayGap(previousAt, eventAt time.Time) string {
	if previousAt.IsZero() || eventAt.IsZero() {
		return "+00:00"
	}
	return "+" + formatReplayDuration(eventAt.Sub(previousAt))
}

func formatReplayDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second).Seconds())
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	seconds %= 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func replayLabel(typ session.EventType) string {
	switch typ {
	case session.EventCreated:
		return "created"
	case session.EventStarted:
		return "started"
	case session.EventFile:
		return "opened"
	case session.EventWrite:
		return "wrote"
	case session.EventTests:
		return "tests"
	case session.EventDiff:
		return "diff"
	case session.EventHint:
		return "hint"
	case session.EventSubmit:
		return "submit"
	case session.EventGrade:
		return "grade"
	case session.EventCommentary:
		return "comment"
	case session.EventTrace:
		return "trace"
	case session.EventClosed:
		return "closed"
	default:
		return string(typ)
	}
}

func replayPayload(event session.Event) string {
	if strings.TrimSpace(event.Payload) == "" {
		return "-"
	}
	return event.Payload
}
