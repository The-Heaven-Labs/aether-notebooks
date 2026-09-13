package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"golang.org/x/crypto/bcrypt"
)

func TestCreateToken_StoresLookupHash(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-lookup-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Lookup Org")

	code, resp := doCreateToken(t, srv, jwt, "lookup-token", "")
	require.Equal(t, http.StatusCreated, code, "create token: %v", resp)
	raw := resp["token"].(string)
	id := resp["id"].(string)

	var lookup *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT token_lookup_hash FROM api_tokens WHERE id = $1`, id).Scan(&lookup))
	require.NotNil(t, lookup)
	require.Equal(t, crypto.TokenLookupHash(testMasterKey, raw), *lookup)
}

// Tokens created before the lookup-hash migration have no token_lookup_hash and
// authenticate through the bcrypt fallback; the matched row is backfilled so
// subsequent requests use the indexed fast path.
func TestAPIToken_LegacyTokenBackfilled(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-legacy-%d@example.com", time.Now().UnixNano())
	registerAndGetToken(t, srv, email, "PAT Legacy Org")

	var userID, orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT u.id, m.org_id FROM users u JOIN org_members m ON m.user_id = u.id WHERE u.email = $1 LIMIT 1`,
		email).Scan(&userID, &orgID))

	raw := "aether_tok_" + strings.Repeat("ab", 28)
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	require.NoError(t, err)

	var tokenID string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO api_tokens (user_id, org_id, name, token_hash) VALUES ($1, $2, 'legacy', $3) RETURNING id`,
		userID, orgID, string(hash)).Scan(&tokenID))

	req := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "legacy PAT should authenticate: %s", rec.Body.String())

	var lookup *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT token_lookup_hash FROM api_tokens WHERE id = $1`, tokenID).Scan(&lookup))
	require.NotNil(t, lookup, "legacy token should be backfilled after first use")
	require.Equal(t, crypto.TokenLookupHash(testMasterKey, raw), *lookup)

	// The backfilled row now authenticates through the fast path too.
	req2 := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	req2.Header.Set("Authorization", "Bearer "+raw)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "backfilled PAT should keep working: %s", rec2.Body.String())
}
