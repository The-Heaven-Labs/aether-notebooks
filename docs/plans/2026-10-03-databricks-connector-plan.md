# Databricks Connector Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `databricks` connector type that runs SQL against a Databricks SQL warehouse via the official `databricks-sql-go` driver (PAT + OAuth M2M), with raw-JSON connector configs and ConfigSchema-driven secret masking.

**Architecture:** One new `ConnectorDriver` + `Executor` pair behind the existing driver registry; the API layer switches `config` from the rigid `models.ConnectorConfig` struct to `json.RawMessage` with key-level update merge and driver-declared secret masking. Frontend adds conditional Databricks form fields and a SQL dialect mapping. No DB migration, no warehouse/relay changes.

**Tech Stack:** Go 1.25 (`net/http` ServeMux, pgx, `database/sql`), `github.com/databricks/databricks-sql-go` v1.16.0, React + TypeScript + React Query + Vitest + MSW, CodeMirror 6.

**Design doc:** `docs/plans/2026-10-03-databricks-connector-design.md`

**Branch:** `feat/databricks-connector` (already checked out, design doc committed)

**Prerequisites:**
- `task infra:up` (or the dev Docker stack) for API tests — tests hit a real Postgres, no mocks.
- All Go test commands use `-count=1 -timeout 3m` (AGENTS.md).
- Live validation needs Databricks credentials (see Task 7 env vars) — never commit them.

**Commit convention:** conventional commits, e.g. `feat: ...`, `refactor: ...`, `docs: ...`. Commit at the end of every task.

---

## Task 1: ConfigField.Secret + declare secrets on existing drivers

**Files:**
- Modify: `internal/executor/driver.go:32-39` (ConfigField)
- Modify: `internal/executor/postgres_driver.go:32-43` (password field)
- Modify: `internal/executor/clickhouse_driver.go:32-43` (password field)
- Modify: `internal/executor/opensearch_driver.go:22-32` (password field)
- Test: `internal/executor/driver_test.go`

**Step 1: Write the failing test**

Add to `internal/executor/driver_test.go`:

```go
func TestSecretFieldsDeclared(t *testing.T) {
	cases := []struct {
		name   string
		driver ConnectorDriver
		secret string
	}{
		{"postgres", &PostgresDriver{}, "password"},
		{"clickhouse", &ClickHouseDriver{}, "password"},
		{"opensearch", &OpenSearchDriver{}, "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, f := range tc.driver.ConfigSchema().Fields {
				if f.Name == tc.secret {
					if !f.Secret {
						t.Fatalf("%s.%s must be marked Secret", tc.name, tc.secret)
					}
					return
				}
			}
			t.Fatalf("%s: field %q not found in schema", tc.name, tc.secret)
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor -run TestSecretFieldsDeclared -count=1 -timeout 3m -v`
Expected: compile failure (`f.Secret` undefined) — this is the failing state.

**Step 3: Implement**

In `internal/executor/driver.go`, add the field:

```go
// ConfigField describes a single configuration field.
type ConfigField struct {
	Name        string
	Type        string // "string", "int", "bool"
	Required    bool
	Default     interface{}
	Description string
	// Secret marks credential fields that must be masked in API responses.
	Secret bool
}
```

In each of the three driver files, add `Secret: true` to the `password` field:

```go
{Name: "password", Type: "string", Required: true, Secret: true, Description: "Postgres password"},
```
```go
{Name: "password", Type: "string", Required: true, Secret: true, Description: "ClickHouse password"},
```
```go
{Name: "password", Type: "string", Required: false, Secret: true, Description: "Password (empty for unauthenticated)"},
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executor -run TestSecretFieldsDeclared -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/executor/driver.go internal/executor/postgres_driver.go internal/executor/clickhouse_driver.go internal/executor/opensearch_driver.go internal/executor/driver_test.go
git commit -m "feat: declare secret connector config fields in ConfigSchema"
```

---

## Task 2: Raw JSON connector config — model, create handler, masking helpers

**Files:**
- Modify: `internal/models/connector.go:5-21` (`Connector.Config` type)
- Modify: `internal/api/connector_handlers.go:19-28` (create request), `:40-146` (create handler)
- Test: `internal/api/connector_handlers_test.go`

**Step 1: Write the failing test**

Add to `internal/api/connector_handlers_test.go`:

```go
// OpenSearch use_tls used to be silently dropped because the create request
// decoded config into models.ConnectorConfig. Raw JSON must preserve it.
func TestCreateConnectorPreservesDriverSpecificConfig(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("raw-config-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Raw Config Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "TLS OpenSearch",
		"type": "opensearch",
		"config": map[string]interface{}{
			"host": "localhost", "port": 9200, "use_tls": true,
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	config := resp["config"].(map[string]interface{})
	if config["use_tls"] != true {
		t.Fatalf("expected use_tls=true in response, got %v", config["use_tls"])
	}

	// Config must be a JSON object; arrays/strings are rejected.
	badBody, _ := json.Marshal(map[string]interface{}{
		"name": "Bad", "type": "opensearch", "config": []string{"nope"},
	})
	req = httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(badBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-object config: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestCreateConnectorPreservesDriverSpecificConfig -count=1 -timeout 3m -v`
Expected: FAIL — `use_tls` missing from the response (current struct drops it).

**Step 3: Implement**

`internal/models/connector.go` — change the import and field:

```go
import (
	"encoding/json"
	"time"
)
```

```go
	Type           ConnectorType   `json:"type"`
	Config         json.RawMessage `json:"config"`
```

`internal/api/connector_handlers.go` — request struct:

```go
type createConnectorRequest struct {
	Name           string               `json:"name"`
	Type           models.ConnectorType `json:"type"`
	Config         json.RawMessage      `json:"config"`
	IsDefault      bool                 `json:"is_default"`
	TimeoutSeconds int                  `json:"timeout_seconds"`
	FolderID       *string              `json:"folder_id,omitempty"`
	TableAllowlist []string             `json:"table_allowlist,omitempty"`
	TableDenylist  []string             `json:"table_denylist,omitempty"`
}
```

Add `"bytes"` to the imports. In `handleCreateConnector`, after the driver lookup, replace the `json.Marshal(req.Config)` block with:

```go
	configJSON := req.Config
	if len(configJSON) == 0 || bytes.Equal(bytes.TrimSpace(configJSON), []byte("null")) {
		configJSON = json.RawMessage(`{}`)
	}
	if !isJSONObject(configJSON) {
		writeError(w, http.StatusBadRequest, "config must be a JSON object")
		return
	}

	encrypted, err := crypto.Encrypt(configJSON, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt config")
		return
	}
```

Replace the response construction (`req.Config.Password = "***"` and the `Config: req.Config` field) with:

```go
	conn := models.Connector{
		ID: id, OrgID: orgID, Name: name, Type: connType,
		Config: s.maskedConnectorConfig(connType, encrypted),
		MaxRows: maxRows, TimeoutSeconds: timeout, IsDefault: isDefault,
		FolderID: folderID,
	}
```

Add the helpers at the bottom of `connector_handlers.go`:

```go
// isJSONObject reports whether raw is a JSON object (the only config shape the
// drivers accept; arrays, strings, and null are rejected at the API boundary).
func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(trimmed, &obj) == nil
}

// secretFieldSet returns the lowercased config keys to mask: the union of the
// driver's ConfigSchema secrets and a conservative default key list. The union
// is always applied so a driver that declares one secret cannot leave another
// credential-looking key unmasked, and keys are lowercased because Go's JSON
// decoder matches struct fields case-insensitively (a "Password" key is consumed
// by the driver even though its tag is "password").
func secretFieldSet(connType models.ConnectorType) map[string]bool {
	keys := map[string]bool{"password": true, "token": true, "client_secret": true}
	if d, ok := executor.GetDriver(connType); ok {
		for _, f := range d.ConfigSchema().Fields {
			if f.Secret {
				keys[strings.ToLower(f.Name)] = true
			}
		}
	}
	return keys
}

// maskedConnectorConfig decrypts a stored config and masks every secret key
// (case-insensitively), returning a JSON object safe for API responses.
func (s *Server) maskedConnectorConfig(connType models.ConnectorType, encrypted []byte) json.RawMessage {
	plain, err := crypto.Decrypt(encrypted, s.masterKey)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	var cfg map[string]any
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return json.RawMessage(`{}`)
	}
	secrets := secretFieldSet(connType)
	for k := range cfg {
		if secrets[strings.ToLower(k)] {
			cfg[k] = "***"
		}
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}
```

Note: `handleGetConnector`, `handleListConnectors`, and the final response in `handleUpdateConnector` still call `json.Unmarshal(plain, &c.Config)` with the old struct logic; they will not compile yet. In each of those three blocks replace the decrypt+mask code with:

```go
	c.Config = s.maskedConnectorConfig(c.Type, encryptedConfig)
```

(in the list loop the variable is `encryptedConfig` too; the update handler's variable is `encryptedConfig` as well.)

`handleUpdateConnector`'s config-merge block is fully replaced in Task 3; for now make it compile by replacing the whole `if req.Config != nil { ... }` block's internals with a temporary raw merge:

```go
	if req.Config != nil {
		var existingEnc []byte
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT config_encrypted FROM connectors WHERE id=$1 AND org_id=$2`,
			id, claims.OrgID,
		).Scan(&existingEnc); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load existing config")
			return
		}
		plain, err := crypto.Decrypt(existingEnc, s.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decrypt config")
			return
		}
		var existing map[string]any
		if err := json.Unmarshal(plain, &existing); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse config")
			return
		}
		var incoming map[string]any
		if err := json.Unmarshal(req.Config, &incoming); err != nil {
			writeError(w, http.StatusBadRequest, "config must be a JSON object")
			return
		}
		for k, v := range incoming {
			existing[k] = v
		}
		configJSON, err := json.Marshal(existing)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid config")
			return
		}
		encrypted, err := crypto.Encrypt(configJSON, s.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt config")
			return
		}
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET config_encrypted=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
			encrypted, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
```

Update `updateConnectorRequest.Config` to `json.RawMessage` and change the nil check to `len(req.Config) > 0`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run 'TestCreateConnectorPreservesDriverSpecificConfig|TestConnectorCRUD|TestUpdateConnector' -count=1 -timeout 3m -v`
Expected: PASS (existing CRUD tests included to catch regressions).

**Step 5: Commit**

```bash
git add internal/models/connector.go internal/api/connector_handlers.go internal/api/connector_handlers_test.go
git commit -m "refactor: accept raw JSON connector config at the API boundary"
```

---

## Task 3: Update merge semantics + response masking tests

**Files:**
- Modify: `internal/api/connector_handlers.go` (update handler config block)
- Create: `internal/api/connector_config_test.go` (package `api`, pure helpers)
- Test: `internal/api/connector_handlers_test.go`

**Step 1: Write the failing tests**

Create `internal/api/connector_config_test.go`:

```go
package api

import (
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
)

func TestMergeConnectorConfig(t *testing.T) {
	existing := map[string]any{"host": "h", "password": "old", "use_tls": true}
	secrets := map[string]bool{"password": true, "token": true}

	// Empty/missing secret keeps the stored value; null never clears a field.
	got := mergeConnectorConfig(existing, map[string]any{
		"password": "", "token": "", "port": nil, "use_tls": false,
	}, secrets)
	if got["password"] != "old" {
		t.Fatalf("empty secret must keep stored value, got %v", got["password"])
	}
	if _, ok := got["port"]; ok {
		t.Fatal("null value must not clear a field")
	}
	if got["use_tls"] != false {
		t.Fatalf("non-secret false must overwrite true, got %v", got["use_tls"])
	}

	// The API mask echoed back is also treated as "keep".
	got = mergeConnectorConfig(existing, map[string]any{"password": "***"}, secrets)
	if got["password"] != "old" {
		t.Fatalf("masked secret must keep stored value, got %v", got["password"])
	}

	// A case-variant secret key is matched case-insensitively; an empty variant
	// must not delete or shadow the canonical stored key.
	got = mergeConnectorConfig(existing, map[string]any{"Password": ""}, secrets)
	if got["Password"] == "" {
		t.Fatal("case-variant empty secret must not be stored")
	}
	if got["password"] != "old" {
		t.Fatalf("canonical stored secret must be preserved, got %v", got["password"])
	}

	// A case-variant real secret replaces the stored key instead of coexisting;
	// Go's JSON decoding is case-insensitive and would otherwise read the stale key.
	got = mergeConnectorConfig(existing, map[string]any{"Password": "new"}, secrets)
	variants := 0
	for k, v := range got {
		if strings.EqualFold(k, "password") {
			variants++
			if v != "new" {
				t.Fatalf("case-variant password must be replaced, got %v", v)
			}
		}
	}
	if variants != 1 {
		t.Fatalf("expected exactly one password key, got %d: %v", variants, got)
	}

	// A real new secret wins.
	got = mergeConnectorConfig(existing, map[string]any{"password": "new"}, secrets)
	if got["password"] != "new" {
		t.Fatalf("new secret must overwrite, got %v", got["password"])
	}
}

func TestMaskedConnectorConfig(t *testing.T) {
	s := &Server{masterKey: []byte("0123456789abcdef0123456789abcdef")}
	enc, err := crypto.Encrypt([]byte(`{"host":"h","password":"pw","use_tls":true}`), s.masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	out := s.maskedConnectorConfig(models.ConnectorPostgres, enc)
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg["password"] != "***" {
		t.Fatalf("expected masked password, got %v", cfg["password"])
	}
	if cfg["host"] != "h" {
		t.Fatalf("expected host preserved, got %v", cfg["host"])
	}
}

func TestSecretFieldSetFallsBackForUnknownDriver(t *testing.T) {
	keys := secretFieldSet(models.ConnectorType("nonexistent"))
	for _, k := range []string{"password", "token", "client_secret"} {
		if !keys[k] {
			t.Fatalf("expected fallback secret key %q", k)
		}
	}
}

func TestSecretFieldSetIncludesFallbackAlongsideDeclared(t *testing.T) {
	keys := secretFieldSet(models.ConnectorPostgres)
	if !keys["password"] || !keys["token"] || !keys["client_secret"] {
		t.Fatalf("expected union of declared and fallback secrets, got %v", keys)
	}
}
```

Use `encoding/json` directly (`json.Unmarshal(out, &cfg)`); the file imports `"encoding/json"` and `"strings"` (the latter for the case-variant assertions).

Add to `internal/api/connector_handlers_test.go`:

```go
// The OpenSearch use_tls fix must survive get and update round-trips.
func TestConnectorRawConfigRoundTrip(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("config-roundtrip-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Roundtrip Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "TLS OS", "type": "opensearch",
		"config": map[string]interface{}{"host": "localhost", "use_tls": true},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&created)
	connID := created["id"].(string)

	// GET returns the driver-specific field.
	req = httptest.NewRequest("GET", "/api/v1/connectors/"+connID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var fetched map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&fetched)
	if fetched["config"].(map[string]interface{})["use_tls"] != true {
		t.Fatal("GET must return use_tls=true")
	}

	// Update toggles it off and preserves unknown behavior via key merge.
	upd, _ := json.Marshal(map[string]interface{}{
		"config": map[string]interface{}{"use_tls": false},
	})
	req = httptest.NewRequest("PUT", "/api/v1/connectors/"+connID, bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var updated map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&updated)
	cfg := updated["config"].(map[string]interface{})
	if cfg["use_tls"] != false {
		t.Fatalf("update must set use_tls=false, got %v", cfg["use_tls"])
	}
	if cfg["host"] != "localhost" {
		t.Fatalf("update must keep host, got %v", cfg["host"])
	}
}
```

Also add `TestUpdateConnectorKeepsStoredSecret` (review hardening): create a postgres connector via the `createConnector` helper, PUT `{"config":{"password":""}}` then `{"config":{"password":"***"}}`, and require `POST /connectors/{id}/test` to return `ok: true` — the saved-connector test authenticates with the stored credential, so it fails if the secret was clobbered.
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run 'TestMergeConnectorConfig|TestMaskedConnectorConfig|TestSecretFieldSetFallsBackForUnknownDriver|TestConnectorRawConfigRoundTrip' -count=1 -timeout 3m -v`
Expected: compile failure (`mergeConnectorConfig` undefined).

**Step 3: Implement**

Extract the merge into a pure helper in `internal/api/connector_handlers.go`:

```go
// mergeConnectorConfig overlays incoming onto existing. Keys declared secret
// whose incoming value is null, empty, or the API mask "***" are skipped, so a
// secret can never be replaced by a placeholder; null values never clear a field.
func mergeConnectorConfig(existing, incoming map[string]any, secrets map[string]bool) map[string]any {
	merged := make(map[string]any, len(existing))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range incoming {
		if v == nil {
			continue
		}
		if secrets[strings.ToLower(k)] {
			if s, ok := v.(string); ok && (s == "" || s == "***") {
				continue
			}
		}
		// Drop any case-variant of the same key: JSON decoding is
		// case-insensitive, so coexisting keys would let a stale value win.
		for existingKey := range merged {
			if existingKey != k && strings.EqualFold(existingKey, k) {
				delete(merged, existingKey)
			}
		}
		merged[k] = v
	}
	return merged
}
```

In `handleUpdateConnector`, change the initial lookup to also fetch the type:

```go
	var orgID string
	var connType models.ConnectorType
	err := s.db.Pool.QueryRow(ctx, `SELECT org_id, type FROM connectors WHERE id=$1`, id).Scan(&orgID, &connType)
```

and replace the temporary merge body (from Task 2) with:

```go
		merged := mergeConnectorConfig(existing, incoming, secretFieldSet(connType))
		configJSON, err := json.Marshal(merged)
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run 'TestMergeConnectorConfig|TestMaskedConnectorConfig|TestSecretFieldSetFallsBackForUnknownDriver|TestConnectorRawConfigRoundTrip' -count=1 -timeout 3m -v`
Expected: PASS.

Also run the full connector + warehouse handler suites to catch regressions:
`go test ./internal/api -run 'TestConnector|TestWarehouse|TestUpdateConnector|TestHandleListConnectorDatabases' -count=1 -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/connector_config_test.go internal/api/connector_handlers_test.go
git commit -m "refactor: key-level connector config merge with schema-driven secret masking"
```

---

## Task 4: Test-config endpoint passes raw JSON through

**Files:**
- Modify: `internal/api/connector_handlers.go:646-677` (`handleTestConnectorConfig`)
- Test: `internal/api/connector_handlers_test.go`

**Step 1: Write the failing test**

Add:

```go
func TestTestConnectorConfigRejectsNonObject(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("test-cfg-%d@example.com", ts), "Test Cfg Org")

	body, _ := json.Marshal(map[string]interface{}{"type": "opensearch", "config": "not-an-object"})
	req := httptest.NewRequest("POST", "/api/v1/connectors/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with ok=false, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["ok"] != false {
		t.Fatalf("expected ok=false, got %v", resp)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestTestConnectorConfigRejectsNonObject -count=1 -timeout 3m -v`
Expected: compile failure — `req.Config` decode currently fails differently; after Task 2 the struct is raw but the handler still calls `json.Marshal(req.Config)`. Either way the test fails against current behavior.

**Step 3: Implement**

Replace the body of `handleTestConnectorConfig` after decoding:

```go
	driver, ok := executor.GetDriver(req.Type)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unsupported connector type"})
		return
	}
	configJSON := req.Config
	if len(configJSON) == 0 {
		configJSON = json.RawMessage(`{}`)
	}
	if !isJSONObject(configJSON) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "config must be a JSON object"})
		return
	}
	if err := driver.TestConfig(r.Context(), configJSON); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api -run TestTestConnectorConfigRejectsNonObject -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/connector_handlers_test.go
git commit -m "refactor: pass raw config through the connector test endpoint"
```

---

## Task 5: Databricks dependency, driver, and executor

**Files:**
- Modify: `go.mod`, `go.sum` (`go get`)
- Modify: `internal/models/connector.go` (constant)
- Create: `internal/executor/databricks_driver.go`
- Create: `internal/executor/databricks.go`
- Create: `internal/executor/databricks_driver_test.go`
- Create: `internal/executor/databricks_test.go`

**Step 1: Add the dependency**

Run:

```bash
go get github.com/databricks/databricks-sql-go@v1.16.0
```

If the module needs a newer Go version than 1.25.7, stop and report; do not bump the Go directive.

**Step 2: Write the failing tests**

Create `internal/executor/databricks_driver_test.go`:

```go
package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func TestDatabricksDriver_Type(t *testing.T) {
	d := &DatabricksDriver{}
	if d.Type() != models.ConnectorDatabricks {
		t.Fatalf("expected %q, got %q", models.ConnectorDatabricks, d.Type())
	}
}

func TestDatabricksDriver_ConfigSchemaSecrets(t *testing.T) {
	d := &DatabricksDriver{}
	secrets := map[string]bool{}
	for _, f := range d.ConfigSchema().Fields {
		if f.Secret {
			secrets[f.Name] = true
		}
	}
	if !secrets["token"] || !secrets["client_secret"] {
		t.Fatalf("token and client_secret must be secret, got %v", secrets)
	}
}

func TestValidateDatabricksConfig(t *testing.T) {
	cases := []struct {
		name    string
		in      databricksConfig
		wantErr string
	}{
		{"missing host", databricksConfig{HTTPPath: "/sql/1.0/warehouses/x", Token: "t"}, "host"},
		{"missing http_path", databricksConfig{Host: "h", Token: "t"}, "http_path"},
		{"pat without token", databricksConfig{Host: "h", HTTPPath: "/p"}, "token"},
		{"m2m without credentials", databricksConfig{Host: "h", HTTPPath: "/p", AuthType: "oauth_m2m"}, "client_id"},
		{"unknown auth", databricksConfig{Host: "h", HTTPPath: "/p", AuthType: "bogus"}, "auth_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateDatabricksConfig(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateDatabricksConfigDefaultsAndNormalization(t *testing.T) {
	got, err := validateDatabricksConfig(databricksConfig{
		Host: "https://dbc-abc.cloud.databricks.com/", HTTPPath: "/sql/1.0/warehouses/x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Host != "dbc-abc.cloud.databricks.com" {
		t.Fatalf("host not normalized: %q", got.Host)
	}
	if got.AuthType != "pat" || got.Port != 443 {
		t.Fatalf("expected auth_type=pat port=443 defaults, got %q/%d", got.AuthType, got.Port)
	}
}

func TestDatabricksDriver_NewExecutor_InvalidConfig(t *testing.T) {
	d := &DatabricksDriver{}
	_, err := d.NewExecutor(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
```

Create `internal/executor/databricks_test.go`:

```go
package executor

import (
	"testing"
	"time"
)

func TestDatabricksIsCommand(t *testing.T) {
	cases := map[string]bool{
		"SELECT 1":                     false,
		"  select * from t":            false,
		"WITH x AS (SELECT 1) SELECT 1": false,
		"SHOW TABLES":                  false,
		"CREATE TABLE t (id INT)":      true,
		"INSERT INTO t VALUES (1)":     true,
		"DELETE FROM t":                true,
		"MERGE INTO t USING s ON 1=1":  true,
		"DROP TABLE t":                 true,
	}
	for q, want := range cases {
		if got := databricksIsCommand(q); got != want {
			t.Errorf("databricksIsCommand(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestNormalizeDatabricksValue(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := normalizeDatabricksValue(ts); got != "2026-01-02T03:04:05Z" {
		t.Fatalf("timestamp: got %v", got)
	}
	if got := normalizeDatabricksValue(time.Time{}); got != nil {
		t.Fatalf("zero time: want nil, got %v", got)
	}
	if got := normalizeDatabricksValue("plain"); got != "plain" {
		t.Fatalf("string passthrough: got %v", got)
	}
}

func TestValidDatabricksCatalogName(t *testing.T) {
	if !validDatabricksCatalogName("main") || !validDatabricksCatalogName("sales_2026") {
		t.Fatal("expected simple identifiers to be valid")
	}
	if validDatabricksCatalogName("bad`name") || validDatabricksCatalogName("") {
		t.Fatal("expected backticks and empty names to be rejected")
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `go test ./internal/executor -run 'TestDatabricks|TestValidateDatabricks|TestNormalizeDatabricks|TestValidDatabricks' -count=1 -timeout 3m -v`
Expected: compile failure (undefined types/functions) — the failing state.

**Step 4: Implement**

Add to `internal/models/connector.go`:

```go
const (
	ConnectorPostgres   ConnectorType = "postgres"
	ConnectorClickHouse ConnectorType = "clickhouse"
	ConnectorOpenSearch ConnectorType = "opensearch"
	ConnectorDatabricks ConnectorType = "databricks"
)
```

Create `internal/executor/databricks_driver.go`:

```go
package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/the-heaven-labs/aether/internal/models"
)

// databricksConfig is the typed config for the Databricks connector.
type databricksConfig struct {
	Host         string `json:"host"`
	HTTPPath     string `json:"http_path"`
	AuthType     string `json:"auth_type"`
	Token        string `json:"token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Catalog      string `json:"catalog"`
	Schema       string `json:"schema"`
	Port         int    `json:"port,omitempty"`
}

// DatabricksDriver implements ConnectorDriver for Databricks SQL.
type DatabricksDriver struct{}

func init() {
	RegisterDriver(&DatabricksDriver{})
}

func (d *DatabricksDriver) Type() models.ConnectorType {
	return models.ConnectorDatabricks
}

func (d *DatabricksDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "Workspace hostname, e.g. dbc-abc123.cloud.databricks.com"},
			{Name: "http_path", Type: "string", Required: true, Description: "Warehouse connection path from the workspace's Connection details, e.g. /sql/1.0/warehouses/abc123"},
			{Name: "auth_type", Type: "string", Required: true, Default: "pat", Description: "Authentication type: pat or oauth_m2m"},
			{Name: "token", Type: "string", Required: false, Secret: true, Description: "Personal access token (auth_type=pat)"},
			{Name: "client_id", Type: "string", Required: false, Description: "Service principal client ID (auth_type=oauth_m2m)"},
			{Name: "client_secret", Type: "string", Required: false, Secret: true, Description: "Service principal OAuth secret (auth_type=oauth_m2m)"},
			{Name: "catalog", Type: "string", Required: false, Description: "Initial catalog (optional)"},
			{Name: "schema", Type: "string", Required: false, Description: "Initial schema (optional)"},
		},
	}
}

func (d *DatabricksDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg databricksConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid databricks config: %w", err)
	}
	cfg, err := validateDatabricksConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewDatabricksExecutor(cfg)
}

func (d *DatabricksDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
```

Create `internal/executor/databricks.go`:

```go
package executor

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	dbsql "github.com/databricks/databricks-sql-go"
)

const (
	// databricksConnectTimeout bounds connector creation and the initial ping.
	databricksConnectTimeout = 10 * time.Second
	defaultDatabricksPort    = 443
)

// databricksCommandPrefixes are statements that return no result set and run
// through ExecContext. Mirrors the ClickHouse executor's classification.
var databricksCommandPrefixes = []string{
	"USE ", "SET ", "CREATE ", "DROP ", "ALTER ", "INSERT ", "UPDATE ",
	"DELETE ", "TRUNCATE ", "MERGE ", "GRANT ", "REVOKE ", "OPTIMIZE ",
	"VACUUM ", "REFRESH ", "MSCK ", "COPY ", "CACHE ", "UNCACHE ",
	"COMMENT ", "ANALYZE ",
}

// databricksCatalogNameRE guards catalog-name interpolation into SQL
// identifiers (catalog names cannot be bound as query parameters).
var databricksCatalogNameRE = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

var _ Executor = (*DatabricksExecutor)(nil)

// DatabricksExecutor executes SQL against a Databricks SQL warehouse or cluster.
type DatabricksExecutor struct {
	db        *sql.DB
	closeOnce sync.Once
	closeErr  error
}

// normalizeDatabricksHost strips an optional scheme and trailing slash so users
// can paste the workspace URL exactly as shown in the browser.
func normalizeDatabricksHost(host string) string {
	h := strings.TrimSpace(host)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	return strings.TrimRight(h, "/")
}

// validateDatabricksConfig checks conditional auth requirements and applies
// defaults. It returns a normalized copy of the config.
func validateDatabricksConfig(cfg databricksConfig) (databricksConfig, error) {
	cfg.Host = normalizeDatabricksHost(cfg.Host)
	if cfg.Host == "" {
		return cfg, fmt.Errorf("databricks host is required")
	}
	if strings.TrimSpace(cfg.HTTPPath) == "" {
		return cfg, fmt.Errorf("databricks http_path is required")
	}
	if cfg.AuthType == "" {
		cfg.AuthType = "pat"
	}
	switch cfg.AuthType {
	case "pat":
		if cfg.Token == "" {
			return cfg, fmt.Errorf("databricks token is required for pat auth_type")
		}
	case "oauth_m2m":
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			return cfg, fmt.Errorf("databricks client_id and client_secret are required for oauth_m2m auth_type")
		}
	default:
		return cfg, fmt.Errorf("unsupported databricks auth_type %q (want pat or oauth_m2m)", cfg.AuthType)
	}
	if cfg.Port == 0 {
		cfg.Port = defaultDatabricksPort
	}
	return cfg, nil
}

// NewDatabricksExecutor opens a pooled connection and verifies it with a
// bounded ping. OAuth M2M credentials are exchanged for short-lived tokens by
// the driver; Aether stores only the client ID/secret.
func NewDatabricksExecutor(cfg databricksConfig) (*DatabricksExecutor, error) {
	opts := []dbsql.ConnOption{
		dbsql.WithServerHostname(cfg.Host),
		dbsql.WithPort(cfg.Port),
		dbsql.WithHTTPPath(cfg.HTTPPath),
	}
	if cfg.AuthType == "oauth_m2m" {
		opts = append(opts, dbsql.WithClientCredentials(cfg.ClientID, cfg.ClientSecret))
	} else {
		opts = append(opts, dbsql.WithAccessToken(cfg.Token))
	}
	if cfg.Catalog != "" || cfg.Schema != "" {
		opts = append(opts, dbsql.WithInitialNamespace(cfg.Catalog, cfg.Schema))
	}

	connector, err := dbsql.NewConnector(opts...)
	if err != nil {
		return nil, fmt.Errorf("databricks connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), databricksConnectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DatabricksExecutor{db: db}, nil
}

// databricksIsCommand reports whether the statement returns no result set.
// Classification happens before the tracing comment is prepended so every
// statement does not look like a read.
func databricksIsCommand(query string) bool {
	return hasPrefixAny(strings.TrimSpace(strings.ToUpper(query)), databricksCommandPrefixes)
}

// normalizeDatabricksValue converts driver-native values into JSON-friendly
// representations, matching the other executors.
func normalizeDatabricksValue(v interface{}) interface{} {
	if t, ok := v.(time.Time); ok {
		if t.IsZero() {
			return nil
		}
		return t.Format(time.RFC3339Nano)
	}
	return v
}

func (d *DatabricksExecutor) Execute(ctx context.Context, query string, params map[string]string, limits OutputLimits) (*ResultSet, error) {
	resolved := ResolveParams(query, params)
	isCommand := databricksIsCommand(resolved)

	// Tag queries with the Aether user email for tracing in Databricks query history.
	if userEmail, ok := ctx.Value(CtxUserEmail{}).(string); ok && userEmail != "" {
		resolved = fmt.Sprintf("/* aether_user:%s */ %s", userEmail, resolved)
	}

	if isCommand {
		if _, err := d.db.ExecContext(ctx, resolved); err != nil {
			return nil, fmt.Errorf("exec: %w", err)
		}
		return &ResultSet{Columns: []Column{}, Rows: [][]interface{}{}}, nil
	}

	rows, err := d.db.QueryContext(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	colTypes := rows.ColumnTypes()
	columns := make([]Column, len(colNames))
	for i, name := range colNames {
		typ := "unknown"
		if i < len(colTypes) {
			if t := colTypes[i].DatabaseTypeName(); t != "" {
				typ = t
			}
		}
		columns[i] = Column{Name: name, Type: typ}
	}

	acc := newRowAccumulator(limits)
	for rows.Next() && !acc.full() {
		values := make([]interface{}, len(colNames))
		ptrs := make([]interface{}, len(colNames))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		for i, v := range values {
			values[i] = normalizeDatabricksValue(v)
		}
		if !acc.add(values) {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if acc.rows == nil {
		acc.rows = [][]interface{}{}
	}
	return acc.result(columns), nil
}

func (d *DatabricksExecutor) TestConnection(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// validDatabricksCatalogName reports whether a catalog name is safe to
// interpolate as a SQL identifier.
func validDatabricksCatalogName(name string) bool {
	return databricksCatalogNameRE.MatchString(name)
}

// schemaForCatalog reads information_schema for one catalog and returns tables
// with three-level names flattened to "catalog.schema".
func (d *DatabricksExecutor) schemaForCatalog(ctx context.Context, catalog string) ([]TableInfo, error) {
	query := fmt.Sprintf(
		"SELECT c.table_schema, c.table_name, c.column_name, c.data_type, c.comment, t.comment "+
			"FROM `%s`.information_schema.columns c "+
			"LEFT JOIN `%s`.information_schema.tables t "+
			"ON t.table_catalog = c.table_catalog AND t.table_schema = c.table_schema AND t.table_name = c.table_name "+
			"WHERE c.table_schema <> 'information_schema' "+
			"ORDER BY c.table_schema, c.table_name, c.ordinal_position",
		catalog, catalog)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query information_schema: %w", err)
	}
	defer rows.Close()

	tableMap := map[string]*TableInfo{}
	var order []string
	for rows.Next() {
		var schema, table, column, dtype string
		var colComment, tableComment sql.NullString
		if err := rows.Scan(&schema, &table, &column, &dtype, &colComment, &tableComment); err != nil {
			return nil, err
		}
		key := schema + "." + table
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &TableInfo{
				Schema: catalog + "." + schema, Name: table, Description: tableComment.String,
			}
			order = append(order, key)
		}
		tableMap[key].Columns = append(tableMap[key].Columns, ColumnInfo{
			Name: column, Type: dtype, Description: colComment.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema rows: %w", err)
	}
	tables := make([]TableInfo, 0, len(order))
	for _, key := range order {
		tables = append(tables, *tableMap[key])
	}
	return tables, nil
}

func (d *DatabricksExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	catalogs, err := d.Databases(ctx)
	if err != nil {
		return nil, err
	}
	tables := []TableInfo{}
	succeeded := 0
	var firstErr error
	for _, catalog := range catalogs {
		if !validDatabricksCatalogName(catalog) {
			continue
		}
		catTables, err := d.schemaForCatalog(ctx, catalog)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("catalog %s: %w", catalog, err)
			}
			continue
		}
		tables = append(tables, catTables...)
		succeeded++
	}
	if succeeded == 0 && firstErr != nil {
		return nil, firstErr
	}
	return &SchemaInfo{Tables: tables}, nil
}

// scanFirstColumnStrings reads every row of a SHOW-style statement and returns
// the first column as strings, regardless of the statement's column layout.
func scanFirstColumnStrings(rows *sql.Rows) ([]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if len(values) == 0 {
			continue
		}
		switch v := values[0].(type) {
		case string:
			out = append(out, v)
		case []byte:
			out = append(out, string(v))
		default:
			out = append(out, fmt.Sprint(v))
		}
	}
	return out, rows.Err()
}

// Databases returns the accessible Unity Catalog catalogs (the UI's database
// picker); the hidden system catalog is excluded.
func (d *DatabricksExecutor) Databases(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW CATALOGS")
	if err != nil {
		return nil, fmt.Errorf("list catalogs: %w", err)
	}
	defer rows.Close()
	names, err := scanFirstColumnStrings(rows)
	if err != nil {
		return nil, err
	}
	var dbs []string
	for _, name := range names {
		if name == "" || name == "system" {
			continue
		}
		dbs = append(dbs, name)
	}
	return dbs, nil
}

func (d *DatabricksExecutor) Close() error {
	d.closeOnce.Do(func() {
		d.closeErr = d.db.Close()
	})
	return d.closeErr
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/executor -run 'TestDatabricks|TestValidateDatabricks|TestNormalizeDatabricks|TestValidDatabricks|TestSecretFieldsDeclared' -count=1 -timeout 3m -v`
Expected: PASS (all Databricks tests skip network paths; bad-config tests fail before connecting).

Run: `go build ./...`
Expected: compiles.

**Step 6: Commit**

```bash
git add go.mod go.sum internal/models/connector.go internal/executor/databricks.go internal/executor/databricks_driver.go internal/executor/databricks_driver_test.go internal/executor/databricks_test.go
git commit -m "feat: add Databricks connector driver and executor"
```

---

## Task 6: Databricks API handler tests + Swagger regeneration

**Files:**
- Test: `internal/api/connector_handlers_test.go`
- Modify: `internal/api/docs/docs.go`, `docs.go`-adjacent generated files (`swagger.json`, `swagger.yaml`)

**Step 1: Write the failing test**

Add:

```go
func TestDatabricksConnectorCRUDMasksSecrets(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("databricks-conn-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Databricks Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "DBX Prod", "type": "databricks",
		"config": map[string]interface{}{
			"host": "dbc-abc.cloud.databricks.com", "http_path": "/sql/1.0/warehouses/abc",
			"auth_type": "pat", "token": "dapi-secret", "catalog": "main", "schema": "sales",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&created)
	connID := created["id"].(string)
	cfg := created["config"].(map[string]interface{})
	if cfg["token"] != "***" {
		t.Fatalf("expected masked token, got %v", cfg["token"])
	}
	if cfg["http_path"] != "/sql/1.0/warehouses/abc" || cfg["catalog"] != "main" {
		t.Fatalf("expected non-secret fields preserved, got %v", cfg)
	}
	if _, leaked := cfg["client_secret"]; leaked {
		t.Fatal("client_secret must not appear when unset")
	}

	// Update without credentials: empty token keeps the stored secret.
	upd, _ := json.Marshal(map[string]interface{}{
		"config": map[string]interface{}{"catalog": "analytics", "token": ""},
	})
	req = httptest.NewRequest("PUT", "/api/v1/connectors/"+connID, bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var updated map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&updated)
	ucfg := updated["config"].(map[string]interface{})
	if ucfg["catalog"] != "analytics" {
		t.Fatalf("catalog not updated: %v", ucfg["catalog"])
	}
	if ucfg["token"] != "***" {
		t.Fatalf("token must stay masked: %v", ucfg["token"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestDatabricksConnectorCRUDMasksSecrets -count=1 -timeout 3m -v`
Expected: FAIL if any piece is missing; with Tasks 1–5 done this may pass already — if so, that is fine (the test pins the behavior; treat PASS as the verification).

**Step 3: Regenerate Swagger**

Run:

```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```

Expected: `internal/api/docs/docs.go`, `swagger.json`, `swagger.yaml` updated — `models.Connector.type` enum now includes `databricks` and `config` is schema-free (object).

**Step 4: Run tests + vet**

Run: `go test ./internal/api -run 'TestDatabricksConnectorCRUDMasksSecrets|TestConnectorCRUD' -count=1 -timeout 3m -v`
Expected: PASS.

Run: `go vet ./... && gofmt -l internal | head`
Expected: no output (clean).

**Step 5: Commit**

```bash
git add internal/api/connector_handlers_test.go internal/api/docs
git commit -m "test: databricks connector API round-trip and secret masking"
```

---

## Task 7: Env-gated live integration tests + run against the workspace

**Files:**
- Create: `internal/executor/databricks_live_test.go`

**Step 1: Write the tests**

```go
package executor

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Live Databricks tests are skipped unless AETHER_TEST_DATABRICKS_HOST and
// AETHER_TEST_DATABRICKS_HTTP_PATH are set. Credentials are never committed.
// When AETHER_TEST_DATABRICKS_CLIENT_ID is set, OAuth M2M is exercised instead
// of PAT.
func liveDatabricksConfig(t *testing.T) databricksConfig {
	t.Helper()
	host := os.Getenv("AETHER_TEST_DATABRICKS_HOST")
	if host == "" {
		t.Skip("AETHER_TEST_DATABRICKS_HOST not set; skipping live Databricks tests")
	}
	cfg := databricksConfig{
		Host:     host,
		HTTPPath: os.Getenv("AETHER_TEST_DATABRICKS_HTTP_PATH"),
		AuthType: "pat",
		Token:    os.Getenv("AETHER_TEST_DATABRICKS_TOKEN"),
		Catalog:  os.Getenv("AETHER_TEST_DATABRICKS_CATALOG"),
		Schema:   os.Getenv("AETHER_TEST_DATABRICKS_SCHEMA"),
	}
	if id := os.Getenv("AETHER_TEST_DATABRICKS_CLIENT_ID"); id != "" {
		cfg.AuthType = "oauth_m2m"
		cfg.ClientID = id
		cfg.ClientSecret = os.Getenv("AETHER_TEST_DATABRICKS_CLIENT_SECRET")
	}
	if cfg.HTTPPath == "" {
		t.Skip("AETHER_TEST_DATABRICKS_HTTP_PATH not set; skipping live Databricks tests")
	}
	return cfg
}

func openLiveDatabricks(t *testing.T) *DatabricksExecutor {
	t.Helper()
	cfg, err := validateDatabricksConfig(liveDatabricksConfig(t))
	if err != nil {
		t.Fatalf("invalid live config: %v", err)
	}
	exec, err := NewDatabricksExecutor(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { exec.Close() })
	return exec
}

func TestDatabricksLiveTestConnection(t *testing.T) {
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.TestConnection(ctx); err != nil {
		t.Fatalf("test connection: %v", err)
	}
}

func TestDatabricksLiveExecuteTypes(t *testing.T) {
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rs, err := exec.Execute(ctx,
		"SELECT 1 AS one, CAST('2026-01-02 03:04:05' AS TIMESTAMP) AS ts, "+
			"CAST('12.34' AS DECIMAL(10,2)) AS dec, array(1, 2, 3) AS arr",
		nil, OutputLimits{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(rs.Rows) != 1 || len(rs.Columns) != 4 {
		t.Fatalf("unexpected shape: %d cols %d rows", len(rs.Columns), len(rs.Rows))
	}
	if fmt.Sprint(rs.Rows[0][0]) != "1" {
		t.Fatalf("SELECT 1 returned %v", rs.Rows[0][0])
	}
	if _, ok := rs.Rows[0][1].(string); !ok {
		t.Fatalf("timestamp must normalize to a string, got %T", rs.Rows[0][1])
	}
	t.Logf("row: %#v", rs.Rows[0])
}

func TestDatabricksLiveSchemaAndDatabases(t *testing.T) {
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dbs, err := exec.Databases(ctx)
	if err != nil {
		t.Fatalf("databases: %v", err)
	}
	if len(dbs) == 0 {
		t.Fatal("expected at least one catalog")
	}
	t.Logf("catalogs: %v", dbs)
	schema, err := exec.Schema(ctx)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Logf("tables: %d", len(schema.Tables))
	if len(schema.Tables) > 0 {
		tbl := schema.Tables[0]
		if tbl.Schema == "" || tbl.Name == "" || len(tbl.Columns) == 0 {
			t.Fatalf("malformed table info: %+v", tbl)
		}
	}
}
```

**Step 2: Verify default behavior (skip)**

Run: `go test ./internal/executor -run TestDatabricksLive -count=1 -timeout 3m -v`
Expected: PASS with `--- SKIP` for each (env vars unset).

**Step 3: Run live against the workspace**

Obtain credentials from the workspace (user-provided; never commit). Example:

```bash
export AETHER_TEST_DATABRICKS_HOST="dbc-xxxx.cloud.databricks.com"
export AETHER_TEST_DATABRICKS_HTTP_PATH="/sql/1.0/warehouses/xxxxxxxx"
export AETHER_TEST_DATABRICKS_TOKEN="dapi..."
export AETHER_TEST_DATABRICKS_CATALOG="main"        # optional
export AETHER_TEST_DATABRICKS_SCHEMA="default"      # optional
go test ./internal/executor -run TestDatabricksLive -count=1 -timeout 3m -v
```

Expected: PASS. Then run once with the service-principal variables
(`AETHER_TEST_DATABRICKS_CLIENT_ID`/`_CLIENT_SECRET` instead of a token) to
validate OAuth M2M.

If text columns arrive as `[]byte` (base64 in JSON) in `TestDatabricksLiveExecuteTypes`,
adjust `normalizeDatabricksValue` to convert `[]byte` to `string` and re-run.

If the connection fails with `ErrReydenThriftUnsupported`, the workspace is a
Real-Time warehouse that rejects the driver's default pure-Go Thrift backend
(the driver only auto-recovers on a kernel-tagged build). Use a classic/Pro SQL
warehouse for validation and report the failure instead of working around it.

**Step 4: Commit**

```bash
git add internal/executor/databricks_live_test.go
git commit -m "test: env-gated live Databricks integration tests"
```

---

## Task 8: Frontend — Databricks form, payload builder, and component tests

**Files:**
- Modify: `web/src/types/index.ts:157-180` (Connector config)
- Modify: `web/src/pages/ConnectorsPage.tsx`
- Test: `web/src/test/ConnectorsPage.test.tsx`

**Step 1: Write the failing test**

Add to `web/src/test/ConnectorsPage.test.tsx`:

```tsx
test('creates a Databricks connector with PAT config (T-DBX)', async () => {
  let postBody: { config?: Record<string, unknown> } | null = null
  server.use(
    http.get('/api/v1/connectors', () => HttpResponse.json([])),
    http.post('/api/v1/connectors', async ({ request }) => {
      postBody = (await request.json()) as { config?: Record<string, unknown> }
      return HttpResponse.json(
        { id: 'c-dbx', name: 'DBX', type: 'databricks', config: {}, created_at: '2026-01-01T00:00:00Z' },
        { status: 201 },
      )
    }),
  )
  renderWithProviders(<ConnectorsPage />)
  fireEvent.click(await screen.findByText('+ New Connector'))
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'DBX' } })
  fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'databricks' } })
  fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'dbc-x.cloud.databricks.com' } })
  fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/sql/1.0/warehouses/abc' } })
  fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'dapi-123' } })
  fireEvent.click(screen.getByText('Create'))

  await waitFor(() =>
    expect(postBody?.config).toEqual({
      host: 'dbc-x.cloud.databricks.com',
      http_path: '/sql/1.0/warehouses/abc',
      auth_type: 'pat',
      catalog: '',
      schema: '',
      token: 'dapi-123',
    }),
  )
})

test('creates a Databricks connector with OAuth M2M config (T-DBX-2)', async () => {
  let postBody: { config?: Record<string, unknown> } | null = null
  server.use(
    http.get('/api/v1/connectors', () => HttpResponse.json([])),
    http.post('/api/v1/connectors', async ({ request }) => {
      postBody = (await request.json()) as { config?: Record<string, unknown> }
      return HttpResponse.json(
        { id: 'c-dbx2', name: 'DBX M2M', type: 'databricks', config: {}, created_at: '2026-01-01T00:00:00Z' },
        { status: 201 },
      )
    }),
  )
  renderWithProviders(<ConnectorsPage />)
  fireEvent.click(await screen.findByText('+ New Connector'))
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'DBX M2M' } })
  fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'databricks' } })
  fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'dbc-x.cloud.databricks.com' } })
  fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/sql/1.0/warehouses/abc' } })
  fireEvent.change(screen.getByLabelText('Auth Type'), { target: { value: 'oauth_m2m' } })
  fireEvent.change(screen.getByLabelText('Client ID'), { target: { value: 'sp-123' } })
  fireEvent.change(screen.getByLabelText('Client Secret'), { target: { value: 'secret-456' } })
  fireEvent.click(screen.getByText('Create'))

  await waitFor(() =>
    expect(postBody?.config).toEqual({
      host: 'dbc-x.cloud.databricks.com',
      http_path: '/sql/1.0/warehouses/abc',
      auth_type: 'oauth_m2m',
      catalog: '',
      schema: '',
      client_id: 'sp-123',
      client_secret: 'secret-456',
    }),
  )
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/ConnectorsPage.test.tsx`
Expected: FAIL — the Type dropdown has no `databricks` option and the payload is missing.

**Step 3: Implement**

`web/src/types/index.ts` — extend the config type:

```ts
  config?: {
    host?: string
    port?: number
    database?: string
    user?: string
    ssl_mode?: string
    use_tls?: boolean
    http_path?: string
    auth_type?: 'pat' | 'oauth_m2m'
    token?: string
    client_id?: string
    client_secret?: string
    catalog?: string
    schema?: string
  }
```

`web/src/pages/ConnectorsPage.tsx`:

1. Type + form interface:

```ts
type ConnectorType = 'postgres' | 'clickhouse' | 'opensearch' | 'databricks'
type DatabricksAuthType = 'pat' | 'oauth_m2m'

interface ConnectorForm {
  name: string
  type: ConnectorType
  host: string
  port: string
  database: string
  user: string
  password: string
  ssl_mode: string
  use_tls: boolean
  http_path: string
  auth_type: DatabricksAuthType
  token: string
  client_id: string
  client_secret: string
  catalog: string
  schema: string
  is_default: boolean
  timeout_seconds: string
  table_allowlist: string
  table_denylist: string
}
```

2. `defaultForm()` gains:

```ts
  http_path: '', auth_type: 'pat', token: '', client_id: '', client_secret: '',
  catalog: '', schema: '',
```

3. Payload builder + validation (module-level functions):

```ts
/** Builds the per-type config object for create (forUpdate=false) and edit
 * (forUpdate=true, secrets omitted when blank to keep the stored value). */
function buildConnectorConfig(f: ConnectorForm, forUpdate: boolean): Record<string, unknown> {
  if (f.type === 'databricks') {
    const cfg: Record<string, unknown> = {
      host: f.host,
      http_path: f.http_path,
      auth_type: f.auth_type,
      catalog: f.catalog,
      schema: f.schema,
    }
    if (f.auth_type === 'pat') {
      if (!forUpdate || f.token !== '') cfg.token = f.token
    } else {
      cfg.client_id = f.client_id
      if (!forUpdate || f.client_secret !== '') cfg.client_secret = f.client_secret
    }
    return cfg
  }
  const cfg: Record<string, unknown> = {
    host: f.host,
    port: parseInt(f.port),
    database: f.database,
    user: f.user,
    ssl_mode: f.ssl_mode,
    ...(f.type === 'opensearch' ? { use_tls: f.use_tls } : {}),
  }
  if (!forUpdate || f.password !== '') cfg.password = f.password
  return cfg
}

/** Whether the form has the fields its type requires. Credentials may stay
 * blank on edit (the server keeps the stored secret). */
function canSubmitConnector(f: ConnectorForm, forUpdate: boolean): boolean {
  if (!f.name || !f.host) return false
  if (f.type === 'postgres' && !f.database) return false
  if (f.type === 'databricks') {
    if (!f.http_path) return false
    if (f.auth_type === 'pat') return forUpdate || f.token !== ''
    return f.client_id !== '' && (forUpdate || f.client_secret !== '')
  }
  return true
}
```

4. Mutations use the builder:

```ts
// updateConnector:
      config: buildConnectorConfig(editForm, true),
// createConnector:
      config: buildConnectorConfig(form, false),
// testFormConnection:
        config: buildConnectorConfig(form, false),
```

5. Edit form initialization (`useEffect` that fills `editForm`) gains:

```ts
          http_path: c.config?.http_path ?? '',
          auth_type: (c.config?.auth_type as DatabricksAuthType) ?? 'pat',
          token: '',
          client_id: c.config?.client_id ?? '',
          client_secret: '',
          catalog: c.config?.catalog ?? '',
          schema: c.config?.schema ?? '',
```

6. Add the option to both type selects:

```tsx
                  <option value="databricks">Databricks</option>
```

and update both port-default expressions to keep their current shape (databricks has no port field, so the expression is untouched):

```tsx
                  port: e.target.value === 'clickhouse' ? '9000' : e.target.value === 'opensearch' ? '9200' : '5432',
```

7. Add a module-level `DatabricksFields` component (before `ConnectorsPage`, after `defaultForm`):

```tsx
function DatabricksFields({ form, setForm, isEdit }: {
  form: ConnectorForm
  setForm: React.Dispatch<React.SetStateAction<ConnectorForm>>
  isEdit?: boolean
}) {
  const set = (field: keyof ConnectorForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))
  return (
    <>
      <label style={styles.label}>HTTP Path
        <input style={styles.input} value={form.http_path} onChange={set('http_path')} placeholder="/sql/1.0/warehouses/…" />
      </label>
      <label style={styles.label}>Auth Type
        <select style={styles.input} value={form.auth_type} onChange={set('auth_type')}>
          <option value="pat">Personal Access Token</option>
          <option value="oauth_m2m">OAuth (Service Principal)</option>
        </select>
      </label>
      {form.auth_type === 'pat' ? (
        <label style={styles.label}>Token{isEdit ? ' (leave blank to keep current)' : ''}
          <input style={styles.input} type="password" value={form.token} onChange={set('token')} />
        </label>
      ) : (
        <>
          <label style={styles.label}>Client ID
            <input style={styles.input} value={form.client_id} onChange={set('client_id')} />
          </label>
          <label style={styles.label}>Client Secret{isEdit ? ' (leave blank to keep current)' : ''}
            <input style={styles.input} type="password" value={form.client_secret} onChange={set('client_secret')} />
          </label>
        </>
      )}
      <label style={styles.label}>Catalog
        <input style={styles.input} value={form.catalog} onChange={set('catalog')} placeholder="main (optional)" />
      </label>
      <label style={styles.label}>Schema
        <input style={styles.input} value={form.schema} onChange={set('schema')} placeholder="default (optional)" />
      </label>
    </>
  )
}
```

`styles` is declared later in the file; the component only executes at render time, after module initialization, so the reference is safe.

8. In both forms, wrap the generic DB fields in a `form.type !== 'databricks'` fragment and render `DatabricksFields` otherwise. For the create form, replace the block from the Port label through the SSL Mode block with:

```tsx
              {form.type !== 'databricks' && (
                <>
                  <label style={styles.label}>Port
                    <input style={styles.input} type="text" value={form.port} onChange={setField('port')} />
                  </label>
                  {(form.type === 'postgres' || form.type === 'clickhouse') && (
                    <label style={styles.label}>Database
                      <input style={styles.input} value={form.database} onChange={setField('database')} />
                    </label>
                  )}
                  <label style={styles.label}>User
                    <input style={styles.input} value={form.user} onChange={setField('user')} />
                  </label>
                  <label style={styles.label}>Password
                    <input style={styles.input} type="password" value={form.password} onChange={setField('password')} />
                  </label>
                  {form.type === 'opensearch' && (
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                      <input type="checkbox" checked={form.use_tls}
                        onChange={e => setForm(f => ({ ...f, use_tls: e.target.checked }))} />
                      Use TLS (HTTPS)
                    </label>
                  )}
                  {(form.type === 'postgres' || form.type === 'clickhouse') && (
                    <label style={styles.label}>SSL Mode
                      <select style={styles.input} value={form.ssl_mode} onChange={setField('ssl_mode')}>
                        <option value="disable">disable</option>
                        <option value="require">require</option>
                        <option value="verify-full">verify-full</option>
                      </select>
                    </label>
                  )}
                </>
              )}
              {form.type === 'databricks' && <DatabricksFields form={form} setForm={setForm} />}
```

For the edit form, do the same using `editForm` / `setEditForm` with inline `onChange` handlers, and pass `isEdit`:

```tsx
              {editForm.type === 'databricks' && <DatabricksFields form={editForm} setForm={setEditForm} isEdit />}
```

9. Update the subtitle and both submit/test buttons:

```tsx
            Connect to your databases (PostgreSQL, ClickHouse, OpenSearch, Databricks) to query data from notebooks.
```

Create form test button and Create button `disabled`/`title` use:

```tsx
                disabled={!canSubmitConnector(form, false) || formTesting}
                title={!canSubmitConnector(form, false) ? 'Fill in the required fields for this connector type' : undefined}
```

and for create:

```tsx
                disabled={!canSubmitConnector(form, false) || createConnector.isPending}
```

Edit form Save button:

```tsx
                disabled={!canSubmitConnector(editForm, true) || updateConnector.isPending}
```

**Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run --project=default src/test/ConnectorsPage.test.tsx`
Expected: PASS.

Run: `cd web && npx tsc --noEmit`
Expected: no errors.

**Step 5: Commit**

```bash
git add web/src/types/index.ts web/src/pages/ConnectorsPage.tsx web/src/test/ConnectorsPage.test.tsx
git commit -m "feat: Databricks connector form with PAT and OAuth M2M"
```

---

## Task 9: Frontend — SQL dialect mapping

**Files:**
- Modify: `web/src/components/Cell.tsx:214-222`
- Modify: `web/src/components/SqlEditor.tsx:1-10`

**Step 1: Implement**

`web/src/components/SqlEditor.tsx`:

```ts
import { sql, MySQL, PostgreSQL, StandardSQL } from '@codemirror/lang-sql'
```

```ts
function languageExtension(connectorType?: string) {
  if (connectorType === 'postgres') return sql({ dialect: PostgreSQL })
  if (connectorType === 'databricks') return sql({ dialect: StandardSQL })
  return sql({ dialect: MySQL })
}
```

`web/src/components/Cell.tsx` — import `StandardSQL` and add the branch:

```ts
import { sql, MySQL, PostgreSQL, StandardSQL } from '@codemirror/lang-sql'
```

```ts
  if (connType === 'databricks') return sql({ dialect: StandardSQL })
```

(Keep the ClickHouse branch before the default.)

**Step 2: Verify build + tests**

Run: `cd web && npx tsc --noEmit && npx vitest run --project=default src/components/Cell.test.tsx`
Expected: no type errors; PASS.

**Step 3: Commit**

```bash
git add web/src/components/Cell.tsx web/src/components/SqlEditor.tsx
git commit -m "feat: Databricks SQL dialect in the code editor"
```

---

## Task 10: Docs, full verification, and real-browser validation

**Files:**
- Modify: `README.md` (connector lists), `FRONTEND.md:550-556` (ConnectorsPage description)
- Verify only: everything else

**Step 1: Update docs**

Run `grep -n "OpenSearch\|ClickHouse" README.md FRONTEND.md` and add Databricks to every connector enumeration that lists supported types. Keep it factual: "Databricks (SQL warehouses, PAT or service principal OAuth)".

**Step 2: Full local CI**

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
task test:e2e
```

Expected: all pass. (`task check` runs fmt + vet + tidy + test with `-timeout 3m`.)

**Step 3: Rebuild the dev stack**

```bash
docker compose -f docker-compose.dev.yml up -d --build api web relay
docker compose -f docker-compose.dev.yml ps
```

**Step 4: Real-browser validation (mandatory per AGENTS.md)**

Using agent-browser against `http://localhost:5173`:

1. `agent-browser open http://localhost:5173/connectors`
2. `agent-browser console --clear`
3. Create a Databricks connector with the live workspace values (host, http_path, PAT) — do not leave credentials in the form after validating; the connector stays stored encrypted, so remove it afterwards if the workspace data is sensitive.
4. Click **Test Connection** → expect the green "Connected" badge.
5. Create a notebook, add a SQL cell, select the Databricks connector, run `SELECT 1` → expect one row.
6. Open the schema browser on the connector → catalogs/tables render.
7. `agent-browser errors` and `agent-browser console` → no unexpected errors.
8. Repeat the connection test with an OAuth M2M connector if a service principal is available.

**Step 5: Commit docs**

```bash
git add README.md FRONTEND.md
git commit -m "docs: document the Databricks connector"
```

**Step 6: Push and open a PR**

```bash
git push -u origin feat/databricks-connector
gh pr create --title "feat: Databricks connector (SQL warehouses)" --body "Implements docs/plans/2026-10-03-databricks-connector-design.md"
```

---

## Plan self-review checklist

- Every Go command includes `-count=1 -timeout 3m`.
- Every task has its own commit; commits are conventional.
- Live credentials are env-only and tests skip without them.
- No migration is needed (`V053` dropped the type CHECK).
- Existing connector types are regression-covered in Tasks 2–4.
