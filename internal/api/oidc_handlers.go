package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/sso"
)

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// provisioningRole maps a provider's default_role to a valid org_members role.
// `viewer` was folded into `non-admin` by migration V077.
func provisioningRole(role string) string {
	if role == "viewer" {
		return "non-admin"
	}
	if role == "" {
		return "non-admin"
	}
	return role
}

// oidcHTTPClient returns an HTTP client tailored to the OIDC provider's discovery URL.
// In Docker dev environments where the provider is at localhost:5557 (host side)
// but the API runs in a container, it rewrites the connection to host.docker.internal:5557
// while preserving the original Host header for correct token issuer validation.
// For all other URLs, it uses the default transport with no rewrite.
func (s *Server) oidcHTTPClient(discoveryURL string) *http.Client {
	if s.oidcRewriteFrom != "" && s.oidcRewriteTo != "" && strings.Contains(discoveryURL, s.oidcRewriteFrom) {
		return &http.Client{
			Transport: &hostRewriteTransport{
				from: s.oidcRewriteFrom,
				to:   s.oidcRewriteTo,
				next: http.DefaultTransport,
			},
			Timeout: 10 * time.Second,
		}
	}
	return http.DefaultClient
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

// issuerURL strips the .well-known/openid-configuration suffix if present,
// returning the base issuer URL expected by oidc.NewProvider.
func issuerURL(raw string) string {
	if idx := strings.Index(raw, "/.well-known/openid-configuration"); idx >= 0 {
		return raw[:idx]
	}
	return strings.TrimSuffix(raw, "/")
}

// callbackURL builds the absolute callback URL for the given provider ID.
// If AETHER_PUBLIC_URL is set it takes precedence; otherwise it is inferred from the request.
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

// callbackURLForID builds the callback URL for a provider without a request,
// using the configured public URL. It returns an empty string when
// AETHER_PUBLIC_URL is unset, since the host cannot be resolved offline.
func (s *Server) callbackURLForID(providerID string) string {
	if s.publicURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/v1/auth/oidc/%s/callback", s.publicURL, providerID)
}

// @Summary OIDC login
// @Description Initiate an OIDC login flow for the given provider
// @Tags auth
// @Param provider path string true "OIDC Provider ID"
// @Success 302
// @Failure 404 {object} map[string]string
// @Router /auth/oidc/{provider}/login [get]
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	ctx := r.Context()

	dbProvider, err := sso.GetCachedProvider(ctx, s.db.Pool, s.Cache.Client(), s.masterKey, providerID, OrgIDFromContext(ctx))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider")
		return
	}

	// Use the configured HTTP client (applies host rewrite for Docker dev if configured)
	issuer := issuerURL(dbProvider.DiscoveryURL)
	oidcCtx := oidc.ClientContext(ctx, s.oidcHTTPClient(issuer))
	provider, err := auth.NewGenericOIDCProvider(oidcCtx, dbProvider.Name, issuer, dbProvider.ClientID, dbProvider.ClientSecret, s.callbackURL(r, providerID), dbProvider.Scopes, dbProvider.GroupsClaim, dbProvider.GetUserInfo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize OIDC provider: "+err.Error())
		return
	}

	state, err := generateState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	// Store the state in Redis. If a subdomain org is resolved, include the
	// org ID and the frontend host so the callback can redirect to the correct
	// subdomain URL regardless of what Host header the callback arrives on.
	stateValue := "1"
	if subdomainOrgID := OrgIDFromContext(ctx); subdomainOrgID != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		stateValue = fmt.Sprintf("org:%s|frontend:%s://%s", subdomainOrgID, scheme, r.Host)
	}
	key := fmt.Sprintf("oidc:state:%s", state)
	stored, err := s.Cache.Client().SetNX(ctx, key, stateValue, 10*time.Minute).Result()
	if err != nil || !stored {
		writeError(w, http.StatusInternalServerError, "failed to store state")
		return
	}

	http.Redirect(w, r, provider.AuthURL(state), http.StatusFound)
}

// @Summary OIDC callback
// @Description Handle the OIDC callback after provider authentication
// @Tags auth
// @Param provider path string true "OIDC Provider ID"
// @Param code query string true "Authorization code"
// @Param state query string true "State parameter"
// @Success 302
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /auth/oidc/{provider}/callback [get]
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	ctx := r.Context()

	// Resolve subdomain org: first check the Host header (if the callback
	// arrives on the subdomain), then fall back to the state stored in Redis
	// during the login handler.
	subdomainOrgID := OrgIDFromContext(ctx)

	dbProvider, err := sso.GetCachedProvider(ctx, s.db.Pool, s.Cache.Client(), s.masterKey, providerID, subdomainOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider")
		return
	}
	oidcIssuer := issuerURL(dbProvider.DiscoveryURL)
	oidcCtx := oidc.ClientContext(ctx, s.oidcHTTPClient(oidcIssuer))
	provider, err := auth.NewGenericOIDCProvider(oidcCtx, dbProvider.Name, oidcIssuer, dbProvider.ClientID, dbProvider.ClientSecret, s.callbackURL(r, providerID), dbProvider.Scopes, dbProvider.GroupsClaim, dbProvider.GetUserInfo)
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
	stateVal, err := s.Cache.Client().GetDel(ctx, key).Result()
	if err == redis.Nil {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state validation error")
		return
	}

	// Parse state value: may contain org ID and frontend URL
	// Format: "org:<uuid>|frontend:<url>" or just "1"
	subdomainFrontendURL := ""
	if strings.HasPrefix(stateVal, "org:") {
		parts := strings.SplitN(stateVal, "|", 2)
		if len(parts) > 0 {
			subdomainOrgID = strings.TrimPrefix(parts[0], "org:")
		}
		if len(parts) > 1 && strings.HasPrefix(parts[1], "frontend:") {
			subdomainFrontendURL = strings.TrimPrefix(parts[1], "frontend:")
		}
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

	// Resolve the org this provider belongs to. For org-scoped providers it's
	// the provider's own org_id; for platform-scoped providers enabled for a
	// specific org, prefer the subdomain-resolved org (existing semantics);
	// otherwise there is no target org.
	providerOrgID := ""
	if dbProvider.OrgID != nil {
		providerOrgID = *dbProvider.OrgID
	}
	targetOrgID := providerOrgID
	if dbProvider.Scope == "platform" && subdomainOrgID != "" {
		targetOrgID = subdomainOrgID
	}

	if err == pgx.ErrNoRows {
		// New user — provision according to the provider's provisioning mode.
		switch dbProvider.ProvisioningMode {
		case "deny":
			writeError(w, http.StatusForbidden, "automatic account creation is disabled for this SSO provider")
			return
		case "join_provider_org":
			if targetOrgID == "" {
				writeError(w, http.StatusForbidden, "SSO provider is not configured for auto-provisioning on this host")
				return
			}
			if len(dbProvider.AllowedDomains) > 0 {
				domain := emailDomain(claims.Email)
				if !slices.Contains(dbProvider.AllowedDomains, domain) {
					writeError(w, http.StatusForbidden, "email domain not allowed for this SSO provider")
					return
				}
			}
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

			orgID = targetOrgID
			role = provisioningRole(dbProvider.DefaultRole)
			_, txErr = tx.Exec(ctx,
				`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
				orgID, userID, role,
			)
			if txErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to add member")
				return
			}

			if txErr = createHomeFolder(ctx, tx, orgID, userID, displayName); txErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to create home folder")
				return
			}

			if txErr = tx.Commit(ctx); txErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit")
				return
			}
			s.audit.Log(ctx, audit.Entry{
				OrgID: orgID, UserID: userID,
				Action: "org.auto_join", ResourceType: "org", ResourceID: orgID,
				Metadata: map[string]any{"provisioning_mode": dbProvider.ProvisioningMode},
			})
		default: // create_org — current behavior (backwards-compatible)
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

			if subdomainOrgID != "" {
				// Provision into the subdomain-resolved org
				orgID = subdomainOrgID
				_, txErr = tx.Exec(ctx,
					`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'non-admin')`,
					orgID, userID,
				)
				if txErr != nil {
					writeError(w, http.StatusInternalServerError, "failed to add member")
					return
				}
				role = "non-admin"
			} else {
				// No subdomain — create a new org (existing behavior)
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
				role = "admin"
			}

			if txErr = createHomeFolder(ctx, tx, orgID, userID, displayName); txErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to create home folder")
				return
			}

			if txErr = tx.Commit(ctx); txErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit")
				return
			}
			s.audit.Log(ctx, audit.Entry{
				OrgID: orgID, UserID: userID,
				Action: "org.auto_join", ResourceType: "org", ResourceID: orgID,
				Metadata: map[string]any{"provisioning_mode": dbProvider.ProvisioningMode},
			})
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Existing user logging in via a join_provider_org provider who is not yet
	// a member of the provider's org — auto-join them (subject to allowed_domains).
	if dbProvider.ProvisioningMode == "join_provider_org" && targetOrgID != "" && targetOrgID != orgID {
		var domainAllowed bool
		if len(dbProvider.AllowedDomains) == 0 {
			domainAllowed = true
		} else {
			domainAllowed = slices.Contains(dbProvider.AllowedDomains, emailDomain(claims.Email))
		}
		if domainAllowed {
			var isMember bool
			s.db.Pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
				targetOrgID, userID,
			).Scan(&isMember)
			if !isMember {
				tx, txErr := s.db.Pool.Begin(ctx)
				if txErr == nil {
					role = provisioningRole(dbProvider.DefaultRole)
					_, txErr = tx.Exec(ctx,
						`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
						targetOrgID, userID, role,
					)
					if txErr == nil {
						txErr = createHomeFolder(ctx, tx, targetOrgID, userID, "")
					}
					if txErr != nil {
						tx.Rollback(ctx)
					} else {
						tx.Commit(ctx)
						s.audit.Log(ctx, audit.Entry{
							OrgID: targetOrgID, UserID: userID,
							Action: "org.auto_join", ResourceType: "org", ResourceID: targetOrgID,
							Metadata: map[string]any{"provisioning_mode": dbProvider.ProvisioningMode},
						})
					}
				}
			}
		}
		// Switch the session to the provider's org (either pre-existing or just auto-joined).
		var isMember bool
		s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
			targetOrgID, userID,
		).Scan(&isMember)
		if isMember {
			orgID = targetOrgID
			role = provisioningRole(dbProvider.DefaultRole)
		}
	}

	// Existing user logging in with a subdomain org that differs from their
	// primary org — auto-join them if their email domain is allowed for that org.
	if subdomainOrgID != "" && subdomainOrgID != orgID {
		var userEmail string
		s.db.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&userEmail)

		var domainAllowed bool
		if userEmail != "" {
			domain := userEmail[strings.LastIndex(userEmail, "@")+1:]
			s.db.Pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM org_allowed_domains WHERE org_id=$1 AND domain=$2 AND auto_join=true)`,
				subdomainOrgID, domain,
			).Scan(&domainAllowed)
		}

		if domainAllowed {
			var isMember bool
			s.db.Pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
				subdomainOrgID, userID,
			).Scan(&isMember)
			if !isMember {
				tx, txErr := s.db.Pool.Begin(ctx)
				if txErr == nil {
					_, txErr = tx.Exec(ctx,
						`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'viewer')`,
						subdomainOrgID, userID,
					)
					if txErr == nil {
						txErr = createHomeFolder(ctx, tx, subdomainOrgID, userID, "")
					}
					if txErr != nil {
						tx.Rollback(ctx)
					} else {
						tx.Commit(ctx)
						s.audit.Log(ctx, audit.Entry{
							OrgID: subdomainOrgID, UserID: userID,
							Action: "org.auto_join", ResourceType: "org", ResourceID: subdomainOrgID,
						})
					}
				}
			}
		}
		// Only switch if member (either pre-existing or just auto-joined)
		var isMember bool
		s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
			subdomainOrgID, userID,
		).Scan(&isMember)
		if isMember {
			orgID = subdomainOrgID
			role = "non-admin"
		}
	}

	// Reconcile group membership via SSO
	if dbProvider.AutoSyncGroups && len(claims.Groups) > 0 {
		SyncSSOGroups(ctx, s.db.Pool, s.audit, dbProvider, orgID, userID, claims.Groups)
	}

	var isPlatformAdmin bool
	s.db.Pool.QueryRow(ctx, `SELECT is_platform_admin FROM users WHERE id=$1`, userID).Scan(&isPlatformAdmin)

	token, err := s.jwt.IssueFull(userID, orgID, role, isPlatformAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	// Redirect to the frontend with the token as a query param so LoginPage can pick it up.
	// Use the subdomain-aware frontend URL if available (preserved in state from login handler).
	redirectURL := s.frontendURL + "/login?token=" + url.QueryEscape(token)
	if subdomainFrontendURL != "" && (strings.HasPrefix(subdomainFrontendURL, s.frontendURL) || strings.HasPrefix(subdomainFrontendURL, "http://localhost:5173")) {
		redirectURL = subdomainFrontendURL + "/login?token=" + url.QueryEscape(token)
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}
