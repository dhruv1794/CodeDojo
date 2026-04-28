package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dhruvmishra/codedojo/internal/session"
)

type Store struct {
	mu       sync.Mutex
	sessions map[string]session.Session
	events   map[string][]session.Event
	nextID   int64
}

func New() *Store {
	return &Store{
		sessions: map[string]session.Session{},
		events:   map[string][]session.Event{},
	}
}

func (s *Store) CreateSession(ctx context.Context, sess session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sess.ID]; ok {
		return fmt.Errorf("session %q already exists", sess.ID)
	}
	s.sessions[sess.ID] = sess
	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return session.Session{}, fmt.Errorf("session %q not found", id)
	}
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out, nil
}

func (s *Store) AppendEvent(ctx context.Context, event session.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	event.ID = s.nextID
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	s.events[event.SessionID] = append(s.events[event.SessionID], event)
	return nil
}

func (s *Store) ListEvents(ctx context.Context, sessionID string) ([]session.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]session.Event(nil), s.events[sessionID]...), nil
}

func (s *Store) UpdateState(ctx context.Context, id string, state session.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	sess.State = state
	s.sessions[id] = sess
	return nil
}

func (s *Store) UpsertScore(ctx context.Context, id string, score int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	sess.Score = score
	s.sessions[id] = sess
	return nil
}
