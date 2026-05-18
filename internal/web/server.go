// SPDX-License-Identifier: MIT

package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhruvmishra/codedojo/internal/app"
	"github.com/dhruvmishra/codedojo/internal/coach"
	"github.com/dhruvmishra/codedojo/internal/session"
)

//go:embed static/*
var staticFS embed.FS

const maxRepoBrowserEntries = 500

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
	s.mux.HandleFunc("GET /api/repos/browse", s.browseRepos)
	s.mux.HandleFunc("POST /api/repos/open", s.openRepo)
	s.mux.HandleFunc("POST /api/preflight", s.preflight)
	s.mux.HandleFunc("GET /api/sessions", s.listSessions)
	s.mux.HandleFunc("POST /api/sessions/learn", s.startLearn)
	s.mux.HandleFunc("POST /api/sessions/review", s.startReview)
	s.mux.HandleFunc("POST /api/sessions/sensei", s.startSensei)
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

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) browseRepos(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	showHidden := r.URL.Query().Get("hidden") == "1"
	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil {
			writeError(w, fmt.Errorf("find home directory: %w", err))
			return
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		writeError(w, fmt.Errorf("resolve browser path: %w", err))
		return
	}
	info, err := os.Lstat(abs)
	if err != nil {
		writeError(w, fmt.Errorf("inspect browser path: %w", err))
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		writeError(w, fmt.Errorf("browser path is a symlink; choose its real target path instead"))
		return
	}
	if !info.IsDir() {
		writeError(w, fmt.Errorf("browser path is not a directory"))
		return
	}
	entries, truncated, err := repoBrowserEntries(abs, showHidden, maxRepoBrowserEntries)
	if err != nil {
		writeError(w, err)
		return
	}
	parent := ""
	if parentPath := filepath.Dir(abs); parentPath != abs {
		parent = parentPath
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":      abs,
		"parent":    parent,
		"is_repo":   looksLikeRepo(abs),
		"entries":   entries,
		"hidden":    showHidden,
		"truncated": truncated,
	})
}

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.app.Preflight(r.Context(), req.Repo)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) openRepo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.app.Preflight(r.Context(), strings.TrimSpace(req.Repo))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
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

func (s *Server) startSensei(w http.ResponseWriter, r *http.Request) {
	var req app.SenseiStartOptions
	if !decodeJSON(w, r, &req) {
		return
	}
	live, err := s.app.StartSensei(r.Context(), req)
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

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.app.ListSessions(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func filePath(r *http.Request) string {
	prefix := "/api/sessions/" + r.PathValue("id") + "/files/"
	return strings.TrimPrefix(r.URL.Path, prefix)
}

type repoBrowserEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Repo    bool   `json:"repo"`
	Hidden  bool   `json:"hidden"`
	Symlink bool   `json:"symlink"`
}

func repoBrowserEntries(path string, showHidden bool, limit int) ([]repoBrowserEntry, bool, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, fmt.Errorf("read browser directory: %w", err)
	}
	entries := make([]repoBrowserEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		name := entry.Name()
		full := filepath.Join(path, name)
		symlink := entry.Type()&os.ModeSymlink != 0
		if !entry.IsDir() {
			if !symlink || !symlinkPointsToDir(full) {
				continue
			}
		}
		hidden := strings.HasPrefix(name, ".")
		if hidden && !showHidden {
			continue
		}
		repo := false
		if !symlink {
			repo = looksLikeRepo(full)
		}
		entries = append(entries, repoBrowserEntry{
			Name:    name,
			Path:    full,
			Repo:    repo,
			Hidden:  hidden,
			Symlink: symlink,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Repo != entries[j].Repo {
			return entries[i].Repo
		}
		if entries[i].Hidden != entries[j].Hidden {
			return !entries[i].Hidden
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	truncated := false
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
		truncated = true
	}
	return entries, truncated, nil
}

func symlinkPointsToDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func looksLikeRepo(path string) bool {
	for _, marker := range []string{".git", "go.mod", "package.json", "pyproject.toml", "requirements.txt", "setup.py", "Cargo.toml", ".codedojo.yaml"} {
		if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
			return true
		}
	}
	return false
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
	ID         string         `json:"id"`
	Mode       session.Mode   `json:"mode"`
	Repo       string         `json:"repo"`
	Task       string         `json:"task"`
	TaskFiles  []app.TaskFile `json:"task_files,omitempty"`
	Difficulty int            `json:"difficulty"`
	HintBudget int            `json:"hint_budget"`
	HintsUsed  int            `json:"hints_used"`
	Streak     int            `json:"streak"`
	StartedAt  string         `json:"started_at"`
	Done       bool           `json:"done"`
}

func publicSession(live *app.LiveSession) sessionResponse {
	return sessionResponse{
		ID:         live.ID,
		Mode:       live.Mode,
		Repo:       live.Repo,
		Task:       live.Task,
		TaskFiles:  live.TaskFiles,
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
	product := app.ExplainError(err)
	writeJSON(w, product.Status, product)
}
