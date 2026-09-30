package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/oauth"
)

// requireMCPOAuth gates OAuth endpoints behind the feature flag. Routes are
// registered unconditionally because the flag can be set after NewServer
// (tests do exactly that).
func (s *Server) requireMCPOAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOAuthEnabled {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// canonicalResourceURI returns the RFC 8707 canonical URI of the MCP server
// for the request: scheme://host/api/v1/mcp (no trailing slash). Subdomain
// deployments each advertise and validate their own resource.
func canonicalResourceURI(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/v1/mcp"
}

// oauthBaseURL is the AS issuer/discovery base for the request host.
func oauthBaseURL(r *http.Request) string {
	return strings.TrimSuffix(canonicalResourceURI(r), "/api/v1/mcp")
}

// writeMCPUnauthorized writes the MCP 401 challenge with the RFC 9728
// resource_metadata parameter so harnesses can discover the OAuth server.
func writeMCPUnauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate",
		`Bearer realm="aether", resource_metadata="`+oauthBaseURL(r)+`/.well-known/oauth-protected-resource"`)
	writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
}

// handleOAuthProtectedResource serves RFC 9728 Protected Resource Metadata.
func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 canonicalResourceURI(r),
		"authorization_servers":    []string{oauthBaseURL(r)},
		"scopes_supported":         oauth.AllScopes,
		"bearer_methods_supported": []string{"header"},
	})
}

// handleOAuthASMetadata serves RFC 8414 Authorization Server Metadata.
func (s *Server) handleOAuthASMetadata(w http.ResponseWriter, r *http.Request) {
	base := oauthBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      oauth.AllScopes,
	})
}

// handleOAuthRegister implements RFC 7591 dynamic client registration for
// public clients only.
func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.RedirectURIs) == 0 {
		oauthErrorResponse(w, "invalid_client_metadata", "redirect_uris is required")
		return
	}
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		oauthErrorResponse(w, "invalid_client_metadata", "only public clients (token_endpoint_auth_method=none) are supported")
		return
	}
	for _, gt := range append(req.GrantTypes, req.ResponseTypes...) {
		switch gt {
		case "", "authorization_code", "refresh_token", "code":
		default:
			oauthErrorResponse(w, "invalid_client_metadata", "unsupported grant or response type: "+gt)
			return
		}
	}
	for _, uri := range req.RedirectURIs {
		if !oauth.ValidateRedirectURI(uri) {
			oauthErrorResponse(w, "invalid_redirect_uri", "redirect URIs must be https or loopback http")
			return
		}
	}
	client, err := s.oauth.CreateClient(r.Context(), req.ClientName, req.RedirectURIs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func oauthErrorResponse(w http.ResponseWriter, code, description string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error": code, "error_description": description,
	})
}

// handleOAuthToken implements the authorization_code and refresh_token grants.
func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	// RFC 6749 §5.1: token responses (success or error) must not be cached.
	// Set before parsing so malformed-form errors carry it too.
	w.Header().Set("Cache-Control", "no-store")
	if err := r.ParseForm(); err != nil {
		oauthErrorResponse(w, "invalid_request", "malformed form body")
		return
	}
	client, err := s.oauth.GetClient(r.Context(), r.PostFormValue("client_id"))
	if err != nil {
		if errors.Is(err, oauth.ErrNotFound) {
			oauthErrorResponse(w, "invalid_client", "unknown client")
		} else {
			writeError(w, http.StatusInternalServerError, "token endpoint failure")
		}
		return
	}

	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		s.handleOAuthTokenAuthCode(w, r, client)
	case "refresh_token":
		s.handleOAuthTokenRefresh(w, r, client)
	default:
		oauthErrorResponse(w, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (s *Server) handleOAuthTokenAuthCode(w http.ResponseWriter, r *http.Request, client *oauth.Client) {
	redirectURI := r.PostFormValue("redirect_uri")
	resource := r.PostFormValue("resource")
	codeRec, err := s.oauth.ConsumeAuthCode(r.Context(), r.PostFormValue("code"))
	if err != nil {
		if errors.Is(err, oauth.ErrNotFound) {
			oauthErrorResponse(w, "invalid_grant", "code is invalid, expired or already used")
		} else {
			writeError(w, http.StatusInternalServerError, "token endpoint failure")
		}
		return
	}
	if codeRec.ClientID != client.ClientID {
		oauthErrorResponse(w, "invalid_grant", "code was issued to another client")
		return
	}
	if redirectURI == "" || redirectURI != codeRec.RedirectURI {
		oauthErrorResponse(w, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if !oauth.VerifyPKCE(codeRec.CodeChallenge, codeRec.ChallengeMethod, r.PostFormValue("code_verifier")) {
		oauthErrorResponse(w, "invalid_grant", "PKCE verification failed")
		return
	}
	if resource != "" && resource != codeRec.Resource {
		oauthErrorResponse(w, "invalid_grant", "resource mismatch")
		return
	}
	// The role recorded at consent time is re-derived at rotation; for the
	// initial exchange fetch it from org_members.
	role, err := s.memberRole(r.Context(), codeRec.OrgID, codeRec.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			oauthErrorResponse(w, "invalid_grant", "user is not a member of the organization")
		} else {
			writeError(w, http.StatusInternalServerError, "token issuance failed")
		}
		return
	}
	access, refresh, err := s.oauth.IssueTokens(r.Context(), s.jwt, client.ClientID,
		codeRec.UserID, codeRec.OrgID, role, codeRec.Scopes, codeRec.Resource)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token issuance failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(oauth.AccessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         strings.Join(codeRec.Scopes, " "),
	})
}

func (s *Server) handleOAuthTokenRefresh(w http.ResponseWriter, r *http.Request, client *oauth.Client) {
	access, newRefresh, err := s.oauth.RotateRefresh(r.Context(), s.jwt, client.ClientID, r.PostFormValue("refresh_token"))
	switch {
	case errors.Is(err, oauth.ErrReused):
		oauthErrorResponse(w, "invalid_grant", "refresh token reuse detected; all tokens in the family were revoked")
	case errors.Is(err, oauth.ErrClientMismatch), errors.Is(err, oauth.ErrNotFound):
		// Non-enumerating: a token presented by the wrong client is
		// indistinguishable from an unknown or expired one.
		oauthErrorResponse(w, "invalid_grant", "refresh token is invalid or expired")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "token rotation failed")
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  access,
			"token_type":    "Bearer",
			"expires_in":    int(oauth.AccessTTL.Seconds()),
			"refresh_token": newRefresh,
		})
	}
}

// handleOAuthAuthorize validates the request, then hands the browser to the
// SPA consent page (the SPA owns the session token in localStorage). The
// consent page re-reads the same query parameters.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, err := s.oauth.GetClient(r.Context(), q.Get("client_id"))
	if err != nil {
		if errors.Is(err, oauth.ErrNotFound) {
			http.Error(w, "invalid authorization request: unknown client", http.StatusBadRequest)
		} else {
			http.Error(w, "authorization endpoint failure", http.StatusInternalServerError)
		}
		return
	}
	redirectURI := q.Get("redirect_uri")
	allowed := false
	for _, uri := range client.RedirectURIs {
		if uri == redirectURI {
			allowed = true
			break
		}
	}
	if !allowed || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	if s.frontendHandler != nil {
		s.frontendHandler.ServeHTTP(w, r)
		return
	}
	http.Error(w, "consent UI is not available", http.StatusNotImplemented)
}

// memberRole returns the caller's role in the org, for stamping access tokens.
func (s *Server) memberRole(ctx context.Context, orgID, userID string) (string, error) {
	var role string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
	return role, err
}
