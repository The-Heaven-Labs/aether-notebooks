package api_test

import (
	"bytes"
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
	req.Header.Set("X-AETHER-Admin-Mode", "true")
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

func TestExecuteCellLimitWithTrailingSemicolon(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("exec-limit-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Exec Limit Org")

	connID := createConnector(t, srv, token)
	nbID := createNotebook(t, srv, token, "Exec Limit NB")

	// Create a cell with source that has trailing semicolons and whitespace
	// This tests that TrimSpace+TrimRight properly handles "SELECT 1;\n" → "SELECT 1" before appending LIMIT
	cellBody, _ := json.Marshal(map[string]any{
		"type":         "code",
		"language":     "sql",
		"source":       "SELECT 1 AS x;\n",
		"connector_id": connID,
	})
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells", bytes.NewReader(cellBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create cell: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cellResp map[string]any
	json.NewDecoder(rec.Body).Decode(&cellResp)
	cellID := cellResp["id"].(string)

	// Execute the cell — this should succeed, not produce a "syntax error at or near LIMIT"
	execReq := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/execute", nil)
	execReq.Header.Set("Authorization", "Bearer "+token)
	execReq.Header.Set("X-AETHER-Admin-Mode", "true")
	execRec := httptest.NewRecorder()
	srv.ServeHTTP(execRec, execReq)

	if execRec.Code != http.StatusOK {
		t.Fatalf("execute cell with trailing semicolon: expected 200, got %d: %s", execRec.Code, execRec.Body.String())
	}

	var execResp map[string]any
	json.NewDecoder(execRec.Body).Decode(&execResp)
	outputs, ok := execResp["outputs"].([]interface{})
	if !ok || len(outputs) == 0 {
		t.Fatal("expected at least one output from cell with trailing semicolon")
	}
}
