// Package crypto provides AES-based encryption/decryption for connector credentials.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// tokenLookupLabel domain-separates the API token lookup hash from other uses
// of the master key.
const tokenLookupLabel = "aether-pat-lookup:v1:"

// DeriveKey derives a 32-byte key from a master key string using SHA-256.
func DeriveKey(masterKey string) []byte {
	h := sha256.Sum256([]byte(masterKey))
	return h[:]
}

// TokenLookupHash returns the deterministic keyed hash used to look up API
// tokens by index. API tokens carry full entropy, so this is not a password
// hash: the HMAC only needs preimage resistance, and keying it prevents
// offline token guessing from a leaked database alone.
func TokenLookupHash(key []byte, token string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(tokenLookupLabel))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// Encrypt encrypts plaintext using AES-256-GCM.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
