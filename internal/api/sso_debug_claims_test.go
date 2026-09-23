package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/cache"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
)

func ssoDebugTestRedisURL() string {
	if u := os.Getenv("AETHER_REDIS_URL"); u != "" {
		return u
	}
	return "redis://localhost:6379"
}

// newSSODebugTestServer builds a Server with the real test DB and Redis;
// setupTestServer lives in package api_test and is not callable here.
func newSSODebugTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := database.Connect(context.Background(), warehouseSyncTestDSN(), "")
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, db.Migrate(context.Background()))

	c, err := cache.New(ssoDebugTestRedisURL())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	require.NoError(t, c.Ping(context.Background()))

	s := NewServer(db, auth.NewJWTIssuer("test-secret", 15*time.Minute), audit.NewLogger(db), crypto.DeriveKey(warehouseSyncTestMasterKey), c)
	t.Cleanup(s.Close)
	return s
}

func seedSSODebugProvider(t *testing.T, s *Server, debug bool) string {
	t.Helper()
	id, _ := seedSSODebugOrgProvider(t, s, debug)
	return id
}

// seedSSODebugOrgProvider inserts an org plus an org-scoped provider in it,
// returning both IDs.
func seedSSODebugOrgProvider(t *testing.T, s *Server, debug bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var orgID string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		"sso-debug", "sso-debug-"+uuid.NewString(),
	).Scan(&orgID))
	var id string
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO sso_providers (scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, debug_claims)
		 VALUES ('org', $1, $2, 'oidc', 'client', 'deadbeef', 'https://idp.example.com', '{}', true, '{}', 'groups', $3)
		 RETURNING id`,
		orgID, "sso-debug-"+uuid.NewString(), debug,
	).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		s.db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, id)
		s.db.Pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, orgID)
		s.Cache.Client().Del(ctx, ssoDebugClaimsKey(id))
	})
	return id, orgID
}

// seedSSODebugAdmin inserts a user and returns a platform-admin token for it.
func seedSSODebugAdmin(t *testing.T, s *Server) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name, is_platform_admin) VALUES ($1, $2, $3, true)`,
		userID, "sso-debug-"+userID+"@test.local", "SSO Debug Admin",
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		s.db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	token, err := s.jwt.IssuePlatformAdmin(userID, uuid.NewString(), "admin")
	require.NoError(t, err)
	return token
}

// seedSSODebugOrgAdmin inserts a user and returns an org-admin token scoped to orgID.
func seedSSODebugOrgAdmin(t *testing.T, s *Server, orgID string) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID, "sso-debug-"+userID+"@test.local", "SSO Debug Org Admin",
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		s.db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	token, err := s.jwt.Issue(userID, orgID, "admin")
	require.NoError(t, err)
	return token
}

func TestSSODebugCaptureRoundTripAndRedaction(t *testing.T) {
	s := newSSODebugTestServer(t)
	ctx := context.Background()
	providerID := seedSSODebugProvider(t, s, true)

	claims := &auth.OIDCClaims{
		Subject: "user-1",
		Email:   "alice@example.com",
		Name:    "Alice",
		Groups:  []string{"aether-analysts"},
		Debug: &auth.OIDCExchangeDebug{
			GroupsClaim: "groups",
			RawIDTokenClaims: map[string]any{
				"email":        "alice@example.com",
				"access_token": "must-not-be-stored",
				"nested":       map[string]any{"refresh-token": "must-not-be-stored", "keep": "yes"},
			},
			UserInfoClaims:   map[string]any{"authorization": "Bearer must-not-be-stored", "groups": []any{"aether-analysts"}},
			GrantedScopes:    []string{"openid", "groups"},
			GroupsClaimValue: []any{"aether-analysts"},
			ParsedGroups:     []string{"aether-analysts"},
		},
	}
	s.storeSSODebugCapture(ctx, providerID, claims)

	capture, err := s.loadSSODebugCapture(ctx, providerID)
	require.NoError(t, err)
	require.NotNil(t, capture)
	assert.Equal(t, "user-1", capture.Subject)
	assert.Equal(t, "alice@example.com", capture.Email)
	assert.Equal(t, "Alice", capture.Name)
	assert.Equal(t, []string{"openid", "groups"}, capture.GrantedScopes)
	assert.Equal(t, []string{"aether-analysts"}, capture.ParsedGroups)
	assert.Equal(t, "groups", capture.GroupsClaim)

	_, hasToken := capture.IDTokenClaims["access_token"]
	assert.False(t, hasToken, "access_token must be redacted")
	nested, ok := capture.IDTokenClaims["nested"].(map[string]any)
	require.True(t, ok)
	_, hasRefresh := nested["refresh-token"]
	assert.False(t, hasRefresh, "nested refresh-token must be redacted")
	assert.Equal(t, "yes", nested["keep"])
	_, hasAuth := capture.UserInfoClaims["authorization"]
	assert.False(t, hasAuth, "authorization must be redacted")

	ttl := s.Cache.Client().TTL(ctx, ssoDebugClaimsKey(providerID)).Val()
	assert.Greater(t, ttl, 30*time.Minute)
	assert.LessOrEqual(t, ttl, time.Hour)
}

func TestSSODebugCaptureLatestWins(t *testing.T) {
	s := newSSODebugTestServer(t)
	ctx := context.Background()
	providerID := seedSSODebugProvider(t, s, true)

	for _, subject := range []string{"first", "second"} {
		s.storeSSODebugCapture(ctx, providerID, &auth.OIDCClaims{
			Subject: subject,
			Debug:   &auth.OIDCExchangeDebug{GroupsClaim: "groups"},
		})
	}
	capture, err := s.loadSSODebugCapture(ctx, providerID)
	require.NoError(t, err)
	require.NotNil(t, capture)
	assert.Equal(t, "second", capture.Subject)
}

func TestAdminGetSSODebugClaims(t *testing.T) {
	s := newSSODebugTestServer(t)
	ctx := context.Background()
	providerID := seedSSODebugProvider(t, s, true)
	adminToken := seedSSODebugAdmin(t, s)

	get := func(token string, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/admin/sso/providers/"+id+"/debug-claims", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	// No capture yet → 404.
	rec := get(adminToken, providerID)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// Unknown provider → 404.
	rec = get(adminToken, uuid.NewString())
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	s.storeSSODebugCapture(ctx, providerID, &auth.OIDCClaims{
		Subject: "user-1",
		Email:   "alice@example.com",
		Debug: &auth.OIDCExchangeDebug{
			GroupsClaim:      "groups",
			RawIDTokenClaims: map[string]any{"email": "alice@example.com", "id_token": "nope"},
			GrantedScopes:    []string{"openid"},
			GroupsClaimValue: "not-an-array",
			ParsedGroups:     []string{},
		},
	})

	// Platform admin → 200 with the redacted capture.
	rec = get(adminToken, providerID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "user-1", got["subject"])
	assert.Equal(t, "groups", got["groups_claim"])
	assert.Equal(t, "not-an-array", got["groups_claim_value"])
	idClaims, ok := got["id_token_claims"].(map[string]any)
	require.True(t, ok)
	_, hasToken := idClaims["id_token"]
	assert.False(t, hasToken, "id_token must not be served")

	// Viewing is audit-logged.
	var count int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='sso.debug_claims.view' AND resource_id=$1`,
		providerID,
	).Scan(&count))
	assert.Equal(t, 1, count)

	// A non-platform org admin is forbidden.
	orgAdminToken, err := s.jwt.Issue(uuid.NewString(), uuid.NewString(), "admin")
	require.NoError(t, err)
	rec = get(orgAdminToken, providerID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestOrgGetSSODebugClaims(t *testing.T) {
	s := newSSODebugTestServer(t)
	ctx := context.Background()
	providerID, orgID := seedSSODebugOrgProvider(t, s, true)
	otherProviderID, otherOrgID := seedSSODebugOrgProvider(t, s, true)

	// A platform-scoped provider is never inspectable through the org route.
	var platformProviderID string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`INSERT INTO sso_providers (scope, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, debug_claims)
		 VALUES ('platform', $1, 'oidc', 'client', 'deadbeef', 'https://idp.example.com', '{}', true, '{}', 'groups', true)
		 RETURNING id`,
		"sso-debug-platform-"+uuid.NewString(),
	).Scan(&platformProviderID))
	t.Cleanup(func() {
		s.db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, platformProviderID)
		s.Cache.Client().Del(ctx, ssoDebugClaimsKey(platformProviderID))
	})

	adminToken := seedSSODebugOrgAdmin(t, s, orgID)
	otherAdminToken := seedSSODebugOrgAdmin(t, s, otherOrgID)

	get := func(token string, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/sso/providers/"+id+"/debug-claims", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	// No capture yet → 404.
	rec := get(adminToken, providerID)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// Unknown provider → 404.
	rec = get(adminToken, uuid.NewString())
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// Another org's provider → 403.
	rec = get(otherAdminToken, providerID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	rec = get(adminToken, otherProviderID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// Platform providers stay platform-admin only → 403.
	rec = get(adminToken, platformProviderID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	s.storeSSODebugCapture(ctx, providerID, &auth.OIDCClaims{
		Subject: "user-1",
		Email:   "alice@example.com",
		Debug: &auth.OIDCExchangeDebug{
			GroupsClaim:      "groups",
			RawIDTokenClaims: map[string]any{"email": "alice@example.com", "access_token": "nope"},
			GrantedScopes:    []string{"openid"},
			ParsedGroups:     []string{"aether-analysts"},
		},
	})

	// Owning org admin → 200 with the redacted capture.
	rec = get(adminToken, providerID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "user-1", got["subject"])
	assert.Equal(t, []any{"aether-analysts"}, got["parsed_groups"])
	idClaims, ok := got["id_token_claims"].(map[string]any)
	require.True(t, ok)
	_, hasToken := idClaims["access_token"]
	assert.False(t, hasToken, "access_token must not be served")

	// Viewing is audit-logged against the org.
	var count int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='sso.debug_claims.view' AND resource_id=$1 AND org_id=$2`,
		providerID, orgID,
	).Scan(&count))
	assert.Equal(t, 1, count)
}
