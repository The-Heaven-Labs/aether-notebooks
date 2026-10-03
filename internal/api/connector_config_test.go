package api

import (
	"encoding/json"
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

	// A case-variant secret key is matched case-insensitively.
	got = mergeConnectorConfig(existing, map[string]any{"Password": ""}, secrets)
	if got["Password"] == "" {
		t.Fatal("case-variant empty secret must not be stored")
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
