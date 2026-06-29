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

// registerAndGetOrgToken registers a new user+org and returns both the token and orgID.
func registerAndGetOrgToken(t *testing.T, srv http.Handler) (token string, orgID string) {
	t.Helper()
	email := fmt.Sprintf("template-%d@example.com", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "pass123", "name": "Template Tester",
	})
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, regReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register failed: %d %s", rec.Code, rec.Body.String())
	}
	var regResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	orgName := fmt.Sprintf("Template Org %d", time.Now().UnixNano())
	orgBody, _ := json.Marshal(map[string]string{"org_name": orgName})
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewReader(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgRec := httptest.NewRecorder()
	srv.ServeHTTP(orgRec, orgReq)
	if orgRec.Code != http.StatusCreated {
		t.Fatalf("org create failed: %d %s", orgRec.Code, orgRec.Body.String())
	}
	var orgResp map[string]interface{}
	json.NewDecoder(orgRec.Body).Decode(&orgResp)
	token = orgResp["token"].(string)
	org := orgResp["org"].(map[string]interface{})
	orgID = org["id"].(string)
	return
}

func TestCreateAndListCellSnippet(t *testing.T) {
	s := setupTestServer(t)
	token, _ := registerAndGetOrgToken(t, s)

	body := `{"name":"Date Range","type":"cell","content":{"source":"WHERE created_at BETWEEN {{start}} AND {{end}}","type":"code","language":"sql"}}`
	req := httptest.NewRequest("POST", "/api/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var createResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&createResp)
	assert.NotEmpty(t, createResp["id"])

	// List
	listReq := httptest.NewRequest("GET", "/api/v1/templates?type=cell", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listW := httptest.NewRecorder()
	s.ServeHTTP(listW, listReq)
	assert.Equal(t, http.StatusOK, listW.Code)
	var resp map[string]interface{}
	json.NewDecoder(listW.Body).Decode(&resp)
	templates := resp["templates"].([]interface{})
	assert.GreaterOrEqual(t, len(templates), 1)
}

func TestTemplateDelete(t *testing.T) {
	s := setupTestServer(t)
	token, _ := registerAndGetOrgToken(t, s)

	// Create
	body := `{"name":"To Delete","type":"cell","content":{}}`
	req := httptest.NewRequest("POST", "/api/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var createResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&createResp)
	id := createResp["id"].(string)

	// Delete
	delReq := httptest.NewRequest("DELETE", "/api/v1/templates/"+id, nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	delW := httptest.NewRecorder()
	s.ServeHTTP(delW, delReq)
	assert.Equal(t, http.StatusNoContent, delW.Code)
}

func TestTemplateVisibilityIsolatedByOrg(t *testing.T) {
	s := setupTestServer(t)

	// Create as org A
	tokenA, _ := registerAndGetOrgToken(t, s)
	body := `{"name":"Org A Template","type":"cell","content":{}}`
	req := httptest.NewRequest("POST", "/api/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenA)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// List as a different org — should not see org A's template
	tokenB, _ := registerAndGetOrgToken(t, s)
	otherOrgReq := httptest.NewRequest("GET", "/api/v1/templates", nil)
	otherOrgReq.Header.Set("Authorization", "Bearer "+tokenB)
	otherW := httptest.NewRecorder()
	s.ServeHTTP(otherW, otherOrgReq)
	assert.Equal(t, http.StatusOK, otherW.Code)
	var resp map[string]interface{}
	json.NewDecoder(otherW.Body).Decode(&resp)
	templates := resp["templates"].([]interface{})
	for _, tmpl := range templates {
		m := tmpl.(map[string]interface{})
		assert.NotEqual(t, "Org A Template", m["name"])
	}
}
