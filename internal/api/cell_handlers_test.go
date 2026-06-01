package api_test

import (
	"bytes"
	"context"
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

func TestCreateCellWithPosition(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("cell-pos-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Cell Pos Org")
	nbID := createNotebook(t, srv, token, "Cell Pos NB")

	// Create first cell at default position (auto: 0)
	cell1 := createCell(t, srv, token, nbID, "sql", "SELECT 1", "")
	// Create second cell at default position (auto: 1)
	cell2 := createCell(t, srv, token, nbID, "sql", "SELECT 2", "")

	_ = cell1
	_ = cell2

	// Now insert a cell at position 1 (between the two existing cells)
	body, _ := json.Marshal(map[string]interface{}{
		"type":     "code",
		"language": "sql",
		"source":   "SELECT 3",
		"position": 1,
	})
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create cell with position: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify positions are unique by checking via the database directly
	rows, err := srv.DB().Pool.Query(context.Background(), "SELECT id, position FROM cells WHERE notebook_id = $1 ORDER BY position", nbID)
	if err != nil {
		t.Fatalf("query cells: %v", err)
	}
	defer rows.Close()

	positions := map[int]bool{}
	cellCount := 0
	for rows.Next() {
		var id string
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if positions[pos] {
			t.Fatalf("duplicate position %d found for cell %s", pos, id)
		}
		positions[pos] = true
		cellCount++
	}
	if cellCount != 3 {
		t.Fatalf("expected 3 cells, got %d", cellCount)
	}
}

func TestDuplicateCellShiftsPositions(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dup-shift-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dup Shift Org")
	nbID := createNotebook(t, srv, token, "Dup Shift NB")

	// Create two cells
	createCell(t, srv, token, nbID, "sql", "SELECT 1", "")
	createCell(t, srv, token, nbID, "sql", "SELECT 2", "")

	// Get the first cell's ID
	rows, err := srv.DB().Pool.Query(context.Background(), "SELECT id FROM cells WHERE notebook_id = $1 ORDER BY position LIMIT 1", nbID)
	if err != nil {
		t.Fatalf("query cells: %v", err)
	}
	var cellID string
	if rows.Next() {
		if err := rows.Scan(&cellID); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	rows.Close()

	// Duplicate the first cell (should be inserted at position 1, shifting second cell to position 2)
	dupReq := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/duplicate", nil)
	dupReq.Header.Set("Authorization", "Bearer "+token)
	dupRec := httptest.NewRecorder()
	srv.ServeHTTP(dupRec, dupReq)

	if dupRec.Code != http.StatusCreated {
		t.Fatalf("duplicate: expected 201, got %d: %s", dupRec.Code, dupRec.Body.String())
	}

	// Verify positions are unique via the database
	rows2, err := srv.DB().Pool.Query(context.Background(), "SELECT id, position FROM cells WHERE notebook_id = $1 ORDER BY position", nbID)
	if err != nil {
		t.Fatalf("query cells: %v", err)
	}
	defer rows2.Close()

	positions := map[int]bool{}
	cellCount := 0
	for rows2.Next() {
		var id string
		var pos int
		if err := rows2.Scan(&id, &pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if positions[pos] {
			t.Fatalf("duplicate position %d found after cell duplication", pos)
		}
		positions[pos] = true
		cellCount++
	}
	if cellCount != 3 {
		t.Fatalf("expected 3 cells after duplicate, got %d", cellCount)
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
