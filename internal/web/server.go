package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/dhruvmishra/codedojo/internal/app"
	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/session"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	app *app.Service
	mux *http.ServeMux
}

func New(service *app.Service) (*Server, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	s := &Server{app: service, mux: http.NewServeMux()}
	s.routes(http.FS(sub))
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes(static http.FileSystem) {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("POST /api/sessions/learn", s.startLearn)
	s.mux.HandleFunc("POST /api/sessions/review", s.startReview)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/files", s.listFiles)
	s.mux.HandleFunc("GET /api/sessions/{id}/files/", s.readFile)
	s.mux.HandleFunc("PUT /api/sessions/{id}/files/", s.writeFile)
	s.mux.HandleFunc("POST /api/sessions/{id}/tests", s.runTests)
	s.mux.HandleFunc("GET /api/sessions/{id}/diff", s.diff)
	s.mux.HandleFunc("POST /api/sessions/{id}/hints", s.hint)
	s.mux.HandleFunc("POST /api/sessions/{id}/submit", s.submit)
	s.mux.HandleFunc("POST /api/sessions/{id}/close", s.closeSession)
	s.mux.Handle("GET /", http.FileServer(static))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) startLearn(w http.ResponseWriter, r *http.Request) {
	var req app.StartOptions
	if !decodeJSON(w, r, &req) {
		return
	}
	live, err := s.app.StartLearn(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, publicSession(live))
}

func (s *Server) startReview(w http.ResponseWriter, r *http.Request) {
	var req app.StartOptions
	if !decodeJSON(w, r, &req) {
		return
	}
	live, err := s.app.StartReview(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, publicSession(live))
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	live, err := s.app.Session(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicSession(live))
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.app.ListFiles(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) readFile(w http.ResponseWriter, r *http.Request) {
	content, err := s.app.ReadFile(r.PathValue("id"), filePath(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) writeFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.app.WriteFile(r.PathValue("id"), filePath(r), req.Content); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) runTests(w http.ResponseWriter, r *http.Request) {
	result, err := s.app.RunTests(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	diff, err := s.app.Diff(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

func (s *Server) hint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	hint, err := s.app.Hint(r.Context(), r.PathValue("id"), parseHintLevel(req.Level))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hint)
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	live, err := s.app.Session(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	switch live.Mode {
	case session.ModeNewcomer:
		result, err := s.app.SubmitLearn(r.Context(), live.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case session.ModeReviewer:
		var req app.ReviewSubmission
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := s.app.SubmitReview(r.Context(), live.ID, req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeError(w, fmt.Errorf("unsupported mode %q", live.Mode))
	}
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	if err := s.app.CloseSession(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func filePath(r *http.Request) string {
	prefix := "/api/sessions/" + r.PathValue("id") + "/files/"
	return strings.TrimPrefix(r.URL.Path, prefix)
}

func parseHintLevel(value string) coach.HintLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "question":
		return coach.LevelQuestion
	case "pointer":
		return coach.LevelPointer
	case "concept":
		return coach.LevelConcept
	default:
		return coach.LevelNudge
	}
}

type sessionResponse struct {
	ID         string       `json:"id"`
	Mode       session.Mode `json:"mode"`
	Repo       string       `json:"repo"`
	Task       string       `json:"task"`
	Difficulty int          `json:"difficulty"`
	HintBudget int          `json:"hint_budget"`
	HintsUsed  int          `json:"hints_used"`
	Streak     int          `json:"streak"`
	StartedAt  string       `json:"started_at"`
	Done       bool         `json:"done"`
}

func publicSession(live *app.LiveSession) sessionResponse {
	return sessionResponse{
		ID:         live.ID,
		Mode:       live.Mode,
		Repo:       live.Repo,
		Task:       live.Task,
		Difficulty: live.Difficulty,
		HintBudget: live.HintBudget,
		HintsUsed:  live.HintsUsed,
		Streak:     live.Streak,
		StartedAt:  live.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		Done:       live.Done,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, fmt.Errorf("invalid json: %w", err))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, http.ErrNoLocation) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
