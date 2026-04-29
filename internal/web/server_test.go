package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
