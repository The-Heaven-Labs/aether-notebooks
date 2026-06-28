package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// dockerOIDCClient creates an HTTP client that rewrites the connection target
// for OIDC providers behind Docker port mapping, so the API (inside a container)
// can reach them while using the correct public hostname for token issuer validation.
func dockerOIDCClient() *http.Client {
	return &http.Client{
		Transport: &hostRewriteTransport{
			from: "localhost:5557",
			to:   "host.docker.internal:5557",
			next: http.DefaultTransport,
		},
		Timeout: 10 * time.Second,
	}
}

type hostRewriteTransport struct {
	from string
	to   string
	next http.RoundTripper
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL != nil && strings.Contains(req.URL.Host, t.from) {
		req2 := req.Clone(req.Context())
		req2.URL.Host = t.to // rewrite connection target
		req2.Host = t.from   // preserve original Host header for correct issuer in tokens
		return t.next.RoundTrip(req2)
	}
	return t.next.RoundTrip(req)
}

// callbackURL builds the absolute callback URL for the given provider ID.
// If HNB_PUBLIC_URL is set it takes precedence; otherwise it is inferred from the request.
func (s *Server) callbackURL(r *http.Request, providerID string) string {
	base := s.publicURL
	if base == "" {
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		base = fmt.Sprintf("%s://%s", scheme, r.Host)
	}
	return fmt.Sprintf("%s/api/v1/auth/oidc/%s/callback", base, providerID)
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	ctx := r.Context()

	dbProvider, err := sso.GetCachedProvider(ctx, s.db.Pool, s.Cache.Client(), s.masterKey, providerID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider")
		return
	}

	// Use a custom HTTP client for Docker dev environments so the API can reach
	// Keycloak via host.docker.internal while keeping localhost:5557 in the issuer.
	oidcCtx := oidc.ClientContext(ctx, dockerOIDCClient())
	provider, err := auth.NewGenericOIDCProvider(oidcCtx, dbProvider.Name, dbProvider.DiscoveryURL, dbProvider.ClientID, dbProvider.ClientSecret, s.callbackURL(r, providerID), dbProvider.Scopes, dbProvider.GroupsClaim, dbProvider.GetUserInfo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize OIDC provider: "+err.Error())
		return
	}

	state, err := generateState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	key := fmt.Sprintf("oidc:state:%s", state)
	stored, err := s.Cache.Client().SetNX(ctx, key, "1", 10*time.Minute).Result()
	if err != nil || !stored {
		writeError(w, http.StatusInternalServerError, "failed to store state")
		return
	}

	http.Redirect(w, r, provider.AuthURL(state), http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	ctx := r.Context()

	dbProvider, err := sso.GetCachedProvider(ctx, s.db.Pool, s.Cache.Client(), s.masterKey, providerID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider")
		return
	}

	oidcCtx := oidc.ClientContext(ctx, dockerOIDCClient())
	provider, err := auth.NewGenericOIDCProvider(oidcCtx, dbProvider.Name, dbProvider.DiscoveryURL, dbProvider.ClientID, dbProvider.ClientSecret, s.callbackURL(r, providerID), dbProvider.Scopes, dbProvider.GroupsClaim, dbProvider.GetUserInfo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize OIDC provider: "+err.Error())
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	key := fmt.Sprintf("oidc:state:%s", state)
	_, err = s.Cache.Client().GetDel(ctx, key).Result()
	if err == redis.Nil {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state validation error")
		return
	}

	claims, err := provider.Exchange(oidcCtx, code)
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

	// Reconcile group membership via SSO
	if dbProvider.AutoSyncGroups && len(claims.Groups) > 0 {
		SyncSSOGroups(ctx, s.db.Pool, s.audit, dbProvider, orgID, userID, claims.Groups)
	}

	token, err := s.jwt.Issue(userID, orgID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	// Redirect to the frontend with the token as a query param so LoginPage can pick it up.
	// In dev the frontend runs on a different port (Vite), so HNB_FRONTEND_URL overrides the base.
	redirectURL := s.frontendURL + "/login?token=" + url.QueryEscape(token)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}
