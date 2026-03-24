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

func TestCellCRUD(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("cell-test-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Cell Org")
	nbID := createNotebook(t, srv, token, "Cell Test NB")

	// Create code cell
	cellBody, _ := json.Marshal(map[string]interface{}{
		"type":     "code",
		"language": "sql",
		"source":   "SELECT 1",
	})
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells", bytes.NewReader(cellBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create cell: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cellResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&cellResp)
	cellID := cellResp["id"].(string)

	// Update cell
	updateBody, _ := json.Marshal(map[string]interface{}{
		"source": "SELECT 2",
	})
	req = httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update cell: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete cell
	req = httptest.NewRequest("DELETE", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete cell: expected 204, got %d", rec.Code)
	}
}
