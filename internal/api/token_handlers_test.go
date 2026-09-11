package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

var patFormat = regexp.MustCompile(`^aether_tok_[0-9a-f]{56}$`)

func doCreateToken(t *testing.T, srv http.Handler, authToken, name, expiresAt string) (int, map[string]any) {
	t.Helper()
	payload := map[string]string{"name": name}
	if expiresAt != "" {
		payload["expires_at"] = expiresAt
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp
}

// Regression test for https://github.com/The-Heaven-Labs/aether-notebooks PAT bug:
// the generated token ("aether_tok_" + hex(32 bytes) = 75 bytes) exceeded bcrypt's
// 72-byte limit, so POST /api/v1/tokens always failed with 500 "failed to hash token".
// The generator now uses 28 random bytes (67 bytes total) so hashing must succeed.
func TestCreateToken_SucceedsWithinBcryptLimit(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Org")

	code, resp := doCreateToken(t, srv, jwt, "my-token", "")
	require.Equal(t, http.StatusCreated, code, "create token response: %v", resp)

	raw, ok := resp["token"].(string)
	require.True(t, ok, "response should contain raw token, got %v", resp)
	require.Regexp(t, patFormat, raw)
	require.Len(t, raw, 67, "token must be 11-byte prefix + 56 hex chars (28 random bytes)")
	require.LessOrEqual(t, len(raw), 72, "token must fit bcrypt's 72-byte limit")

	id, ok := resp["id"].(string)
	require.True(t, ok && id != "", "response should contain token id")

	// Stored hash must roundtrip against the full presented token.
	var hash string
	err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT token_hash FROM api_tokens WHERE id = $1`, id).Scan(&hash)
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw)))
}

func TestCreateToken_RequiresName(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-noname-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Org")

	code, _ := doCreateToken(t, srv, jwt, "", "")
	require.Equal(t, http.StatusBadRequest, code)
}

func TestCreateToken_RequiresAuth(t *testing.T) {
	srv := setupTestServer(t)
	code, _ := doCreateToken(t, srv, "", "no-auth", "")
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestCreateToken_InvalidExpiresAt(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-exp-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Org")

	code, _ := doCreateToken(t, srv, jwt, "bad-expiry", "not-a-date")
	require.Equal(t, http.StatusBadRequest, code)
}

func TestAPIToken_AuthenticatesListAndRevocation(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-auth-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Org")

	code, resp := doCreateToken(t, srv, jwt, "auth-token", "")
	require.Equal(t, http.StatusCreated, code, "create token: %v", resp)
	raw := resp["token"].(string)
	id := resp["id"].(string)

	// PAT authenticates: list tokens with the PAT as bearer.
	listReq := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	listReq.Header.Set("Authorization", "Bearer "+raw)
	listRec := httptest.NewRecorder()
	srv.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code, "PAT should authenticate list: %s", listRec.Body.String())

	var listed []map[string]any
	require.NoError(t, json.NewDecoder(listRec.Body).Decode(&listed))
	found := false
	for _, tok := range listed {
		if tok["id"] == id {
			found = true
		}
	}
	require.True(t, found, "newly created token should appear in list")

	// Revoke via JWT delete, then the PAT must stop working.
	delReq := httptest.NewRequest("DELETE", "/api/v1/tokens/"+id, nil)
	delReq.Header.Set("Authorization", "Bearer "+jwt)
	delRec := httptest.NewRecorder()
	srv.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusOK, delRec.Code, "delete token: %s", delRec.Body.String())

	retryReq := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	retryReq.Header.Set("Authorization", "Bearer "+raw)
	retryRec := httptest.NewRecorder()
	srv.ServeHTTP(retryRec, retryReq)
	require.Equal(t, http.StatusUnauthorized, retryRec.Code, "revoked PAT must be rejected")
}

func TestAPIToken_ExpiredIsRejected(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("pat-expired-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "PAT Org")

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	code, resp := doCreateToken(t, srv, jwt, "expired-token", past)
	require.Equal(t, http.StatusCreated, code, "create expired token: %v", resp)
	raw := resp["token"].(string)

	req := httptest.NewRequest("GET", "/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "expired PAT must be rejected")
}
