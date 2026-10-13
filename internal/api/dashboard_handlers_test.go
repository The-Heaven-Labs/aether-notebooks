package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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

func TestDashboardAppSettings(t *testing.T) {
	// Registration is rate-limited per IP in Redis, shared across test runs;
	// raise the limit like the other dashboard tests do.
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-app-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash App Org")

	// Invalid app settings on create → 400 (the invariant must hold on every write path).
	badBody, _ := json.Marshal(map[string]interface{}{
		"title":    "Bad App Settings",
		"settings": map[string]interface{}{"app": map[string]interface{}{"width": "wide"}},
	})
	badReq := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(badBody))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("Authorization", "Bearer "+token)
	badReq.Header.Set("X-AETHER-Admin-Mode", "true")
	badRec := httptest.NewRecorder()
	srv.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid create: expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	body, _ := json.Marshal(map[string]interface{}{"title": "App Settings"})
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

	put := func(settings map[string]interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]interface{}{"settings": settings})
		r := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}

	// Valid app settings round-trip.
	rec = put(map[string]interface{}{
		"app": map[string]interface{}{
			"title":             "Q3 Revenue Review",
			"subtitle":          "Sales ops · updated hourly",
			"width":             "full",
			"show_header":       true,
			"show_refresh":      false,
			"show_last_updated": true,
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	type appSettings struct {
		Title       string `json:"title"`
		Subtitle    string `json:"subtitle"`
		Width       string `json:"width"`
		ShowRefresh *bool  `json:"show_refresh"`
		FilterScope string `json:"filter_scope"`
	}
	getApp := func() appSettings {
		r := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Settings struct {
				App appSettings `json:"app"`
			} `json:"settings"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Settings.App
	}

	got := getApp()
	if got.Title != "Q3 Revenue Review" || got.Width != "full" {
		t.Fatalf("app settings did not round-trip: %+v", got)
	}
	if got.ShowRefresh == nil || *got.ShowRefresh {
		t.Fatalf("expected show_refresh=false to survive, got %+v", got.ShowRefresh)
	}

	// Invalid width → 400, and the rejected update must leave stored settings untouched.
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"width": "wide"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid width: expected 400, got %d", rec.Code)
	}
	if got := getApp(); got.Title != "Q3 Revenue Review" || got.Width != "full" {
		t.Fatalf("rejected update changed stored settings: %+v", got)
	}

	// Invalid layout → 400.
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"layout": "nonsense"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid layout: expected 400, got %d", rec.Code)
	}

	// Subtitle boundary: 200 characters pass, 201 are rejected.
	subtitle := strings.Repeat("s", 200)
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"subtitle": subtitle}}); rec.Code != http.StatusOK {
		t.Fatalf("200-char subtitle: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := getApp(); got.Subtitle != subtitle {
		t.Fatalf("200-char subtitle did not round-trip, got %d chars", len(got.Subtitle))
	}
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"subtitle": subtitle + "s"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("201-char subtitle: expected 400, got %d", rec.Code)
	}

	// Overlong title → 400.
	long := strings.Repeat("x", 200)
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"title": long}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("overlong title: expected 400, got %d", rec.Code)
	}

	// filter_scope: the two enum values round-trip; anything else is rejected
	// and leaves the stored value untouched.
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"filter_scope": "tab"}}); rec.Code != http.StatusOK {
		t.Fatalf("filter_scope=tab: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := getApp(); got.FilterScope != "tab" {
		t.Fatalf("filter_scope=tab did not round-trip: %+v", got)
	}
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"filter_scope": "all"}}); rec.Code != http.StatusOK {
		t.Fatalf("filter_scope=all: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := getApp(); got.FilterScope != "all" {
		t.Fatalf("filter_scope=all did not round-trip: %+v", got)
	}
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"filter_scope": "everywhere"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("filter_scope=everywhere: expected 400, got %d", rec.Code)
	}
	if got := getApp(); got.FilterScope != "all" {
		t.Fatalf("rejected filter_scope changed stored settings: %+v", got)
	}

	// Absent filter_scope is omitted from the serialized settings.
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"title": "No Scope"}}); rec.Code != http.StatusOK {
		t.Fatalf("app without filter_scope: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rawReq := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
	rawReq.Header.Set("Authorization", "Bearer "+token)
	rawReq.Header.Set("X-AETHER-Admin-Mode", "true")
	rawRec := httptest.NewRecorder()
	srv.ServeHTTP(rawRec, rawReq)
	var raw struct {
		Settings struct {
			App map[string]interface{} `json:"app"`
		} `json:"settings"`
	}
	json.NewDecoder(rawRec.Body).Decode(&raw)
	if _, ok := raw.Settings.App["filter_scope"]; ok {
		t.Fatalf("absent filter_scope should be omitted, got %v", raw.Settings.App["filter_scope"])
	}
}

func TestDashboardTabsAndTheme(t *testing.T) {
	// Registration is rate-limited per IP in Redis, shared across test runs;
	// raise the limit like the other dashboard tests do.
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-tabs-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash Tabs Org")

	body, _ := json.Marshal(map[string]interface{}{"title": "Tabs Dashboard"})
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

	put := func(settings map[string]interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]interface{}{"settings": settings})
		r := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}

	type tab struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	type theme struct {
		Accent       string   `json:"accent"`
		Background   string   `json:"background"`
		Surface      string   `json:"surface"`
		Text         string   `json:"text"`
		Border       string   `json:"border"`
		Radius       *int     `json:"radius"`
		FontFamily   string   `json:"font_family"`
		ChartPalette []string `json:"chart_palette"`
		ColorScheme  string   `json:"color_scheme"`
	}
	getSettings := func() ([]tab, theme) {
		r := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Settings struct {
				Tabs []tab `json:"tabs"`
				App  struct {
					Theme theme `json:"theme"`
				} `json:"app"`
			} `json:"settings"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Settings.Tabs, resp.Settings.App.Theme
	}
	getWidgetLayoutTab := func(widgetID string) (string, bool) {
		r := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Widgets []struct {
				ID     string `json:"id"`
				Layout struct {
					Tab string `json:"tab"`
				} `json:"layout"`
			} `json:"widgets"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		for _, widget := range resp.Widgets {
			if widget.ID == widgetID {
				return widget.Layout.Tab, true
			}
		}
		return "", false
	}

	// Valid tabs + theme round-trip.
	rec = put(map[string]interface{}{
		"tabs": []map[string]interface{}{
			{"id": "overview", "title": "Overview"},
			{"id": "deep-dive", "title": "Deep dive"},
		},
		"app": map[string]interface{}{
			"theme": map[string]interface{}{
				"accent": "#7c5cff", "background": "#ffffff", "surface": "#fafafa",
				"text": "#111111", "border": "#e8e8e8", "radius": 6,
				"font_family": "jetbrains-mono", "color_scheme": "light",
				"chart_palette": []string{"#7c5cff", "#22c55e", "#f59e0b"},
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tabs, gotTheme := getSettings()
	if len(tabs) != 2 || tabs[0].ID != "overview" || tabs[0].Title != "Overview" ||
		tabs[1].ID != "deep-dive" || tabs[1].Title != "Deep dive" {
		t.Fatalf("tabs did not round-trip in order: %+v", tabs)
	}
	if gotTheme.Accent != "#7c5cff" || gotTheme.Background != "#ffffff" || gotTheme.Surface != "#fafafa" ||
		gotTheme.Text != "#111111" || gotTheme.Border != "#e8e8e8" ||
		gotTheme.FontFamily != "jetbrains-mono" || gotTheme.ColorScheme != "light" {
		t.Fatalf("theme did not round-trip: %+v", gotTheme)
	}
	if gotTheme.Radius == nil || *gotTheme.Radius != 6 {
		t.Fatalf("theme radius did not round-trip: %+v", gotTheme.Radius)
	}
	if len(gotTheme.ChartPalette) != 3 || gotTheme.ChartPalette[0] != "#7c5cff" ||
		gotTheme.ChartPalette[1] != "#22c55e" || gotTheme.ChartPalette[2] != "#f59e0b" {
		t.Fatalf("chart palette did not round-trip: %+v", gotTheme.ChartPalette)
	}

	// Short uppercase hex is accepted.
	if rec = put(map[string]interface{}{
		"app": map[string]interface{}{"theme": map[string]interface{}{"accent": "#ABC"}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("short uppercase hex: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, gotTheme := getSettings(); gotTheme.Accent != "#ABC" {
		t.Fatalf("short uppercase hex did not round-trip: %q", gotTheme.Accent)
	}

	// Radius boundaries: 0 is a present zero, 24 is the maximum, 25 is rejected.
	if rec = put(map[string]interface{}{
		"app": map[string]interface{}{"theme": map[string]interface{}{"radius": 0}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("radius 0: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, gotTheme := getSettings(); gotTheme.Radius == nil || *gotTheme.Radius != 0 {
		t.Fatalf("radius 0 did not round-trip as present: %+v", gotTheme.Radius)
	}
	if rec = put(map[string]interface{}{
		"app": map[string]interface{}{"theme": map[string]interface{}{"radius": 24}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("radius 24: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec = put(map[string]interface{}{
		"app": map[string]interface{}{"theme": map[string]interface{}{"radius": 25}},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("radius 25: expected 400, got %d", rec.Code)
	}

	tooManyTabs := make([]map[string]interface{}, 0, 11)
	for i := 0; i < 11; i++ {
		tooManyTabs = append(tooManyTabs, map[string]interface{}{
			"id": fmt.Sprintf("tab-%d", i), "title": fmt.Sprintf("Tab %d", i),
		})
	}

	// Invalid values → 400, and the rejected updates must leave stored settings untouched.
	invalid := []map[string]interface{}{
		{"app": map[string]interface{}{"theme": map[string]interface{}{"accent": "red"}}},
		{"app": map[string]interface{}{"theme": map[string]interface{}{"radius": 40}}},
		{"app": map[string]interface{}{"theme": map[string]interface{}{"font_family": "comic-sans"}}},
		{"app": map[string]interface{}{"theme": map[string]interface{}{"color_scheme": "sepia"}}},
		{"app": map[string]interface{}{"theme": map[string]interface{}{"chart_palette": []string{"#7c5cff", "nope"}}}},
		{"tabs": "nope"},
		{"tabs": []map[string]interface{}{{"id": "", "title": "Empty id"}}},
		{"tabs": []map[string]interface{}{{"id": "Overview", "title": "Bad charset"}}},
		{"tabs": []map[string]interface{}{{"id": "overview", "title": "Dup"}, {"id": "overview", "title": "Dup again"}}},
		{"tabs": tooManyTabs},
	}
	for i, settings := range invalid {
		if rec = put(settings); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid case %d: expected 400, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	if tabs, _ := getSettings(); len(tabs) != 2 {
		t.Fatalf("rejected updates changed stored tabs: %+v", tabs)
	}

	// Clearing: an empty tabs list and an explicit null theme both persist.
	if rec = put(map[string]interface{}{"tabs": []map[string]interface{}{}}); rec.Code != http.StatusOK {
		t.Fatalf("clear tabs: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if tabs, _ := getSettings(); len(tabs) != 0 {
		t.Fatalf("tabs were not cleared: %+v", tabs)
	}
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"theme": nil}}); rec.Code != http.StatusOK {
		t.Fatalf("clear theme: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, gotTheme := getSettings(); gotTheme.Accent != "" || gotTheme.Radius != nil || gotTheme.ChartPalette != nil {
		t.Fatalf("theme was not cleared: %+v", gotTheme)
	}

	// Create-path tabs validation: 11 tabs are rejected there too.
	badCreateBody, _ := json.Marshal(map[string]interface{}{
		"title":    "Too Many Tabs",
		"settings": map[string]interface{}{"tabs": tooManyTabs},
	})
	badCreateReq := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(badCreateBody))
	badCreateReq.Header.Set("Content-Type", "application/json")
	badCreateReq.Header.Set("Authorization", "Bearer "+token)
	badCreateReq.Header.Set("X-AETHER-Admin-Mode", "true")
	badCreateRec := httptest.NewRecorder()
	srv.ServeHTTP(badCreateRec, badCreateReq)
	if badCreateRec.Code != http.StatusBadRequest {
		t.Fatalf("create with 11 tabs: expected 400, got %d: %s", badCreateRec.Code, badCreateRec.Body.String())
	}

	// Widget tab validation: a bad id charset is rejected, a valid one is
	// accepted and persists through the document into the widget's layout.
	addWidgetWithTab := func(tab string) (int, string) {
		b, _ := json.Marshal(map[string]interface{}{
			"type":   "chart",
			"layout": map[string]interface{}{"row": 0, "col": 0, "width": 6, "height": 8, "tab": tab},
		})
		r := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets", bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		var resp struct {
			ID string `json:"id"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return w.Code, resp.ID
	}
	if code, _ := addWidgetWithTab("OVERVIEW"); code != http.StatusBadRequest {
		t.Fatalf("widget with bad tab id: expected 400, got %d", code)
	}
	code, widgetID := addWidgetWithTab("overview")
	if code != http.StatusCreated {
		t.Fatalf("widget with valid tab id: expected 201, got %d", code)
	}
	if tab, ok := getWidgetLayoutTab(widgetID); !ok || tab != "overview" {
		t.Fatalf("widget layout.tab did not persist: ok=%v tab=%q", ok, tab)
	}

	// Widget update path: a bad tab id is rejected there too.
	updateBody, _ := json.Marshal(map[string]interface{}{
		"layout": map[string]interface{}{"row": 0, "col": 0, "width": 6, "height": 8, "tab": "BAD"},
	})
	updateReq := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("X-AETHER-Admin-Mode", "true")
	updateRec := httptest.NewRecorder()
	srv.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("widget update with bad tab id: expected 400, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
}

// doDashboardJSON issues an authenticated, admin-mode JSON request against a
// dashboard test server. Shared by the html widget and org-switch tests.
func doDashboardJSON(t *testing.T, srv *api.Server, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-AETHER-Admin-Mode", "true")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestDashboardHTMLWidget(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-html-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash HTML Org")

	rec := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "HTML Widgets"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID := dashResp["id"].(string)

	// Source-less html widget round-trips with its config.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type":   "html",
		"layout": map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
		"config": map[string]any{"html": `<div class="stat">42</div>`, "css": `.stat{font-size:28px}`},
	})
	require.Equal(t, 201, rec.Code)

	// GET returns type html + config preserved.
	type fetchedWidget struct {
		ID     string         `json:"id"`
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	getWidgets := func() []fetchedWidget {
		getRec := doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
		require.Equal(t, 200, getRec.Code)
		var resp struct {
			Widgets []fetchedWidget `json:"widgets"`
		}
		require.NoError(t, json.NewDecoder(getRec.Body).Decode(&resp))
		return resp.Widgets
	}
	widgets := getWidgets()
	require.Len(t, widgets, 1)
	require.Equal(t, "html", widgets[0].Type)
	require.Equal(t, `<div class="stat">42</div>`, widgets[0].Config["html"])
	require.Equal(t, `.stat{font-size:28px}`, widgets[0].Config["css"])

	// PUT type conversion to html on a text widget succeeds (204, like every
	// widget update) and the projected type flips.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type":   "text",
		"layout": map[string]int{"row": 6, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"text": map[string]any{"markdown": "hello"}},
	})
	require.Equal(t, 201, rec.Code)
	var textWidget map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&textWidget))
	textWidgetID := textWidget["id"].(string)
	rec = doDashboardJSON(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+textWidgetID, map[string]any{"type": "html"})
	require.Equal(t, 204, rec.Code)
	var converted *fetchedWidget
	convertedWidgets := getWidgets()
	for i := range convertedWidgets {
		if convertedWidgets[i].ID == textWidgetID {
			converted = &convertedWidgets[i]
		}
	}
	require.NotNil(t, converted, "converted widget must still exist")
	require.Equal(t, "html", converted.Type)

	// Oversize markup → 400 naming the cap.
	big := strings.Repeat("x", 256<<10+1)
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "html", "layout": map[string]int{"row": 10, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"html": big},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "256 KiB")

	// Oversize css → 400 too (the update path validates the same caps).
	rec = doDashboardJSON(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+textWidgetID, map[string]any{
		"config": map[string]any{"css": strings.Repeat("x", 64<<10+1)},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "64 KiB")

	// Genuinely unknown types are still rejected.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "sankey_widget", "layout": map[string]int{"row": 10, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 400, rec.Code)
}

func TestOrgHTMLPanelsSwitch(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-org-switch-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "HTML Switch Org")

	rec := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Switch"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID := dashResp["id"].(string)

	// A text widget for the update-path enforcement checks (created while the
	// switch is still absent/enabled).
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type":   "text",
		"layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"text": map[string]any{"markdown": "hello"}},
	})
	require.Equal(t, 201, rec.Code)
	var textWidget map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&textWidget))
	textWidgetID := textWidget["id"].(string)
	widgetPath := "/api/v1/dashboards/" + dashID + "/widgets/" + textWidgetID

	// Default: absent means enabled — dashboard GET reports true.
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":true`)

	// Disable org-wide; the setting round-trips and the dashboard GET flips.
	rec = doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings", map[string]any{"html_panels_enabled": false})
	require.Equal(t, 200, rec.Code)
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/org/settings", nil)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":false`)
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":false`)

	// Creating an html widget is rejected while disabled…
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "html", "layout": map[string]int{"row": 5, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")

	// …as is converting a widget to html and writing panel config keys.
	rec = doDashboardJSON(t, srv, token, "PUT", widgetPath, map[string]any{"type": "html"})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")
	rec = doDashboardJSON(t, srv, token, "PUT", widgetPath, map[string]any{"config": map[string]any{"html": "<p>x</p>"}})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")

	// The public dashboard GET carries the resolved switch too (field present
	// and false while disabled).
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/share", nil)
	require.Contains(t, []int{200, 201}, rec.Code)
	var shareResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&shareResp))
	publicToken := shareResp["token"].(string)
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/public/"+publicToken, nil)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":false`)

	// …and allowed again after re-enabling (explicit true round-trips too).
	rec = doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings", map[string]any{"html_panels_enabled": true})
	require.Equal(t, 200, rec.Code)
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/org/settings", nil)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":true`)
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "html", "layout": map[string]int{"row": 5, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 201, rec.Code)

	// The re-enabled update paths work again.
	rec = doDashboardJSON(t, srv, token, "PUT", widgetPath, map[string]any{"type": "html"})
	require.Equal(t, 204, rec.Code)
	rec = doDashboardJSON(t, srv, token, "PUT", widgetPath, map[string]any{"config": map[string]any{"html": "<p>x</p>"}})
	require.Equal(t, 204, rec.Code)
}

func TestDashboardCustomWidget(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-custom-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Custom Org")
	// Enable custom code for the org (default is disabled).
	require.Equal(t, 200, doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings",
		map[string]any{"custom_code_enabled": true}).Code)

	rec := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Custom Widgets"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID := dashResp["id"].(string)

	// The custom widget binds another widget of the same dashboard: seed a
	// query widget first (a raw connector row is enough — the binding ref
	// check only needs existence on the dashboard).
	connID := uuid.NewString()
	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		SELECT $1, org_id, 'binding target', 'postgres', '\x00' FROM dashboards WHERE id = $2`,
		connID, dashID)
	require.NoError(t, err)
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "table", "connector_id": connID, "query": "SELECT 1", "language": "sql",
		"layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 201, rec.Code)
	var target map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&target))
	targetID := target["id"].(string)

	// Custom widget round-trips with its bundle + bindings.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 4, "col": 0, "width": 6, "height": 6},
		"config": map[string]any{
			"html": `<div id="root"></div>`, "css": `#root{color:red}`, "js": `document.getElementById('root').textContent='hi'`,
			"bindings": []map[string]any{{"key": "sales", "widget_id": targetID}},
		},
	})
	require.Equal(t, 201, rec.Code)
	var custom map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&custom))
	require.Equal(t, "custom", custom["type"])

	// A binding to a widget of another dashboard (or none) → 400.
	other := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Elsewhere"})
	var otherResp map[string]any
	require.NoError(t, json.NewDecoder(other.Body).Decode(&otherResp))
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 12, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"bindings": []map[string]any{{"key": "x", "widget_id": uuid.NewString()}}},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "binding")

	// Oversize js → 400 naming the cap; unknown types still rejected.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 16, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"js": strings.Repeat("x", 128<<10+1)},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "128 KiB")
}

func TestOrgCustomCodeSwitch(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-custom-switch-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Custom Switch Org")

	rec := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Switch"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID := dashResp["id"].(string)

	// Default: absent means DISABLED — unlike html panels.
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"custom_code_enabled":false`)
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")

	// Enabling round-trips and flips the GET + public GET.
	require.Equal(t, 200, doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings",
		map[string]any{"custom_code_enabled": true}).Code)
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"custom_code_enabled":true`)
	share := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/share", nil)
	var shareResp map[string]any
	require.NoError(t, json.NewDecoder(share.Body).Decode(&shareResp))
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/public/"+shareResp["token"].(string), nil)
	require.Contains(t, rec.Body.String(), `"custom_code_enabled":true`)

	// Creating works now; writes to custom config keys are rejected again
	// once disabled (fail closed) while the widget keeps its config.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 201, rec.Code)
	var customWidget map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&customWidget))
	customWidgetID := customWidget["id"].(string)
	require.Equal(t, 200, doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings",
		map[string]any{"custom_code_enabled": false}).Code)

	widgetPath := "/api/v1/dashboards/" + dashID + "/widgets/" + customWidgetID
	// Conversion and authoring both fail closed while disabled.
	rec = doDashboardJSON(t, srv, token, "PUT", widgetPath, map[string]any{"type": "custom"})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")
	rec = doDashboardJSON(t, srv, token, "PUT", widgetPath, map[string]any{"config": map[string]any{"js": "x"}})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")
}
