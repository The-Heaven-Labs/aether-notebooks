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

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/api"
)

// createWebhookTool POSTs a webhook tool through the API and returns its ID.
func createWebhookTool(t *testing.T, srv *api.Server, token string, config map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":   fmt.Sprintf("Secret Tool %d", time.Now().UnixNano()),
		"type":   "webhook",
		"config": config,
	})
	req := httptest.NewRequest("POST", "/api/v1/tools", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tool: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"]
}

// storedToolConfigRaw reads the raw config JSONB of a tool straight from the DB.
func storedToolConfigRaw(t *testing.T, srv *api.Server, toolID string) []byte {
	t.Helper()
	var raw []byte
	if err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT config FROM tools WHERE id = $1`, toolID).Scan(&raw); err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	return raw
}

func TestWebhookHeaderSecretsEncryptedAtRest(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("tool-headers-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Tool Headers Org")

	toolID := createWebhookTool(t, srv, token, map[string]any{
		"url":     "https://example.com/hook",
		"method":  "POST",
		"headers": map[string]string{"X-Api-Key": "supersecret", "X-Mode": "live"},
		"secrets": []string{"X-Api-Key"},
	})

	t.Run("stored config is ciphertext", func(t *testing.T) {
		raw := storedToolConfigRaw(t, srv, toolID)
		if strings.Contains(string(raw), "supersecret") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("decode stored config: %v (%s)", err, raw)
		}
		hdrs, _ := cfg["headers"].(map[string]any)
		if hdrs["X-Mode"] != "live" {
			t.Fatalf("expected plaintext X-Mode, got %v", hdrs["X-Mode"])
		}
		secret, _ := hdrs["X-Api-Key"].(string)
		if !strings.HasPrefix(secret, "enc:v1:") {
			t.Fatalf("expected enc:v1: ciphertext, got %q", secret)
		}
		secrets, _ := cfg["secrets"].([]any)
		if len(secrets) != 1 || secrets[0] != "X-Api-Key" {
			t.Fatalf("expected stored secrets [X-Api-Key], got %v", cfg["secrets"])
		}
	})

	t.Run("get redacts the secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/tools/"+toolID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get tool: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var tool map[string]any
		json.NewDecoder(rec.Body).Decode(&tool)
		cfg, _ := tool["config"].(map[string]any)
		hdrs, _ := cfg["headers"].(map[string]any)
		if hdrs["X-Mode"] != "live" {
			t.Fatalf("expected visible X-Mode, got %v", hdrs["X-Mode"])
		}
		if hdrs["X-Api-Key"] != "__AETHER_REDACTED__" {
			t.Fatalf("expected redacted secret, got %v", hdrs["X-Api-Key"])
		}
	})

	t.Run("update with sentinel keeps the stored ciphertext", func(t *testing.T) {
		var beforeCfg map[string]any
		json.Unmarshal(storedToolConfigRaw(t, srv, toolID), &beforeCfg)
		beforeHdrs, _ := beforeCfg["headers"].(map[string]any)

		body, _ := json.Marshal(map[string]any{
			"config": map[string]any{
				"url":     "https://example.com/hook",
				"method":  "POST",
				"headers": map[string]string{"X-Api-Key": "__AETHER_REDACTED__", "X-Mode": "staging"},
				"secrets": []string{"X-Api-Key"},
			},
		})
		req := httptest.NewRequest("PUT", "/api/v1/tools/"+toolID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var afterCfg map[string]any
		json.Unmarshal(storedToolConfigRaw(t, srv, toolID), &afterCfg)
		afterHdrs, _ := afterCfg["headers"].(map[string]any)
		if afterHdrs["X-Api-Key"] != beforeHdrs["X-Api-Key"] {
			t.Fatalf("sentinel update must keep the stored ciphertext unchanged: before %v, after %v",
				beforeHdrs["X-Api-Key"], afterHdrs["X-Api-Key"])
		}
		if afterHdrs["X-Mode"] != "staging" {
			t.Fatalf("expected updated X-Mode, got %v", afterHdrs["X-Mode"])
		}
	})

	t.Run("update with a new value re-encrypts", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"config": map[string]any{
				"url":     "https://example.com/hook",
				"method":  "POST",
				"headers": map[string]string{"X-Api-Key": "brand-new-secret"},
				"secrets": []string{"X-Api-Key"},
			},
		})
		req := httptest.NewRequest("PUT", "/api/v1/tools/"+toolID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		raw := storedToolConfigRaw(t, srv, toolID)
		if strings.Contains(string(raw), "brand-new-secret") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var cfg map[string]any
		json.Unmarshal(raw, &cfg)
		hdrs, _ := cfg["headers"].(map[string]any)
		secret, _ := hdrs["X-Api-Key"].(string)
		if !strings.HasPrefix(secret, "enc:v1:") {
			t.Fatalf("expected re-encrypted ciphertext, got %q", secret)
		}
	})

	t.Run("legacy plaintext row is redacted and upgraded on save", func(t *testing.T) {
		// Seed a pre-encryption row directly in the DB, ACL included, exactly
		// as the old write path would have stored it.
		var userID, orgID string
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT u.id, om.org_id FROM users u JOIN org_members om ON om.user_id = u.id WHERE u.email = $1`,
			email).Scan(&userID, &orgID); err != nil {
			t.Fatalf("lookup org/user: %v", err)
		}
		legacyID := uuid.New().String()
		legacyConfig, _ := json.Marshal(map[string]any{
			"url":     "https://example.com/hook",
			"method":  "POST",
			"headers": map[string]string{"X-Api-Key": "legacy-secret"},
			"secrets": []string{"X-Api-Key"},
		})
		if _, err := srv.DB().Pool.Exec(context.Background(), `
			INSERT INTO tools (id, org_id, name, type, config, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, 'webhook', $4, $5, NOW(), NOW())
		`, legacyID, orgID, fmt.Sprintf("Legacy Secret Tool %d", time.Now().UnixNano()), legacyConfig, userID); err != nil {
			t.Fatalf("insert legacy tool: %v", err)
		}
		if _, err := srv.DB().Pool.Exec(context.Background(), `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'tool', $2, 'user', $3, ARRAY['view','edit','delete','use'])
		`, orgID, legacyID, userID); err != nil {
			t.Fatalf("insert legacy acl: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/v1/tools/"+legacyID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get legacy tool: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var tool map[string]any
		json.NewDecoder(rec.Body).Decode(&tool)
		cfg, _ := tool["config"].(map[string]any)
		hdrs, _ := cfg["headers"].(map[string]any)
		if hdrs["X-Api-Key"] != "__AETHER_REDACTED__" {
			t.Fatalf("legacy secret must be redacted, got %v", hdrs["X-Api-Key"])
		}

		// Re-save with the sentinel: the restored legacy value must be encrypted.
		body, _ := json.Marshal(map[string]any{
			"config": map[string]any{
				"url":     "https://example.com/hook",
				"method":  "POST",
				"headers": map[string]string{"X-Api-Key": "__AETHER_REDACTED__"},
				"secrets": []string{"X-Api-Key"},
			},
		})
		req = httptest.NewRequest("PUT", "/api/v1/tools/"+legacyID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("legacy update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		raw := storedToolConfigRaw(t, srv, legacyID)
		if strings.Contains(string(raw), "legacy-secret") {
			t.Fatalf("legacy secret must be encrypted after save: %s", raw)
		}
		if !strings.Contains(string(raw), "enc:v1:") {
			t.Fatalf("expected ciphertext after save: %s", raw)
		}
	})
}
