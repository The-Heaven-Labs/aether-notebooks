package auth_test

import (
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/auth"
)

func TestJWTRoundTrip(t *testing.T) {
	secret := "test-jwt-secret-long-enough"
	issuer := auth.NewJWTIssuer(secret, 15*time.Minute)

	token, err := issuer.Issue("user-123", "org-456", "editor")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	claims, err := issuer.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Fatalf("expected user-123, got %s", claims.UserID)
	}
	if claims.OrgID != "org-456" {
		t.Fatalf("expected org-456, got %s", claims.OrgID)
	}
	if claims.Role != "editor" {
		t.Fatalf("expected editor, got %s", claims.Role)
	}
}

func TestJWTExpired(t *testing.T) {
	secret := "test-jwt-secret-long-enough"
	issuer := auth.NewJWTIssuer(secret, -1*time.Minute) // already expired

	token, err := issuer.Issue("user-123", "org-456", "editor")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = issuer.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
