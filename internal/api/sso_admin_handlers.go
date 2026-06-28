package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/jackc/pgx/v5"
)

// providerResponse is the JSON shape returned for SSO provider responses.
// client_secret is intentionally omitted.
type providerResponse struct {
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	Name           string    `json:"name"`
	ProviderType   string    `json:"provider_type"`
	ClientID       string    `json:"client_id"`
	DiscoveryURL   string    `json:"discovery_url"`
	AllowedDomains []string  `json:"allowed_domains"`
	Enabled        bool      `json:"enabled"`
	Scopes         []string  `json:"scopes"`
	GroupsClaim    string    `json:"groups_claim"`
	GroupPrefix    string    `json:"group_prefix"`
	AutoSyncGroups bool      `json:"auto_sync_groups"`
	GetUserInfo    bool      `json:"get_user_info"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func providerToResponse(p sso.Provider) providerResponse {
	domains := p.AllowedDomains
	if domains == nil {
		domains = []string{}
	}
	scopes := p.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return providerResponse{
		ID:             p.ID,
		Scope:          p.Scope,
		Name:           p.Name,
		ProviderType:   p.ProviderType,
		ClientID:       p.ClientID,
		DiscoveryURL:   p.DiscoveryURL,
		AllowedDomains: domains,
		Enabled:        p.Enabled,
		Scopes:         scopes,
		GroupsClaim:    p.GroupsClaim,
		GroupPrefix:    p.GroupPrefix,
		AutoSyncGroups: p.AutoSyncGroups,
		GetUserInfo:    p.GetUserInfo,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

type ssoProviderRequest struct {
	Name           string   `json:"name"`
	ClientID       string   `json:"client_id"`
	ClientSecret   *string  `json:"client_secret"`
	DiscoveryURL   string   `json:"discovery_url"`
	AllowedDomains []string `json:"allowed_domains"`
	Enabled        bool     `json:"enabled"`
	Scopes         []string `json:"scopes"`
	GroupsClaim    string   `json:"groups_claim"`
	GroupPrefix    string   `json:"group_prefix"`
	AutoSyncGroups bool     `json:"auto_sync_groups"`
	GetUserInfo    bool     `json:"get_user_info"`
}

func (s *Server) handleAdminListSSOProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := sso.ListPlatformProviders(r.Context(), s.db.Pool, s.masterKey)
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

func (s *Server) handleAdminCreateSSOProvider(w http.ResponseWriter, r *http.Request) {
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

	p := sso.Provider{
		Scope:          "platform",
		Name:           req.Name,
		ProviderType:   "oidc",
		ClientID:       req.ClientID,
		ClientSecret:   *req.ClientSecret,
		DiscoveryURL:   req.DiscoveryURL,
		AllowedDomains: domains,
		Enabled:        req.Enabled,
		Scopes:         req.Scopes,
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

	s.invalidateSSOPlatformCache(r)
	s.audit.Log(r.Context(), audit.Entry{
		UserID: claims.UserID,
		Action: "sso_provider.create", ResourceType: "sso_provider", ResourceID: created.ID, ResourceName: created.Name,
	})
	writeJSON(w, http.StatusCreated, providerToResponse(created))
}

func (s *Server) handleAdminUpdateSSOProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req ssoProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
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

	s.invalidateSSOPlatformCache(r)
	s.audit.Log(r.Context(), audit.Entry{
		UserID: claims.UserID,
		Action: "sso_provider.update", ResourceType: "sso_provider", ResourceID: id, ResourceName: req.Name,
	})
	writeJSON(w, http.StatusOK, providerToResponse(updated))
}

func (s *Server) handleAdminDeleteSSOProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	if err := sso.DeleteProvider(r.Context(), s.db.Pool, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete provider")
		return
	}

	s.invalidateSSOPlatformCache(r)
	s.audit.Log(r.Context(), audit.Entry{
		UserID: claims.UserID,
		Action: "sso_provider.delete", ResourceType: "sso_provider", ResourceID: id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// invalidateSSOPlatformCache deletes the platform SSO provider cache key.
func (s *Server) invalidateSSOPlatformCache(r *http.Request) {
	if s.Cache != nil {
		s.Cache.Client().Del(r.Context(), "sso:providers:platform")
	}
}

// handleAdminTestSSOProvider tests connectivity to an OIDC provider by fetching its discovery document.
func (s *Server) handleAdminTestSSOProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Get the provider from database
	var discoveryURL string
	err := s.db.Pool.QueryRow(r.Context(),
		`SELECT discovery_url FROM sso_providers WHERE id = $1`,
		id,
	).Scan(&discoveryURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get provider")
		return
	}

	// Append well-known path if not already present (the stored URL is the issuer URL)
	wellKnown := discoveryURL
	if !strings.HasSuffix(wellKnown, "/openid-configuration") {
		wellKnown = strings.TrimRight(wellKnown, "/") + "/.well-known/openid-configuration"
	}

	// Try to fetch the discovery document
	client := s.oidcHTTPClient(wellKnown)
	resp, err := client.Get(wellKnown)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"error":       fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status),
			"status_code": resp.StatusCode,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"message":     "Discovery document fetched successfully",
		"status_code": resp.StatusCode,
	})
}
