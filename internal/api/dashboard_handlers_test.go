package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/api"
)

func TestDashboardCRUD(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-test-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash Org")

	// Create dashboard
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Sales Dashboard",
	})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var dashResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	// List
	req = httptest.NewRequest("GET", "/api/v1/dashboards", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Get
	req = httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Share — generate public token
	req = httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/share", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("share: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var shareResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&shareResp)
	publicToken := shareResp["token"].(string)
	if publicToken == "" {
		t.Fatal("expected non-empty public token")
	}

	// Public view
	req = httptest.NewRequest("GET", "/api/v1/public/"+publicToken, nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}

func addWidget(t *testing.T, srv *api.Server, token, dashID string, layout map[string]int) int {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"type":   "chart",
		"layout": layout,
	})
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code
}

func TestWidgetLayoutValidation_OutOfBounds(t *testing.T) {
	os.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("wval-oob-%d@example.com", ts), "WVal OOB Org")

	// Create dashboard
	body, _ := json.Marshal(map[string]interface{}{"title": "Validation Dashboard"})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create dashboard: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var dashResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	// Widget with col:6 + width:12 = 18 > 12 columns
	code := addWidget(t, srv, token, dashID, map[string]int{"row": 0, "col": 6, "width": 12, "height": 8})
	if code != http.StatusBadRequest {
		t.Fatalf("out-of-bounds: expected 400, got %d", code)
	}
}

func TestWidgetLayoutValidation_NegativeValues(t *testing.T) {
	os.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("wval-neg-%d@example.com", ts), "WVal Neg Org")

	body, _ := json.Marshal(map[string]interface{}{"title": "Validation Dashboard"})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create dashboard: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var dashResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	// Negative col
	code := addWidget(t, srv, token, dashID, map[string]int{"row": 0, "col": -1, "width": 6, "height": 8})
	if code != http.StatusBadRequest {
		t.Fatalf("negative col: expected 400, got %d", code)
	}
}

func TestWidgetLayoutValidation_Overlap(t *testing.T) {
	os.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("wval-overlap-%d@example.com", ts), "WVal Overlap Org")

	body, _ := json.Marshal(map[string]interface{}{"title": "Overlap Dashboard"})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create dashboard: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var dashResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	// First widget — should succeed
	code := addWidget(t, srv, token, dashID, map[string]int{"row": 0, "col": 0, "width": 6, "height": 8})
	if code != http.StatusCreated {
		t.Fatalf("first widget: expected 201, got %d", code)
	}

	// Second widget at same position — should fail (overlap)
	code = addWidget(t, srv, token, dashID, map[string]int{"row": 0, "col": 0, "width": 6, "height": 8})
	if code != http.StatusBadRequest {
		t.Fatalf("overlapping widget: expected 400, got %d", code)
	}
}

func TestWidgetLayoutValidation_Valid(t *testing.T) {
	os.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("wval-valid-%d@example.com", ts), "WVal Valid Org")

	body, _ := json.Marshal(map[string]interface{}{"title": "Validation Dashboard"})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create dashboard: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var dashResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	// First widget — left column
	code := addWidget(t, srv, token, dashID, map[string]int{"row": 0, "col": 0, "width": 6, "height": 8})
	if code != http.StatusCreated {
		t.Fatalf("first widget: expected 201, got %d", code)
	}

	// Second widget — right column, no overlap
	code = addWidget(t, srv, token, dashID, map[string]int{"row": 0, "col": 6, "width": 6, "height": 8})
	if code != http.StatusCreated {
		t.Fatalf("non-overlapping widget: expected 201, got %d", code)
	}
}
