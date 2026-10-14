package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

// insertWebhookTool seeds a webhook tool row directly, bypassing the API's
// encryption write path so tests control the stored config exactly.
func insertWebhookTool(t *testing.T, db *database.DB, orgID, userID, name string, config map[string]any) string {
	t.Helper()
	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config for %s: %v", name, err)
	}
	id := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO tools (id, org_id, name, description, type, schema, config, require_confirmation, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, '', 'webhook', '{}', $4, false, $5, NOW(), NOW())
	`, id, orgID, name, rawConfig, userID)
	if err != nil {
		t.Fatalf("insert tool %s: %v", name, err)
	}
	return id
}

// TestLoadAgentToolDefsDecryptsWebhookSecrets pins the read side of webhook
// header encryption: loadAgentToolDefs must hand makeWebhookToolDef plaintext
// header values, both for enc:v1: ciphertext (decrypted with the master key)
// and for legacy untagged rows (passthrough, so pre-encryption tools keep
// working without a migration).
func TestLoadAgentToolDefsDecryptsWebhookSecrets(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	engine.SetToolAllowedDomains([]string{"127.0.0.1"})
	masterKey := testMasterKey()

	gotHeaders := make(chan http.Header, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer hook.Close()

	encrypted, err := crypto.EncryptString("supersecret", masterKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}

	toolA := insertWebhookTool(t, db, orgID, userID, "Encrypted Hook", map[string]any{
		"url":     hook.URL,
		"method":  "POST",
		"headers": map[string]string{"X-Api-Key": encrypted, "X-Legacy": "legacy-plain"},
		"secrets": []string{"X-Api-Key", "X-Legacy"},
	})
	toolB := insertWebhookTool(t, db, orgID, userID, "Legacy Hook", map[string]any{
		"url":     hook.URL,
		"method":  "POST",
		"headers": map[string]string{"X-Legacy": "legacy-plain"},
		"secrets": []string{"X-Legacy"},
	})

	agent := models.Agent{ID: uuid.New().String(), OrgID: orgID, ToolIDs: []string{toolA, toolB}}
	defs := engine.loadAgentToolDefs(context.Background(), agent, masterKey)
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs, got %d", len(defs))
	}

	var hookDef *ToolDef
	for _, d := range defs {
		if d.Function.Name == "Encrypted Hook" {
			hookDef = d
		}
	}
	if hookDef == nil {
		t.Fatal("Encrypted Hook def was not loaded")
	}
	if _, err := hookDef.Handler(json.RawMessage(`{}`), &ToolContext{Context: context.Background()}); err != nil {
		t.Fatalf("invoke webhook: %v", err)
	}

	select {
	case h := <-gotHeaders:
		if h.Get("X-Api-Key") != "supersecret" {
			t.Fatalf("expected decrypted X-Api-Key %q, got %q", "supersecret", h.Get("X-Api-Key"))
		}
		if h.Get("X-Legacy") != "legacy-plain" {
			t.Fatalf("expected legacy X-Legacy passthrough %q, got %q", "legacy-plain", h.Get("X-Legacy"))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook server never received the request")
	}
}

// TestLoadAgentToolDefsSkipsUndecryptableWebhookSecret pins the fail-closed
// branch: a webhook tool whose tagged secret cannot be decrypted is omitted
// entirely — no ciphertext can reach the wire — while healthy siblings still
// load for the same agent.
func TestLoadAgentToolDefsSkipsUndecryptableWebhookSecret(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	masterKey := testMasterKey()

	badID := insertWebhookTool(t, db, orgID, userID, "Bad Hook", map[string]any{
		"url":     "https://example.com/bad",
		"method":  "POST",
		"headers": map[string]string{"X-Api-Key": "enc:v1:!!!not-base64!!!"},
		"secrets": []string{"X-Api-Key"},
	})
	goodID := insertWebhookTool(t, db, orgID, userID, "Good Hook", map[string]any{
		"url":     "https://example.com/good",
		"method":  "POST",
		"headers": map[string]string{"X-Legacy": "legacy-plain"},
		"secrets": []string{"X-Legacy"},
	})

	agent := models.Agent{ID: uuid.New().String(), OrgID: orgID, ToolIDs: []string{badID, goodID}}
	defs := engine.loadAgentToolDefs(context.Background(), agent, masterKey)
	if len(defs) != 1 {
		t.Fatalf("expected only the healthy tool to load, got %d defs", len(defs))
	}
	if defs[0].Function.Name != "Good Hook" {
		t.Fatalf("expected Good Hook, got %q", defs[0].Function.Name)
	}
}
