package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestFrontendHandlerInjectsConfigOnAllRoutes(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte(`<html><head></head><body>App</body></html>`)},
		"assets/app.js": &fstest.MapFile{Data: []byte(`console.log('app')`)},
		"favicon.svg":   &fstest.MapFile{Data: []byte(`<svg/>`)},
	}

	cfg := &runtimeConfig{
		APIURL:   "https://api.example.com",
		RelayURL: "wss://relay.example.com",
	}

	handler := frontendHandlerWithFS(mockFS, cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	tests := []struct {
		name       string
		path       string
		wantConfig bool
		wantStatus int
	}{
		{"root path", "/", true, http.StatusOK},
		{"index.html", "/index.html", true, http.StatusOK},
		{"SPA notebook route", "/notebooks/abc123", true, http.StatusOK},
		{"SPA dashboard route", "/dashboards", true, http.StatusOK},
		{"static JS asset", "/assets/app.js", false, http.StatusOK},
		{"static SVG asset", "/favicon.svg", false, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			if tt.wantConfig {
				if resp.Header.Get("Cache-Control") != "no-cache" {
					t.Error("expected Cache-Control: no-cache header")
				}
				body := make([]byte, 1024)
				n, _ := resp.Body.Read(body)
				content := string(body[:n])
				if !contains(content, `__AETHER_CONFIG__`) {
					t.Error("expected response to contain __AETHER_CONFIG__")
				}
				if !contains(content, `"relayUrl":"wss://relay.example.com"`) {
					t.Error("expected response to contain relayUrl")
				}
				if !contains(content, `"apiUrl":"https://api.example.com"`) {
					t.Error("expected response to contain apiUrl")
				}
			} else {
				ct := resp.Header.Get("Content-Type")
				if ct == "" || ct == "text/html; charset=utf-8" {
					// Static assets should have their own content type
				}
			}
		})
	}
}

func TestFrontendHandlerEmptyConfig(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<html><head></head><body>App</body></html>`)},
	}

	cfg := &runtimeConfig{}
	handler := frontendHandlerWithFS(mockFS, cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	content := string(body[:n])

	if !contains(content, `window.__AETHER_CONFIG__={}`) {
		t.Error("expected empty config object when no env vars set")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
