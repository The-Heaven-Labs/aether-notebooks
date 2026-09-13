package crypto_test

import (
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
