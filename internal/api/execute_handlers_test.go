package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecuteCell(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("exec-test-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Exec Org")

	connID := createConnector(t, srv, token)
	nbID := createNotebook(t, srv, token, "Exec NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1 AS result", connID)

	// Execute cell
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("execute: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	outputs := resp["outputs"].([]interface{})
	if len(outputs) == 0 {
		t.Fatal("expected at least one output")
	}
}
