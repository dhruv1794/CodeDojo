// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteErrorReturnsProductMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, errors.New("no test command detected for repo"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body struct {
		Code    string   `json:"code"`
		Title   string   `json:"title"`
		Message string   `json:"message"`
		Actions []string `json:"actions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "no_test_command" || body.Title == "" || body.Message == "" {
		t.Fatalf("body = %#v, want structured product error", body)
	}
	if len(body.Actions) == 0 {
		t.Fatalf("actions are empty: %#v", body)
	}
}

func TestOpenRepoRouteReportsInvalidJSON(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/open", bytes.NewBufferString(`{`))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "request_failed" {
		t.Fatalf("code = %q, want request_failed", body.Code)
	}
}

func TestStartSenseiRouteReportsInvalidJSON(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sensei", bytes.NewBufferString(`{`))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "request_failed" {
		t.Fatalf("code = %q, want request_failed", body.Code)
	}
}

func TestBrowseReposListsDirectoriesAndMarksRepos(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "project")
	plainPath := filepath.Join(root, "notes")
	hiddenPath := filepath.Join(root, ".hidden")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo marker: %v", err)
	}
	if err := os.MkdirAll(plainPath, 0o755); err != nil {
		t.Fatalf("mkdir plain dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(hiddenPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir hidden dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/browse?path="+root, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Path    string `json:"path"`
		Parent  string `json:"parent"`
		IsRepo  bool   `json:"is_repo"`
		Hidden  bool   `json:"hidden"`
		Entries []struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			Repo   bool   `json:"repo"`
			Hidden bool   `json:"hidden"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Path != root || body.Parent == "" || body.IsRepo {
		t.Fatalf("body metadata = %#v, want browsed non-repo root with parent", body)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("entries = %#v, want two directories", body.Entries)
	}
	if body.Entries[0].Name != "project" || !body.Entries[0].Repo || body.Entries[0].Path != repoPath {
		t.Fatalf("first entry = %#v, want repo directory first", body.Entries[0])
	}
	if body.Entries[1].Name != "notes" || body.Entries[1].Repo {
		t.Fatalf("second entry = %#v, want plain directory", body.Entries[1])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/repos/browse?hidden=1&path="+root, nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("hidden status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var hiddenBody struct {
		Hidden  bool `json:"hidden"`
		Entries []struct {
			Name   string `json:"name"`
			Hidden bool   `json:"hidden"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hiddenBody); err != nil {
		t.Fatalf("decode hidden body: %v", err)
	}
	if !hiddenBody.Hidden {
		t.Fatalf("hidden flag = false, want true")
	}
	foundHidden := false
	for _, entry := range hiddenBody.Entries {
		if entry.Name == ".hidden" && entry.Hidden {
			foundHidden = true
		}
	}
	if !foundHidden {
		t.Fatalf("entries = %#v, want hidden directory when hidden=1", hiddenBody.Entries)
	}
}

func TestBrowseReposMarksSymlinkedDirsWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target-repo")
	link := filepath.Join(root, "linked-repo")
	if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir target repo: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	server, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/browse?path="+root, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Entries []struct {
			Name    string `json:"name"`
			Repo    bool   `json:"repo"`
			Symlink bool   `json:"symlink"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	var found bool
	for _, entry := range body.Entries {
		if entry.Name != "linked-repo" {
			continue
		}
		found = true
		if !entry.Symlink {
			t.Fatalf("linked entry = %#v, want symlink marker", entry)
		}
		if entry.Repo {
			t.Fatalf("linked entry = %#v, want repo detection to avoid following symlink", entry)
		}
	}
	if !found {
		t.Fatalf("entries = %#v, want linked-repo symlinked directory", body.Entries)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/repos/browse?path="+link, nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("symlink status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
