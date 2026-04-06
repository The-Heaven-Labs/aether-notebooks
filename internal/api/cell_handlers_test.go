package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDuplicateCell(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dup-cell-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "DupCell Org")
	nbID := createNotebook(t, srv, token, "Dup Cell NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", "")

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/duplicate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var dup map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dup)
	if dup["source"] != "SELECT 1" {
		t.Fatalf("expected source 'SELECT 1', got %v", dup["source"])
	}
	if dup["position"].(float64) != 1 {
		t.Fatalf("expected position 1, got %v", dup["position"])
	}
}

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

func TestCellParameters(t *testing.T) {
	s := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("cell-params-%d@example.com", ts)
	token := registerAndGetToken(t, s, email, "Cell Params Org")
	nbID := createNotebook(t, s, token, "Param Test NB")
	cellID := createCell(t, s, token, nbID, "sql", "SELECT 1", "")

	params := `[{"name":"start_date","type":"string","default":"2024-01-01"}]`
	body := fmt.Sprintf(`{"source":"SELECT 1","parameters":%s}`, params)
	req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT cell: got %d, body: %s", w.Code, w.Body.String())
	}

	var cell map[string]interface{}
	json.NewDecoder(w.Body).Decode(&cell)
	params_resp, ok := cell["parameters"].([]interface{})
	if !ok || len(params_resp) != 1 {
		t.Fatalf("expected 1 parameter, got %v", cell["parameters"])
	}
	p := params_resp[0].(map[string]interface{})
	if p["name"] != "start_date" {
		t.Fatalf("expected name=start_date, got %v", p["name"])
	}
}
