package crypto_test

import (
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

func TestEncryptDecrypt(t *testing.T) {
	key := crypto.DeriveKey("test-master-key-that-is-long-enough-32")
	plaintext := []byte(`{"host":"localhost","port":5432,"password":"secret"}`)

	encrypted, err := crypto.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := crypto.Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := crypto.DeriveKey("key-one-long-enough-for-testing-32chars")
	key2 := crypto.DeriveKey("key-two-long-enough-for-testing-32chars")

	encrypted, err := crypto.Encrypt([]byte("secret"), key1)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = crypto.Decrypt(encrypted, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestTokenLookupHash(t *testing.T) {
	key := crypto.DeriveKey("test-master-key-that-is-long-enough-32")
	token := "aether_tok_0123456789abcdef0123456789abcdef0123456789abcdef01234567"

	first := crypto.TokenLookupHash(key, token)
	if len(first) != 64 {
		t.Fatalf("expected 64 hex chars, got %d (%q)", len(first), first)
	}
	if second := crypto.TokenLookupHash(key, token); second != first {
		t.Fatalf("expected deterministic hash, got %q and %q", first, second)
	}
	if other := crypto.TokenLookupHash(key, token+"x"); other == first {
		t.Fatal("different tokens must produce different hashes")
	}
	if otherKey := crypto.TokenLookupHash(crypto.DeriveKey("another-master-key-long-enough-32"), token); otherKey == first {
		t.Fatal("different keys must produce different hashes")
	}
}

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
