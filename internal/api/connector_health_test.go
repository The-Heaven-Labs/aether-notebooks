package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/database"
)

// updateConnectorHealth writes the health columns directly so read paths can
// be asserted independently of the recording call sites.
func updateConnectorHealth(t *testing.T, db *database.DB, connID string, lastSuccess, lastFailure *time.Time, lastError string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE connectors SET last_success_at = $2, last_failure_at = $3, last_error = $4 WHERE id = $1`,
		connID, lastSuccess, lastFailure, lastError)
	require.NoError(t, err)
}

// getConnectorJSON fetches a connector through the API and decodes the full
// response body, so nil (omitted) health fields are observable as absent keys.
func getConnectorJSON(t *testing.T, srv *api.Server, token, connID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/connectors/"+connID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

func TestConnectorHealthFieldsRoundTrip(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-health-%d@example.com", ts), "Conn Health Org")
	connID := createConnector(t, srv, token)

	success := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	failure := time.Now().UTC().Truncate(time.Microsecond)
	updateConnectorHealth(t, db, connID, &success, &failure, "dial tcp: connection refused")

	// GET returns the timeline.
	got := getConnectorJSON(t, srv, token, connID)
	successStr, ok := got["last_success_at"].(string)
	require.True(t, ok, "last_success_at must be present: %v", got)
	parsedSuccess, err := time.Parse(time.RFC3339Nano, successStr)
	require.NoError(t, err)
	require.WithinDuration(t, success, parsedSuccess, time.Millisecond)
	require.Equal(t, "dial tcp: connection refused", got["last_error"])

	// List returns the same fields.
	req := httptest.NewRequest("GET", "/api/v1/connectors", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	var found map[string]any
	for _, c := range list {
		if c["id"] == connID {
			found = c
			break
		}
	}
	require.NotNil(t, found, "connector %s missing from list", connID)
	require.Equal(t, "dial tcp: connection refused", found["last_error"])
	failureStr, ok := found["last_failure_at"].(string)
	require.True(t, ok, "last_failure_at must be present: %v", found)
	parsedFailure, err := time.Parse(time.RFC3339Nano, failureStr)
	require.NoError(t, err)
	require.WithinDuration(t, failure, parsedFailure, time.Millisecond)
}
