package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/jackc/pgx/v5"
)

// platformProviderResponse extends providerResponse with an org-specific enabled flag.
type platformProviderResponse struct {
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	Name           string    `json:"name"`
	ProviderType   string    `json:"provider_type"`
	ClientID       string    `json:"client_id"`
	DiscoveryURL   string    `json:"discovery_url"`
	AllowedDomains []string  `json:"allowed_domains"`
	Enabled        bool      `json:"enabled"`
	EnabledForOrg  bool      `json:"enabled_for_org"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ssoSettingsResponse is the JSON shape for org SSO settings.
type ssoSettingsResponse struct {
	SSOPasswordLogin bool `json:"sso_password_login"`
}

// handleOrgListSSOProviders returns all org-scoped providers owned by the caller's org,
// plus all enabled platform providers for context.
func (s *Server) handleOrgListSSOProviders(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	providers, err := sso.ListOrgProviders(r.Context(), s.db.Pool, s.masterKey, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list providers")
		return
	}

	resp := make([]providerResponse, len(providers))
	for i, p := range providers {
		resp[i] = providerToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": resp})
}

// handleOrgCreateSSOProvider creates an org-scoped SSO provider for the caller's org.
// Scope and org_id are always forced to "org" and the caller's org — callers cannot override.
func (s *Server) handleOrgCreateSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req ssoProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.ClientID == "" || req.ClientSecret == "" || req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "name, client_id, client_secret, and discovery_url are required")
		return
	}

	domains := req.AllowedDomains
	if domains == nil {
		domains = []string{}
	}

	orgID := claims.OrgID
	p := sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           req.Name,
		ProviderType:   "oidc",
		ClientID:       req.ClientID,
		ClientSecret:   req.ClientSecret,
		DiscoveryURL:   req.DiscoveryURL,
		AllowedDomains: domains,
		Enabled:        req.Enabled,
	}

	created, err := sso.CreateProvider(r.Context(), s.db.Pool, s.masterKey, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	writeJSON(w, http.StatusCreated, providerToResponse(created))
}

// handleOrgUpdateSSOProvider updates an org-scoped provider.
// Returns 403 if the provider doesn't belong to the caller's org.
func (s *Server) handleOrgUpdateSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	// Verify ownership
	existing, err := sso.GetProvider(r.Context(), s.db.Pool, s.masterKey, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch provider")
		return
	}
	if existing.Scope != "org" || existing.OrgID == nil || *existing.OrgID != claims.OrgID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var req ssoProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.ClientID == "" || req.ClientSecret == "" || req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "name, client_id, client_secret, and discovery_url are required")
		return
	}

	domains := req.AllowedDomains
	if domains == nil {
		domains = []string{}
	}

	p := sso.Provider{
		ID:             id,
		Name:           req.Name,
		ClientID:       req.ClientID,
		ClientSecret:   req.ClientSecret,
		DiscoveryURL:   req.DiscoveryURL,
		AllowedDomains: domains,
		Enabled:        req.Enabled,
	}

	updated, err := sso.UpdateProvider(r.Context(), s.db.Pool, s.masterKey, p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	writeJSON(w, http.StatusOK, providerToResponse(updated))
}

// handleOrgDeleteSSOProvider deletes an org-scoped provider.
// Returns 403 if the provider doesn't belong to the caller's org.
func (s *Server) handleOrgDeleteSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	// Verify ownership
	existing, err := sso.GetProvider(r.Context(), s.db.Pool, s.masterKey, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch provider")
		return
	}
	if existing.Scope != "org" || existing.OrgID == nil || *existing.OrgID != claims.OrgID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := sso.DeleteProvider(r.Context(), s.db.Pool, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	w.WriteHeader(http.StatusNoContent)
}

// handleOrgListPlatformProviders returns all platform-scoped providers with an enabled_for_org
// boolean indicating whether the org has enabled each one.
func (s *Server) handleOrgListPlatformProviders(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	providers, err := sso.ListPlatformProviders(ctx, s.db.Pool, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list platform providers")
		return
	}

	// Fetch enabled platform provider IDs for this org in one query.
	rows, err := s.db.Pool.Query(ctx,
		`SELECT provider_id FROM org_platform_providers WHERE org_id=$1`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch enabled providers")
		return
	}
	defer rows.Close()

	enabledSet := make(map[string]bool)
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		enabledSet[pid] = true
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error")
		return
	}

	domains := func(d []string) []string {
		if d == nil {
			return []string{}
		}
		return d
	}

	resp := make([]platformProviderResponse, len(providers))
	for i, p := range providers {
		resp[i] = platformProviderResponse{
			ID:             p.ID,
			Scope:          p.Scope,
			Name:           p.Name,
			ProviderType:   p.ProviderType,
			ClientID:       p.ClientID,
			DiscoveryURL:   p.DiscoveryURL,
			AllowedDomains: domains(p.AllowedDomains),
			Enabled:        p.Enabled,
			EnabledForOrg:  enabledSet[p.ID],
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": resp})
}

// handleOrgEnablePlatformProvider adds a platform provider to this org's enabled list.
func (s *Server) handleOrgEnablePlatformProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	// Verify the provider exists and is platform-scoped
	existing, err := sso.GetProvider(r.Context(), s.db.Pool, s.masterKey, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch provider")
		return
	}
	if existing.Scope != "platform" {
		writeError(w, http.StatusBadRequest, "provider is not a platform provider")
		return
	}

	if err := sso.EnablePlatformProvider(r.Context(), s.db.Pool, claims.OrgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enable provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	w.WriteHeader(http.StatusNoContent)
}

// handleOrgDisablePlatformProvider removes a platform provider from this org's enabled list.
func (s *Server) handleOrgDisablePlatformProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	if err := sso.DisablePlatformProvider(r.Context(), s.db.Pool, claims.OrgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	w.WriteHeader(http.StatusNoContent)
}

// handleOrgGetSSOSettings returns the SSO settings for the caller's org.
func (s *Server) handleOrgGetSSOSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var passwordLogin bool
	err := s.db.Pool.QueryRow(r.Context(),
		`SELECT sso_password_login FROM orgs WHERE id=$1`,
		claims.OrgID,
	).Scan(&passwordLogin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "org not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch settings")
		return
	}

	writeJSON(w, http.StatusOK, ssoSettingsResponse{SSOPasswordLogin: passwordLogin})
}

// handleOrgUpdateSSOSettings updates the SSO settings for the caller's org.
func (s *Server) handleOrgUpdateSSOSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req ssoSettingsResponse
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_, err := s.db.Pool.Exec(r.Context(),
		`UPDATE orgs SET sso_password_login=$1 WHERE id=$2`,
		req.SSOPasswordLogin, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	writeJSON(w, http.StatusOK, ssoSettingsResponse{SSOPasswordLogin: req.SSOPasswordLogin})
}

// handleOrgTestSSOProvider tests an OIDC provider configuration by fetching its discovery document.
func (s *Server) handleOrgTestSSOProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DiscoveryURL string `json:"discovery_url"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "discovery_url is required")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(req.DiscoveryURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   "Failed to reach discovery URL: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Discovery URL returned status %d", resp.StatusCode),
		})
		return
	}

	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   "Invalid OIDC discovery document: " + err.Error(),
		})
		return
	}

	if discovery.Issuer == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   "Discovery document missing required 'issuer' field",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"issuer":  discovery.Issuer,
		"provider_info": map[string]string{
			"issuer":                 discovery.Issuer,
			"authorization_endpoint": discovery.AuthorizationEndpoint,
			"token_endpoint":         discovery.TokenEndpoint,
		},
	})
}

// invalidateSSOOrgCache deletes the org-specific SSO provider cache key.
func (s *Server) invalidateSSOOrgCache(r *http.Request, orgID string) {
	if s.Cache != nil {
		s.Cache.Client().Del(r.Context(), fmt.Sprintf("sso:providers:%s", orgID))
	}
}
