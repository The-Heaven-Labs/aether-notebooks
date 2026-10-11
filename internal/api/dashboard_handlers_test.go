package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	// Widget tab validation: a bad id charset is rejected, a valid one is accepted.
	addWidgetWithTab := func(tab string) int {
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
		return w.Code
	}
	if code := addWidgetWithTab("OVERVIEW"); code != http.StatusBadRequest {
		t.Fatalf("widget with bad tab id: expected 400, got %d", code)
	}
	if code := addWidgetWithTab("overview"); code != http.StatusCreated {
		t.Fatalf("widget with valid tab id: expected 201, got %d", code)
	}
}
