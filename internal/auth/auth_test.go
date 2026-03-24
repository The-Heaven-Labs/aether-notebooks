package auth_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/auth"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("mypassword123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if !auth.VerifyPassword("mypassword123", hash) {
		t.Fatal("expected password to verify")
	}

	if auth.VerifyPassword("wrongpassword", hash) {
		t.Fatal("expected wrong password to fail")
	}
}
