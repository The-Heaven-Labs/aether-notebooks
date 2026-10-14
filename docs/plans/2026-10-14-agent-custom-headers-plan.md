# Custom Headers for Agents Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Admins can attach custom HTTP headers (with encrypted-at-rest sensitive values) to LLM model configs and HTTP MCP servers, and webhook-tool sensitive headers become encrypted at rest instead of plaintext.

**Architecture:** A new `internal/headers` package owns the header-map semantics (validation, encrypt, decrypt, redact, restore) shared by `internal/api` (write paths) and `internal/agent` (runtime). Sensitive values are stored inline in JSONB columns as `enc:v1:`-tagged AES-GCM ciphertext produced by `internal/crypto`; API responses mask them with `__AETHER_REDACTED__`; runtime paths decrypt with the master key when building the actual HTTP request. Webhook tools keep their `config.headers` + `config.secrets` shape and upgrade lazily.

**Tech Stack:** Go (stdlib `net/http`, pgx v5, existing `crypto` package), React + TypeScript + Vitest, Postgres migration `V134`.

**Design doc:** `docs/plans/2026-10-14-agent-custom-headers-design.md`

**Prerequisites:**
- Branch `feat/agent-custom-headers` (already created; design doc committed as `30bd3951`).
- Infra up for DB-backed tests: `task infra:up` (or the full dev stack).
- All Go test commands that hit Postgres go through `bash scripts/go-test.sh ...` (resets the disposable test DB, injects `-timeout 3m`). Pure unit-test packages can run with plain `go test ... -timeout 3m`.
- Never edit an already-applied migration; `V134` is new.

---

### Task 1: Tagged string encryption in `internal/crypto`

**Files:**
- Modify: `internal/crypto/crypto.go`
- Test: `internal/crypto/crypto_test.go`

**Step 1: Write the failing tests**

Append to `internal/crypto/crypto_test.go` (add `"strings"` to its imports):

```go
func TestEncryptStringRoundTrip(t *testing.T) {
	key := crypto.DeriveKey("test-master-key-that-is-long-enough-32")

	enc, err := crypto.EncryptString("sk-secret", key)
	if err != nil {
		t.Fatalf("encrypt string: %v", err)
	}
	if !strings.HasPrefix(enc, "enc:v1:") {
		t.Fatalf("expected enc:v1: prefix, got %q", enc)
	}
	if crypto.IsEncryptedString("plain-value") {
		t.Fatal("plain value must not be reported as encrypted")
	}
	if !crypto.IsEncryptedString(enc) {
		t.Fatal("tagged value must be reported as encrypted")
	}

	dec, err := crypto.DecryptString(enc, key)
	if err != nil {
		t.Fatalf("decrypt string: %v", err)
	}
	if dec != "sk-secret" {
		t.Fatalf("expected sk-secret, got %q", dec)
	}
}

func TestDecryptStringLegacyPlaintext(t *testing.T) {
	key := crypto.DeriveKey("test-master-key-that-is-long-enough-32")

	dec, err := crypto.DecryptString("legacy-plain", key)
	if err != nil {
		t.Fatalf("decrypt legacy plaintext: %v", err)
	}
	if dec != "legacy-plain" {
		t.Fatalf("expected passthrough, got %q", dec)
	}
}

func TestDecryptStringWrongKey(t *testing.T) {
	enc, err := crypto.EncryptString("secret", crypto.DeriveKey("key-one-long-enough-for-testing-32chars"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := crypto.DecryptString(enc, crypto.DeriveKey("key-two-long-enough-for-testing-32chars")); err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/crypto/ -run 'TestEncryptString|TestDecryptString' -v -count=1 -timeout 3m
```

Expected: build failure — `crypto.EncryptString undefined`.

**Step 3: Implement**

In `internal/crypto/crypto.go`, add `"encoding/base64"` and `"strings"` to the imports, then append:

```go
// encryptedStringPrefix tags inline-encrypted string values (e.g. sensitive
// HTTP headers stored in JSON columns) so decryption can distinguish
// ciphertext from legacy plaintext.
const encryptedStringPrefix = "enc:v1:"

// EncryptString encrypts a string value for inline storage in a JSON column.
// The result carries the enc:v1: prefix so DecryptString can detect it.
func EncryptString(plaintext string, key []byte) (string, error) {
	ciphertext, err := Encrypt([]byte(plaintext), key)
	if err != nil {
		return "", err
	}
	return encryptedStringPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString decrypts a value produced by EncryptString. Values without the
// enc:v1: prefix are returned unchanged, so rows written before encryption was
// introduced keep working (lazy migration).
func DecryptString(value string, key []byte) (string, error) {
	encoded, ok := strings.CutPrefix(value, encryptedStringPrefix)
	if !ok {
		return value, nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted string: %w", err)
	}
	plaintext, err := Decrypt(ciphertext, key)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// IsEncryptedString reports whether value carries the inline-encryption tag.
func IsEncryptedString(value string) bool {
	return strings.HasPrefix(value, encryptedStringPrefix)
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/crypto/ -v -count=1 -timeout 3m
```

Expected: PASS (all existing crypto tests plus the three new ones).

**Step 5: Commit**

```bash
git add internal/crypto/crypto.go internal/crypto/crypto_test.go
git commit -m "feat(crypto): tagged string encryption for inline-stored secrets"
```

---

### Task 2: `internal/headers` package

**Files:**
- Create: `internal/headers/headers.go`
- Create: `internal/headers/headers_test.go`

**Step 1: Write the failing tests**

Create `internal/headers/headers_test.go`:

```go
package headers_test

import (
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/headers"
)

func testKey() []byte { return crypto.DeriveKey("test-master-key-that-is-long-enough-32") }

func TestValidate(t *testing.T) {
	t.Run("accepts a valid map", func(t *testing.T) {
		h := map[string]any{"X-Title": "Aether", "X-Api-Key": "secret"}
		if err := headers.Validate(h, []string{"X-Api-Key"}); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})
	t.Run("rejects reserved names", func(t *testing.T) {
		for _, name := range []string{"Host", "content-length", "Content-Type"} {
			if err := headers.Validate(map[string]any{name: "x"}, nil); err == nil {
				t.Fatalf("expected %q to be rejected", name)
			}
		}
	})
	t.Run("rejects invalid token names", func(t *testing.T) {
		if err := headers.Validate(map[string]any{"Bad Header": "x"}, nil); err == nil {
			t.Fatal("expected invalid name to be rejected")
		}
		if err := headers.Validate(map[string]any{"": "x"}, nil); err == nil {
			t.Fatal("expected empty name to be rejected")
		}
	})
	t.Run("rejects case-insensitive duplicates", func(t *testing.T) {
		h := map[string]any{"X-Api-Key": "a", "x-api-key": "b"}
		if err := headers.Validate(h, nil); err == nil {
			t.Fatal("expected duplicate names to be rejected")
		}
	})
	t.Run("rejects oversized values", func(t *testing.T) {
		h := map[string]any{"X-Big": strings.Repeat("a", 8193)}
		if err := headers.Validate(h, nil); err == nil {
			t.Fatal("expected oversized value to be rejected")
		}
	})
	t.Run("rejects secrets that are not configured", func(t *testing.T) {
		h := map[string]any{"X-Title": "Aether"}
		if err := headers.Validate(h, []string{"X-Api-Key"}); err == nil {
			t.Fatal("expected unknown secret to be rejected")
		}
	})
}

func TestEncryptDecryptSecrets(t *testing.T) {
	key := testKey()
	h := map[string]any{"X-Title": "Aether", "X-Api-Key": "supersecret"}

	enc, err := headers.EncryptSecrets(h, []string{"X-Api-Key"}, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc["X-Title"] != "Aether" {
		t.Fatalf("non-secret value must stay plaintext, got %v", enc["X-Title"])
	}
	secretVal, _ := enc["X-Api-Key"].(string)
	if !crypto.IsEncryptedString(secretVal) {
		t.Fatalf("expected tagged ciphertext, got %q", secretVal)
	}

	// Idempotent: re-encrypting a tagged value leaves it untouched.
	enc2, err := headers.EncryptSecrets(enc, []string{"X-Api-Key"}, key)
	if err != nil {
		t.Fatalf("re-encrypt: %v", err)
	}
	if enc2["X-Api-Key"] != secretVal {
		t.Fatal("already-tagged value must not be re-encrypted")
	}

	dec, err := headers.DecryptSecrets(enc, []string{"X-Api-Key"}, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec["X-Api-Key"] != "supersecret" {
		t.Fatalf("expected decrypted secret, got %q", dec["X-Api-Key"])
	}
	if dec["X-Title"] != "Aether" {
		t.Fatalf("expected plaintext passthrough, got %q", dec["X-Title"])
	}
}

func TestDecryptSecretsLegacyPlaintext(t *testing.T) {
	key := testKey()
	h := map[string]any{"X-Api-Key": "legacy-plain"}

	dec, err := headers.DecryptSecrets(h, []string{"X-Api-Key"}, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec["X-Api-Key"] != "legacy-plain" {
		t.Fatalf("legacy plaintext must pass through, got %q", dec["X-Api-Key"])
	}
}

func TestRedactSecrets(t *testing.T) {
	h := map[string]any{"X-Title": "Aether", "x-api-key": "supersecret"}
	headers.RedactSecrets(h, []string{"X-Api-Key"}) // case-insensitive match
	if h["X-Title"] != "Aether" {
		t.Fatalf("non-secret must stay visible, got %v", h["X-Title"])
	}
	if h["x-api-key"] != headers.RedactedSentinel {
		t.Fatalf("expected sentinel, got %v", h["x-api-key"])
	}
}

func TestRestoreSecrets(t *testing.T) {
	t.Run("restores sentinel when still sensitive", func(t *testing.T) {
		old := map[string]any{"X-Api-Key": "enc:v1:stored"}
		new := map[string]any{"X-Api-Key": headers.RedactedSentinel}
		headers.RestoreSecrets(old, new, []string{"X-Api-Key"})
		if new["X-Api-Key"] != "enc:v1:stored" {
			t.Fatalf("expected stored value restored, got %v", new["X-Api-Key"])
		}
	})
	t.Run("deletes sentinel when no longer sensitive", func(t *testing.T) {
		old := map[string]any{"X-Api-Key": "enc:v1:stored"}
		new := map[string]any{"X-Api-Key": headers.RedactedSentinel}
		headers.RestoreSecrets(old, new, nil)
		if _, exists := new["X-Api-Key"]; exists {
			t.Fatal("expected sentinel header removed")
		}
	})
	t.Run("leaves fresh values untouched", func(t *testing.T) {
		new := map[string]any{"X-Api-Key": "brand-new"}
		headers.RestoreSecrets(map[string]any{"X-Api-Key": "old"}, new, []string{"X-Api-Key"})
		if new["X-Api-Key"] != "brand-new" {
			t.Fatalf("expected fresh value kept, got %v", new["X-Api-Key"])
		}
	})
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/headers/ -v -count=1 -timeout 3m
```

Expected: build failure — package does not exist.

**Step 3: Implement**

Create `internal/headers/headers.go`:

```go
// Package headers implements custom HTTP header maps whose sensitive values
// are encrypted at rest, redacted in API responses, and decrypted only where a
// request is actually made. It is shared by the REST handlers (internal/api)
// and the runtime (internal/agent) so write and read semantics cannot drift.
package headers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

// RedactedSentinel is the placeholder returned to clients in place of a stored
// sensitive value. Sending it back on update keeps the stored value.
const RedactedSentinel = "__AETHER_REDACTED__"

const (
	maxHeaders     = 50
	maxNameLength  = 128
	maxValueLength = 8192
)

// tokenRe matches RFC 7230 header field names.
var tokenRe = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// reserved headers are managed by the HTTP transport or by the JSON wire
// format; overriding them can only break the request.
var reserved = map[string]bool{
	"host":           true,
	"content-length": true,
	"content-type":   true,
}

// Validate checks an incoming header map and its sensitive-name list. It is
// called on every write path before values are encrypted.
func Validate(headersIn map[string]any, secrets []string) error {
	if len(headersIn) > maxHeaders {
		return fmt.Errorf("too many headers: %d (max %d)", len(headersIn), maxHeaders)
	}
	seen := make(map[string]string, len(headersIn))
	for name, raw := range headersIn {
		if name == "" || len(name) > maxNameLength {
			return fmt.Errorf("invalid header name %q", name)
		}
		if !tokenRe.MatchString(name) {
			return fmt.Errorf("invalid header name %q (must be an RFC 7230 token)", name)
		}
		if reserved[strings.ToLower(name)] {
			return fmt.Errorf("header %q is reserved and cannot be set", name)
		}
		lower := strings.ToLower(name)
		if prev, dup := seen[lower]; dup {
			return fmt.Errorf("duplicate header %q (also configured as %q)", name, prev)
		}
		seen[lower] = name
		value, ok := raw.(string)
		if !ok {
			return fmt.Errorf("header %q value must be a string", name)
		}
		if len(value) > maxValueLength {
			return fmt.Errorf("header %q value exceeds %d characters", name, maxValueLength)
		}
	}
	for _, secret := range secrets {
		if !containsName(headersIn, secret) {
			return fmt.Errorf("sensitive header %q is not configured", secret)
		}
	}
	return nil
}

// EncryptSecrets returns a copy of the header map where every value named in
// secrets (case-insensitive) is stored as tagged ciphertext. Values already
// carrying the encryption tag are left as-is, so re-saving never
// double-encrypts and legacy plaintext rows upgrade on their next save.
func EncryptSecrets(headersIn map[string]any, secrets []string, key []byte) (map[string]any, error) {
	out := make(map[string]any, len(headersIn))
	if len(headersIn) == 0 {
		return out, nil
	}
	secretSet := toSet(secrets)
	for name, raw := range headersIn {
		value := fmt.Sprintf("%v", raw)
		if !secretSet[strings.ToLower(name)] {
			out[name] = value
			continue
		}
		if crypto.IsEncryptedString(value) {
			out[name] = value
			continue
		}
		encrypted, err := crypto.EncryptString(value, key)
		if err != nil {
			return nil, fmt.Errorf("encrypt header %q: %w", name, err)
		}
		out[name] = encrypted
	}
	return out, nil
}

// DecryptSecrets resolves a runtime header table: tagged ciphertext is
// decrypted with key, everything else passes through as plaintext.
func DecryptSecrets(headersIn map[string]any, secrets []string, key []byte) (map[string]string, error) {
	out := make(map[string]string, len(headersIn))
	secretSet := toSet(secrets)
	for name, raw := range headersIn {
		value := fmt.Sprintf("%v", raw)
		if !secretSet[strings.ToLower(name)] || !crypto.IsEncryptedString(value) {
			out[name] = value
			continue
		}
		decrypted, err := crypto.DecryptString(value, key)
		if err != nil {
			return nil, fmt.Errorf("decrypt header %q: %w", name, err)
		}
		out[name] = decrypted
	}
	return out, nil
}

// RedactSecrets replaces sensitive values with RedactedSentinel in place, for
// API responses. Ciphertext must never reach a client.
func RedactSecrets(headersIn map[string]any, secrets []string) {
	secretSet := toSet(secrets)
	for name := range headersIn {
		if secretSet[strings.ToLower(name)] {
			headersIn[name] = RedactedSentinel
		}
	}
}

// RestoreSecrets applies update semantics in place: a sentinel value is
// restored from oldHeaders when the name is still sensitive, and removed when
// it is no longer sensitive. Fresh (non-sentinel) values are untouched.
func RestoreSecrets(oldHeaders, newHeaders map[string]any, secrets []string) {
	secretSet := toSet(secrets)
	for name, raw := range newHeaders {
		value, ok := raw.(string)
		if !ok || value != RedactedSentinel {
			continue
		}
		if secretSet[strings.ToLower(name)] {
			if oldValue, exists := findName(oldHeaders, name); exists {
				newHeaders[name] = oldValue
			}
		} else {
			delete(newHeaders, name)
		}
	}
}

// NamesFromJSON normalizes a JSON-decoded secrets list ([]any of strings,
// []string, or nil) into a string slice.
func NamesFromJSON(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if name, ok := item.(string); ok {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = true
	}
	return set
}

func containsName(headersIn map[string]any, name string) bool {
	_, ok := findName(headersIn, name)
	return ok
}

func findName(headersIn map[string]any, name string) (any, bool) {
	lower := strings.ToLower(name)
	for k, v := range headersIn {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return nil, false
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/headers/ -v -count=1 -timeout 3m
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/headers/
git commit -m "feat(headers): encrypted header maps with redaction and restore"
```

---

### Task 3: Migration `V134` and model structs

**Files:**
- Create: `internal/database/migrations/V134__custom_headers.sql`
- Modify: `internal/models/agent.go:7-25` (`ModelConfig`), `internal/models/agent.go:39-49` (`MCPServerOrg`)

**Step 1: Write the migration**

Create `internal/database/migrations/V134__custom_headers.sql`:

```sql
-- Migration 134: custom headers for model configs and MCP servers.
-- Sensitive header values are stored as enc:v1:-tagged AES-GCM ciphertext
-- (internal/crypto.EncryptString); the *_secrets columns list which header
-- names are sensitive. Webhook tools keep tools.config.headers/secrets and
-- upgrade to ciphertext lazily on their next save.
ALTER TABLE model_configs
    ADD COLUMN custom_headers JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN custom_header_secrets TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE mcp_servers
    ADD COLUMN headers JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN header_secrets TEXT[] NOT NULL DEFAULT '{}';
```

**Step 2: Update the model structs**

In `internal/models/agent.go`, add to `ModelConfig` (after `DefaultParams`):

```go
	CustomHeaders          JSONMap   `json:"custom_headers,omitempty"`
	CustomHeaderSecrets    []string  `json:"custom_header_secrets,omitempty"`
```

Add to `MCPServerOrg` (after `Args`):

```go
	Headers       JSONMap  `json:"headers,omitempty"`
	HeaderSecrets []string `json:"header_secrets,omitempty"`
```

**Step 3: Verify migration applies and nothing broke**

```bash
bash scripts/go-test.sh ./internal/database/ -count=1
```

Expected: PASS. The test DB is reset and all migrations (including V134) apply cleanly.

**Step 4: Commit**

```bash
git add internal/database/migrations/V134__custom_headers.sql internal/models/agent.go
git commit -m "feat(db): custom header columns for model configs and MCP servers"
```

---

### Task 4: Model config API — store, redact, validate

**Files:**
- Modify: `internal/api/model_config_handlers.go`
- Test: `internal/api/model_config_headers_test.go` (create)

**Step 1: Write the failing test**

Create `internal/api/model_config_headers_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

```bash
bash scripts/go-test.sh ./internal/api/ -run TestModelConfigCustomHeaders -v -count=1
```

Expected: FAIL — `custom_headers` is not stored/returned (GET returns no `custom_headers`, DB has nothing).

**Step 3: Implement in `internal/api/model_config_handlers.go`**

Add the import `"github.com/the-heaven-labs/aether/internal/headers"`.

In `handleList`, change the query to include the new columns and redact before appending:

```go
	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, org_id, name, provider, base_url, model, api_key_encrypted,
			   default_params, context_window, price_per_input_token, price_per_output_token,
			   price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			   api_key_env_var, custom_headers, custom_header_secrets
		FROM model_configs WHERE org_id = $1 ORDER BY name
	`, claims.OrgID)
```

In the scan loop:

```go
	for rows.Next() {
		var c models.ModelConfig
		var defaultParams, customHeaders []byte
		var customHeaderSecrets []string
		rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &c.APIKeyEncrypted,
			&defaultParams, &c.ContextWindow, &c.PricePerInputToken, &c.PricePerOutputToken,
			&c.PricePerCacheReadToken, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
			&c.APIKeyEnvVar, &customHeaders, &customHeaderSecrets)
		json.Unmarshal(defaultParams, &c.DefaultParams)
		if customHeaders != nil {
			json.Unmarshal(customHeaders, &c.CustomHeaders)
		}
		if c.CustomHeaders == nil {
			c.CustomHeaders = models.JSONMap{}
		}
		c.CustomHeaderSecrets = customHeaderSecrets
		headers.RedactSecrets(c.CustomHeaders, c.CustomHeaderSecrets)
		allowed, _ := h.server.checkPermission(...)
		...
	}
```

In `handleGet`, same query columns / scan / redact before `writeJSON`.

In `handleCreate`, extend the request struct:

```go
	var req struct {
		Name                   string         `json:"name"`
		...
		FolderID               *string        `json:"folder_id"`
		CustomHeaders          models.JSONMap `json:"custom_headers"`
		CustomHeaderSecrets    []string       `json:"custom_header_secrets"`
	}
```

After `decodeJSON` and before the INSERT:

```go
	if err := headers.Validate(req.CustomHeaders, req.CustomHeaderSecrets); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	encryptedHeaders, err := headers.EncryptSecrets(req.CustomHeaders, req.CustomHeaderSecrets, h.server.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt custom headers")
		return
	}
	customHeadersJSON, _ := json.Marshal(encryptedHeaders)
```

Extend the INSERT (columns and `$16, $17`):

```sql
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, price_per_input_token, price_per_output_token,
			price_per_cache_read_token, folder_id, created_by, created_at, updated_at,
			api_key_env_var, custom_headers, custom_header_secrets)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW(), $15, $16, $17)
```

with args `..., apiKeyEnvVar, customHeadersJSON, req.CustomHeaderSecrets`.

Note: the existing `var err error` in the handler conflicts with `encryptedHeaders, err :=`; use `encryptedHeaders, encErr :=` or reuse `err`. Preserve the existing env-var key flow exactly.

In `handleUpdate`, extend the request struct with pointers:

```go
		CustomHeaders          *models.JSONMap `json:"custom_headers"`
		CustomHeaderSecrets    *[]string       `json:"custom_header_secrets"`
```

Before the final UPDATE:

```go
	var customHeadersJSON []byte
	var customHeaderSecrets []string
	if req.CustomHeaders != nil {
		secrets := []string{}
		if req.CustomHeaderSecrets != nil {
			secrets = *req.CustomHeaderSecrets
		}
		if err := headers.Validate(*req.CustomHeaders, secrets); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Sentinel values mean "keep the stored secret".
		var oldRaw []byte
		if err := h.server.db.Pool.QueryRow(r.Context(),
			`SELECT custom_headers FROM model_configs WHERE id = $1 AND org_id = $2`,
			cfgID, claims.OrgID).Scan(&oldRaw); err == nil && len(oldRaw) > 0 {
			var old models.JSONMap
			if err := json.Unmarshal(oldRaw, &old); err == nil {
				headers.RestoreSecrets(old, *req.CustomHeaders, secrets)
			}
		}
		encrypted, encErr := headers.EncryptSecrets(*req.CustomHeaders, secrets, h.server.masterKey)
		if encErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt custom headers")
			return
		}
		customHeadersJSON, _ = json.Marshal(encrypted)
		customHeaderSecrets = secrets
	}
```

IMPORTANT: `customHeadersJSON` is `nil` when the request omits `custom_headers` — that must stay SQL NULL so `COALESCE` preserves the existing value. Do NOT `json.Marshal` a nil pointer. Extend the UPDATE:

```sql
		UPDATE model_configs SET
			name = COALESCE($2, name),
			provider = COALESCE($3, provider),
			base_url = COALESCE($4, base_url),
			model = COALESCE($5, model),
			default_params = COALESCE($6, default_params),
			context_window = COALESCE($7, context_window),
			price_per_input_token = COALESCE($8, price_per_input_token),
			price_per_output_token = COALESCE($9, price_per_output_token),
			price_per_cache_read_token = COALESCE($10, price_per_cache_read_token),
			custom_headers = COALESCE($11, custom_headers),
			custom_header_secrets = COALESCE($12, custom_header_secrets),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $13
```

with args `..., req.PricePerCacheReadToken, customHeadersJSON, customHeaderSecrets, claims.OrgID`. When the request omits the headers, `customHeaderSecrets` defaults to `nil` (SQL NULL) — but if `customHeadersJSON` is non-nil, `customHeaderSecrets` was set to `secrets` (possibly `[]string{}`), which pgx encodes as an empty array, not NULL.

**Step 4: Run test to verify it passes**

```bash
bash scripts/go-test.sh ./internal/api/ -run TestModelConfigCustomHeaders -v -count=1
```

Expected: PASS.

Also run the existing model-config permission tests to catch regressions:

```bash
bash scripts/go-test.sh ./internal/api/ -run 'TestModelConfig|TestPermissionsAudit_ModelConfig' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/model_config_handlers.go internal/api/model_config_headers_test.go
git commit -m "feat(api): custom headers on model configs"
```

---

### Task 5: `LLMClient` sends custom headers

**Files:**
- Modify: `internal/agent/llm.go`
- Test: `internal/agent/llm_headers_test.go` (create)
- Modify (mechanical call-site updates): `internal/agent/engine.go`, `internal/api/model_config_handlers.go`, and test files listed below

**Step 1: Write the failing test**

Create `internal/agent/llm_headers_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

func testMasterKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestLLMClientSendsCustomHeaders(t *testing.T) {
	masterKey := testMasterKey()
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"X-Title": "Aether", "X-Api-Key": secret}, []string{"X-Api-Key"})

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title header, got %q", got.Get("X-Title"))
	}
	if got.Get("X-Api-Key") != "topsecret" {
		t.Fatalf("expected decrypted secret header, got %q", got.Get("X-Api-Key"))
	}
	if got.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("expected bearer auth, got %q", got.Get("Authorization"))
	}
	if got.Get("Content-Type") != "application/json" {
		t.Fatalf("expected JSON content type, got %q", got.Get("Content-Type"))
	}
}

func TestLLMClientCustomHeaderOverridesAuthorization(t *testing.T) {
	masterKey := testMasterKey()
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"Authorization": "Custom gateway-auth"}, nil)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got.Get("Authorization") != "Custom gateway-auth" {
		t.Fatalf("expected custom Authorization to win, got %q", got.Get("Authorization"))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go build ./internal/agent/ 2>&1 | head -5
```

Expected: compile error — `NewLLMClient` takes 4 arguments, not 6.

**Step 3: Implement in `internal/agent/llm.go`**

Add the import `"github.com/the-heaven-labs/aether/internal/headers"` and extend the client:

```go
type LLMClient struct {
	baseURL       string
	model         string
	apiKey        []byte
	defaultParams map[string]any
	customHeaders map[string]any
	headerSecrets []string
	httpClient    *http.Client
}

func NewLLMClient(baseURL, model string, apiKey []byte, defaultParams map[string]any, customHeaders map[string]any, headerSecrets []string) *LLMClient {
	return &LLMClient{
		baseURL:       baseURL,
		model:         model,
		apiKey:        apiKey,
		defaultParams: defaultParams,
		customHeaders: customHeaders,
		headerSecrets: headerSecrets,
		httpClient:    &http.Client{},
	}
}
```

In `Chat`, after `decryptedKey` is obtained (before the retry loop):

```go
	customHeaders, err := headers.DecryptSecrets(c.customHeaders, c.headerSecrets, masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt custom headers: %w", err)
	}
```

Inside the retry loop, after the two built-in `req.Header.Set` calls:

```go
		// Custom headers are applied last so they may intentionally override
		// Authorization for gateways with custom auth schemes. Host,
		// Content-Length and Content-Type are rejected at write time.
		for name, value := range customHeaders {
			req.Header.Set(name, value)
		}
```

In `ChatStream`, after `decryptedKey` is obtained:

```go
	customHeaders, err := headers.DecryptSecrets(c.customHeaders, c.headerSecrets, masterKey)
	if err != nil {
		return fmt.Errorf("decrypt custom headers: %w", err)
	}
```

and after the two built-in `req.Header.Set` calls:

```go
	for name, value := range customHeaders {
		req.Header.Set(name, value)
	}
```

**Step 4: Update every `NewLLMClient` call site**

Run:

```bash
grep -rn "NewLLMClient(" --include="*.go" internal/ | grep -v "func NewLLMClient"
```

Add `, nil, nil` before the closing paren of every existing call (the two new params), except tests that intend to set headers.

Production sites (set from task data; for now pass `nil, nil` — Task 6 wires the real values):
- `internal/agent/engine.go:581`, `:1582`, `:1782`, `:1876`
- `internal/api/model_config_handlers.go:383`

Test sites (pass `nil, nil`):
- `internal/agent/engine_tools_test.go:654,722,790,879,944,1033,1108`
- `internal/agent/engine_retry_test.go:39,121,196,309,417`
- `internal/agent/engine_chat_ux_test.go:64,141,233,270,365,407,790`
- `internal/agent/session_usage_test.go:91,293` (line 293 uses the `agent.` prefix)
- `internal/agent/warehouse_identity_test.go:830`

Then compile:

```bash
go build ./... && go vet ./internal/agent/ ./internal/api/ ./internal/headers/ ./internal/crypto/
```

Expected: clean.

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/agent/ -run 'TestLLMClient' -v -count=1 -timeout 3m
```

Expected: PASS. (These two tests use httptest only and need no DB.)

Then run the existing agent suite touched by the signature change:

```bash
bash scripts/go-test.sh ./internal/agent/ -run 'TestProcessMessage|TestTool|TestRetry' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/agent/ internal/api/model_config_handlers.go
git commit -m "feat(agent): send custom headers on LLM requests"
```

---

### Task 6: Wire stored custom headers through every LLM path

**Files:**
- Modify: `internal/agent/engine.go` (4 model-config SELECTs; around lines 561, 1572, 1774, 1870)
- Modify: `internal/api/model_config_handlers.go` (`handleTest`, around line 371)
- Test: `internal/agent/engine_model_headers_test.go` (create)
- Test: `internal/api/model_config_headers_test.go` (append one test)

**Step 1: Write the failing tests**

Create `internal/agent/engine_model_headers_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

func TestProcessMessageSendsModelConfigCustomHeaders(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	masterKey := testMasterKey()

	var gotHeaders http.Header
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "done"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
		})
	}))
	defer llmSrv.Close()

	encKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)
	customHeaders, _ := json.Marshal(map[string]any{"X-Title": "Aether", "X-Api-Key": secret})

	mcID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, custom_headers, custom_header_secrets, created_by, created_at, updated_at)
		VALUES ($1,$2,'HeaderModel','openai',$3,'gpt-4',$4,'{}',128000,$5,ARRAY['X-Api-Key'],$6,NOW(),NOW())
	`, mcID, orgID, llmSrv.URL, encKey, customHeaders, userID); err != nil {
		t.Fatalf("create model config: %v", err)
	}

	agentID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, model_config_id, created_by, created_at, updated_at)
		VALUES ($1,$2,'HeaderAgent','','you are helpful','{}','{}',NULL,10,$3,$4,NOW(),NOW())
	`, agentID, orgID, mcID, userID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	_, _ = db.Pool.Exec(context.Background(), `INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions) VALUES ($1,'agent',$2,'user',$3, ARRAY['view','edit','delete']) ON CONFLICT DO NOTHING`, orgID, agentID, userID)

	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool, nil)

	if _, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if gotHeaders.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title on LLM request, got %q", gotHeaders.Get("X-Title"))
	}
	if gotHeaders.Get("X-Api-Key") != "topsecret" {
		t.Fatalf("expected decrypted X-Api-Key on LLM request, got %q", gotHeaders.Get("X-Api-Key"))
	}
}
```

Append to `internal/api/model_config_headers_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

```bash
bash scripts/go-test.sh ./internal/agent/ -run TestProcessMessageSendsModelConfigCustomHeaders -v -count=1
bash scripts/go-test.sh ./internal/api/ -run TestModelConfigTestEndpointSendsCustomHeaders -v -count=1
```

Expected: both FAIL — headers are not sent (SELECTs don't load the columns yet).

**Step 3: Implement the four engine SELECTs**

Each site follows the same pattern. Example — `ProcessMessage` (around line 561):

```go
		var mc models.ModelConfig
		var defaultParams, customHeaders []byte
		var customHeaderSecrets []string
		err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at, custom_headers, custom_header_secrets FROM model_configs WHERE id = $1`, modelConfigID).Scan(
			&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt, &customHeaders, &customHeaderSecrets)
		if err != nil {
			return "", "", nil, events, nil, fmt.Errorf("get model config: %w", err)
		}
		if defaultParams != nil {
			json.Unmarshal(defaultParams, &mc.DefaultParams)
		}
		if customHeaders != nil {
			json.Unmarshal(customHeaders, &mc.CustomHeaders)
		}
		mc.CustomHeaderSecrets = customHeaderSecrets
```

and replace the client construction with:

```go
		llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted, mc.DefaultParams, mc.CustomHeaders, mc.CustomHeaderSecrets)
```

Apply the same edit to:
- `defaultSubagentLLM` (around line 1572; SELECT `id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window` → append `custom_headers, custom_header_secrets`; note this query has no `mc.FolderID` scan).
- `summarizeSession` (around line 1774; same columns as ProcessMessage).
- `generateSessionTitle` (around line 1870; same columns as ProcessMessage).

**Step 4: Implement the model-config test endpoint**

In `handleTest`, extend the SELECT with `custom_headers, custom_header_secrets`, scan into `customHeadersRaw []byte` / `customHeaderSecrets []string`, unmarshal into `mc.CustomHeaders` when non-nil, then:

```go
	llmClient := agent.NewLLMClient(mc.BaseURL, mc.Model, apiKeyEncrypted, mc.DefaultParams, mc.CustomHeaders, mc.CustomHeaderSecrets)
```

**Step 5: Run tests to verify they pass**

```bash
bash scripts/go-test.sh ./internal/agent/ -run TestProcessMessageSendsModelConfigCustomHeaders -v -count=1
bash scripts/go-test.sh ./internal/api/ -run 'TestModelConfig' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/agent/engine.go internal/api/model_config_handlers.go internal/agent/engine_model_headers_test.go internal/api/model_config_headers_test.go
git commit -m "feat(agent): use model config custom headers in every LLM path"
```

---

### Task 7: MCP server API — store, redact, validate, test

**Files:**
- Modify: `internal/api/mcp_server_handlers.go`
- Test: `internal/api/mcp_server_headers_test.go` (create)

**Step 1: Write the failing test**

Create `internal/api/mcp_server_headers_test.go`:

```go
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

func TestMCPServerCustomHeaders(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-headers-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MCP Headers Org")

	var mcpID string
	t.Run("create with headers", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":    "Header MCP",
			"type":    "http",
			"command": "https://mcp.example.com",
			"headers": map[string]string{
				"Authorization": "Bearer mcp-token",
				"X-Env":         "prod",
			},
			"header_secrets": []string{"Authorization"},
		})
		req := httptest.NewRequest("POST", "/api/v1/mcp-servers", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		mcpID = resp["id"]
	})

	t.Run("secret is ciphertext at rest", func(t *testing.T) {
		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&raw); err != nil {
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
	})

	t.Run("get redacts the secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/mcp-servers/"+mcpID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var s map[string]any
		json.NewDecoder(rec.Body).Decode(&s)
		hdrs, _ := s["headers"].(map[string]any)
		if hdrs["X-Env"] != "prod" {
			t.Fatalf("expected visible X-Env, got %v", hdrs["X-Env"])
		}
		if hdrs["Authorization"] != "__AETHER_REDACTED__" {
			t.Fatalf("expected redacted Authorization, got %v", hdrs["Authorization"])
		}
	})

	t.Run("update with sentinel keeps the stored secret", func(t *testing.T) {
		var before []byte
		srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&before)

		body, _ := json.Marshal(map[string]any{
			"headers":        map[string]string{"Authorization": "__AETHER_REDACTED__", "X-Env": "staging"},
			"header_secrets": []string{"Authorization"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/mcp-servers/"+mcpID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var after []byte
		srv.DB().Pool.QueryRow(context.Background(),
			`SELECT headers FROM mcp_servers WHERE id = $1`, mcpID).Scan(&after)
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

	t.Run("rejects reserved header", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name": "Bad MCP", "type": "http", "command": "https://x.example.com",
			"headers": map[string]string{"Content-Type": "text/plain"},
		})
		req := httptest.NewRequest("POST", "/api/v1/mcp-servers", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for reserved header, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("test endpoint sends decrypted headers", func(t *testing.T) {
		var gotAuth string
		probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer probe.Close()

		body, _ := json.Marshal(map[string]any{
			"name": "Probe MCP", "type": "http", "command": probe.URL,
			"headers":        map[string]string{"Authorization": "Bearer probe-token"},
			"header_secrets": []string{"Authorization"},
		})
		req := httptest.NewRequest("POST", "/api/v1/mcp-servers", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create probe: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var created map[string]string
		json.NewDecoder(rec.Body).Decode(&created)

		testReq := httptest.NewRequest("POST", "/api/v1/mcp-servers/"+created["id"]+"/test", nil)
		testReq.Header.Set("Authorization", "Bearer "+token)
		testRec := httptest.NewRecorder()
		srv.ServeHTTP(testRec, testReq)
		if testRec.Code != http.StatusOK {
			t.Fatalf("test endpoint: expected 200, got %d: %s", testRec.Code, testRec.Body.String())
		}
		if gotAuth != "Bearer probe-token" {
			t.Fatalf("expected decrypted Authorization on probe, got %q", gotAuth)
		}
	})
}
```

**Step 2: Run test to verify it fails**

```bash
bash scripts/go-test.sh ./internal/api/ -run TestMCPServerCustomHeaders -v -count=1
```

Expected: FAIL — headers are not stored/returned/sent.

**Step 3: Implement in `internal/api/mcp_server_handlers.go`**

Import `"github.com/the-heaven-labs/aether/internal/headers"` and `"encoding/json"` if not already imported.

`handleList`: query adds `headers, header_secrets`; scan into `headersRaw []byte` + `secrets []string`; `json.Unmarshal` when non-nil; default to `models.JSONMap{}`; `headers.RedactSecrets(s.Headers, s.HeaderSecrets)` before append.

`handleGet`: same additions.

`handleCreate`: request gains

```go
		Headers       models.JSONMap `json:"headers"`
		HeaderSecrets []string       `json:"header_secrets"`
```

After the type checks:

```go
	if err := headers.Validate(req.Headers, req.HeaderSecrets); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	encryptedHeaders, err := headers.EncryptSecrets(req.Headers, req.HeaderSecrets, h.server.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt headers")
		return
	}
	headersJSON, _ := json.Marshal(encryptedHeaders)
```

INSERT gains `headers, header_secrets` columns (`$8, $9`).

`handleUpdate`: request gains pointers

```go
		Headers       *models.JSONMap `json:"headers"`
		HeaderSecrets *[]string       `json:"header_secrets"`
```

Before the UPDATE, mirror the model-config logic (validate → load old `headers` → `headers.RestoreSecrets` → `headers.EncryptSecrets` → marshal), then:

```sql
		UPDATE mcp_servers SET
			name = COALESCE($2, name),
			type = COALESCE($3, type),
			command = COALESCE($4, command),
			args = COALESCE($5, args),
			headers = COALESCE($6, headers),
			header_secrets = COALESCE($7, header_secrets),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $8
```

`handleTestMCPServer`: add `headers, header_secrets` to the SELECT; when `serverType == "http"`, decrypt and apply:

```go
	if serverType == "http" {
		var headersRaw []byte
		var secretNames []string
		// (scan these in the SELECT above)
		var stored models.JSONMap
		if headersRaw != nil {
			json.Unmarshal(headersRaw, &stored)
		}
		plain, err := headers.DecryptSecrets(stored, secretNames, h.server.masterKey)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest(http.MethodGet, command, nil)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		for name, value := range plain {
			req.Header.Set(name, value)
		}
		resp, err := client.Do(req)
		...
	}
```

(keep the existing response shape/flow; only the request construction changes).

**Step 4: Run tests to verify they pass**

```bash
bash scripts/go-test.sh ./internal/api/ -run 'TestMCPServer' -count=1
```

Expected: PASS (new tests plus the existing `TestMCPServerCRUD`).

**Step 5: Commit**

```bash
git add internal/api/mcp_server_handlers.go internal/api/mcp_server_headers_test.go
git commit -m "feat(api): custom headers on MCP servers"
```

---

### Task 8: MCP runtime applies custom headers

**Files:**
- Modify: `internal/agent/tools_mcp.go`
- Modify: `internal/agent/engine.go` (MCP SELECT around line 483; handler build around line 845)
- Test: `internal/agent/tools_mcp_headers_test.go` (create)

**Step 1: Write the failing test**

Create `internal/agent/tools_mcp_headers_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPHTTPHandlersSendHeaders(t *testing.T) {
	var listHeaders, callHeaders, callContentType http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tools/list":
			listHeaders = r.Header.Clone()
		case "/tools/call":
			callHeaders = r.Header.Clone()
			callContentType = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	hdrs := map[string]string{"Authorization": "Bearer mcp-token", "X-Env": "prod"}
	toolCtx := &ToolContext{Context: context.Background()}

	if _, err := makeMCPToolListHandlerHTTP(srv.URL, hdrs)(nil, toolCtx); err != nil {
		t.Fatalf("list handler: %v", err)
	}
	if listHeaders.Get("Authorization") != "Bearer mcp-token" || listHeaders.Get("X-Env") != "prod" {
		t.Fatalf("list request missing headers: %v", listHeaders)
	}

	if _, err := makeMCPToolCallHandlerHTTP(srv.URL, hdrs)(json.RawMessage(`{"tool":"weather","arguments":{}}`), toolCtx); err != nil {
		t.Fatalf("call handler: %v", err)
	}
	if callHeaders.Get("Authorization") != "Bearer mcp-token" {
		t.Fatalf("call request missing Authorization: %v", callHeaders)
	}
	if callContentType.Get("Content-Type") != "application/json" {
		t.Fatalf("call request must keep JSON content type, got %q", callContentType.Get("Content-Type"))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/agent/ -run TestMCPHTTPHandlersSendHeaders -v -count=1 -timeout 3m
```

Expected: build failure — `makeMCPToolListHandlerHTTP` takes 1 argument.

**Step 3: Implement in `internal/agent/tools_mcp.go`**

Change both HTTP handler factories to accept the headers and apply them:

```go
func makeMCPToolListHandlerHTTP(url string, headers map[string]string) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		...
		req, err := http.NewRequestWithContext(httpCtx, "GET", url+"/tools/list", nil)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		...
	}
}
```

and for `makeMCPToolCallHandlerHTTP`, apply the headers after `req2.Header.Set("Content-Type", "application/json")`.

Also add a `Headers map[string]string` field to the `MCPClient` struct and apply it in `makeMCPToolListHandler(srv)` / `makeMCPToolCallHandler(srv)` with the same loop (keeps the struct-based variants coherent even though they are currently unreferenced).

**Step 4: Wire the engine**

Around engine.go:483, extend the query and scan:

```go
	mcpRows, err := e.pool.Query(ctx, `
		SELECT ms.id, ms.org_id, ms.name, ms.type, ms.command, ms.args, ms.created_by, ms.created_at, ms.updated_at, ms.headers, ms.header_secrets
		FROM agent_mcp_servers ams
		JOIN mcp_servers ms ON ms.id = ams.mcp_server_id
		WHERE ams.agent_id = $1
		ORDER BY ms.name
	`, session.AgentID)
	...
			var args, headersRaw []byte
			var headerSecrets []string
			if err := mcpRows.Scan(&s.ID, &s.OrgID, &s.Name, &s.Type, &s.Command, &args, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt, &headersRaw, &headerSecrets); err == nil {
				if args != nil {
					json.Unmarshal(args, &s.Args)
				}
				if headersRaw != nil {
					json.Unmarshal(headersRaw, &s.Headers)
				}
				s.HeaderSecrets = headerSecrets
				...
			}
```

Around engine.go:843, when building tool defs:

```go
		if ms.Type == "http" {
			plainHeaders, err := headers.DecryptSecrets(ms.Headers, ms.HeaderSecrets, masterKey)
			if err != nil {
				slog.Warn("engine: failed to decrypt MCP server headers", "server", ms.Name, "session_id", sessionID, "error", err)
				continue
			}
			allTools = append(allTools,
				mcpListToolDef(ms.Name, makeMCPToolListHandlerHTTP(ms.Command, plainHeaders)),
				mcpCallToolDef(ms.Name, makeMCPToolCallHandlerHTTP(ms.Command, plainHeaders)),
			)
		}
```

Add the `headers` import to engine.go. Verify `masterKey` is in scope at the handler-build site (it is — a `ProcessMessage` parameter).

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/agent/ -run TestMCP -v -count=1 -timeout 3m
bash scripts/go-test.sh ./internal/agent/ -run 'TestProcessMessage|TestMCP' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/agent/tools_mcp.go internal/agent/engine.go internal/agent/tools_mcp_headers_test.go
git commit -m "feat(agent): send custom headers on MCP requests"
```

---

### Task 9: Encrypt webhook header secrets at rest (write path)

**Files:**
- Modify: `internal/api/tool_handlers.go`
- Test: `internal/api/tool_headers_encryption_test.go` (create)

**Step 1: Write the failing test**

Create `internal/api/tool_headers_encryption_test.go`:

```go
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

func createWebhookTool(t *testing.T, srv interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, token string, config map[string]any) string {
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
		var raw []byte
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT config FROM tools WHERE id = $1`, toolID).Scan(&raw); err != nil {
			t.Fatalf("read config: %v", err)
		}
		if strings.Contains(string(raw), "supersecret") {
			t.Fatalf("plaintext secret found in DB: %s", raw)
		}
		var cfg map[string]any
		json.Unmarshal(raw, &cfg)
		hdrs, _ := cfg["headers"].(map[string]any)
		if hdrs["X-Mode"] != "live" {
			t.Fatalf("expected plaintext X-Mode, got %v", hdrs["X-Mode"])
		}
		secret, _ := hdrs["X-Api-Key"].(string)
		if !strings.HasPrefix(secret, "enc:v1:") {
			t.Fatalf("expected enc:v1: ciphertext, got %q", secret)
		}
	})

	t.Run("get redacts the secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/tools/"+toolID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
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
		var before []byte
		srv.DB().Pool.QueryRow(context.Background(), `SELECT config FROM tools WHERE id = $1`, toolID).Scan(&before)

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
		var after []byte
		srv.DB().Pool.QueryRow(context.Background(), `SELECT config FROM tools WHERE id = $1`, toolID).Scan(&after)
		var beforeCfg, afterCfg map[string]any
		json.Unmarshal(before, &beforeCfg)
		json.Unmarshal(after, &afterCfg)
		beforeHdrs, _ := beforeCfg["headers"].(map[string]any)
		afterHdrs, _ := afterCfg["headers"].(map[string]any)
		if afterHdrs["X-Api-Key"] != beforeHdrs["X-Api-Key"] {
			t.Fatal("sentinel update must keep the stored ciphertext unchanged")
		}
		if afterHdrs["X-Mode"] != "staging" {
			t.Fatalf("expected updated X-Mode, got %v", afterHdrs["X-Mode"])
		}
	})

	t.Run("legacy plaintext rows are redacted and upgraded on save", func(t *testing.T) {
		legacyID := createWebhookTool(t, srv, token, map[string]any{
			"url":     "https://example.com/hook",
			"method":  "POST",
			"headers": map[string]string{"X-Api-Key": "legacy-secret"},
			"secrets": []string{"X-Api-Key"},
		})
		// Simulate a pre-encryption row.
		if _, err := srv.DB().Pool.Exec(context.Background(),
			`UPDATE tools SET config = '{"url":"https://example.com/hook","method":"POST","headers":{"X-Api-Key":"legacy-secret"},"secrets":["X-Api-Key"]}' WHERE id = $1`,
			legacyID); err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/v1/tools/"+legacyID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
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
		var raw []byte
		srv.DB().Pool.QueryRow(context.Background(), `SELECT config FROM tools WHERE id = $1`, legacyID).Scan(&raw)
		if strings.Contains(string(raw), "legacy-secret") {
			t.Fatalf("legacy secret must be encrypted after save: %s", raw)
		}
		if !strings.Contains(string(raw), "enc:v1:") {
			t.Fatalf("expected ciphertext after save: %s", raw)
		}
	})
}
```

**Step 2: Run test to verify it fails**

```bash
bash scripts/go-test.sh ./internal/api/ -run TestWebhookHeaderSecretsEncryptedAtRest -v -count=1
```

Expected: FAIL — `supersecret` is stored in plaintext.

**Step 3: Implement in `internal/api/tool_handlers.go`**

Add the import `"github.com/the-heaven-labs/aether/internal/headers"`.

Replace `redactToolConfig` and delete `restoreSecrets` + `redactedSentinel`:

```go
func redactToolConfig(t *models.Tool) {
	if t.Type != "webhook" {
		return
	}
	rawHeaders, _ := t.Config["headers"].(map[string]any)
	headers.RedactSecrets(rawHeaders, headers.NamesFromJSON(t.Config["secrets"]))
}
```

Add a helper that validates and encrypts in one place:

```go
// encryptWebhookHeaderSecrets validates the header map and encrypts values
// listed in config.secrets before storage. Values already tagged by
// crypto.EncryptString are left untouched, so re-saving never double-encrypts
// and legacy plaintext rows upgrade on their next save.
func (h *toolHandlers) encryptWebhookHeaderSecrets(cfg models.JSONMap) error {
	if cfg == nil {
		return nil
	}
	rawHeaders, ok := cfg["headers"].(map[string]any)
	if !ok || len(rawHeaders) == 0 {
		return nil
	}
	if err := headers.Validate(rawHeaders, headers.NamesFromJSON(cfg["secrets"])); err != nil {
		return err
	}
	encrypted, err := headers.EncryptSecrets(rawHeaders, headers.NamesFromJSON(cfg["secrets"]), h.server.masterKey)
	if err != nil {
		return err
	}
	cfg["headers"] = encrypted
	return nil
}
```

To return 400 for validation errors and 500 for encryption errors, split into two calls in the handlers instead: `headers.Validate(rawHeaders, secrets)` → 400; then `EncryptSecrets` → 500. (Pick this split form; the helper above is the shape, adjusted at the call site.)

In `handleCreate`, after `req.Config` nil-initialization (only when `req.Type == "webhook"`):

```go
	if req.Type == "webhook" {
		rawHeaders, _ := req.Config["headers"].(map[string]any)
		secrets := headers.NamesFromJSON(req.Config["secrets"])
		if err := headers.Validate(rawHeaders, secrets); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		encrypted, encErr := headers.EncryptSecrets(rawHeaders, secrets, h.server.masterKey)
		if encErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt header secrets")
			return
		}
		req.Config["headers"] = encrypted
	}
```

In `handleUpdate`, replace the `restoreSecrets(oldConfig, *req.Config)` block with:

```go
	if req.Config != nil {
		var oldConfigRaw []byte
		err := h.server.db.Pool.QueryRow(r.Context(), `SELECT config FROM tools WHERE id = $1 AND org_id = $2`, toolID, claims.OrgID).Scan(&oldConfigRaw)
		if err == nil && len(oldConfigRaw) > 0 {
			var oldConfig models.JSONMap
			json.Unmarshal(oldConfigRaw, &oldConfig)
			oldHeaders, _ := oldConfig["headers"].(map[string]any)
			newHeaders, _ := (*req.Config)["headers"].(map[string]any)
			secrets := headers.NamesFromJSON((*req.Config)["secrets"])
			headers.RestoreSecrets(oldHeaders, newHeaders, secrets)
		}
		if rawHeaders, ok := (*req.Config)["headers"].(map[string]any); ok {
			secrets := headers.NamesFromJSON((*req.Config)["secrets"])
			if err := headers.Validate(rawHeaders, secrets); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			encrypted, encErr := headers.EncryptSecrets(rawHeaders, secrets, h.server.masterKey)
			if encErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to encrypt header secrets")
				return
			}
			(*req.Config)["headers"] = encrypted
		}
	}
```

Note the subtlety: `RestoreSecrets` mutates the map returned by the type assertion (`newHeaders` aliases into `*req.Config`), which is what makes the subsequent encrypt-and-assign see the restored values.

**Step 4: Run tests to verify they pass**

```bash
bash scripts/go-test.sh ./internal/api/ -run 'TestWebhookHeaderSecretsEncryptedAtRest|TestToolCRUD' -count=1
```

Expected: PASS. `TestToolCRUD` proves the refactor preserved existing create/list/update/delete behavior.

**Step 5: Commit**

```bash
git add internal/api/tool_handlers.go internal/api/tool_headers_encryption_test.go
git commit -m "fix(api): encrypt webhook header secrets at rest"
```

---

### Task 10: Decrypt webhook secrets at runtime and in the tool test

**Files:**
- Modify: `internal/agent/engine.go` (`loadAgentToolDefs` around line 1410; caller at line 512)
- Modify: `internal/api/tool_handlers.go` (`handleTest`, around line 318)
- Test: `internal/agent/tools_webhook_headers_test.go` (create)
- Test: update `loadAgentToolDefs` call sites in `internal/agent/engine_tools_test.go` (lines 126, 160, 190, 221, 252, 277, 311, 824, 835)

**Step 1: Write the failing tests**

Create `internal/agent/tools_webhook_headers_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
)

func TestLoadAgentToolDefsDecryptsWebhookSecrets(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	engine.SetToolAllowedDomains([]string{"127.0.0.1"})
	masterKey := testMasterKey()

	var gotSecret, gotLegacy string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Api-Key")
		gotLegacy = r.Header.Get("X-Legacy")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer hook.Close()

	secret, _ := crypto.EncryptString("supersecret", masterKey)
	encryptedConfig, _ := json.Marshal(map[string]any{
		"url": hook.URL, "method": "POST",
		"headers": map[string]any{"X-Api-Key": secret, "X-Legacy": "legacy-plain"},
		"secrets": []string{"X-Api-Key", "X-Legacy"},
	})
	legacyConfig, _ := json.Marshal(map[string]any{
		"url": hook.URL, "method": "POST",
		"headers": map[string]any{"X-Legacy": "legacy-plain"},
		"secrets": []string{"X-Legacy"},
	})

	toolA := uuid.New().String()
	toolB := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO tools (id, org_id, name, description, type, schema, config, require_confirmation, created_by, created_at, updated_at)
		VALUES ($1,$2,'Encrypted Hook','','webhook','{}',$3,false,$4,NOW(),NOW()),
		       ($5,$2,'Legacy Hook','','webhook','{}',$6,false,$4,NOW(),NOW())
	`, toolA, orgID, encryptedConfig, userID, toolB, legacyConfig)
	if err != nil {
		t.Fatalf("insert tools: %v", err)
	}

	agent := models.Agent{ID: uuid.New().String(), OrgID: orgID, ToolIDs: []string{toolA, toolB}}
	defs := engine.loadAgentToolDefs(context.Background(), agent, masterKey)
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs, got %d", len(defs))
	}

	// Invoke the encrypted tool's handler and assert the wire values.
	for _, def := range defs {
		if def.Function.Name == "Encrypted Hook" {
			if _, err := def.Handler(json.RawMessage(`{}`), &ToolContext{Context: context.Background()}); err != nil {
				t.Fatalf("handler: %v", err)
			}
		}
	}
	if gotSecret != "supersecret" {
		t.Fatalf("expected decrypted secret on the wire, got %q", gotSecret)
	}
	if gotLegacy != "legacy-plain" {
		t.Fatalf("expected legacy plaintext passthrough, got %q", gotLegacy)
	}
}
```

Note: `loadAgentToolDefs` gains a `masterKey []byte` parameter; tests above call it with the new signature. Existing `engine_tools_test.go` call sites must be updated to pass `nil`.

**Step 2: Run tests to verify they fail**

```bash
bash scripts/go-test.sh ./internal/agent/ -run TestLoadAgentToolDefsDecryptsWebhookSecrets -v -count=1
```

Expected: build failure or FAIL — the encrypted value is sent as `enc:v1:...` (or the signature doesn't compile).

**Step 3: Implement in `internal/agent/engine.go`**

Change `loadAgentToolDefs` to take the master key and decrypt webhook headers before resolving:

```go
func (e *Engine) loadAgentToolDefs(ctx context.Context, agent models.Agent, masterKey []byte) []*ToolDef {
	...
	for toolRows.Next() {
		var t models.Tool
		var schema, config []byte
		if err := toolRows.Scan(...); err != nil {
			continue
		}
		...
		if config != nil {
			json.Unmarshal(config, &t.Config)
		}
		if t.Type == models.ToolTypeWebhook {
			if err := decryptWebhookHeaderSecrets(&t, masterKey); err != nil {
				slog.Warn("engine: failed to decrypt webhook header secrets", "tool", t.Name, "error", err)
				continue
			}
		}
		toolDef, err := e.resolveToolDef(&t)
		...
	}
}
```

Add next to it:

```go
// decryptWebhookHeaderSecrets resolves enc:v1: values named in
// tools.config.secrets into plaintext before the tool def is built. Untagged
// values pass through, so pre-encryption rows keep working.
func decryptWebhookHeaderSecrets(t *models.Tool, masterKey []byte) error {
	rawHeaders, ok := t.Config["headers"].(map[string]any)
	if !ok || len(rawHeaders) == 0 {
		return nil
	}
	plain, err := headers.DecryptSecrets(rawHeaders, headers.NamesFromJSON(t.Config["secrets"]), masterKey)
	if err != nil {
		return err
	}
	plainAny := make(map[string]any, len(plain))
	for k, v := range plain {
		plainAny[k] = v
	}
	t.Config["headers"] = plainAny
	return nil
}
```

Update the caller in `ProcessMessage` (line 512):

```go
	agentTools := e.loadAgentToolDefs(ctx, agent, masterKey)
```

Update the nine test call sites in `engine_tools_test.go` to pass `nil` as the third argument (e.g. `engine.loadAgentToolDefs(context.Background(), agent, nil)`).

**Step 4: Implement the tool test endpoint**

In `internal/api/tool_handlers.go` `handleTest`, decrypt before the probe:

```go
		headers := make(map[string]string)
		if h, ok := cfg["headers"].(map[string]any); ok {
			plain, err := headers.DecryptSecrets(h, headers.NamesFromJSON(cfg["secrets"]), h.server.masterKey)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to decrypt header secrets")
				return
			}
			headers = plain
		}
```

Careful: the local variable `headers` shadows the package inside this function — the package calls in the same scope must use an aliased import (`headerutil "github.com/the-heaven-labs/aether/internal/headers"`) or the same block must avoid package references after the shadow. Simplest: keep the package import unaliased at file scope and rename the local map to `probeHeaders`. Then apply `probeHeaders` in the existing loop.

**Step 5: Run tests to verify they pass**

```bash
bash scripts/go-test.sh ./internal/agent/ -run 'TestLoadAgentToolDefsDecryptsWebhookSecrets|TestProcessMessage' -count=1
bash scripts/go-test.sh ./internal/api/ -run 'TestTool|TestWebhook' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/agent/engine.go internal/agent/tools_webhook_headers_test.go internal/agent/engine_tools_test.go internal/api/tool_handlers.go
git commit -m "fix(agent): decrypt webhook header secrets at runtime"
```

---

### Task 11: Frontend — shared `HeadersEditor` component

**Files:**
- Create: `web/src/components/HeadersEditor.tsx`
- Test: `web/src/components/HeadersEditor.test.tsx`

**Step 1: Write the failing test**

Create `web/src/components/HeadersEditor.test.tsx`:

```tsx
import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { HeadersEditor, headersToRows, rowsToPayload, REDACTED_SENTINEL } from './HeadersEditor'

describe('HeadersEditor', () => {
  test('renders rows and edits values', () => {
    const onChange = vi.fn()
    render(<HeadersEditor rows={[{ key: 'X-Title', value: 'Aether', sensitive: false }]} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Header 1 value'), { target: { value: 'Aether 2' } })
    expect(onChange).toHaveBeenCalledWith([{ key: 'X-Title', value: 'Aether 2', sensitive: false }])
  })

  test('masks sensitive values that are redacted on the server', () => {
    render(<HeadersEditor rows={[{ key: 'X-Api-Key', value: REDACTED_SENTINEL, sensitive: true }]} onChange={vi.fn()} />)
    const input = screen.getByLabelText('Header 1 value') as HTMLInputElement
    expect(input.type).toBe('password')
    expect(input.value).toBe('')
  })

  test('adds and removes rows', () => {
    const onChange = vi.fn()
    render(<HeadersEditor rows={[{ key: 'A', value: '1', sensitive: false }]} onChange={onChange} />)
    fireEvent.click(screen.getByText('+ Add Header') as HTMLElement)
    expect(onChange).toHaveBeenCalledWith([
      { key: 'A', value: '1', sensitive: false },
      { key: '', value: '', sensitive: false },
    ])
    fireEvent.click(screen.getByLabelText('Remove header 1'))
    expect(onChange).toHaveBeenCalledWith([])
  })

  test('rowsToPayload round-trips the sentinel for unchanged secrets', () => {
    const payload = rowsToPayload([
      { key: 'X-Title', value: 'Aether', sensitive: false },
      { key: 'X-Api-Key', value: REDACTED_SENTINEL, sensitive: true },
      { key: '', value: 'ignored', sensitive: false },
    ])
    expect(payload.headers).toEqual({ 'X-Title': 'Aether', 'X-Api-Key': REDACTED_SENTINEL })
    expect(payload.secrets).toEqual(['X-Api-Key'])
  })

  test('headersToRows maps API payloads and defaults to one empty row', () => {
    expect(headersToRows({ 'X-Api-Key': 'v' }, ['X-Api-Key'])).toEqual([
      { key: 'X-Api-Key', value: 'v', sensitive: true },
    ])
    expect(headersToRows(undefined, undefined)).toEqual([{ key: '', value: '', sensitive: false }])
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/components/HeadersEditor.test.tsx
```

Expected: FAIL — module does not exist.

**Step 3: Implement**

Create `web/src/components/HeadersEditor.tsx`:

```tsx
import type React from 'react'

export const REDACTED_SENTINEL = '__AETHER_REDACTED__'

export interface HeaderRow {
  key: string
  value: string
  sensitive: boolean
}

export const emptyHeaderRow = (): HeaderRow => ({ key: '', value: '', sensitive: false })

/** Map an API header map + secrets list into editable rows. */
export function headersToRows(headers?: Record<string, unknown>, secrets?: string[]): HeaderRow[] {
  const entries = headers ? Object.entries(headers) : []
  if (entries.length === 0) return [emptyHeaderRow()]
  const secretNames = secrets ?? []
  return entries.map(([key, value]) => ({ key, value: String(value), sensitive: secretNames.includes(key) }))
}

/**
 * Build the API payload for a rows list. Rows without a key or value are
 * skipped; an unchanged sensitive value round-trips as the server's redaction
 * sentinel so the stored secret is preserved.
 */
export function rowsToPayload(rows: HeaderRow[]): { headers: Record<string, string>; secrets: string[] } {
  const headers: Record<string, string> = {}
  const secrets: string[] = []
  for (const h of rows) {
    if (h.key && h.value) {
      headers[h.key] = h.value
      if (h.sensitive) secrets.push(h.key)
    }
  }
  return { headers, secrets }
}

export function HeadersEditor({ rows, onChange, title = 'Headers', hint, style }: {
  rows: HeaderRow[]
  onChange: (rows: HeaderRow[]) => void
  title?: string
  hint?: string
  style?: React.CSSProperties
}) {
  const update = (i: number, patch: Partial<HeaderRow>) => {
    const next = [...rows]
    next[i] = { ...next[i], ...patch }
    onChange(next)
  }
  const add = () => onChange([...rows, emptyHeaderRow()])
  const remove = (i: number) => onChange(rows.filter((_, idx) => idx !== i))
  const toggleSensitive = (i: number) => {
    const row = rows[i]
    update(i, { sensitive: !row.sensitive, value: row.sensitive ? '' : row.value })
  }

  return (
    <label style={{ ...styles.label, ...style }}>
      {title}
      {hint && <span style={styles.hint}>{hint}</span>}
      <div style={styles.rows}>
        {rows.map((h, i) => (
          <div key={i} style={styles.row}>
            <input
              aria-label={`Header ${i + 1} name`}
              style={{ ...styles.input, flex: 1 }}
              placeholder="Key"
              value={h.key}
              onChange={e => update(i, { key: e.target.value })}
            />
            <input
              aria-label={`Header ${i + 1} value`}
              type={h.sensitive ? 'password' : 'text'}
              style={{ ...styles.input, flex: 1 }}
              placeholder="Value"
              value={h.sensitive && h.value === REDACTED_SENTINEL ? '' : h.value}
              onChange={e => update(i, { value: e.target.value })}
            />
            <label style={styles.sensitiveLabel}>
              <input type="checkbox" checked={h.sensitive} onChange={() => toggleSensitive(i)} />
              sensitive
            </label>
            <button type="button" aria-label={`Remove header ${i + 1}`} style={styles.removeBtn} onClick={() => remove(i)}>×</button>
          </div>
        ))}
        <button type="button" style={styles.addBtn} onClick={add}>+ Add Header</button>
      </div>
    </label>
  )
}

const styles: Record<string, React.CSSProperties> = {
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  hint: { fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 },
  rows: { display: 'flex', flexDirection: 'column', gap: 6, marginTop: 4 },
  row: { display: 'flex', gap: 6, alignItems: 'center' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  sensitiveLabel: { fontSize: 11, display: 'flex', alignItems: 'center', gap: 3, cursor: 'pointer', color: 'var(--text-secondary)', whiteSpace: 'nowrap', fontWeight: 400 },
  removeBtn: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--error-full)', fontSize: 14, padding: '4px 6px' },
  addBtn: { alignSelf: 'flex-start', padding: '4px 10px', background: 'var(--bg-secondary)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, cursor: 'pointer' },
}
```

**Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/components/HeadersEditor.test.tsx
```

Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/HeadersEditor.tsx web/src/components/HeadersEditor.test.tsx
git commit -m "feat(web): reusable headers editor with secret masking"
```

---

### Task 12: Models page — custom headers editor

**Files:**
- Modify: `web/src/types/agent.ts` (`ModelConfig`)
- Modify: `web/src/pages/ModelsPage.tsx`
- Test: `web/src/test/ModelsPage.test.tsx` (append)

**Step 1: Write the failing test**

Append to `web/src/test/ModelsPage.test.tsx`:

```tsx
test('sends custom headers and round-trips a masked secret', async () => {
  let putBody: Record<string, unknown> | null = null
  server.use(
    http.get('/api/v1/model-configs', () => HttpResponse.json([
      {
        ...mockModels[0],
        custom_headers: { 'X-Title': 'Aether', 'X-Api-Key': '__AETHER_REDACTED__' },
        custom_header_secrets: ['X-Api-Key'],
      },
    ])),
    http.put('/api/v1/model-configs/:id', async ({ request }) => {
      putBody = await request.json() as Record<string, unknown>
      return HttpResponse.json({ id: 'm-1' })
    }),
  )
  renderWithProviders(<ModelsPage />)
  await screen.findByText('GPT-4o')

  fireEvent.click(screen.getByLabelText('Edit model config'))
  const dialog = await screen.findByRole('dialog')

  const secretInput = within(dialog).getByLabelText('Header 2 value') as HTMLInputElement
  expect(secretInput.value).toBe('')
  expect(secretInput.type).toBe('password')

  fireEvent.click(within(dialog).getByText('Save'))
  await waitFor(() => expect(putBody).not.toBeNull())
  expect(putBody).toMatchObject({
    custom_headers: { 'X-Title': 'Aether', 'X-Api-Key': '__AETHER_REDACTED__' },
    custom_header_secrets: ['X-Api-Key'],
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/test/ModelsPage.test.tsx
```

Expected: FAIL — no `Header 2 value` element.

**Step 3: Implement**

`web/src/types/agent.ts` — extend `ModelConfig`:

```ts
  custom_headers?: Record<string, unknown>
  custom_header_secrets?: string[]
```

`web/src/pages/ModelsPage.tsx`:
- Import `HeadersEditor, emptyHeaderRow, headersToRows, rowsToPayload` and `HeaderRow` from `../components/HeadersEditor`.
- `ModelConfigForm` gains `headers: HeaderRow[]`; `emptyForm()` sets `headers: [emptyHeaderRow()]`.
- Build payload in both mutations:

```ts
      const { headers: customHeaders, secrets: customHeaderSecrets } = rowsToPayload(form.headers)
```
```ts
      custom_headers: customHeaders,
      custom_header_secrets: customHeaderSecrets,
```

- `startEdit` adds:

```ts
      headers: headersToRows(config.custom_headers, config.custom_header_secrets),
```

- `ModelFormFields` renders (after the pricing fields):

```tsx
      <HeadersEditor
        rows={form.headers}
        onChange={rows => setForm(f => ({ ...f, headers: rows }))}
        title="Custom Headers"
        hint="Sent on every request to this endpoint. Sensitive values are encrypted and never shown again."
        style={{ gridColumn: '1 / -1' }}
      />
```

**Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/test/ModelsPage.test.tsx
```

Expected: PASS (new test plus the existing four).

**Step 5: Commit**

```bash
git add web/src/types/agent.ts web/src/pages/ModelsPage.tsx web/src/test/ModelsPage.test.tsx
git commit -m "feat(web): custom headers editor on model configs"
```

---

### Task 13: MCP page — custom headers editor

**Files:**
- Modify: `web/src/types/agent.ts` (`MCPServerOrg`)
- Modify: `web/src/pages/MCPPage.tsx`
- Test: `web/src/test/MCPPage.test.tsx` (create)

**Step 1: Write the failing test**

Create `web/src/test/MCPPage.test.tsx`:

```tsx
import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { MCPPage } from '../pages/MCPPage'
import { renderWithProviders } from './utils'

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const mockServers = [
  {
    id: 's-1',
    name: 'Weather MCP',
    type: 'http',
    command: 'https://mcp.example.com',
    args: [],
    headers: { 'X-Env': 'prod', Authorization: '__AETHER_REDACTED__' },
    header_secrets: ['Authorization'],
    created_at: '2026-01-01T00:00:00Z',
  },
]

beforeEach(() => {
  server.use(http.get('/api/v1/mcp-servers', () => HttpResponse.json(mockServers)))
  vi.clearAllMocks()
})

describe('MCPPage', () => {
  test('round-trips masked header secrets on save', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/mcp-servers/:id', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 's-1' })
      }),
    )
    renderWithProviders(<MCPPage />)
    await screen.findByText('Weather MCP')

    fireEvent.click(screen.getByLabelText('Edit MCP server'))
    const dialog = await screen.findByRole('dialog')
    expect(screen.getByLabelText('Header 2 value')).toHaveValue('')

    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody).toMatchObject({
      headers: { 'X-Env': 'prod', Authorization: '__AETHER_REDACTED__' },
      header_secrets: ['Authorization'],
    })
  })

  test('hides the headers editor for stdio servers', async () => {
    server.use(http.get('/api/v1/mcp-servers', () => HttpResponse.json([
      { ...mockServers[0], type: 'stdio', command: '/usr/bin/mcp' },
    ])))
    renderWithProviders(<MCPPage />)
    await screen.findByText('Weather MCP')

    fireEvent.click(screen.getByLabelText('Edit MCP server'))
    await screen.findByRole('dialog')
    expect(screen.queryByText('Headers')).toBeNull()
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/test/MCPPage.test.tsx
```

Expected: FAIL — no headers editor exists.

**Step 3: Implement**

`web/src/types/agent.ts` — extend `MCPServerOrg`:

```ts
  headers?: Record<string, unknown>
  header_secrets?: string[]
```

`web/src/pages/MCPPage.tsx`:
- Import the same helpers from `../components/HeadersEditor`.
- `MCPForm` gains `headers: HeaderRow[]`; `emptyForm()` sets `headers: [emptyHeaderRow()]`.
- Create/update payloads:

```ts
      ...(form.type === 'http'
        ? (() => {
            const { headers, secrets } = rowsToPayload(form.headers)
            return { headers, header_secrets: secrets }
          })()
        : {}),
```

- `startEdit` adds `headers: headersToRows(s.headers, s.header_secrets)`.
- In `MCPFormFields`, render after Command/Arguments when `form.type === 'http'`:

```tsx
      {form.type === 'http' && (
        <HeadersEditor
          rows={form.headers}
          onChange={rows => setForm(f => ({ ...f, headers: rows }))}
          title="Headers"
          hint="Sent on every /tools/list and /tools/call request. Sensitive values are encrypted and never shown again."
          style={{ gridColumn: '1 / -1' }}
        />
      )}
```

Behavior note: switching a server to `stdio` omits headers from the payload, so stored headers remain in the DB but are unused at runtime (MCP headers only apply to HTTP servers).

**Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/test/MCPPage.test.tsx
```

Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/types/agent.ts web/src/pages/MCPPage.tsx web/src/test/MCPPage.test.tsx
git commit -m "feat(web): custom headers editor on MCP servers"
```

---

### Task 14: Tools page adopts the shared editor

**Files:**
- Modify: `web/src/pages/ToolsPage.tsx`

**Step 1: Refactor**

- Remove the inline `headers` row editor block (the label with `updateHeader`/`addHeader`/`removeHeader`) and the local `headers` field handling, replacing it with:

```tsx
          <HeadersEditor
            rows={form.headers}
            onChange={rows => setForm(f => ({ ...f, headers: rows }))}
            title="Headers"
            hint="Sensitive values are encrypted at rest and never shown again."
            style={{ gridColumn: '1 / -1' }}
          />
```

- In `buildPayload`, replace the manual `headers`/`secrets` loop with:

```ts
      const { headers, secrets } = rowsToPayload(form.headers)
      payload.config = {
        url: form.url,
        method: form.method,
        content_type: form.content_type,
        headers,
        ...(secrets.length > 0 ? { secrets } : {}),
      }
```

- In `startEdit`, replace the manual `headerEntries` mapping with `headers: headersToRows(config.headers as Record<string, unknown>, secrets)`.
- Delete `emptyHeaderRow`-equivalent literals in favor of the shared `emptyHeaderRow()`.

Behavioral parity requirements (the refactor must preserve these exactly):
- Masked sensitive values show as empty password inputs but round-trip as the sentinel.
- Toggling `sensitive` on clears the value.
- Rows without a key or value are skipped on save.

**Step 2: Verify no regressions**

```bash
cd web && npx vitest run src/components/HeadersEditor.test.tsx src/test/ModelsPage.test.tsx src/test/MCPPage.test.tsx
cd web && npx tsc --noEmit
```

Expected: PASS and clean type-check.

**Step 3: Commit**

```bash
git add web/src/pages/ToolsPage.tsx
git commit -m "refactor(web): tools page uses the shared headers editor"
```

---

### Task 15: Documentation, full verification, and real-browser validation

**Files:**
- Modify: `AGENTS.md` (Key Patterns section)
- Modify: `docs/plans/2026-10-14-agent-custom-headers-design.md` (status)

**Step 1: Document the pattern in `AGENTS.md`**

Add a paragraph to the Key Patterns section, matching the style of the surrounding entries:

> **Agent custom headers**: `model_configs.custom_headers` + `custom_header_secrets` and `mcp_servers.headers` + `header_secrets` carry custom HTTP headers applied after the built-in ones on every LLM provider call (chat, streaming, subagent, summarization, title, test) and on MCP `/tools/list` + `/tools/call` (and the MCP test probe). Sensitive values are stored as `enc:v1:`-tagged AES-GCM ciphertext (`crypto.EncryptString`, master key), masked as `__AETHER_REDACTED__` in API responses, and restored from the stored value when the sentinel is saved back. Reserved names `Host`, `Content-Length`, `Content-Type` are rejected with 400; `Authorization` may be intentionally overridden. Webhook tool secret headers (`tools.config.headers` + `config.secrets`) use the same encryption; existing plaintext rows upgrade lazily on their next save and decrypt transparently at runtime. The shared semantics live in `internal/headers`.

**Step 2: Update the design doc status**

Change `**Status:** 🚧 In progress` to `**Status:** ✅ Implemented` in `docs/plans/2026-10-14-agent-custom-headers-design.md`.

**Step 3: Full local CI**

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
```

Expected: all green. Do NOT raise the 3-minute Go test timeout — narrow runs instead.

**Step 4: Real-browser validation (mandatory for UI changes)**

With the dev stack up (`docker compose -f docker-compose.dev.yml up -d`, then `docker compose -f docker-compose.dev.yml restart web` if the Vite cache looks stale), use the `agent-browser` skill:

1. Log in as `nova@heaven-labs.com` / `nova123`.
2. Models page: create a model config with headers `X-Title: Aether` (plain) and `X-Api-Key: test-secret` (sensitive); click Test. Re-open edit: `X-Api-Key` must be empty/password-masked, `X-Title` visible. Save without touching the secret; re-open and confirm the secret is still stored (Test still succeeds against the endpoint); check `agent-browser errors`/`console` for React errors.
3. MCP page: create an HTTP MCP server with an `Authorization` header marked sensitive; test the endpoint behavior; verify masking on edit and that stdio hides the editor.
4. Tools page: edit an existing webhook tool with a sensitive header; confirm the masked round-trip still works and the header survives a save (Test action).
5. Take screenshots of the three forms as evidence.

If any flow misbehaves, fix it and re-run the relevant Go/Vitest tests before continuing.

**Step 5: Commit**

```bash
git add AGENTS.md docs/plans/2026-10-14-agent-custom-headers-design.md
git commit -m "docs: document custom header configuration"
```

---

## Verification summary

- `go test ./internal/crypto/ ./internal/headers/` — unit coverage for tagged crypto and header semantics.
- `bash scripts/go-test.sh ./internal/api/ -count=1` — model config + MCP CRUD, redaction, validation, webhook encryption, test endpoints.
- `bash scripts/go-test.sh ./internal/agent/ -count=1` — LLM/MCP/webhook runtime header application, engine integration.
- `cd web && npx vitest run` — component and page tests.
- `task check`, `tsc --noEmit`, `npm run build` (web), `npm run build` (relay).
- Real-browser validation of the three editors per AGENTS.md.

## Deviations from the design doc (intentional, agreed during planning)

- Decrypting webhook headers happens in `loadAgentToolDefs` before `resolveToolDef` builds the def, instead of changing `makeWebhookToolDef`'s signature — same behavior, smaller blast radius on existing tests.
- `MCPServerOrg`/`ModelConfig` new JSON fields use `omitempty` so agent GET responses don't grow null fields.
