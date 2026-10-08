package api_test

import (
	"bytes"
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

	// A freshly created connector has no health timeline: the never-used state
	// is represented by all three keys being omitted (and the NULL last_error
	// must not break the scan).
	fresh := getConnectorJSON(t, srv, token, connID)
	for _, key := range []string{"last_success_at", "last_failure_at", "last_error"} {
		_, present := fresh[key]
		require.False(t, present, "never-used connector must omit %s: %v", key, fresh)
	}

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
	failureStr, ok := got["last_failure_at"].(string)
	require.True(t, ok, "last_failure_at must be present: %v", got)
	parsedFailure, err := time.Parse(time.RFC3339Nano, failureStr)
	require.NoError(t, err)
	require.WithinDuration(t, failure, parsedFailure, time.Millisecond)
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
	listSuccessStr, ok := found["last_success_at"].(string)
	require.True(t, ok, "last_success_at must be present: %v", found)
	parsedListSuccess, err := time.Parse(time.RFC3339Nano, listSuccessStr)
	require.NoError(t, err)
	require.WithinDuration(t, success, parsedListSuccess, time.Millisecond)
	listFailureStr, ok := found["last_failure_at"].(string)
	require.True(t, ok, "last_failure_at must be present: %v", found)
	parsedListFailure, err := time.Parse(time.RFC3339Nano, listFailureStr)
	require.NoError(t, err)
	require.WithinDuration(t, failure, parsedListFailure, time.Millisecond)
}

// createConnectorWithConfig posts a postgres connector with the given raw
// config and returns its id.
func createConnectorWithConfig(t *testing.T, srv *api.Server, token, name string, config map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "type": "postgres", "config": config})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp["id"].(string)
}

// testConnectorEndpoint calls POST /connectors/{id}/test and returns the body.
func testConnectorEndpoint(t *testing.T, srv *api.Server, token, connID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/connectors/"+connID+"/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

func TestConnectorHealthRecordedOnTestEndpoint(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-test-health-%d@example.com", ts), "Conn Test Health Org")

	// Success: the helper connector points at the dev database.
	goodID := createConnector(t, srv, token)
	resp := testConnectorEndpoint(t, srv, token, goodID)
	require.Equal(t, true, resp["ok"], resp)
	good := getConnectorJSON(t, srv, token, goodID)
	require.NotNil(t, good["last_success_at"], "a successful test must persist last_success_at")
	require.Nil(t, good["last_failure_at"], "a successful test must not set last_failure_at")

	// Failure: nothing listens on port 1, so TestConnection's ping is refused.
	badID := createConnectorWithConfig(t, srv, token, "Broken DB", map[string]any{
		"host": "127.0.0.1", "port": 1, "user": "x", "password": "x", "database": "x",
	})
	resp = testConnectorEndpoint(t, srv, token, badID)
	require.Equal(t, false, resp["ok"], resp)
	bad := getConnectorJSON(t, srv, token, badID)
	require.NotNil(t, bad["last_failure_at"], "a failed test must persist last_failure_at")
	require.NotEmpty(t, bad["last_error"], "a failed test must persist the error text")
	require.Nil(t, bad["last_success_at"], "a failed test must not set last_success_at")
}

func TestConnectorHealthSuccessDebounced(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-debounce-%d@example.com", ts), "Conn Debounce Org")
	connID := createConnector(t, srv, token)

	require.Equal(t, true, testConnectorEndpoint(t, srv, token, connID)["ok"])
	first := getConnectorJSON(t, srv, token, connID)["last_success_at"]
	require.NotNil(t, first, "the first test must persist a success timestamp")

	// Without the debounce, the second write would land on a later timestamp
	// (Postgres NOW() has microsecond resolution).
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, true, testConnectorEndpoint(t, srv, token, connID)["ok"])
	second := getConnectorJSON(t, srv, token, connID)["last_success_at"]

	require.Equal(t, first, second, "success writes within the ~30s window must be debounced")
}
