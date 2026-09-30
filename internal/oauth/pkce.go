package oauth

import (
	"crypto/sha256"
	"encoding/base64"
)

// VerifyPKCE validates a code_verifier against a stored code_challenge.
// Only S256 is supported; plain is rejected per OAuth 2.1. RFC 7636 §4.1
// requires the verifier to be 43–128 characters long.
func VerifyPKCE(challenge, method, verifier string) bool {
	if challenge == "" || method != "S256" {
		return false
	}
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return computed == challenge
}
