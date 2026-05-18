// SPDX-License-Identifier: MIT

package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalRequestGuardAllowsLocalHostAndOrigin(t *testing.T) {
	t.Parallel()

	guarded := localRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "127.0.0.1:18080")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18080/api/preflight", nil)
	req.Host = "localhost:18080"
	req.Header.Set("Origin", "http://localhost:18080")
	rec := httptest.NewRecorder()

	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestLocalRequestGuardRejectsUnexpectedHost(t *testing.T) {
	t.Parallel()

	guarded := localRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler was called")
	}), "127.0.0.1:18080")

	req := httptest.NewRequest(http.MethodGet, "http://evil.test/api/health", nil)
	req.Host = "evil.test"
	rec := httptest.NewRecorder()

	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestLocalRequestGuardRejectsCrossOriginRequest(t *testing.T) {
	t.Parallel()

	guarded := localRequestGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler was called")
	}), "127.0.0.1:18080")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18080/api/preflight", nil)
	req.Host = "127.0.0.1:18080"
	req.Header.Set("Origin", "http://evil.test")
	rec := httptest.NewRecorder()

	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
