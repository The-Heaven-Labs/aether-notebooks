package oauth

import (
	"crypto/sha256"
	"encoding/base64"
)

// VerifyPKCE validates a code_verifier against a stored code_challenge.
// Only S256 is supported; plain is rejected per OAuth 2.1.
func VerifyPKCE(challenge, method, verifier string) bool {
	if challenge == "" || verifier == "" || method != "S256" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return computed == challenge
}
