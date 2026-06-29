package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/auth"
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

func TestSubdomainMiddlewareResolvesOrg(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() {
		s.DB().Pool.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, orgID)
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := api.OrgIDFromContext(r.Context())
		if got != orgID {
			t.Errorf("expected org %q, got %q", orgID, got)
		}
	})
	wrapped := api.SubdomainMiddleware(s.DB().Pool)(handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = slug + ".aether.test"
	wrapped.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSubdomainMiddlewareSkipsSinglePartHost(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := api.OrgIDFromContext(r.Context())
		if got != "" {
			t.Errorf("expected empty org for single-part host, got %q", got)
		}
	})
	wrapped := api.SubdomainMiddleware(nil)(handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "aether"
	wrapped.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSubdomainMiddlewareSkipsLocalhost(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := api.OrgIDFromContext(r.Context())
		if got != "" {
			t.Errorf("expected empty org for localhost, got %q", got)
		}
	})
	wrapped := api.SubdomainMiddleware(nil)(handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost:8080"
	wrapped.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSubdomainMiddlewareUnknownOrg(t *testing.T) {
	s := setupTestServer(t)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := api.SubdomainMiddleware(s.DB().Pool)(handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "nonexistent.aether.test"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if !called {
		t.Error("handler should be called for unknown org (passes through)")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
