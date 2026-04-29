package app

import (
	"errors"
	"net/http"
	"testing"
)

func TestExplainErrorMapsRecoverableFailures(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantTitle string
	}{
		{
			name:      "test command",
			err:       errors.New("no test command detected for repo"),
			wantCode:  "no_test_command",
			wantTitle: "No test command detected",
		},
		{
			name:      "learn commits",
			err:       errors.New("no suitable newcomer commits found"),
			wantCode:  "no_learn_commits",
			wantTitle: "No Learn tasks found",
		},
		{
			name:      "review language",
			err:       errors.New("review mode currently supports Go repositories only; detected rust"),
			wantCode:  "review_unsupported_language",
			wantTitle: "Review is Go-only right now",
		},
		{
			name:      "review candidates",
			err:       errors.New("no mutation candidates found"),
			wantCode:  "no_review_candidates",
			wantTitle: "No hidden-bug candidates found",
		},
		{
			name:      "docker",
			err:       errors.New("docker daemon unavailable"),
			wantCode:  "docker_unavailable",
			wantTitle: "Docker sandbox unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExplainError(tt.err)
			if got.Code != tt.wantCode || got.Title != tt.wantTitle {
				t.Fatalf("ExplainError() = %#v, want code %q title %q", got, tt.wantCode, tt.wantTitle)
			}
			if got.Message == "" {
				t.Fatalf("Message is empty: %#v", got)
			}
			if len(got.Actions) == 0 {
				t.Fatalf("Actions are empty: %#v", got)
			}
			if got.Status != http.StatusBadRequest {
				t.Fatalf("Status = %d, want %d", got.Status, http.StatusBadRequest)
			}
		})
	}
}

func TestExplainErrorMapsMissingSessionToNotFound(t *testing.T) {
	got := ExplainError(errors.New(`session "missing" not found`))
	if got.Code != "session_not_found" {
		t.Fatalf("Code = %q, want session_not_found", got.Code)
	}
	if got.Status != http.StatusNotFound {
		t.Fatalf("Status = %d, want %d", got.Status, http.StatusNotFound)
	}
}

func TestExplainErrorHandlesNil(t *testing.T) {
	got := ExplainError(nil)
	if got.Code != "request_failed" {
		t.Fatalf("Code = %q, want request_failed", got.Code)
	}
	if got.Status != http.StatusInternalServerError {
		t.Fatalf("Status = %d, want %d", got.Status, http.StatusInternalServerError)
	}
}
