package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePendingEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"lowercases", "Alice@Example.com", "alice@example.com", true},
		{"trims surrounding whitespace", "  alice@example.com  ", "alice@example.com", true},
		{"rejects missing domain", "alice@", "", false},
		{"rejects missing local part", "@example.com", "", false},
		{"rejects double at", "alice@@example.com", "", false},
		{"rejects inner whitespace", "ali ce@example.com", "", false},
		{"rejects empty", "", "", false},
		{"rejects plain name", "alice", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizePendingEmail(tc.in)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
