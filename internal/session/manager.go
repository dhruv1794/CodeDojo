package session

import (
	"context"
	"fmt"
	"time"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

type Store interface {
	CreateSession(ctx context.Context, sess Session) error
	GetSession(ctx context.Context, id string) (Session, error)
	AppendEvent(ctx context.Context, event Event) error
	UpdateState(ctx context.Context, id string, state State) error
}

type Manager struct {
	Coach  coach.Coach
	Store  Store
	Driver sandbox.Driver
}

func (m Manager) New(ctx context.Context, sess Session, spec sandbox.Spec) (sandbox.Session, error) {
	if sess.ID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	sess.State = StateCreated
	if sess.StartedAt.IsZero() {
		sess.StartedAt = time.Now()
	}
	if err := m.Store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	if err := m.Store.AppendEvent(ctx, Event{SessionID: sess.ID, Type: EventCreated}); err != nil {
		return nil, err
	}
	box, err := m.Driver.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	if err := m.Store.UpdateState(ctx, sess.ID, StateRunning); err != nil {
		_ = box.Close()
		return nil, err
	}
	if err := m.Store.AppendEvent(ctx, Event{SessionID: sess.ID, Type: EventStarted}); err != nil {
		_ = box.Close()
		return nil, err
	}
	return box, nil
}

func (m Manager) RequestHint(ctx context.Context, sessionID string, level coach.HintLevel, text string) (coach.Hint, error) {
	hint, err := m.Coach.Hint(ctx, coach.HintRequest{SessionID: sessionID, Level: level, Context: text})
	if err != nil {
		return coach.Hint{}, err
	}
	if err := m.Store.AppendEvent(ctx, Event{SessionID: sessionID, Type: EventHint, Payload: hint.Content}); err != nil {
		return coach.Hint{}, err
	}
	return hint, nil
}

func (m Manager) Submit(ctx context.Context, sessionID, payload string) error {
	sess, err := m.Store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := Transition(sess.State, StateSubmitted); err != nil {
		return err
	}
	if err := m.Store.UpdateState(ctx, sessionID, StateSubmitted); err != nil {
		return err
	}
	return m.Store.AppendEvent(ctx, Event{SessionID: sessionID, Type: EventSubmit, Payload: payload})
}

func (m Manager) Close(ctx context.Context, sessionID string, box sandbox.Session) error {
	sess, err := m.Store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := Transition(sess.State, StateClosed); err != nil {
		return err
	}
	if box != nil {
		if err := box.Close(); err != nil {
			return err
		}
	}
	if err := m.Store.UpdateState(ctx, sessionID, StateClosed); err != nil {
		return err
	}
	return m.Store.AppendEvent(ctx, Event{SessionID: sessionID, Type: EventClosed})
}
