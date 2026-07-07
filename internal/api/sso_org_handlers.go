package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/sso"
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
	Scopes         []string  `json:"scopes"`
	GroupsClaim    string    `json:"groups_claim"`
	GroupPrefix    string    `json:"group_prefix"`
	AutoSyncGroups bool      `json:"auto_sync_groups"`
	GetUserInfo    bool      `json:"get_user_info"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ssoSettingsResponse is the JSON shape for org SSO settings.
type ssoSettingsResponse struct {
	SSOPasswordLogin bool `json:"sso_password_login"`
}

// @Summary List org SSO providers
// @Description List all org-scoped SSO providers for the caller's org
// @Tags sso
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/providers [get]
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

// @Summary Create org SSO provider
// @Description Create an org-scoped SSO provider for the caller's org
// @Tags sso
// @Accept json
// @Produce json
// @Param request body object true "SSO provider details"
// @Success 201 {object} providerResponse
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/providers [post]
func (s *Server) handleOrgCreateSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req ssoProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" || req.ClientID == "" || req.ClientSecret == nil || *req.ClientSecret == "" || req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "name, client_id, client_secret, and discovery_url are required")
		return
	}

	domains := req.AllowedDomains
	if domains == nil {
		domains = []string{}
	}
	scopes := req.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	orgID := claims.OrgID
	p := sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           req.Name,
		ProviderType:   "oidc",
		ClientID:       req.ClientID,
		ClientSecret:   *req.ClientSecret,
		DiscoveryURL:   req.DiscoveryURL,
		AllowedDomains: domains,
		Enabled:        req.Enabled,
		Scopes:         scopes,
		GroupsClaim:    req.GroupsClaim,
		GroupPrefix:    req.GroupPrefix,
		AutoSyncGroups: req.AutoSyncGroups,
		GetUserInfo:    req.GetUserInfo,
	}

	created, err := sso.CreateProvider(r.Context(), s.db.Pool, s.masterKey, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "sso_provider.create", ResourceType: "sso_provider", ResourceID: created.ID, ResourceName: created.Name,
	})
	writeJSON(w, http.StatusCreated, providerToResponse(created))
}

// @Summary Update org SSO provider
// @Description Update an org-scoped SSO provider
// @Tags sso
// @Accept json
// @Produce json
// @Param id path string true "Provider ID"
// @Param request body object true "SSO provider details"
// @Success 200 {object} providerResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sso/providers/{id} [put]
func (s *Server) handleOrgUpdateSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	// Verify ownership
	existing, err := sso.GetProvider(r.Context(), s.db.Pool, s.masterKey, id, claims.OrgID)
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

	if req.Name == "" || req.ClientID == "" || req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "name, client_id, and discovery_url are required")
		return
	}

	domains := req.AllowedDomains
	if domains == nil {
		domains = []string{}
	}

	secret := ""
	if req.ClientSecret != nil {
		secret = *req.ClientSecret
	}

	p := sso.Provider{
		ID:             id,
		Name:           req.Name,
		ClientID:       req.ClientID,
		ClientSecret:   secret,
		DiscoveryURL:   req.DiscoveryURL,
		AllowedDomains: domains,
		Enabled:        req.Enabled,
		Scopes:         req.Scopes,
		GroupsClaim:    req.GroupsClaim,
		GroupPrefix:    req.GroupPrefix,
		AutoSyncGroups: req.AutoSyncGroups,
		GetUserInfo:    req.GetUserInfo,
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
	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "sso_provider.update", ResourceType: "sso_provider", ResourceID: id, ResourceName: req.Name,
	})
	writeJSON(w, http.StatusOK, providerToResponse(updated))
}

// @Summary Delete org SSO provider
// @Description Delete an org-scoped SSO provider
// @Tags sso
// @Produce json
// @Param id path string true "Provider ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sso/providers/{id} [delete]
func (s *Server) handleOrgDeleteSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	// Verify ownership
	existing, err := sso.GetProvider(r.Context(), s.db.Pool, s.masterKey, id, claims.OrgID)
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
	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "sso_provider.delete", ResourceType: "sso_provider", ResourceID: id, ResourceName: existing.Name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary List platform SSO providers
// @Description List all platform-scoped SSO providers with enabled_for_org status
// @Tags sso
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/platform-providers [get]
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
		scopes := p.Scopes
		if scopes == nil {
			scopes = []string{}
		}
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
			Scopes:         scopes,
			GroupsClaim:    p.GroupsClaim,
			GroupPrefix:    p.GroupPrefix,
			AutoSyncGroups: p.AutoSyncGroups,
			GetUserInfo:    p.GetUserInfo,
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": resp})
}

// @Summary Enable platform SSO provider
// @Description Enable a platform-level SSO provider for the caller's org
// @Tags sso
// @Produce json
// @Param id path string true "Provider ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sso/platform-providers/{id}/enable [post]
func (s *Server) handleOrgEnablePlatformProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	// Verify the provider exists and is platform-scoped
	existing, err := sso.GetProvider(r.Context(), s.db.Pool, s.masterKey, id, claims.OrgID)
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
	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "sso_provider.enable", ResourceType: "sso_provider", ResourceID: id, ResourceName: existing.Name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Disable platform SSO provider
// @Description Disable a platform-level SSO provider for the caller's org
// @Tags sso
// @Produce json
// @Param id path string true "Provider ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/platform-providers/{id}/enable [delete]
func (s *Server) handleOrgDisablePlatformProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	if err := sso.DisablePlatformProvider(r.Context(), s.db.Pool, claims.OrgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable provider")
		return
	}

	s.invalidateSSOOrgCache(r, claims.OrgID)
	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "sso_provider.disable", ResourceType: "sso_provider", ResourceID: id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Get org SSO settings
// @Description Get the SSO settings for the caller's org
// @Tags sso
// @Produce json
// @Success 200 {object} ssoSettingsResponse
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/settings [get]
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

// @Summary Update org SSO settings
// @Description Update the SSO settings for the caller's org
// @Tags sso
// @Accept json
// @Produce json
// @Param request body object true "SSO settings"
// @Success 200 {object} ssoSettingsResponse
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/settings [put]
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

	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "sso_settings.update", ResourceType: "org", ResourceID: claims.OrgID,
		Metadata: map[string]any{"sso_password_login": req.SSOPasswordLogin},
	})
	writeJSON(w, http.StatusOK, ssoSettingsResponse{SSOPasswordLogin: req.SSOPasswordLogin})
}

// @Summary Test SSO provider
// @Description Test an OIDC provider configuration by fetching its discovery document
// @Tags sso
// @Accept json
// @Produce json
// @Param request body object true "Provider test details"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /sso/providers/test [post]
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

	// Append well-known path if not already present
	wellKnown := req.DiscoveryURL
	if !strings.HasSuffix(wellKnown, "/openid-configuration") {
		wellKnown = strings.TrimRight(wellKnown, "/") + "/.well-known/openid-configuration"
	}

	client := s.oidcHTTPClient(wellKnown)
	resp, err := client.Get(wellKnown)
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
