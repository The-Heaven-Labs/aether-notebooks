package headers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/headers"
)

func testKey() []byte { return crypto.DeriveKey("test-master-key-that-is-long-enough-32") }

func TestApply(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://example.test/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	headers.Apply(req, map[string]string{
		"content-TYPE":  "text/plain", // reserved, case-insensitive: must be skipped
		"Host":          "evil.test",  // reserved: must be skipped
		"Authorization": "Custom gateway-auth",
		"x-title":       "Aether",
	})

	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("reserved Content-Type must survive, got %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Custom gateway-auth" {
		t.Fatalf("expected Authorization applied, got %q", got)
	}
	if got := req.Header.Get("X-Title"); got != "Aether" {
		t.Fatalf("expected normal header applied, got %q", got)
	}
}

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
	t.Run("rejects too many headers", func(t *testing.T) {
		h := make(map[string]any, 51)
		for i := 0; i < 51; i++ {
			h[fmt.Sprintf("X-Header-%d", i)] = "x"
		}
		if err := headers.Validate(h, nil); err == nil {
			t.Fatal("expected more than 50 headers to be rejected")
		}
	})
	t.Run("enforces name length limits", func(t *testing.T) {
		if err := headers.Validate(map[string]any{strings.Repeat("a", 128): "x"}, nil); err != nil {
			t.Fatalf("expected 128-byte name to be accepted, got %v", err)
		}
		if err := headers.Validate(map[string]any{strings.Repeat("a", 129): "x"}, nil); err == nil {
			t.Fatal("expected 129-byte name to be rejected")
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

func TestEncryptSecretsDoesNotMutateInput(t *testing.T) {
	key := testKey()
	input := map[string]any{"X-Title": "Aether", "X-Api-Key": "supersecret"}

	out, err := headers.EncryptSecrets(input, []string{"X-Api-Key"}, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if input["X-Api-Key"] != "supersecret" {
		t.Fatalf("input map must keep plaintext, got %v", input["X-Api-Key"])
	}
	encrypted, _ := out["X-Api-Key"].(string)
	if !crypto.IsEncryptedString(encrypted) {
		t.Fatalf("output map must hold tagged ciphertext, got %q", encrypted)
	}
}

func TestEncryptSecretsNilMap(t *testing.T) {
	out, err := headers.EncryptSecrets(nil, nil, testKey())
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %v", out)
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
	t.Run("deletes sentinel when old map has no value", func(t *testing.T) {
		new := map[string]any{"X-Api-Key": headers.RedactedSentinel}
		headers.RestoreSecrets(map[string]any{}, new, []string{"X-Api-Key"})
		if _, exists := new["X-Api-Key"]; exists {
			t.Fatal("expected orphaned sentinel to be removed")
		}
	})
	t.Run("restores under the new key spelling", func(t *testing.T) {
		old := map[string]any{"X-Api-Key": "enc:v1:stored"}
		new := map[string]any{"x-api-key": headers.RedactedSentinel}
		headers.RestoreSecrets(old, new, []string{"X-API-KEY"})
		if new["x-api-key"] != "enc:v1:stored" {
			t.Fatalf("expected restored value under new spelling, got %v", new["x-api-key"])
		}
		if len(new) != 1 {
			t.Fatalf("expected exactly one entry, got %v", new)
		}
	})
}

func TestDropOrphanSentinels(t *testing.T) {
	t.Run("drops a sentinel for a declared secret", func(t *testing.T) {
		h := map[string]any{"X-Api-Key": headers.RedactedSentinel, "X-Title": "Aether"}
		kept := headers.DropOrphanSentinels(h, []string{"X-Api-Key"})
		if _, exists := h["X-Api-Key"]; exists {
			t.Fatal("expected sentinel entry dropped")
		}
		if h["X-Title"] != "Aether" {
			t.Fatalf("expected non-secret untouched, got %v", h["X-Title"])
		}
		if len(kept) != 0 {
			t.Fatalf("expected pruned secret list, got %v", kept)
		}
	})
	t.Run("keeps fresh values and their secret names", func(t *testing.T) {
		h := map[string]any{"X-Api-Key": "brand-new"}
		kept := headers.DropOrphanSentinels(h, []string{"X-Api-Key"})
		if h["X-Api-Key"] != "brand-new" {
			t.Fatalf("expected fresh value kept, got %v", h["X-Api-Key"])
		}
		if len(kept) != 1 || kept[0] != "X-Api-Key" {
			t.Fatalf("expected kept secret X-Api-Key, got %v", kept)
		}
	})
	t.Run("keeps a sentinel on a non-secret name", func(t *testing.T) {
		h := map[string]any{"X-Api-Key": headers.RedactedSentinel, "X-Note": headers.RedactedSentinel}
		kept := headers.DropOrphanSentinels(h, []string{"X-Api-Key"})
		if h["X-Note"] != headers.RedactedSentinel {
			t.Fatalf("only declared secrets are dropped, got %v", h["X-Note"])
		}
		if len(kept) != 0 {
			t.Fatalf("expected no kept secrets, got %v", kept)
		}
	})
	t.Run("returns a non-nil empty slice for empty input", func(t *testing.T) {
		kept := headers.DropOrphanSentinels(nil, []string{"X-Api-Key"})
		if kept == nil {
			t.Fatal("expected non-nil empty slice so callers never store SQL NULL")
		}
		if len(kept) != 0 {
			t.Fatalf("expected empty slice, got %v", kept)
		}
	})
}

func TestPresentSecrets(t *testing.T) {
	t.Run("keeps only names present in the map", func(t *testing.T) {
		h := map[string]any{"X-Api-Key": "enc:v1:stored", "X-Title": "Aether"}
		got := headers.PresentSecrets(h, []string{"X-Missing", "X-Api-Key"})
		if len(got) != 1 || got[0] != "X-Api-Key" {
			t.Fatalf("expected only X-Api-Key, got %v", got)
		}
	})
	t.Run("matches names case-insensitively", func(t *testing.T) {
		h := map[string]any{"x-api-key": "enc:v1:stored"}
		got := headers.PresentSecrets(h, []string{"X-Api-Key"})
		if len(got) != 1 || got[0] != "X-Api-Key" {
			t.Fatalf("expected case-variant match, got %v", got)
		}
	})
	t.Run("returns a non-nil empty slice when nothing matches", func(t *testing.T) {
		got := headers.PresentSecrets(map[string]any{"X-Title": "Aether"}, []string{"X-Api-Key"})
		if got == nil {
			t.Fatal("expected non-nil empty slice so callers never store SQL NULL")
		}
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %v", got)
		}
	})
}

func TestEncryptUpdate(t *testing.T) {
	key := testKey()

	t.Run("sentinel keeps the exact stored ciphertext", func(t *testing.T) {
		stored := map[string]any{"X-Api-Key": "enc:v1:stored-ciphertext"}
		incoming := map[string]any{"X-Api-Key": headers.RedactedSentinel}
		out, kept, err := headers.EncryptUpdate(incoming, []string{"X-Api-Key"}, stored, key)
		if err != nil {
			t.Fatalf("encrypt update: %v", err)
		}
		if out["X-Api-Key"] != "enc:v1:stored-ciphertext" {
			t.Fatalf("expected stored ciphertext preserved, got %v", out["X-Api-Key"])
		}
		if len(kept) != 1 || kept[0] != "X-Api-Key" {
			t.Fatalf("expected kept secret X-Api-Key, got %v", kept)
		}
	})

	t.Run("orphaned sentinel is dropped and pruned", func(t *testing.T) {
		incoming := map[string]any{"X-Api-Key": headers.RedactedSentinel}
		out, kept, err := headers.EncryptUpdate(incoming, []string{"X-Api-Key"}, map[string]any{}, key)
		if err != nil {
			t.Fatalf("encrypt update: %v", err)
		}
		if _, exists := out["X-Api-Key"]; exists {
			t.Fatalf("expected orphaned sentinel dropped, got %v", out)
		}
		if len(kept) != 0 {
			t.Fatalf("expected no kept secrets, got %v", kept)
		}
	})

	t.Run("fresh sensitive value is tagged", func(t *testing.T) {
		incoming := map[string]any{"X-Api-Key": "brand-new-secret"}
		out, kept, err := headers.EncryptUpdate(incoming, []string{"X-Api-Key"}, nil, key)
		if err != nil {
			t.Fatalf("encrypt update: %v", err)
		}
		value, _ := out["X-Api-Key"].(string)
		if !crypto.IsEncryptedString(value) {
			t.Fatalf("expected tagged ciphertext, got %q", value)
		}
		if len(kept) != 1 || kept[0] != "X-Api-Key" {
			t.Fatalf("expected kept secret X-Api-Key, got %v", kept)
		}
		dec, err := headers.DecryptSecrets(out, kept, key)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if dec["X-Api-Key"] != "brand-new-secret" {
			t.Fatalf("expected round-trip to the original value, got %q", dec["X-Api-Key"])
		}
	})

	t.Run("fresh non-sensitive value stays plaintext", func(t *testing.T) {
		incoming := map[string]any{"X-Title": "Aether"}
		out, kept, err := headers.EncryptUpdate(incoming, nil, nil, key)
		if err != nil {
			t.Fatalf("encrypt update: %v", err)
		}
		if out["X-Title"] != "Aether" {
			t.Fatalf("expected plaintext passthrough, got %v", out["X-Title"])
		}
		if len(kept) != 0 {
			t.Fatalf("expected no kept secrets, got %v", kept)
		}
	})

	t.Run("pruned secrets exclude dropped names", func(t *testing.T) {
		incoming := map[string]any{
			"X-Api-Key": headers.RedactedSentinel, // no stored value: dropped
			"X-Token":   "fresh-token",
		}
		out, kept, err := headers.EncryptUpdate(incoming, []string{"X-Api-Key", "X-Token"}, nil, key)
		if err != nil {
			t.Fatalf("encrypt update: %v", err)
		}
		if _, exists := out["X-Api-Key"]; exists {
			t.Fatalf("expected dropped sentinel absent, got %v", out)
		}
		if len(kept) != 1 || kept[0] != "X-Token" {
			t.Fatalf("expected only X-Token kept, got %v", kept)
		}
		value, _ := out["X-Token"].(string)
		if !crypto.IsEncryptedString(value) {
			t.Fatalf("expected tagged ciphertext, got %q", value)
		}
	})
}

func TestNamesFromJSON(t *testing.T) {
	t.Run("passes through a string slice", func(t *testing.T) {
		got := headers.NamesFromJSON([]string{"X-Api-Key"})
		if len(got) != 1 || got[0] != "X-Api-Key" {
			t.Fatalf("expected passthrough, got %v", got)
		}
	})
	t.Run("drops non-string entries", func(t *testing.T) {
		got := headers.NamesFromJSON([]any{"X-Api-Key", 42})
		if len(got) != 1 || got[0] != "X-Api-Key" {
			t.Fatalf("expected only the string entry, got %v", got)
		}
	})
	t.Run("nil and unknown types yield nil", func(t *testing.T) {
		if got := headers.NamesFromJSON(nil); got != nil {
			t.Fatalf("expected nil for nil input, got %v", got)
		}
		if got := headers.NamesFromJSON(42); got != nil {
			t.Fatalf("expected nil for unknown type, got %v", got)
		}
	})
}
