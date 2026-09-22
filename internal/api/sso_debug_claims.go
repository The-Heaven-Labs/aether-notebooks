package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/the-heaven-labs/aether/internal/auth"
)

// ssoDebugClaimsTTL bounds how long a captured IDP payload stays available.
const ssoDebugClaimsTTL = time.Hour

// ssoDebugCapture is the redacted IDP payload snapshot stored for a provider
// while its debug_claims flag is on. Latest login wins; token strings are never
// captured (only parsed claim JSON, errors, and granted scope names).
type ssoDebugCapture struct {
	CapturedAt       time.Time      `json:"captured_at"`
	ProviderID       string         `json:"provider_id"`
	Subject          string         `json:"subject"`
	Email            string         `json:"email"`
	Name             string         `json:"name"`
	GrantedScopes    []string       `json:"granted_scopes"`
	IDTokenClaims    map[string]any `json:"id_token_claims"`
	UserInfoClaims   map[string]any `json:"user_info_claims,omitempty"`
	UserInfoError    string         `json:"user_info_error,omitempty"`
	GroupsClaim      string         `json:"groups_claim"`
	GroupsClaimValue any            `json:"groups_claim_value,omitempty"`
	ParsedGroups     []string       `json:"parsed_groups"`
}

func ssoDebugClaimsKey(providerID string) string {
	return "sso:debug_claims:" + providerID
}

// storeSSODebugCapture persists the latest redacted exchange capture for a
// provider. Best-effort: a cache outage must never block a login.
func (s *Server) storeSSODebugCapture(ctx context.Context, providerID string, claims *auth.OIDCClaims) {
	if s.Cache == nil || claims == nil || claims.Debug == nil {
		return
	}
	capture := ssoDebugCapture{
		CapturedAt:       time.Now().UTC(),
		ProviderID:       providerID,
		Subject:          claims.Subject,
		Email:            claims.Email,
		Name:             claims.Name,
		GrantedScopes:    claims.Debug.GrantedScopes,
		IDTokenClaims:    auth.RedactTokenMaterial(claims.Debug.RawIDTokenClaims),
		UserInfoClaims:   auth.RedactTokenMaterial(claims.Debug.UserInfoClaims),
		UserInfoError:    claims.Debug.UserInfoError,
		GroupsClaim:      claims.Debug.GroupsClaim,
		GroupsClaimValue: auth.RedactTokenMaterialValue(claims.Debug.GroupsClaimValue),
		ParsedGroups:     claims.Debug.ParsedGroups,
	}
	data, err := json.Marshal(capture)
	if err != nil {
		slog.Warn("failed to encode SSO debug claims capture", "provider_id", providerID, "error", err)
		return
	}
	if err := s.Cache.Client().Set(ctx, ssoDebugClaimsKey(providerID), data, ssoDebugClaimsTTL).Err(); err != nil {
		slog.Warn("failed to store SSO debug claims capture", "provider_id", providerID, "error", err)
	}
}

// loadSSODebugCapture returns the stored capture, or nil when none exists (or
// the cache is unavailable).
func (s *Server) loadSSODebugCapture(ctx context.Context, providerID string) (*ssoDebugCapture, error) {
	if s.Cache == nil {
		return nil, nil
	}
	data, err := s.Cache.Client().Get(ctx, ssoDebugClaimsKey(providerID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var capture ssoDebugCapture
	if err := json.Unmarshal(data, &capture); err != nil {
		return nil, err
	}
	return &capture, nil
}
