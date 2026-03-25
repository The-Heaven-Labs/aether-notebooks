package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// oidcStateStore holds short-lived state tokens to mitigate CSRF in the OIDC flow.
type oidcStateStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

var globalStateStore = &oidcStateStore{
	entries: make(map[string]time.Time),
}

func (s *oidcStateStore) set(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Purge entries older than 10 minutes
	now := time.Now()
	for k, v := range s.entries {
		if now.Sub(v) > 10*time.Minute {
			delete(s.entries, k)
		}
	}
	s.entries[state] = now
}

func (s *oidcStateStore) consume(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, ok := s.entries[state]
	if !ok {
		return false
	}
	delete(s.entries, state)
	return time.Since(created) <= 10*time.Minute
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	provider, ok := s.oidcProviders[providerName]
	if !ok {
		writeError(w, http.StatusNotFound, "OIDC provider not found: "+providerName)
		return
	}

	state, err := generateState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	globalStateStore.set(state)
	http.Redirect(w, r, provider.AuthURL(state), http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	provider, ok := s.oidcProviders[providerName]
	if !ok {
		writeError(w, http.StatusNotFound, "OIDC provider not found: "+providerName)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	if !globalStateStore.consume(state) {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}

	ctx := r.Context()
	claims, err := provider.Exchange(ctx, code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "OIDC exchange failed: "+err.Error())
		return
	}

	// Look up existing user by email
	var userID, orgID, role string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT u.id, om.org_id, om.role
		 FROM users u
		 JOIN org_members om ON om.user_id = u.id
		 WHERE u.email = $1
		 ORDER BY om.created_at ASC LIMIT 1`,
		claims.Email,
	).Scan(&userID, &orgID, &role)

	if err == pgx.ErrNoRows {
		// New user — provision user + org in a transaction
		tx, txErr := s.db.Pool.Begin(ctx)
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		defer tx.Rollback(ctx)

		displayName := claims.Name
		if displayName == "" {
			displayName = claims.Email
		}

		txErr = tx.QueryRow(ctx,
			`INSERT INTO users (email, name, email_verified) VALUES ($1, $2, TRUE) RETURNING id`,
			claims.Email, displayName,
		).Scan(&userID)
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}

		orgName := displayName + "'s Org"
		orgSlug := slugify(displayName)
		txErr = tx.QueryRow(ctx,
			`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
			orgName, orgSlug,
		).Scan(&orgID)
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create org")
			return
		}

		_, txErr = tx.Exec(ctx,
			`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
			orgID, userID,
		)
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to add member")
			return
		}

		if txErr = tx.Commit(ctx); txErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit")
			return
		}

		role = "admin"
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	token, err := s.jwt.Issue(userID, orgID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	// Redirect to the frontend with the token as a query param so LoginPage can pick it up
	redirectURL := "/#/login?token=" + url.QueryEscape(token)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}
