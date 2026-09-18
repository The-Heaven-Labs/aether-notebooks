package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/sso"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func createTestGroup(t *testing.T, s http.Handler, token, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var g map[string]any
	json.NewDecoder(rec.Body).Decode(&g)
	return g["id"].(string)
}

func postPendingMembers(t *testing.T, s http.Handler, token, groupID string, emails []string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"emails": emails})
	req := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/pending-members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp
}

func getPendingMembers(t *testing.T, s http.Handler, token, groupID string) (int, []map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/groups/"+groupID+"/pending-members", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var resp []map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp
}

func deletePendingMember(t *testing.T, s http.Handler, token, groupID, email string) int {
	t.Helper()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID+"/pending-members/"+email, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

func orgIDForEmail(t *testing.T, s *api.Server, email string) string {
	t.Helper()
	var orgID string
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(),
		`SELECT om.org_id FROM org_members om JOIN users u ON u.id = om.user_id WHERE u.email = $1`, email).Scan(&orgID))
	return orgID
}

func skipReasons(resp map[string]any) map[string]string {
	out := map[string]string{}
	skipped, _ := resp["skipped"].([]any)
	for _, s := range skipped {
		m, _ := s.(map[string]any)
		out[fmt.Sprintf("%v", m["email"])] = fmt.Sprintf("%v", m["reason"])
	}
	return out
}

// ─── Endpoint tests ──────────────────────────────────────────────────────────

func TestPendingGroupMemberEndpoints(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pgm-crud-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending CRUD Org")
	groupID := createTestGroup(t, srv, token, "Contractors")

	// Case-insensitive duplicate within one request: first wins, second skipped
	// as already_pending; invalid entries are reported.
	code, resp := postPendingMembers(t, srv, token, groupID,
		[]string{"Alice@Example.com", "bob@example.com", "alice@example.com", "not-an-email"})
	require.Equal(t, http.StatusOK, code, "response: %v", resp)
	assert.Equal(t, float64(2), resp["added"])
	reasons := skipReasons(resp)
	assert.Equal(t, "already_pending", reasons["alice@example.com"])
	assert.Equal(t, "invalid", reasons["not-an-email"])

	// Listing normalizes display and returns both rows.
	code, pending := getPendingMembers(t, srv, token, groupID)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, pending, 2)
	assert.Equal(t, "alice@example.com", pending[0]["email"])
	assert.Equal(t, "bob@example.com", pending[1]["email"])

	// Re-adding all is a no-op with already_pending for each.
	_, resp2 := postPendingMembers(t, srv, token, groupID, []string{"ALICE@example.com", "bob@example.com"})
	assert.Equal(t, float64(0), resp2["added"])
	assert.Equal(t, "already_pending", skipReasons(resp2)["alice@example.com"])

	// Delete is case-insensitive.
	require.Equal(t, http.StatusNoContent, deletePendingMember(t, srv, token, groupID, "ALICE@example.com"))
	_, pending2 := getPendingMembers(t, srv, token, groupID)
	require.Len(t, pending2, 1)
	assert.Equal(t, "bob@example.com", pending2[0]["email"])

	// Also accept the URL-encoded form the frontend sends (encodeURIComponent).
	code, _ = postPendingMembers(t, srv, token, groupID, []string{"carol@example.com"})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, http.StatusNoContent, deletePendingMember(t, srv, token, groupID, "CAROL%40example.com"))
	_, pending3 := getPendingMembers(t, srv, token, groupID)
	require.Len(t, pending3, 1)
	assert.Equal(t, "bob@example.com", pending3[0]["email"])
}

func TestPendingGroupMembersExistingUserDirectAdd(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("pgm-direct-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Direct Org")
	orgID := orgIDForEmail(t, srv, email)
	groupID := createTestGroup(t, srv, token, "Direct")

	// A second user who is already an org member.
	memberEmail := fmt.Sprintf("pgm-existing-%d@example.com", time.Now().UnixNano())
	memberID := pendingTestUser(t, srv, memberEmail)
	_, err := srv.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'non-admin')`, orgID, memberID)
	require.NoError(t, err)

	// Added directly, not staged.
	code, resp := postPendingMembers(t, srv, token, groupID, []string{memberEmail})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, float64(1), resp["added"])
	assert.Equal(t, 1, groupMemberCount(t, srv, groupID, memberID))
	assert.Equal(t, 0, pendingRowCount(t, srv, orgID, memberEmail), "existing user must not be staged")

	// Second attempt is an already_member skip.
	_, resp2 := postPendingMembers(t, srv, token, groupID, []string{memberEmail})
	assert.Equal(t, float64(0), resp2["added"])
	assert.Equal(t, "already_member", skipReasons(resp2)[memberEmail])
}

func TestPendingGroupMembersEveryoneRejected(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("pgm-everyone-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Everyone Org")
	orgID := orgIDForEmail(t, srv, email)

	var everyoneID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE org_id = $1 AND name = 'Everyone'`, orgID).Scan(&everyoneID))

	code, _ := postPendingMembers(t, srv, token, everyoneID, []string{"someone@example.com"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = getPendingMembers(t, srv, token, everyoneID)
	assert.Equal(t, http.StatusOK, code, "Everyone is a valid group to inspect; it just cannot have pending rows")
}

func TestPendingGroupMembersOrgIsolation(t *testing.T) {
	srv := setupTestServer(t)

	// Org A admin stages a pending row.
	emailA := fmt.Sprintf("pgm-org-a-%d@example.com", time.Now().UnixNano())
	tokenA := registerAndGetToken(t, srv, emailA, "Pending Org A")
	groupA := createTestGroup(t, srv, tokenA, "A Group")
	code, _ := postPendingMembers(t, srv, tokenA, groupA, []string{"x@example.com"})
	require.Equal(t, http.StatusOK, code)

	// Org B admin cannot see or modify org A's group.
	emailB := fmt.Sprintf("pgm-org-b-%d@example.com", time.Now().UnixNano())
	tokenB := registerAndGetToken(t, srv, emailB, "Pending Org B")

	code, _ = getPendingMembers(t, srv, tokenB, groupA)
	assert.Equal(t, http.StatusNotFound, code, "cross-org listing must 404")
	code, _ = postPendingMembers(t, srv, tokenB, groupA, []string{"y@example.com"})
	assert.Equal(t, http.StatusNotFound, code, "cross-org add must 404")
	assert.Equal(t, http.StatusNotFound, deletePendingMember(t, srv, tokenB, groupA, "x@example.com"))
}

// ─── Materialization on first appearance ─────────────────────────────────────

func TestPendingGroupMembersMaterializeOnRegistration(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()

	adminEmail := fmt.Sprintf("pgm-reg-admin-%d@example.com", time.Now().UnixNano())
	adminToken := registerAndGetToken(t, srv, adminEmail, "Pending Reg Org")
	orgID := orgIDForEmail(t, srv, adminEmail)
	groupID := createTestGroup(t, srv, adminToken, "Newcomers")

	domain := fmt.Sprintf("pgmreg-%d.example.com", time.Now().UnixNano())
	_, err := srv.DB().Pool.Exec(ctx,
		`INSERT INTO org_allowed_domains (org_id, domain, auto_join) VALUES ($1, $2, true)`, orgID, domain)
	require.NoError(t, err)

	newcomerEmail := "newcomer@" + domain
	code, resp := postPendingMembers(t, srv, adminToken, groupID, []string{newcomerEmail})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, float64(1), resp["added"])

	// Register with a domain that auto-joins the org.
	regBody := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Newcomer"}`, newcomerEmail)
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader([]byte(regBody)))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	srv.ServeHTTP(regRec, regReq)
	require.Equal(t, http.StatusCreated, regRec.Code, "register: %s", regRec.Body.String())

	var userID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, newcomerEmail).Scan(&userID))

	assert.Equal(t, 1, groupMemberCount(t, srv, groupID, userID), "pending membership should materialize on registration")
	assert.Equal(t, 0, pendingRowCount(t, srv, orgID, newcomerEmail), "pending row should be consumed")
}

func TestPendingGroupMembersMaterializeOnInviteJoin(t *testing.T) {
	srv := setupTestServer(t)

	adminOrgID, adminToken := createTestOrgAndAdmin(t, srv)
	groupID := createTestGroup(t, srv, adminToken, "Invitees")

	inviteeEmail := fmt.Sprintf("pgm-invitee-%d@example.com", time.Now().UnixNano())
	inviteToken := createTestInvite(t, srv, adminOrgID, adminToken, inviteeEmail, "viewer")

	code, resp := postPendingMembers(t, srv, adminToken, groupID, []string{inviteeEmail})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, float64(1), resp["added"])

	// Register account-only, then redeem the invite.
	regBody := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Invitee"}`, inviteeEmail)
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader([]byte(regBody)))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	srv.ServeHTTP(regRec, regReq)
	require.Equal(t, http.StatusCreated, regRec.Code, "register: %s", regRec.Body.String())
	var regResp map[string]any
	json.NewDecoder(regRec.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	joinReq := httptest.NewRequest("POST", "/api/v1/auth/org/join",
		bytes.NewReader([]byte(`{"invite_token":"`+inviteToken+`"}`)))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	joinRec := httptest.NewRecorder()
	srv.ServeHTTP(joinRec, joinReq)
	require.Equal(t, http.StatusOK, joinRec.Code, "join: %s", joinRec.Body.String())

	var userID string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, inviteeEmail).Scan(&userID))
	assert.Equal(t, 1, groupMemberCount(t, srv, groupID, userID), "pending membership should materialize on invite redemption")
	assert.Equal(t, 0, pendingRowCount(t, srv, adminOrgID, inviteeEmail), "pending row should be consumed")
}

func TestPendingGroupMembersMaterializeOnSSOFirstLogin(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	orgID := pendingTestOrg(t, s)
	adminID := pendingTestUser(t, s, fmt.Sprintf("pgm-sso-admin-%d@example.com", time.Now().UnixNano()))
	groupID := pendingTestGroup(t, s, orgID, "SSO Newcomers")

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("pgm-sso-%d@example.com", ts)
	stagePending(t, s, orgID, groupID, email, adminID)

	oidcSrv := newTestOIDCServer(t, fmt.Sprintf("pgm-sso-sub-%d", ts), email, "SSO Newcomer", nil, false)
	provider, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), sso.Provider{
		Scope:            "org",
		OrgID:            &orgID,
		Name:             "Pending SSO Provider",
		ProviderType:     "oidc",
		ClientID:         "test-client-id",
		ClientSecret:     "test-secret",
		DiscoveryURL:     oidcSrv.baseURL,
		AllowedDomains:   []string{"example.com"},
		Scopes:           []string{"openid", "profile", "email"},
		Enabled:          true,
		ProvisioningMode: "join_provider_org",
		DefaultRole:      "viewer",
	})
	require.NoError(t, err)

	state := fmt.Sprintf("pgm-state-%d", ts)
	_, err = s.Cache.Client().SetNX(ctx, fmt.Sprintf("oidc:state:%s", state), "1", 10*time.Minute).Result()
	require.NoError(t, err)

	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/%s/callback?code=test-code&state=%s", provider.ID, state)
	req := httptest.NewRequest("GET", callbackURL, nil)
	req.Host = "localhost"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code, "callback: %s", rec.Body.String())

	var userID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID))
	assert.Equal(t, 1, groupMemberCount(t, s, groupID, userID), "pending membership should materialize on SSO first login")
	assert.Equal(t, 0, pendingRowCount(t, s, orgID, email), "pending row should be consumed")
}

// TestPendingGroupMembersMaterializeOnSSOExistingUserJoin covers the
// existing-user auto-join path: a user that already has an account (in another
// org) is auto-joined to the provider org and receives its pending groups.
func TestPendingGroupMembersMaterializeOnSSOExistingUserJoin(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	// Existing user with their own org.
	existingEmail := fmt.Sprintf("pgm-sso-existing-%d@example.com", time.Now().UnixNano())
	existingToken := registerAndGetToken(t, s, existingEmail, "Existing User Org")
	_ = existingToken
	var existingUserID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, existingEmail).Scan(&existingUserID))

	// Target org with a pending group for that email.
	targetOrgID := pendingTestOrg(t, s)
	adminID := pendingTestUser(t, s, fmt.Sprintf("pgm-target-admin-%d@example.com", time.Now().UnixNano()))
	groupID := pendingTestGroup(t, s, targetOrgID, "Auto Join Group")
	stagePending(t, s, targetOrgID, groupID, existingEmail, adminID)

	ts := time.Now().UnixNano()
	oidcSrv := newTestOIDCServer(t, fmt.Sprintf("pgm-existing-sub-%d", ts), existingEmail, "Existing User", nil, false)
	provider, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), sso.Provider{
		Scope:            "org",
		OrgID:            &targetOrgID,
		Name:             "Pending SSO Existing Provider",
		ProviderType:     "oidc",
		ClientID:         "test-client-id",
		ClientSecret:     "test-secret",
		DiscoveryURL:     oidcSrv.baseURL,
		AllowedDomains:   []string{"example.com"},
		Scopes:           []string{"openid", "profile", "email"},
		Enabled:          true,
		ProvisioningMode: "join_provider_org",
		DefaultRole:      "viewer",
	})
	require.NoError(t, err)

	state := fmt.Sprintf("pgm-existing-state-%d", ts)
	_, err = s.Cache.Client().SetNX(ctx, fmt.Sprintf("oidc:state:%s", state), "1", 10*time.Minute).Result()
	require.NoError(t, err)

	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/%s/callback?code=test-code&state=%s", provider.ID, state)
	req := httptest.NewRequest("GET", callbackURL, nil)
	req.Host = "localhost"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code, "callback: %s", rec.Body.String())

	assert.Equal(t, 1, groupMemberCount(t, s, groupID, existingUserID), "pending membership should materialize on existing-user auto-join")
	assert.Equal(t, 0, pendingRowCount(t, s, targetOrgID, existingEmail), "pending row should be consumed")
}

// TestPendingGroupMembersMaterializeOnSubdomainRegistration covers the
// subdomain-resolved registration path in handleRegister.
func TestPendingGroupMembersMaterializeOnSubdomainRegistration(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("pgmsub-%d", time.Now().UnixNano())
	var orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&orgID))

	adminID := pendingTestUser(t, srv, fmt.Sprintf("pgmsub-admin-%d@example.com", time.Now().UnixNano()))
	groupID := pendingTestGroup(t, srv, orgID, "Subdomain Group")
	email := fmt.Sprintf("pgmsub-user-%d@example.com", time.Now().UnixNano())
	stagePending(t, srv, orgID, groupID, email, adminID)

	regBody := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Subdomain User"}`, email)
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader([]byte(regBody)))
	regReq.Header.Set("Content-Type", "application/json")
	regReq.Host = slug + ".localhost"
	regRec := httptest.NewRecorder()
	srv.ServeHTTP(regRec, regReq)
	require.Equal(t, http.StatusCreated, regRec.Code, "register: %s", regRec.Body.String())

	var userID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID))
	assert.Equal(t, 1, groupMemberCount(t, srv, groupID, userID), "pending membership should materialize on subdomain registration")
	assert.Equal(t, 0, pendingRowCount(t, srv, orgID, email), "pending row should be consumed")
}

// Guard against accidental normalization regressions: the endpoint must reject
// values without an @ and store lowercased emails.
func TestPendingGroupMembersEmailNormalization(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pgm-norm-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Norm Org")
	groupID := createTestGroup(t, srv, token, "Norm")

	code, resp := postPendingMembers(t, srv, token, groupID, []string{"  Mixed@Case.COM  ", "@nodomain", "nodot@", ""})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, float64(1), resp["added"])
	assert.True(t, strings.HasPrefix(skipReasons(resp)["@nodomain"], "invalid"))

	_, pending := getPendingMembers(t, srv, token, groupID)
	require.Len(t, pending, 1)
	assert.Equal(t, "mixed@case.com", pending[0]["email"])
}
