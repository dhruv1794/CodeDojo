package session_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/coach/mock"
	"github.com/dhruvmishra/codedojo/internal/sandbox"
	"github.com/dhruvmishra/codedojo/internal/sandbox/local"
	"github.com/dhruvmishra/codedojo/internal/session"
	"github.com/dhruvmishra/codedojo/internal/store/memory"
)

func TestManagerHappyPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repoPath := newManagerGitFixture(t)
	store := memory.New()
	manager := session.Manager{
		Coach:  mock.Coach{},
		Store:  store,
		Driver: local.Driver{},
	}

	box, err := manager.New(ctx, session.Session{
		ID:         "sess-happy",
		Mode:       session.ModeReviewer,
		Repo:       repoPath,
		Task:       "find the regression",
		HintBudget: 3,
	}, sandbox.Spec{RepoMount: repoPath, Network: sandbox.NetworkNone})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	sess, err := store.GetSession(ctx, "sess-happy")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sess.State != session.StateRunning {
		t.Fatalf("session state after New = %s, want %s", sess.State, session.StateRunning)
	}
	if sess.StartedAt.IsZero() {
		t.Fatal("StartedAt is zero after New")
	}

	result, err := box.Exec(ctx, []string{"go", "test", "./..."})
	if err != nil {
		t.Fatalf("sandbox Exec() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("go test failed\nstdout:\n%s\nstderr:\n%s", result.Stdout, result.Stderr)
	}

	hint, err := manager.RequestHint(ctx, "sess-happy", coach.LevelPointer, "failing edge case", "Go", 3, 3)
	if err != nil {
		t.Fatalf("RequestHint() error = %v", err)
	}
	if hint.Level != coach.LevelPointer || hint.Content == "" || hint.Cost != 20 {
		t.Fatalf("RequestHint() = %+v, want pointer hint with content and cost", hint)
	}

	if err := manager.Submit(ctx, "sess-happy", "calc/add.go:4 addition edge case"); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	sess, err = store.GetSession(ctx, "sess-happy")
	if err != nil {
		t.Fatalf("GetSession() after Submit error = %v", err)
	}
	if sess.State != session.StateSubmitted {
		t.Fatalf("session state after Submit = %s, want %s", sess.State, session.StateSubmitted)
	}

	if err := manager.Close(ctx, "sess-happy", box); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	sess, err = store.GetSession(ctx, "sess-happy")
	if err != nil {
		t.Fatalf("GetSession() after Close error = %v", err)
	}
	if sess.State != session.StateClosed {
		t.Fatalf("session state after Close = %s, want %s", sess.State, session.StateClosed)
	}
	if _, err := box.Exec(ctx, []string{"go", "test", "./..."}); err == nil {
		t.Fatal("Exec() after manager Close succeeded, want closed sandbox error")
	}

	events, err := store.ListEvents(ctx, "sess-happy")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	gotTypes := make([]session.EventType, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	wantTypes := []session.EventType{session.EventCreated, session.EventStarted, session.EventHint, session.EventSubmit, session.EventClosed}
	if strings.Join(eventTypesToStrings(gotTypes), ",") != strings.Join(eventTypesToStrings(wantTypes), ",") {
		t.Fatalf("events = %v, want %v", gotTypes, wantTypes)
	}
	if events[2].Payload != hint.Content {
		t.Fatalf("hint event payload = %q, want %q", events[2].Payload, hint.Content)
	}
	if events[3].Payload != "calc/add.go:4 addition edge case" {
		t.Fatalf("submit event payload = %q, want submitted payload", events[3].Payload)
	}
}

func TestManagerSubmitRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, state := range []session.State{session.StateCreated, session.StateSubmitted, session.StateGraded, session.StateClosed} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			store := memory.New()
			if err := store.CreateSession(ctx, session.Session{ID: "sess-" + string(state), State: state}); err != nil {
				t.Fatalf("CreateSession() error = %v", err)
			}
			manager := session.Manager{Store: store}

			err := manager.Submit(ctx, "sess-"+string(state), "payload")
			if err == nil {
				t.Fatal("Submit() succeeded, want invalid transition error")
			}
			if !strings.Contains(err.Error(), "invalid session transition") {
				t.Fatalf("Submit() error = %v, want invalid transition", err)
			}
			sess, err := store.GetSession(ctx, "sess-"+string(state))
			if err != nil {
				t.Fatalf("GetSession() error = %v", err)
			}
			if sess.State != state {
				t.Fatalf("state after rejected Submit = %s, want %s", sess.State, state)
			}
			events, err := store.ListEvents(ctx, "sess-"+string(state))
			if err != nil {
				t.Fatalf("ListEvents() error = %v", err)
			}
			if len(events) != 0 {
				t.Fatalf("events after rejected Submit = %v, want none", events)
			}
		})
	}
}

func TestManagerCloseRejectsClosedSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	if err := store.CreateSession(ctx, session.Session{ID: "sess-closed", State: session.StateClosed}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	box := &recordingSandbox{}
	manager := session.Manager{Store: store}

	err := manager.Close(ctx, "sess-closed", box)
	if err == nil {
		t.Fatal("Close() succeeded, want invalid transition error")
	}
	if !strings.Contains(err.Error(), "invalid session transition") {
		t.Fatalf("Close() error = %v, want invalid transition", err)
	}
	if box.closed {
		t.Fatal("Close() closed sandbox after rejected transition")
	}
	events, err := store.ListEvents(ctx, "sess-closed")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events after rejected Close = %v, want none", events)
	}
}

func TestManagerCloseAllowsEveryOpenState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, state := range []session.State{session.StateCreated, session.StateRunning, session.StateSubmitted, session.StateGraded} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			store := memory.New()
			id := "sess-" + string(state)
			if err := store.CreateSession(ctx, session.Session{ID: id, State: state}); err != nil {
				t.Fatalf("CreateSession() error = %v", err)
			}
			box := &recordingSandbox{}
			manager := session.Manager{Store: store}

			if err := manager.Close(ctx, id, box); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if !box.closed {
				t.Fatal("Close() did not close sandbox")
			}
			sess, err := store.GetSession(ctx, id)
			if err != nil {
				t.Fatalf("GetSession() error = %v", err)
			}
			if sess.State != session.StateClosed {
				t.Fatalf("state after Close = %s, want %s", sess.State, session.StateClosed)
			}
		})
	}
}

type recordingSandbox struct {
	closed bool
}

func (s *recordingSandbox) Exec(ctx context.Context, cmd []string) (sandbox.ExecResult, error) {
	return sandbox.ExecResult{}, nil
}

func (s *recordingSandbox) WriteFile(path string, data []byte) error {
	return nil
}

func (s *recordingSandbox) ReadFile(path string) ([]byte, error) {
	return nil, nil
}

func (s *recordingSandbox) Diff() (string, error) {
	return "", nil
}

func (s *recordingSandbox) Close() error {
	s.closed = true
	return nil
}

func eventTypesToStrings(types []session.EventType) []string {
	out := make([]string, 0, len(types))
	for _, typ := range types {
		out = append(out, string(typ))
	}
	return out
}

func newManagerGitFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runManagerGit(t, dir, "init")
	runManagerGit(t, dir, "config", "user.email", "codedojo@example.com")
	runManagerGit(t, dir, "config", "user.name", "CodeDojo Test")

	writeManagerFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.23\n")
	writeManagerFile(t, dir, "calc/add.go", "package calc\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeManagerFile(t, dir, "calc/add_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"wrong sum\")\n\t}\n}\n")
	runManagerGit(t, dir, "add", ".")
	runManagerGit(t, dir, "commit", "-m", "initial")
	return dir
}

func writeManagerFile(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runManagerGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
