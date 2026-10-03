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

func TestHandleListConnectorDatabases(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("conn-db-test-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Conn DB Org")
	connID := createConnector(t, srv, token)

	req := httptest.NewRequest("GET", "/api/v1/connectors/"+connID+"/databases", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string][]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["databases"] == nil {
		t.Fatal("expected databases key in response")
	}
}

func TestUpdateConnector(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("update-conn-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "UpdateConn Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "OriginalName",
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

	updateBody, _ := json.Marshal(map[string]interface{}{"name": "UpdatedName"})
	req = httptest.NewRequest("PUT", "/api/v1/connectors/"+connID, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated["name"] != "UpdatedName" {
		t.Fatalf("expected name UpdatedName, got %v", updated["name"])
	}
}

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

// OpenSearch use_tls used to be silently dropped because the create request
// decoded config into models.ConnectorConfig. Raw JSON must preserve it.
func TestCreateConnectorPreservesDriverSpecificConfig(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("raw-config-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Raw Config Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "TLS OpenSearch",
		"type": "opensearch",
		"config": map[string]interface{}{
			"host": "localhost", "port": 9200, "use_tls": true,
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
	config := resp["config"].(map[string]interface{})
	if config["use_tls"] != true {
		t.Fatalf("expected use_tls=true in response, got %v", config["use_tls"])
	}

	// Config must be a JSON object; arrays/strings are rejected.
	badBody, _ := json.Marshal(map[string]interface{}{
		"name": "Bad", "type": "opensearch", "config": []string{"nope"},
	})
	req = httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(badBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-object config: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
