package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// createTestOrgAndAdmin registers a user and creates an org via onboarding flow.
func createTestOrgAndAdmin(t *testing.T, s http.Handler) (string, string) {
	t.Helper()
	email := fmt.Sprintf("admin-%d@test.com", time.Now().UnixNano())
	regBody := fmt.Sprintf(`{"email":%q,"password":"password123","name":"Admin"}`, email)
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	s.ServeHTTP(regW, regReq)
	if regW.Code != http.StatusCreated {
		t.Fatalf("register failed: %d %s", regW.Code, regW.Body.String())
	}
	var regResp map[string]interface{}
	json.NewDecoder(regW.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	orgName := fmt.Sprintf("Test Org %d", time.Now().UnixNano())
	orgBody := fmt.Sprintf(`{"org_name":%q}`, orgName)
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewBufferString(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgW := httptest.NewRecorder()
	s.ServeHTTP(orgW, orgReq)
	if orgW.Code != http.StatusCreated {
		t.Fatalf("org create failed: %d %s", orgW.Code, orgW.Body.String())
	}
	var orgResp map[string]interface{}
	json.NewDecoder(orgW.Body).Decode(&orgResp)
	token := orgResp["token"].(string)
	orgID := orgResp["org"].(map[string]interface{})["id"].(string)
	return orgID, token
}

// createTestInvite POSTs to /api/v1/members/invite and returns the invite token string.
func createTestInvite(t *testing.T, s http.Handler, orgID, adminToken, email, role string) string {
	t.Helper()
	if role != "admin" {
		role = "admin"
	}
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

// orgBrandingBody mirrors the branding block of GET /org/settings.
type orgBrandingBody struct {
	AppName             string   `json:"app_name"`
	LogoAttachmentID    string   `json:"logo_attachment_id"`
	AccentColor         string   `json:"accent_color"`
	DefaultChartPalette []string `json:"default_chart_palette"`
}

// putOrgSettings PUTs a settings object to /org/settings and returns the recorder.
func putOrgSettings(t *testing.T, s http.Handler, token string, settings map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(settings)
	req := httptest.NewRequest("PUT", "/api/v1/org/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// getOrgBranding fetches /org/settings and returns the branding block (nil when
// unset) plus the status code.
func getOrgBranding(t *testing.T, s http.Handler, token string) (*orgBrandingBody, int) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/org/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return nil, rec.Code
	}
	var resp struct {
		Branding *orgBrandingBody `json:"branding"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp.Branding, rec.Code
}

func TestOrgBranding(t *testing.T) {
	// Registration and login are rate-limited per IP in Redis, shared across
	// test runs; raise the limits like other tests do.
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "500")
	srv := setupTestServerWithAttachDir(t)
	orgID, token := createTestOrgAndAdmin(t, srv)

	// Branding logos reference an org attachment uploaded through the org route.
	logoID := uploadOrgAttachment(t, srv, token)

	// Valid branding round-trips through PUT → GET.
	rec := putOrgSettings(t, srv, token, map[string]any{
		"branding": map[string]any{
			"app_name":              "Acme Analytics",
			"logo_attachment_id":    logoID,
			"accent_color":          "#7c5cff",
			"default_chart_palette": []string{"#7c5cff", "#22c55e"},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	branding, code := getOrgBranding(t, srv, token)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, branding)
	assert.Equal(t, "Acme Analytics", branding.AppName)
	assert.Equal(t, logoID, branding.LogoAttachmentID)
	assert.Equal(t, "#7c5cff", branding.AccentColor)
	assert.Equal(t, []string{"#7c5cff", "#22c55e"}, branding.DefaultChartPalette)

	// A partial PUT that omits branding leaves it in place: the stored settings
	// merge only touches provided top-level keys, so the typed round-trip is
	// what keeps branding alive.
	rec = putOrgSettings(t, srv, token, map[string]any{"audit_retention_days": 30})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	if branding, _ := getOrgBranding(t, srv, token); branding == nil || branding.AccentColor != "#7c5cff" {
		t.Fatalf("partial PUT wiped branding: %+v", branding)
	}

	// Invalid branding values are rejected and leave the stored settings alone.
	palette := make([]string, 13)
	for i := range palette {
		palette[i] = "#000000"
	}
	invalid := []map[string]any{
		{"branding": map[string]any{"accent_color": "red"}},
		{"branding": map[string]any{"default_chart_palette": palette}},
		{"branding": map[string]any{"default_chart_palette": []string{"#7c5cff", "nope"}}},
		{"branding": map[string]any{"app_name": strings.Repeat("x", 61)}},
		{"branding": map[string]any{"logo_attachment_id": "00000000-0000-0000-0000-000000000000"}},
		{"branding": map[string]any{"logo_attachment_id": "not-a-uuid"}},
	}
	for i, settings := range invalid {
		if rec := putOrgSettings(t, srv, token, settings); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid branding case %d: expected 400, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	if branding, _ := getOrgBranding(t, srv, token); branding == nil || branding.AppName != "Acme Analytics" {
		t.Fatalf("rejected update changed stored branding: %+v", branding)
	}

	// Non-admin members can neither write nor read org settings.
	memberToken := inviteNonAdminMember(t, srv, token, orgID)
	if rec := putOrgSettings(t, srv, memberToken, map[string]any{"branding": map[string]any{"app_name": "Nope"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin PUT: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, code := getOrgBranding(t, srv, memberToken); code != http.StatusForbidden {
		t.Fatalf("non-admin GET: expected 403, got %d", code)
	}
}
