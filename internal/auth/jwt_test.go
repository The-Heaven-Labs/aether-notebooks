package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/auth"
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

func TestIssueMCPAccessTokenRoundTrip(t *testing.T) {
	issuer := auth.NewJWTIssuer("test-secret", time.Hour)

	tok, err := issuer.IssueMCPAccessToken(
		"user-1", "org-1", "editor", "mcp:query mcp:read", "mcp_abc",
		"https://a.example.com/api/v1/mcp", 15*time.Minute)
	require.NoError(t, err)

	claims, err := issuer.Validate(tok)
	require.NoError(t, err)
	require.Equal(t, "user-1", claims.UserID)
	require.Equal(t, "org-1", claims.OrgID)
	require.Equal(t, "editor", claims.Role)
	require.Equal(t, "mcp:query mcp:read", claims.Scope)
	require.Equal(t, "mcp_abc", claims.ClientID)
	require.Contains(t, []string(claims.Audience), "https://a.example.com/api/v1/mcp")
	require.NotContains(t, []string(claims.Audience), "https://other.example.com/api/v1/mcp")
}
