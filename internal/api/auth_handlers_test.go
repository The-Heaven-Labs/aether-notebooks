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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAndLogin(t *testing.T) {
	srv := setupTestServer(t)

	// Use a unique email per test run to avoid conflicts
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("test-%d@example.com", ts)
	orgName := fmt.Sprintf("Test Org %d", ts)

	// Register
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "securepass123",
		"name":     "Test User",
		"org_name": orgName,
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
	if regResp["token"] == nil {
		t.Fatal("register: expected token in response")
	}

	// Login
	body, _ = json.Marshal(map[string]string{
		"email":    email,
		"password": "securepass123",
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

	// Register with the designated platform admin email (with org, uses flow 1)
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "pass1234",
		"name":     "Platform Admin",
		"org_name": fmt.Sprintf("Org %d", ts),
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Verify is_platform_admin is true in the DB
	var isPlatformAdmin bool
	err := s.DB().Pool.QueryRow(context.Background(),
		`SELECT is_platform_admin FROM users WHERE email=$1`, email,
	).Scan(&isPlatformAdmin)
	require.NoError(t, err)
	assert.True(t, isPlatformAdmin, "user should be promoted to platform admin on registration")
}
