package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/crypto"
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

	t.Run("stored secret decrypts to the original value", func(t *testing.T) {
		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&raw); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		plaintext, err := crypto.DecryptString(stored["X-Api-Key"], crypto.DeriveKey("test-master-key-for-tests-only!"))
		if err != nil {
			t.Fatalf("decrypt stored secret: %v", err)
		}
		if plaintext != "supersecret" {
			t.Fatalf("expected original secret, got %q", plaintext)
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

	t.Run("list redacts the secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/model-configs", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		bodyText := rec.Body.String()
		if strings.Contains(bodyText, "enc:v1:") {
			t.Fatalf("ciphertext leaked in list response: %s", bodyText)
		}
		var cfgs []map[string]any
		json.Unmarshal([]byte(bodyText), &cfgs)
		found := false
		for _, cfg := range cfgs {
			if cfg["id"] != cfgID {
				continue
			}
			found = true
			hdrs, _ := cfg["custom_headers"].(map[string]any)
			if hdrs["X-Title"] != "Aether" {
				t.Fatalf("expected visible X-Title in list, got %v", hdrs["X-Title"])
			}
			if hdrs["X-Api-Key"] != "__AETHER_REDACTED__" {
				t.Fatalf("expected redacted secret in list, got %v", hdrs["X-Api-Key"])
			}
		}
		if !found {
			t.Fatalf("model config %s not found in list", cfgID)
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

	t.Run("update without custom_headers keeps stored headers", func(t *testing.T) {
		var before []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&before); err != nil {
			t.Fatalf("read headers before: %v", err)
		}

		body, _ := json.Marshal(map[string]any{"name": "Renamed"})
		req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var after []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&after); err != nil {
			t.Fatalf("read headers after: %v", err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("omitting custom_headers must preserve stored headers: before %s, after %s", before, after)
		}
	})

	t.Run("sentinel without a declared secret deletes the header", func(t *testing.T) {
		// Fail-safe: the sentinel cannot be restored when its name is not in
		// custom_header_secrets, so the entry is dropped rather than persisted
		// literally (and never encrypted as the sentinel string).
		seed, _ := json.Marshal(map[string]any{
			"custom_headers":        map[string]string{"X-Pin-Key": "pin-me"},
			"custom_header_secrets": []string{"X-Pin-Key"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(seed)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("seed update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		body, _ := json.Marshal(map[string]any{
			"custom_headers": map[string]string{"X-Pin-Key": "__AETHER_REDACTED__"},
		})
		req = httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&raw); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if _, exists := stored["X-Pin-Key"]; exists {
			t.Fatalf("expected orphaned sentinel dropped, got %q", stored["X-Pin-Key"])
		}
	})

	t.Run("clearing headers with an empty map", func(t *testing.T) {
		seed, _ := json.Marshal(map[string]any{
			"custom_headers": map[string]string{"X-Repopulate": "v"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(seed)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("seed update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		body, _ := json.Marshal(map[string]any{
			"custom_headers":        map[string]string{},
			"custom_header_secrets": []string{},
		})
		req = httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("clear update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT custom_headers FROM model_configs WHERE id = $1`, cfgID).Scan(&raw); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if len(stored) != 0 {
			t.Fatalf("expected empty stored headers, got %v", stored)
		}

		req = httptest.NewRequest("GET", "/api/v1/model-configs/"+cfgID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var cfg map[string]any
		json.NewDecoder(rec.Body).Decode(&cfg)
		if hdrs, ok := cfg["custom_headers"].(map[string]any); ok && len(hdrs) != 0 {
			t.Fatalf("expected omitted or empty custom_headers, got %v", hdrs)
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

	t.Run("rejected headers leave other fields untouched", func(t *testing.T) {
		var before []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT api_key_encrypted FROM model_configs WHERE id = $1`, cfgID).Scan(&before); err != nil {
			t.Fatalf("read api key before: %v", err)
		}

		body, _ := json.Marshal(map[string]any{
			"api_key":        "sk-should-not-apply",
			"custom_headers": map[string]string{"Bad Header": "x"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/model-configs/"+cfgID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid headers, got %d: %s", rec.Code, rec.Body.String())
		}

		var after []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT api_key_encrypted FROM model_configs WHERE id = $1`, cfgID).Scan(&after); err != nil {
			t.Fatalf("read api key after: %v", err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("rejected update must not replace the API key")
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

func TestModelConfigTestEndpointSendsCustomHeaders(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mc-test-headers-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MC Test Headers Org")

	var gotHeaders http.Header
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-1", "model": "gpt-4o",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer llmSrv.Close()

	body, _ := json.Marshal(map[string]any{
		"name": "Test Headers", "provider": "openai", "base_url": llmSrv.URL,
		"model": "gpt-4o", "api_key": "sk-test",
		"custom_headers":        map[string]string{"X-Title": "Aether", "X-Api-Key": "supersecret"},
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
	var created map[string]string
	json.NewDecoder(rec.Body).Decode(&created)

	testReq := httptest.NewRequest("POST", "/api/v1/model-configs/"+created["id"]+"/test", nil)
	testReq.Header.Set("Authorization", "Bearer "+token)
	testRec := httptest.NewRecorder()
	srv.ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test endpoint: expected 200, got %d: %s", testRec.Code, testRec.Body.String())
	}
	if gotHeaders.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title on test request, got %q", gotHeaders.Get("X-Title"))
	}
	if gotHeaders.Get("X-Api-Key") != "supersecret" {
		t.Fatalf("expected decrypted X-Api-Key on test request, got %q", gotHeaders.Get("X-Api-Key"))
	}
}
