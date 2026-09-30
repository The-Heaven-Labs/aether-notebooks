package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The per-request MCP write deadline must only ever extend the server's 60s
// WriteTimeout: SetWriteDeadline replaces the baseline, so a small configured
// SQL ceiling would otherwise shorten every MCP call's budget.
func TestMCPWriteDeadlineOverride(t *testing.T) {
	tests := []struct {
		name    string
		ceiling time.Duration
		want    time.Duration
		apply   bool
	}{
		{"unset keeps server baseline", 0, 0, false},
		{"floor keeps server baseline", time.Second, 0, false},
		{"margin exactly at baseline keeps it", 30 * time.Second, 0, false},
		{"just above baseline extends", 31 * time.Second, 61 * time.Second, true},
		{"default ceiling extends", 10 * time.Minute, 10*time.Minute + 30*time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mcpWriteDeadlineOverride(tt.ceiling)
			require.Equal(t, tt.apply, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
