package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModelConfigCustomHeaders(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mc-headers-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MC Headers Org")

	var cfgID string
	t.Run("create with custom headers", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "Headers Model",
			"provider": "openai",
			"base_url": "https://api.example.com/v1",
			"model":    "gpt-4o",
			"api_key":  "sk-test",
			"custom_headers": map[string]string{
				"X-Title":   "Aether",
				"X-Api-Key": "supersecret",
			},
			"custom_header_secrets": []string{"X-Api-Key"},
		})
		req := httptest.NewRequest("POST", "/api/v1/model-configs", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		cfgID = resp["id"]
	})

	t.Run("secret is ciphertext at rest", func(t *testing.T) {
		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&raw); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		if strings.Contains(string(raw), "supersecret") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if stored["X-Title"] != "Aether" {
			t.Fatalf("expected plaintext X-Title, got %q", stored["X-Title"])
		}
		if !strings.HasPrefix(stored["X-Api-Key"], "enc:v1:") {
			t.Fatalf("expected enc:v1: ciphertext, got %q", stored["X-Api-Key"])
		}
	})

	t.Run("get redacts the secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/model-configs/"+cfgID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var cfg map[string]any
		json.NewDecoder(rec.Body).Decode(&cfg)
		hdrs, _ := cfg["custom_headers"].(map[string]any)
		if hdrs["X-Title"] != "Aether" {
			t.Fatalf("expected visible X-Title, got %v", hdrs["X-Title"])
		}
		if hdrs["X-Api-Key"] != "__AETHER_REDACTED__" {
			t.Fatalf("expected redacted secret, got %v", hdrs["X-Api-Key"])
		}
	})

	t.Run("update with sentinel keeps the stored secret", func(t *testing.T) {
		var before []byte
		srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&before)

		body, _ := json.Marshal(map[string]any{
			"custom_headers": map[string]string{
				"X-Title":   "Aether 2",
				"X-Api-Key": "__AETHER_REDACTED__",
			},
			"custom_header_secrets": []string{"X-Api-Key"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var after []byte
		srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&after)
		var beforeMap, afterMap map[string]string
		json.Unmarshal(before, &beforeMap)
		json.Unmarshal(after, &afterMap)
		if afterMap["X-Api-Key"] != beforeMap["X-Api-Key"] {
			t.Fatal("sentinel update must keep the stored ciphertext unchanged")
		}
		if afterMap["X-Title"] != "Aether 2" {
			t.Fatalf("expected updated X-Title, got %q", afterMap["X-Title"])
		}
	})

	t.Run("update with a new value re-encrypts", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"custom_headers": map[string]string{
				"X-Api-Key": "brand-new-secret",
			},
			"custom_header_secrets": []string{"X-Api-Key"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var raw []byte
		srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&raw)
		if strings.Contains(string(raw), "brand-new-secret") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if !strings.HasPrefix(stored["X-Api-Key"], "enc:v1:") {
			t.Fatalf("expected re-encrypted ciphertext, got %q", stored["X-Api-Key"])
		}
	})

	t.Run("rejects reserved header", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name": "Bad", "provider": "openai", "base_url": "https://x.example.com",
			"model": "gpt-4o", "api_key": "sk",
			"custom_headers": map[string]string{"Content-Type": "text/plain"},
		})
		req := httptest.NewRequest("POST", "/api/v1/model-configs", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for reserved header, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects invalid header name and unknown secret", func(t *testing.T) {
		for _, payload := range []map[string]any{
			{"custom_headers": map[string]string{"Bad Header": "x"}},
			{"custom_headers": map[string]string{"X-Title": "x"}, "custom_header_secrets": []string{"X-Missing"}},
		} {
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %v, got %d: %s", payload, rec.Code, rec.Body.String())
			}
		}
	})
}
