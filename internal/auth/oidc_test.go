package auth_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/auth"
)

func TestOIDCProviderInterface(t *testing.T) {
	// Verify GenericOIDCProvider implements OIDCProvider
	var _ auth.OIDCProvider = (*auth.GenericOIDCProvider)(nil)
}
