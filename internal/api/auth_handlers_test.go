package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAndLogin(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("test-%d@example.com", ts)

	// Register
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "securepass123",
		"name":     "Test User",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var regResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&regResp)
	onboardingToken, ok := regResp["onboarding_token"].(string)
	if !ok {
		t.Fatal("register: expected onboarding_token in response")
	}

	// Create org so user has an org membership to login with
	orgName := fmt.Sprintf("Login Org %d", ts)
	orgBody, _ := json.Marshal(map[string]string{"org_name": orgName})
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewReader(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgRec := httptest.NewRecorder()
	srv.ServeHTTP(orgRec, orgReq)
	if orgRec.Code != http.StatusCreated {
		t.Fatalf("org create: expected 201, got %d: %s", orgRec.Code, orgRec.Body.String())
	}
	var orgResp map[string]interface{}
	json.NewDecoder(orgRec.Body).Decode(&orgResp)
	orgID := orgResp["org"].(map[string]interface{})["id"].(string)

	// Login
	body, _ = json.Marshal(map[string]string{
		"email":    email,
		"password": "securepass123",
		"org_id":   orgID,
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var loginResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&loginResp)
	if loginResp["token"] == nil {
		t.Fatal("login: expected token in response")
	}
}

func TestRegister_PlatformAdminEmail(t *testing.T) {
	s := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("padmin-%d@example.com", ts)

	s.SetPlatformAdminEmail(email)

	// Register with the designated platform admin email
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "pass1234",
		"name":     "Platform Admin",
	})
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, regReq)
	require.Equal(t, http.StatusCreated, rec.Code)

	var regResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&regResp))
	onboardingToken, ok := regResp["onboarding_token"].(string)
	require.True(t, ok, "response should contain onboarding_token, got %v", regResp)

	// Create org to get a proper JWT
	orgBody, _ := json.Marshal(map[string]string{"org_name": fmt.Sprintf("Org %d", ts)})
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewReader(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgRec := httptest.NewRecorder()
	s.ServeHTTP(orgRec, orgReq)
	require.Equal(t, http.StatusCreated, orgRec.Code, "org create: %s", orgRec.Body.String())
	var orgResp map[string]any
	require.NoError(t, json.NewDecoder(orgRec.Body).Decode(&orgResp))

	// Verify is_platform_admin is true in the DB
	var dbFlag bool
	err := s.DB().Pool.QueryRow(context.Background(),
		`SELECT is_platform_admin FROM users WHERE email=$1`, email,
	).Scan(&dbFlag)
	require.NoError(t, err)
	assert.True(t, dbFlag, "user should be promoted to platform admin on registration")

	// Verify the issued JWT also carries the platform admin flag
	issuer := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	token, ok := orgResp["token"].(string)
	require.True(t, ok, "org create response should contain a token")
	claims, err := issuer.Validate(token)
	require.NoError(t, err)
	assert.True(t, claims.IsPlatformAdmin, "JWT should have is_platform_admin=true")
}
