package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const claimsKey contextKey = "claims"

const adminModeKey contextKey = "admin_mode"

// adminModeFromContext returns whether admin mode is enabled.
// Defaults to false (admin mode OFF) unless explicitly set.
func adminModeFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v := ctx.Value(adminModeKey)
	if v == nil {
		return false
	}
	enabled, ok := v.(bool)
	return ok && enabled
}

func AuthMiddleware(issuer *auth.JWTIssuer, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""

			// WebSocket connections can't set Authorization header, so accept token via query param
			if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
				token = strings.TrimPrefix(header, "Bearer ")
			} else if queryToken := r.URL.Query().Get("token"); queryToken != "" {
				token = queryToken
			}

			if token == "" {
				writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
				return
			}

			// Check if this is a personal access token (starts with hnb_tok_)
			if strings.HasPrefix(token, "hnb_tok_") {
				validateAPIToken(w, r, next, pool, token)
				return
			}

			claims, err := issuer.Validate(token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			adminMode := r.Header.Get("X-HNB-Admin-Mode") != "false"
			ctx = context.WithValue(ctx, adminModeKey, adminMode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validateAPIToken checks a personal access token against the api_tokens table.
func validateAPIToken(w http.ResponseWriter, r *http.Request, next http.Handler, pool *pgxpool.Pool, token string) {
	rows, err := pool.Query(r.Context(),
		`SELECT id, user_id, org_id, token_hash, expires_at FROM api_tokens`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auth error")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, userID, orgID, hash string
		var expiresAt *time.Time
		if err := rows.Scan(&id, &userID, &orgID, &hash, &expiresAt); err != nil {
			continue
		}
		if expiresAt != nil && expiresAt.Before(time.Now()) {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)); err == nil {
			// Token valid — look up role
			var role string
			pool.QueryRow(r.Context(),
				`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
				orgID, userID).Scan(&role)
			if role == "" {
				role = "member"
			}

			claims := &auth.Claims{
				UserID: userID,
				OrgID:  orgID,
				Role:   role,
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			adminMode := r.Header.Get("X-HNB-Admin-Mode") != "false"
			ctx = context.WithValue(ctx, adminModeKey, adminMode)

			// Update last_used_at in background
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				pool.Exec(bgCtx, `UPDATE api_tokens SET last_used_at = NOW() WHERE id = $1`, id)
			}()

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
	}
	writeError(w, http.StatusUnauthorized, "invalid or expired API token")
}

func ClaimsFromContext(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(claimsKey).(*auth.Claims)
	return claims
}

// RequirePlatformAdmin returns middleware that enforces the IsPlatformAdmin claim.
func RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil || !claims.IsPlatformAdmin {
			writeError(w, http.StatusForbidden, "platform admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole returns middleware that enforces the admin role.
// Editor and viewer roles have been removed — ACLs handle all non-admin permissioning.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			if role == "admin" && claims.Role != "admin" {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
