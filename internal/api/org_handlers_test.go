package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
