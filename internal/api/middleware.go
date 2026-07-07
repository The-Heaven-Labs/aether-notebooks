package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const claimsKey contextKey = "claims"

const adminModeKey contextKey = "admin_mode"

const subdomainKey contextKey = "subdomain_org"

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

// AuthMiddleware validates JWT tokens and sets user claims in the request context.
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

			// Check if this is a personal access token (starts with aether_tok_)
			if strings.HasPrefix(token, "aether_tok_") {
				validateAPIToken(w, r, next, pool, token)
				return
			}

			claims, err := issuer.Validate(token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)

			// Validate subdomain org matches JWT org when both are present.
			// Platform admins operate at the instance level — override their org
			// to the subdomain org so they see the correct org's data.
			if subdomainOrg := OrgIDFromContext(r.Context()); subdomainOrg != "" && subdomainOrg != claims.OrgID {
				if claims.IsPlatformAdmin {
					claims.OrgID = subdomainOrg
				} else {
					writeError(w, http.StatusForbidden, "organization mismatch between subdomain and token")
					return
				}
			}

			adminMode := r.Header.Get("X-AETHER-Admin-Mode") == "true"
			ctx = context.WithValue(ctx, adminModeKey, adminMode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validateAPIToken checks a personal access token against the api_tokens table.
func validateAPIToken(w http.ResponseWriter, r *http.Request, next http.Handler, pool *pgxpool.Pool, token string) {
	subdomainOrg := OrgIDFromContext(r.Context())
	var rows pgx.Rows
	var err error
	if subdomainOrg != "" {
		rows, err = pool.Query(r.Context(),
			`SELECT id, user_id, org_id, token_hash, expires_at FROM api_tokens WHERE org_id = $1`, subdomainOrg)
	} else {
		rows, err = pool.Query(r.Context(),
			`SELECT id, user_id, org_id, token_hash, expires_at FROM api_tokens`)
	}
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

			// Validate subdomain org matches API token org when both are present.
			// Platform admins operate at the instance level — override their org
			// to the subdomain org so they see the correct org's data.
			if subdomainOrg := OrgIDFromContext(r.Context()); subdomainOrg != "" && subdomainOrg != orgID {
				if claims.IsPlatformAdmin {
					orgID = subdomainOrg
					claims.OrgID = subdomainOrg
				} else {
					writeError(w, http.StatusForbidden, "organization mismatch between subdomain and token")
					return
				}
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			adminMode := r.Header.Get("X-AETHER-Admin-Mode") == "true"
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

// SubdomainMiddleware resolves the organization from the request's host subdomain and sets the org context.
func SubdomainMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := strings.ToLower(strings.Split(r.Host, ":")[0])
			parts := strings.SplitN(host, ".", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" && parts[0] != "www" && parts[0] != "localhost" {
				var orgID string
				err := pool.QueryRow(r.Context(),
					`SELECT id FROM orgs WHERE slug = $1`, parts[0],
				).Scan(&orgID)
				if err == nil {
					ctx := context.WithValue(r.Context(), subdomainKey, orgID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if errors.Is(err, pgx.ErrNoRows) {
					// Unknown subdomain — pass through without org context.
					// Routes that require an org will get it from the JWT claims.
					next.ServeHTTP(w, r)
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to resolve organization")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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

// OrgIDFromContext returns the org ID resolved from the subdomain, or falls
// back to the org ID in the JWT claims. Returns empty string if neither is available.
func OrgIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(subdomainKey); v != nil {
		if id, ok := v.(string); ok && id != "" {
			return id
		}
	}
	if claims := ClaimsFromContext(ctx); claims != nil {
		return claims.OrgID
	}
	return ""
}
