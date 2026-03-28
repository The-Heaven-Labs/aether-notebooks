package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAccountOnly(t *testing.T) {
	s := setupTestServer(t)

	email := fmt.Sprintf("newuser-%d@test.com", time.Now().UnixNano())
	body := fmt.Sprintf(`{"email":%q,"password":"password123","name":"New User"}`, email)
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.NotEmpty(t, resp["onboarding_token"])
	assert.Nil(t, resp["org"])
}

func TestRegisterOldFlowBackcompat(t *testing.T) {
	s := setupTestServer(t)
	email := fmt.Sprintf("legacyuser-%d@test.com", time.Now().UnixNano())
	body := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Legacy","org_name":"Legacy Org %d"}`, email, time.Now().UnixNano())
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.NotEmpty(t, resp["token"])
	assert.NotNil(t, resp["org"])
}

// createTestOrgAndAdmin registers a user with org_name (backcompat flow) and returns (orgID, token).
func createTestOrgAndAdmin(t *testing.T, s *api.Server) (string, string) {
	t.Helper()
	email := fmt.Sprintf("admin-%d@test.com", time.Now().UnixNano())
	orgName := fmt.Sprintf("Test Org %d", time.Now().UnixNano())
	body := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Admin","org_name":%q}`, email, orgName)
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestOrgAndAdmin register failed: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	token := resp["token"].(string)
	orgID := resp["org"].(map[string]interface{})["id"].(string)
	return orgID, token
}

// createTestInvite POSTs to /api/v1/members/invite and returns the invite token string.
func createTestInvite(t *testing.T, s *api.Server, orgID, adminToken, email, role string) string {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"role":%q}`, email, role)
	req := httptest.NewRequest("POST", "/api/v1/members/invite", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestInvite failed: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["token"].(string)
}

func TestOrgCreate(t *testing.T) {
	s := setupTestServer(t)

	// Register account-only to get onboarding token
	email := fmt.Sprintf("neworg-%d@test.com", time.Now().UnixNano())
	regBody := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Org Creator"}`, email)
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	s.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code)
	var regResp map[string]interface{}
	json.NewDecoder(regW.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	// Create org using onboarding token
	orgName := fmt.Sprintf("New Org %d", time.Now().UnixNano())
	orgBody := fmt.Sprintf(`{"org_name":%q}`, orgName)
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewBufferString(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgW := httptest.NewRecorder()
	s.ServeHTTP(orgW, orgReq)

	assert.Equal(t, http.StatusCreated, orgW.Code)
	var orgResp map[string]interface{}
	json.NewDecoder(orgW.Body).Decode(&orgResp)
	assert.NotEmpty(t, orgResp["token"])
	assert.NotNil(t, orgResp["org"])
}

func TestOrgJoinWithInviteToken(t *testing.T) {
	s := setupTestServer(t)

	// Setup: create an org and an invite
	adminOrgID, adminToken := createTestOrgAndAdmin(t, s)
	inviteeEmail := fmt.Sprintf("invitee-%d@test.com", time.Now().UnixNano())
	inviteToken := createTestInvite(t, s, adminOrgID, adminToken, inviteeEmail, "viewer")

	// Register the invitee account-only
	regBody := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Invitee"}`, inviteeEmail)
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	s.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code)
	var regResp map[string]interface{}
	json.NewDecoder(regW.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	// Join org using invite token
	joinBody := `{"invite_token":"` + inviteToken + `"}`
	joinReq := httptest.NewRequest("POST", "/api/v1/auth/org/join", bytes.NewBufferString(joinBody))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	joinW := httptest.NewRecorder()
	s.ServeHTTP(joinW, joinReq)

	assert.Equal(t, http.StatusOK, joinW.Code)
	var joinResp map[string]interface{}
	json.NewDecoder(joinW.Body).Decode(&joinResp)
	assert.NotEmpty(t, joinResp["token"])
	assert.Equal(t, adminOrgID, joinResp["org"].(map[string]interface{})["id"])
}
