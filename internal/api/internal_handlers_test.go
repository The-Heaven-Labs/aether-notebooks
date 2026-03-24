package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInternalYjsEndpoints(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("yjs-%d@example.com", ts), "Yjs Org")
	nbID := createNotebook(t, srv, token, "Collab Notebook")

	// GET before any state — should return 200 with empty body (no state yet)
	req := httptest.NewRequest("GET", "/internal/yjs/"+nbID, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET yjs (empty): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// PUT some binary Yjs state
	state := []byte{0x01, 0x02, 0x03, 0xAB, 0xCD}
	req = httptest.NewRequest("PUT", "/internal/yjs/"+nbID, bytes.NewReader(state))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT yjs: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// GET again — should return the stored state
	req = httptest.NewRequest("GET", "/internal/yjs/"+nbID, nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET yjs (after PUT): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), state) {
		t.Fatalf("GET yjs: expected %v, got %v", state, rec.Body.Bytes())
	}
}

func TestInternalAuthValidate(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("validate-%d@example.com", ts), "Validate Org")

	// Valid token
	req := httptest.NewRequest("GET", "/internal/auth/validate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Missing token
	req = httptest.NewRequest("GET", "/internal/auth/validate", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("validate (no token): expected 401, got %d", rec.Code)
	}

	// Invalid token
	req = httptest.NewRequest("GET", "/internal/auth/validate", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("validate (bad token): expected 401, got %d", rec.Code)
	}
}
