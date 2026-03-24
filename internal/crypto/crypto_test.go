package crypto_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/crypto"
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
