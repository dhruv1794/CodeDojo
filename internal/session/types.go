package session

import "time"

type Mode string

const (
	ModeReviewer Mode = "reviewer"
	ModeNewcomer Mode = "newcomer"
)

type State string

const (
	StateCreated   State = "created"
	StateRunning   State = "running"
	StateSubmitted State = "submitted"
	StateGraded    State = "graded"
	StateClosed    State = "closed"
)

type EventType string

const (
	EventCreated EventType = "created"
	EventStarted EventType = "started"
	EventHint    EventType = "hint"
	EventSubmit  EventType = "submit"
	EventGrade   EventType = "grade"
	EventClosed  EventType = "closed"
)

type Session struct {
	ID         string
	Mode       Mode
	Repo       string
	Task       string
	HintBudget int
	HintsUsed  int
	Score      int
	State      State
	StartedAt  time.Time
}

type Event struct {
	ID        int64
	SessionID string
	Type      EventType
	Payload   string
	CreatedAt time.Time
}
