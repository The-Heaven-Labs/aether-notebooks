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

func TestConnectorCRUD(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("conn-test-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Conn Org")

	// Create connector
	body, _ := json.Marshal(map[string]interface{}{
		"name": "Dev Postgres",
		"type": "postgres",
		"config": map[string]interface{}{
			"host": "localhost", "port": 5432,
			"user": "dev", "password": "secret", "database": "analytics",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	connID := resp["id"].(string)

	// Verify password is masked
	config := resp["config"].(map[string]interface{})
	if config["password"] != "***" {
		t.Fatal("expected password to be masked in response")
	}

	// List connectors
	req = httptest.NewRequest("GET", "/api/v1/connectors", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/v1/connectors/"+connID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}
