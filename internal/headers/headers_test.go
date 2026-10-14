package headers_test

import (
	"fmt"
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
