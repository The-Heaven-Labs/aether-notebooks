package api_test

import (
	"testing"
)

func TestServerHasCache(t *testing.T) {
	s := setupTestServer(t)
	if s.Cache == nil {
		t.Fatal("Server.Cache is nil — Redis not wired")
	}
}
