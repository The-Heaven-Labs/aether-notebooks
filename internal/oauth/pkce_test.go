package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyPKCE(t *testing.T) {
	verifier := "correct-horse-battery-staple-0123456789abcdef"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	require.True(t, VerifyPKCE(challenge, "S256", verifier))
	require.False(t, VerifyPKCE(challenge, "S256", "wrong-verifier"))
	require.False(t, VerifyPKCE(challenge, "plain", verifier), "plain must be rejected")
	require.False(t, VerifyPKCE("", "S256", verifier))
	require.False(t, VerifyPKCE(challenge, "S256", ""))
}
