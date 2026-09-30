package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "correct-horse-battery-staple-0123456789abcdef"
	challenge := challengeFor(verifier)

	require.True(t, VerifyPKCE(challenge, "S256", verifier))
	require.False(t, VerifyPKCE(challenge, "S256", "wrong-verifier"))
	require.False(t, VerifyPKCE(challenge, "plain", verifier), "plain must be rejected")
	require.False(t, VerifyPKCE("", "S256", verifier))
	require.False(t, VerifyPKCE(challenge, "S256", ""))
}

// RFC 7636 §4.1: verifiers must be 43–128 characters long. A matching hash of
// an out-of-range verifier must still be rejected.
func TestVerifyPKCELengthBounds(t *testing.T) {
	cases := []struct {
		length int
		valid  bool
	}{
		{42, false},
		{43, true},
		{128, true},
		{129, false},
	}
	for _, tc := range cases {
		verifier := strings.Repeat("a", tc.length)
		require.Equalf(t, tc.valid, VerifyPKCE(challengeFor(verifier), "S256", verifier),
			"verifier of length %d", tc.length)
	}
}
