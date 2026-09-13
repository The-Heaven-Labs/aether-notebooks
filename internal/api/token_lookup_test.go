package api_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"golang.org/x/crypto/bcrypt"
)

// randomPAT returns a unique token in the production format
// (aether_tok_ + 28 random bytes hex) so reruns against the shared dev/test
// database cannot collide.
func randomPAT(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 28)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return "aether_tok_" + hex.EncodeToString(raw)
}

func TestCreateToken_StoresLookupHash(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-lookup-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Lookup Org")

	code, resp := doCreateToken(t, srv, jwt, "lookup-token", "")
	require.Equal(t, http.StatusCreated, code, "create token: %v", resp)
	raw := resp["token"].(string)
	id := resp["id"].(string)
	t.Cleanup(func() {
		srv.DB().Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id = $1`, id)
	})

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

	raw := randomPAT(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	require.NoError(t, err)

	var tokenID string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO api_tokens (user_id, org_id, name, token_hash) VALUES ($1, $2, 'legacy', $3) RETURNING id`,
		userID, orgID, string(hash)).Scan(&tokenID))
	t.Cleanup(func() {
		srv.DB().Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id = $1`, tokenID)
	})

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

// The fast path trusts the keyed lookup hash and does not bcrypt-verify. Break
// token_hash after creation and confirm the token still authenticates.
func TestAPIToken_FastPathSkipsBcrypt(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-fastpath-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Fastpath Org")

	code, resp := doCreateToken(t, srv, jwt, "fastpath", "")
	require.Equal(t, http.StatusCreated, code, "create token: %v", resp)
	raw := resp["token"].(string)
	id := resp["id"].(string)
	t.Cleanup(func() {
		srv.DB().Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id = $1`, id)
	})

	otherHash, err := bcrypt.GenerateFromPassword([]byte("a-different-token"), bcrypt.DefaultCost)
	require.NoError(t, err)
	_, err = srv.DB().Pool.Exec(context.Background(),
		`UPDATE api_tokens SET token_hash = $1 WHERE id = $2`, string(otherHash), id)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "fast path must not depend on token_hash: %s", rec.Body.String())
}

// A lookup hash computed with a different master key must not authenticate,
// even though the row's bcrypt hash would still match. This is the documented
// rotation behavior for rows that already have a lookup hash.
func TestAPIToken_LookupHashFromDifferentKeyRejected(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-rotation-%d@example.com", time.Now().UnixNano())
	registerAndGetToken(t, srv, email, "PAT Rotation Org")

	var userID, orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT u.id, m.org_id FROM users u JOIN org_members m ON m.user_id = u.id WHERE u.email = $1 LIMIT 1`,
		email).Scan(&userID, &orgID))

	raw := randomPAT(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	require.NoError(t, err)
	otherKey := crypto.DeriveKey("a-different-master-key-for-tests")

	var tokenID string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO api_tokens (user_id, org_id, name, token_hash, token_lookup_hash) VALUES ($1, $2, 'rotated', $3, $4) RETURNING id`,
		userID, orgID, string(hash), crypto.TokenLookupHash(otherKey, raw)).Scan(&tokenID))
	t.Cleanup(func() {
		srv.DB().Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id = $1`, tokenID)
	})

	req := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "token hashed under a different master key must be rejected")
}

func TestAPIToken_BogusRejected(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+randomPAT(t))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
