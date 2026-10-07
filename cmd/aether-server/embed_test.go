package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/andybalholm/brotli"
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

// compressiblePayload is a >1KB body that both gzip and brotli will shrink.
func compressiblePayload() []byte {
	return bytes.Repeat([]byte("console.log('aether frontend asset payload');\n"), 200)
}

func testHandlerFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte(`<html><head></head><body>App</body></html>`)},
		"assets/app.js": &fstest.MapFile{Data: compressiblePayload()},
		"favicon.svg":   &fstest.MapFile{Data: []byte(`<svg>tiny</svg>`)},
	}
}

func TestFrontendHandlerCompression(t *testing.T) {
	handler := frontendHandlerWithFS(testHandlerFS(), &runtimeConfig{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	payload := compressiblePayload()

	get := func(t *testing.T, acceptEncoding string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/assets/app.js", nil)
		if err != nil {
			t.Fatal(err)
		}
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		// Disable the transport's automatic decompression so the raw
		// Content-Encoding/body can be inspected.
		tr := &http.Transport{DisableCompression: true}
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("identity when no accept-encoding", func(t *testing.T) {
		resp := get(t, "")
		defer resp.Body.Close()
		if enc := resp.Header.Get("Content-Encoding"); enc != "" {
			t.Errorf("expected no Content-Encoding, got %q", enc)
		}
		body, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(body, payload) {
			t.Error("expected raw payload")
		}
	})

	t.Run("brotli preferred", func(t *testing.T) {
		resp := get(t, "gzip, deflate, br")
		defer resp.Body.Close()
		if enc := resp.Header.Get("Content-Encoding"); enc != "br" {
			t.Fatalf("expected Content-Encoding br, got %q", enc)
		}
		if v := resp.Header.Get("Vary"); v != "Accept-Encoding" {
			t.Errorf("expected Vary: Accept-Encoding, got %q", v)
		}
		body, _ := io.ReadAll(brotli.NewReader(resp.Body))
		if !bytes.Equal(body, payload) {
			t.Error("expected brotli-decoded payload to match")
		}
		if got, want := resp.ContentLength, int64(len(body)); got == want {
			// Sanity: the wire length should be the compressed length, not
			// the decoded length.
			t.Errorf("Content-Length %d should be compressed, decoded is %d", got, want)
		}
	})

	t.Run("gzip when brotli not accepted", func(t *testing.T) {
		resp := get(t, "gzip, deflate")
		defer resp.Body.Close()
		if enc := resp.Header.Get("Content-Encoding"); enc != "gzip" {
			t.Fatalf("expected Content-Encoding gzip, got %q", enc)
		}
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(zr)
		if !bytes.Equal(body, payload) {
			t.Error("expected gzip-decoded payload to match")
		}
	})

	t.Run("identity when encoding refused", func(t *testing.T) {
		resp := get(t, "br;q=0, gzip;q=0")
		defer resp.Body.Close()
		if enc := resp.Header.Get("Content-Encoding"); enc != "" {
			t.Errorf("expected no Content-Encoding, got %q", enc)
		}
		body, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(body, payload) {
			t.Error("expected raw payload")
		}
	})
}

func TestFrontendHandlerSmallFilesAreNotCompressed(t *testing.T) {
	handler := frontendHandlerWithFS(testHandlerFS(), &runtimeConfig{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/favicon.svg", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	resp, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("small file should not be compressed, got Content-Encoding %q", enc)
	}
}

func TestFrontendHandlerCacheHeaders(t *testing.T) {
	handler := frontendHandlerWithFS(testHandlerFS(), &runtimeConfig{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	tests := []struct {
		path string
		want string
	}{
		{"/assets/app.js", "public, max-age=31536000, immutable"},
		{"/favicon.svg", "no-cache"},
		{"/", "no-cache"},
		{"/notebooks/abc", "no-cache"},
	}
	for _, tt := range tests {
		resp, err := http.Get(srv.URL + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Cache-Control"); got != tt.want {
			t.Errorf("%s: expected Cache-Control %q, got %q", tt.path, tt.want, got)
		}
	}
}

func TestFrontendHandlerETagAndNotModified(t *testing.T) {
	handler := frontendHandlerWithFS(testHandlerFS(), &runtimeConfig{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	for _, path := range []string{"/assets/app.js", "/"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Fatalf("%s: expected an ETag", path)
		}

		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("If-None-Match", etag)
		resp2, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusNotModified {
			t.Errorf("%s: expected 304 with matching If-None-Match, got %d", path, resp2.StatusCode)
		}
	}
}

func TestFrontendHandlerRangeServedFromIdentity(t *testing.T) {
	handler := frontendHandlerWithFS(testHandlerFS(), &runtimeConfig{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/assets/app.js", nil)
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("Accept-Encoding", "gzip, br")
	resp, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("range responses must be identity, got %q", enc)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 10 {
		t.Errorf("expected 10 bytes, got %d", len(body))
	}
}

func TestEncodingQuality(t *testing.T) {
	tests := []struct {
		header string
		enc    string
		want   float64
	}{
		{"gzip, deflate, br", "br", 1},
		{"gzip;q=0.8, br;q=0.5", "br", 0.5},
		{"gzip;q=0.8, br;q=0.5", "gzip", 0.8},
		{"br;q=0", "br", 0},
		{"", "gzip", 0},
		{"deflate", "gzip", 0},
	}
	for _, tt := range tests {
		if got := encodingQuality(tt.header, tt.enc); got != tt.want {
			t.Errorf("encodingQuality(%q, %q) = %v, want %v", tt.header, tt.enc, got, tt.want)
		}
	}
}

func TestETagMatches(t *testing.T) {
	etag := `"abc123"`
	if !etagMatches(`"abc123"`, etag) {
		t.Error("exact match should succeed")
	}
	if !etagMatches(`W/"abc123"`, etag) {
		t.Error("weak prefix should match")
	}
	if !etagMatches(`"other", "abc123"`, etag) {
		t.Error("list match should succeed")
	}
	if !etagMatches(`*`, etag) {
		t.Error("wildcard should match")
	}
	if etagMatches(`"other"`, etag) {
		t.Error("non-matching etag should not match")
	}
	if etagMatches("", etag) {
		t.Error("empty header should not match")
	}
}

func TestCompressionHelpersProduceSmallerOutput(t *testing.T) {
	payload := compressiblePayload()
	for name, out := range map[string][]byte{
		"gzip":   gzipBytes(payload),
		"brotli": brotliBytes(payload),
	} {
		if len(out) == 0 {
			t.Fatalf("%s: expected output", name)
		}
		if len(out) >= len(payload) {
			t.Errorf("%s: expected smaller output, got %d vs %d", name, len(out), len(payload))
		}
	}
}
