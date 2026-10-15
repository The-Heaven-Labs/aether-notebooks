package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildTSQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single word", "revenue", "'revenue':*"},
		{"two words", "Order Items", "'order':* & 'items':*"},
		{"apostrophe", "don't", "'don':* & 't':*"},
		{"underscore token kept", "order_items", "'order_items':*"},
		{"mixed punctuation", "SELECT * FROM order_items;", "'select':* & 'from':* & 'order_items':*"},
		{"punctuation only", "%_&;:", ""},
		{"underscore only", "_ __ _", ""},
		{"whitespace only", "   \t\n ", ""},
		{"empty", "", ""},
		{"cjk only", "你好世界", ""},
		{"repeated tokens kept", "order order", "'order':* & 'order':*"},
		{"trailing separator", "order ", "'order':*"},
		{"leading separator", "  order", "'order':*"},
		{"digits", "v2 beta-3", "'v2':* & 'beta':* & '3':*"},
		{"mixed case lowered", "ReVeNuE", "'revenue':*"},
		{"underscore separated from alphanumerics", "a_b _c", "'a_b':* & '_c':*"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, BuildTSQuery(tc.in))
		})
	}
}

func TestBuildTSQueryCapsInput(t *testing.T) {
	// The cap is a hard 256-byte prefix: a 300-char run yields exactly the
	// first 256 characters as one quoted lexeme.
	longRun := strings.Repeat("a", 300)
	require.Equal(t, "'"+strings.Repeat("a", maxTSQueryInputBytes)+"':*", BuildTSQuery(longRun))

	// A separator boundary: 500 input characters, 256 kept. "ab " is 3 bytes,
	// so 85 full tokens plus the leading "a" of the 86th fit in the prefix.
	in := strings.Repeat("ab ", 90)
	expr := BuildTSQuery(in)
	require.NotEmpty(t, expr)
	tokens := strings.Split(expr, " & ")
	require.Len(t, tokens, 86)

	// Every byte of lexeme content comes from the capped prefix, and the
	// expression stays bounded: at most 128 one-byte tokens fit in 256 bytes,
	// each contributing at most len(lexeme)+4 bytes of quoting/flag plus a
	// 3-byte separator.
	var tokenBytes int
	for _, token := range tokens {
		lexeme := strings.TrimSuffix(strings.TrimPrefix(token, "'"), "':*")
		require.Contains(t, in[:maxTSQueryInputBytes], lexeme)
		tokenBytes += len(lexeme)
	}
	require.LessOrEqual(t, tokenBytes, maxTSQueryInputBytes)
	require.Less(t, len(expr), 1152)
}

func TestBuildTSQueryNeverLeavesEmptyClauses(t *testing.T) {
	for _, in := range []string{"order ", " order", " order ", "a b", "a&&b", "a && b"} {
		expr := BuildTSQuery(in)
		require.NotEmpty(t, expr)
		require.NotContains(t, expr, "& &")
		require.False(t, strings.HasPrefix(expr, "&"), "expr %q starts with &", expr)
		require.False(t, strings.HasSuffix(expr, "&"), "expr %q ends with &", expr)
		for _, token := range strings.Split(expr, " & ") {
			require.Regexp(t, `^'[a-z0-9_]+':\*$`, token)
		}
	}
}
