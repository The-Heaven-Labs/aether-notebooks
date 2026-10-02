package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/oauth"
)

var consentScopeDescriptions = map[string]string{
	oauth.ScopeQuery: "Run read-only SQL queries against your connectors",
	oauth.ScopeRead:  "View notebooks, dashboards and other resources",
	oauth.ScopeWrite: "Create and modify notebooks and dashboards",
}

// handleOAuthConsentInfo backs the SPA consent page: describes the client,
// the org and the requested scopes. The org comes from the subdomain when one
// is present (AuthMiddleware enforces token org == subdomain org before this
// runs); OrgIDFromContext falls back to the JWT claims org otherwise.
func (s *Server) handleOAuthConsentInfo(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	orgID := OrgIDFromContext(r.Context())
	if claims == nil || orgID == "" {
		writeError(w, http.StatusForbidden, "consent requires an authenticated organization member")
		return
	}
	client, err := s.oauth.GetClient(r.Context(), r.URL.Query().Get("client_id"))
	if err != nil {
		if errors.Is(err, oauth.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "unknown client")
		} else {
			writeError(w, http.StatusInternalServerError, "client lookup failed")
		}
		return
	}
	scopes := oauth.NormalizeScopes(oauth.ParseScopes(r.URL.Query().Get("scope")))
	if len(scopes) == 0 {
		writeError(w, http.StatusBadRequest, "no valid scopes requested")
		return
	}
	var orgName string
	if err := s.db.Pool.QueryRow(r.Context(),
		`SELECT name FROM orgs WHERE id = $1`, orgID).Scan(&orgName); err != nil {
		writeError(w, http.StatusInternalServerError, "org lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client_name":        client.ClientName,
		"org_id":             orgID,
		"org_name":           orgName,
		"scopes":             scopes,
		"scope_descriptions": consentScopeDescriptions,
	})
}

type oauthConsentDecisionRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	Resource            string `json:"resource"`
	State               string `json:"state"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Approve             bool   `json:"approve"`
}

// handleOAuthConsentDecision validates everything server-side and returns the
// redirect the SPA should follow (code or error appended). The auth code is
// single-use and bound to client + challenge + resource + org.
func (s *Server) handleOAuthConsentDecision(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	orgID := OrgIDFromContext(r.Context())
	if claims == nil || orgID == "" {
		writeError(w, http.StatusForbidden, "consent requires an authenticated organization member")
		return
	}
	var req oauthConsentDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	client, err := s.oauth.GetClient(r.Context(), req.ClientID)
	if err != nil {
		if errors.Is(err, oauth.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "unknown client")
		} else {
			writeError(w, http.StatusInternalServerError, "client lookup failed")
		}
		return
	}
	allowed := false
	for _, uri := range client.RedirectURIs {
		if uri == req.RedirectURI {
			allowed = true
			break
		}
	}
	if !allowed {
		writeError(w, http.StatusBadRequest, "redirect_uri is not registered for this client")
		return
	}
	target, err := url.Parse(req.RedirectURI)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid redirect_uri")
		return
	}
	if req.CodeChallenge == "" || req.CodeChallengeMethod != "S256" {
		writeError(w, http.StatusBadRequest, "code_challenge with S256 is required")
		return
	}
	q := target.Query()
	q.Set("state", req.State)
	if !req.Approve {
		q.Set("error", "access_denied")
		target.RawQuery = q.Encode()
		writeJSON(w, http.StatusOK, map[string]any{"redirect": target.String()})
		return
	}
	canonical := canonicalResourceURI(r, s.publicURL)
	if req.Resource != "" && req.Resource != canonical {
		writeError(w, http.StatusBadRequest, "resource does not match this MCP server (expected "+canonical+")")
		return
	}
	resource := req.Resource
	if resource == "" {
		resource = canonical
	}
	scopes := oauth.NormalizeScopes(oauth.ParseScopes(req.Scope))
	if len(scopes) == 0 {
		writeError(w, http.StatusBadRequest, "no valid scopes requested")
		return
	}
	if _, err := s.memberRole(r.Context(), orgID, claims.UserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusForbidden, "you are not a member of this organization")
		} else {
			writeError(w, http.StatusInternalServerError, "membership lookup failed")
		}
		return
	}
	code, err := s.oauth.IssueAuthCode(r.Context(), oauth.AuthCode{
		ClientID:        client.ClientID,
		UserID:          claims.UserID,
		OrgID:           orgID,
		Scopes:          scopes,
		Resource:        resource,
		RedirectURI:     req.RedirectURI,
		CodeChallenge:   req.CodeChallenge,
		ChallengeMethod: req.CodeChallengeMethod,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue authorization code")
		return
	}
	q.Set("code", code)
	target.RawQuery = q.Encode()
	writeJSON(w, http.StatusOK, map[string]any{"redirect": target.String()})
}
