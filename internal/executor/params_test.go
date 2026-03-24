package executor_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/executor"
)

func TestResolveParams(t *testing.T) {
	query := "SELECT * FROM orders WHERE env = '{{env}}' AND date > '{{start_date}}'"
	params := map[string]string{"env": "prod", "start_date": "2026-01-01"}

	result := executor.ResolveParams(query, params)
	expected := "SELECT * FROM orders WHERE env = 'prod' AND date > '2026-01-01'"

	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}
