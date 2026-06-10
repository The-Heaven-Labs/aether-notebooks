package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/auth"
)

func TestAuthMiddleware(t *testing.T) {
	issuer := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	mw := api.AuthMiddleware(issuer, nil)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := api.ClaimsFromContext(r.Context())
		if claims.UserID != "user-1" {
			t.Fatalf("expected user-1, got %s", claims.UserID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	token, _ := issuer.Issue("user-1", "org-1", "editor")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareNoToken(t *testing.T) {
	issuer := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	mw := api.AuthMiddleware(issuer, nil)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
