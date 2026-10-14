package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPServerCustomHeaders(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-headers-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MCP Headers Org")

	do := func(t *testing.T, method, path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		var body io.Reader
		if payload != nil {
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			body = strings.NewReader(string(raw))
		}
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	var mcpID string
	t.Run("create with headers", func(t *testing.T) {
		rec := do(t, "POST", "/api/v1/mcp-servers", map[string]any{
			"name":    "Header MCP",
			"type":    "http",
			"command": "https://mcp.example.com/mcp",
			"headers": map[string]string{
				"Authorization": "Bearer mcp-token",
				"X-Env":         "prod",
			},
			"header_secrets": []string{"Authorization"},
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		if resp["id"] == "" {
			t.Fatalf("create: expected an id in the response, got %v", resp)
		}
		mcpID = resp["id"]
	})

	t.Run("secret is ciphertext at rest", func(t *testing.T) {
		var raw []byte
		var secretNames []string
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers, header_secrets FROM mcp_servers WHERE id = $1`, mcpID).Scan(&raw, &secretNames); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		if strings.Contains(string(raw), "mcp-token") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if stored["X-Env"] != "prod" {
			t.Fatalf("expected plaintext X-Env, got %q", stored["X-Env"])
		}
		if !strings.HasPrefix(stored["Authorization"], "enc:v1:") {
			t.Fatalf("expected enc:v1: ciphertext, got %q", stored["Authorization"])
		}
		if len(secretNames) != 1 || secretNames[0] != "Authorization" {
			t.Fatalf("expected header_secrets [Authorization], got %v", secretNames)
		}
	})

	t.Run("get redacts the secret", func(t *testing.T) {
		rec := do(t, "GET", "/api/v1/mcp-servers/"+mcpID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var s map[string]any
		json.NewDecoder(rec.Body).Decode(&s)
		hdrs, _ := s["headers"].(map[string]any)
		if hdrs["X-Env"] != "prod" {
			t.Fatalf("expected visible X-Env, got %v", hdrs["X-Env"])
		}
		if hdrs["Authorization"] != "__AETHER_REDACTED__" {
			t.Fatalf("expected redacted secret, got %v", hdrs["Authorization"])
		}
	})

	t.Run("list redacts the secret", func(t *testing.T) {
		rec := do(t, "GET", "/api/v1/mcp-servers", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		bodyText := rec.Body.String()
		if strings.Contains(bodyText, "enc:v1:") {
			t.Fatalf("ciphertext leaked in list response: %s", bodyText)
		}
		var servers []map[string]any
		json.Unmarshal([]byte(bodyText), &servers)
		found := false
		for _, s := range servers {
			if s["id"] != mcpID {
				continue
			}
			found = true
			hdrs, _ := s["headers"].(map[string]any)
			if hdrs["X-Env"] != "prod" {
				t.Fatalf("expected visible X-Env in list, got %v", hdrs["X-Env"])
			}
			if hdrs["Authorization"] != "__AETHER_REDACTED__" {
				t.Fatalf("expected redacted secret in list, got %v", hdrs["Authorization"])
			}
		}
		if !found {
			t.Fatalf("MCP server %s not found in list", mcpID)
		}
	})

	t.Run("update with sentinel keeps the stored ciphertext", func(t *testing.T) {
		var before []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&before); err != nil {
			t.Fatalf("read headers before: %v", err)
		}

		rec := do(t, "PUT", "/api/v1/mcp-servers/"+mcpID, map[string]any{
			"headers": map[string]string{
				"Authorization": "__AETHER_REDACTED__",
				"X-Env":         "staging",
			},
			"header_secrets": []string{"Authorization"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var after []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&after); err != nil {
			t.Fatalf("read headers after: %v", err)
		}
		var beforeMap, afterMap map[string]string
		json.Unmarshal(before, &beforeMap)
		json.Unmarshal(after, &afterMap)
		if afterMap["Authorization"] != beforeMap["Authorization"] {
			t.Fatal("sentinel update must keep the stored ciphertext unchanged")
		}
		if afterMap["X-Env"] != "staging" {
			t.Fatalf("expected updated X-Env, got %q", afterMap["X-Env"])
		}
	})

	t.Run("update with a new value re-encrypts", func(t *testing.T) {
		rec := do(t, "PUT", "/api/v1/mcp-servers/"+mcpID, map[string]any{
			"headers": map[string]string{
				"Authorization": "rotated-mcp-token",
				"X-Env":         "staging",
			},
			"header_secrets": []string{"Authorization"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var raw []byte
		srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&raw)
		if strings.Contains(string(raw), "rotated-mcp-token") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if !strings.HasPrefix(stored["Authorization"], "enc:v1:") {
			t.Fatalf("expected re-encrypted ciphertext, got %q", stored["Authorization"])
		}
	})

	t.Run("clearing headers with an empty map", func(t *testing.T) {
		rec := do(t, "PUT", "/api/v1/mcp-servers/"+mcpID, map[string]any{
			"headers":        map[string]string{},
			"header_secrets": []string{},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("clear update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&raw); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if len(stored) != 0 {
			t.Fatalf("expected empty stored headers, got %v", stored)
		}

		rec = do(t, "GET", "/api/v1/mcp-servers/"+mcpID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var s map[string]any
		json.NewDecoder(rec.Body).Decode(&s)
		if hdrs, ok := s["headers"].(map[string]any); ok && len(hdrs) != 0 {
			t.Fatalf("expected omitted or empty headers, got %v", hdrs)
		}
	})

	t.Run("rejects reserved header", func(t *testing.T) {
		rec := do(t, "POST", "/api/v1/mcp-servers", map[string]any{
			"name":    "Bad Headers",
			"type":    "http",
			"command": "https://bad.example.com/mcp",
			"headers": map[string]string{"Content-Type": "text/plain"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for reserved header, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects invalid name and unknown secret", func(t *testing.T) {
		for _, payload := range []map[string]any{
			{"headers": map[string]string{"Bad Header": "x"}},
			{"headers": map[string]string{"X-Env": "x"}, "header_secrets": []string{"X-Missing"}},
		} {
			rec := do(t, "PUT", "/api/v1/mcp-servers/"+mcpID, payload)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %v, got %d: %s", payload, rec.Code, rec.Body.String())
			}
		}

		// Rejected updates are validated before any write, so the stored
		// headers stay empty after the failed attempts above.
		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&raw); err != nil {
			t.Fatalf("read stored headers: %v", err)
		}
		var stored map[string]string
		json.Unmarshal(raw, &stored)
		if len(stored) != 0 {
			t.Fatalf("rejected updates must not modify stored headers, got %v", stored)
		}
	})

	t.Run("test endpoint sends decrypted headers", func(t *testing.T) {
		var gotAuth string
		probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer probe.Close()

		rec := do(t, "POST", "/api/v1/mcp-servers", map[string]any{
			"name":           "Probe MCP",
			"type":           "http",
			"command":        probe.URL,
			"headers":        map[string]string{"Authorization": "Bearer probe-token"},
			"header_secrets": []string{"Authorization"},
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var created map[string]string
		json.NewDecoder(rec.Body).Decode(&created)

		testRec := do(t, "POST", "/api/v1/mcp-servers/"+created["id"]+"/test", nil)
		if testRec.Code != http.StatusOK {
			t.Fatalf("test endpoint: expected 200, got %d: %s", testRec.Code, testRec.Body.String())
		}
		var result map[string]any
		json.NewDecoder(testRec.Body).Decode(&result)
		if result["success"] != true {
			t.Fatalf("expected successful probe, got %v", result)
		}
		if gotAuth != "Bearer probe-token" {
			t.Fatalf("expected decrypted Authorization header on probe, got %q", gotAuth)
		}
	})
}
