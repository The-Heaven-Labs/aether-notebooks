package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const claimsKey contextKey = "claims"

const subdomainKey contextKey = "subdomain_org"

// adminModeFromContext returns whether admin mode is enabled.
// Defaults to false (admin mode OFF) unless explicitly set. The flag is stored
// via executor.WithAdminMode so agent and MCP execution paths can stamp the
// same signal into their tool contexts.
func adminModeFromContext(ctx context.Context) bool {
	return executor.AdminModeFromContext(ctx)
}

// AuthMiddleware validates JWT tokens and sets user claims in the request context.
func AuthMiddleware(issuer *auth.JWTIssuer, pool *pgxpool.Pool, masterKey []byte) func(http.Handler) http.Handler {
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
				validateAPIToken(w, r, next, pool, masterKey, token)
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

			adminMode := r.Header.Get("X-AETHER-Admin-Mode") == "true" || r.URL.Query().Get("admin_mode") == "true"
			ctx = executor.WithAdminMode(ctx, adminMode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validateAPIToken checks a personal access token against the api_tokens table.
// Tokens created after the lookup-hash migration are found with a single indexed
// query; older rows fall back to a bcrypt scan over un-backfilled rows and are
// backfilled on first successful match.
func validateAPIToken(w http.ResponseWriter, r *http.Request, next http.Handler, pool *pgxpool.Pool, masterKey []byte, token string) {
	subdomainOrg := OrgIDFromContext(r.Context())
	lookupHash := crypto.TokenLookupHash(masterKey, token)

	query := `SELECT id, user_id, org_id, expires_at FROM api_tokens WHERE token_lookup_hash = $1`
	args := []any{lookupHash}
	if subdomainOrg != "" {
		query += ` AND org_id = $2`
		args = append(args, subdomainOrg)
	}

	var id, userID, orgID string
	var expiresAt *time.Time
	err := pool.QueryRow(r.Context(), query, args...).Scan(&id, &userID, &orgID, &expiresAt)
	if err == nil {
		completeAPITokenAuth(w, r, next, pool, id, userID, orgID, expiresAt)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "auth error")
		return
	}

	validateLegacyAPIToken(w, r, next, pool, token, lookupHash, subdomainOrg)
}

// validateLegacyAPIToken bcrypt-verifies tokens that have no lookup hash yet
// (created before the migration), backfilling the matched row so subsequent
// requests take the fast path. Rows are matched by lookup hash as well as NULL
// so a concurrent first use that backfilled the row after the fast path missed
// it still authenticates.
func validateLegacyAPIToken(w http.ResponseWriter, r *http.Request, next http.Handler, pool *pgxpool.Pool, token, lookupHash, subdomainOrg string) {
	query := `SELECT id, user_id, org_id, token_hash, expires_at FROM api_tokens WHERE (token_lookup_hash = $1 OR token_lookup_hash IS NULL)`
	args := []any{lookupHash}
	if subdomainOrg != "" {
		query += ` AND org_id = $2`
		args = append(args, subdomainOrg)
	}
	rows, err := pool.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auth error")
		return
	}

	var id, userID, orgID string
	var expiresAt *time.Time
	matched := false
	for rows.Next() {
		var hash string
		if err := rows.Scan(&id, &userID, &orgID, &hash, &expiresAt); err != nil {
			continue
		}
		if expiresAt != nil && expiresAt.Before(time.Now()) {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)); err == nil {
			matched = true
			break
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "auth error")
		return
	}
	if !matched {
		writeError(w, http.StatusUnauthorized, "invalid or expired API token")
		return
	}

	if _, err := pool.Exec(r.Context(),
		`UPDATE api_tokens SET token_lookup_hash = $1 WHERE id = $2 AND token_lookup_hash IS NULL`,
		lookupHash, id); err != nil {
		slog.Debug("PAT lookup hash backfill failed", "token_id", id, "error", err)
	}
	completeAPITokenAuth(w, r, next, pool, id, userID, orgID, expiresAt)
}

// completeAPITokenAuth finishes a successful token match: expiry check, role
// lookup, claims context, and last-used bookkeeping.
func completeAPITokenAuth(w http.ResponseWriter, r *http.Request, next http.Handler, pool *pgxpool.Pool, id, userID, orgID string, expiresAt *time.Time) {
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		writeError(w, http.StatusUnauthorized, "invalid or expired API token")
		return
	}

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
	adminMode := r.Header.Get("X-AETHER-Admin-Mode") == "true" || r.URL.Query().Get("admin_mode") == "true"
	ctx = executor.WithAdminMode(ctx, adminMode)

	// Update last_used_at in background
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Exec(bgCtx, `UPDATE api_tokens SET last_used_at = NOW() WHERE id = $1`, id)
	}()

	next.ServeHTTP(w, r.WithContext(ctx))
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
