package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
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
// deployments each advertise and validate their own resource. Token minting
// (consent) and audience validation (AuthMiddleware) must both use this so the
// resource they agree on is identical.
func canonicalResourceURI(r *http.Request, publicURL string) string {
	scheme, host := requestSchemeAndHost(r, publicURL)
	return scheme + "://" + host + "/api/v1/mcp"
}

// oauthBaseURL is the AS issuer/discovery base for the request host.
func oauthBaseURL(r *http.Request, publicURL string) string {
	return strings.TrimSuffix(canonicalResourceURI(r, publicURL), "/api/v1/mcp")
}

// requestSchemeAndHost resolves the externally visible scheme and host for r.
//
// The request-derived host is the default: it keeps subdomain org binding
// working (each org subdomain advertises and validates its own resource).
// Proxies that preserve the client-facing host in X-Forwarded-Host take
// precedence; when Host has been rewritten to an in-cluster name and no
// X-Forwarded-Host is present, the configured public URL is used instead.
// Loopback and literal-IP hosts are always kept — they are externally
// meaningful in local development and port-forward testing.
//
// Scheme: X-Forwarded-Proto first, then direct TLS, then the configured public
// URL's scheme when the resolved host is the public host or one of its
// subdomains. TLS-terminating proxies often forward Host/X-Forwarded-Host
// without X-Forwarded-Proto; without that last fallback an https deployment
// would advertise http:// discovery URLs and break client issuer and audience
// checks. Unrelated external hosts keep the request-derived scheme.
func requestSchemeAndHost(r *http.Request, publicURL string) (scheme, host string) {
	var public *url.URL
	if publicURL != "" {
		if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
			public = u
		}
	}

	if proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}

	host = firstForwardedHost(r)
	if host == "" {
		host = r.Host
		if public != nil && isClusterInternalHost(host) {
			host = public.Host
		}
	}
	if scheme == "" && public != nil && public.Scheme != "" && chHostWithinPublicDomain(host, public.Host) {
		scheme = public.Scheme
	}
	if scheme == "" {
		scheme = "http"
	}
	return scheme, host
}

// chHostWithinPublicDomain reports whether host (optionally with a port) is the
// public URL's host or one of its subdomains, ignoring case and ports. The
// public URL says nothing about unrelated external hosts, so those are not
// rewritten.
func chHostWithinPublicDomain(host, publicHost string) bool {
	h, p := strings.ToLower(hostnameOnly(host)), strings.ToLower(hostnameOnly(publicHost))
	if h == "" || p == "" {
		return false
	}
	return h == p || strings.HasSuffix(h, "."+p)
}

// hostnameOnly strips a port from a host, leaving IPv6 brackets intact.
func hostnameOnly(host string) string {
	if hp, _, err := net.SplitHostPort(host); err == nil {
		return hp
	}
	return host
}

// firstForwardedHost returns the first entry of the X-Forwarded-Host chain: a
// comma-separated list whose leftmost value is the proxy-observed client host.
func firstForwardedHost(r *http.Request) string {
	return firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
}

// firstHeaderValue returns the first comma-separated value of a forwarded
// header, trimmed. Trust model matches the existing X-Forwarded-Proto handling:
// values are trusted from the edge, so deployments must strip client-supplied
// X-Forwarded-* at the boundary.
func firstHeaderValue(v string) string {
	if v == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(v, ",")[0])
}

// isClusterInternalHost reports whether host is only resolvable inside the
// cluster: a bare hostname without dots (k8s service/pod short name) or a
// *.cluster.local name. Loopback and literal-IP hosts are externally
// meaningful and never treated as internal.
func isClusterInternalHost(host string) bool {
	h := host
	if hp, _, err := net.SplitHostPort(host); err == nil {
		h = hp
	}
	h = strings.Trim(h, "[]")
	if h == "" || strings.EqualFold(h, "localhost") {
		return false
	}
	if net.ParseIP(h) != nil {
		return false
	}
	if !strings.Contains(h, ".") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(h), ".cluster.local")
}

// writeMCPUnauthorized writes the MCP 401 challenge with the RFC 9728
// resource_metadata parameter so harnesses can discover the OAuth server.
func writeMCPUnauthorized(w http.ResponseWriter, r *http.Request, publicURL, msg string) {
	w.Header().Set("WWW-Authenticate",
		`Bearer realm="aether", resource_metadata="`+oauthBaseURL(r, publicURL)+`/.well-known/oauth-protected-resource"`)
	writeError(w, http.StatusUnauthorized, msg)
}

// handleOAuthProtectedResource serves RFC 9728 Protected Resource Metadata.
func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 canonicalResourceURI(r, s.publicURL),
		"authorization_servers":    []string{oauthBaseURL(r, s.publicURL)},
		"scopes_supported":         oauth.AllScopes,
		"bearer_methods_supported": []string{"header"},
	})
}

// handleOAuthASMetadata serves RFC 8414 Authorization Server Metadata.
func (s *Server) handleOAuthASMetadata(w http.ResponseWriter, r *http.Request) {
	base := oauthBaseURL(r, s.publicURL)
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
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
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
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
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
	if rt := q.Get("response_type"); rt != "" && rt != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
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
