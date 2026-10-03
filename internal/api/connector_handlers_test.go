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

// A case-variant key ("Password") is consumed by the driver's case-insensitive
// JSON decode, so it must be masked exactly like the canonical key.
func TestConnectorSecretMaskingIsCaseInsensitive(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("case-mask-%d@example.com", ts), "Case Mask Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "Case Keys", "type": "postgres",
		"config": map[string]interface{}{
			"host": "localhost", "port": 5432, "user": "u",
			"Password": "sekret-case", "database": "d",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sekret-case") {
		t.Fatalf("case-variant secret leaked: %s", rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	cfg := resp["config"].(map[string]interface{})
	if cfg["Password"] != "***" {
		t.Fatalf("expected Password masked, got %v", cfg["Password"])
	}
}

// The CLI sends "config": null when --config is omitted; that must behave like
// an absent config, not a 400.
func TestCreateConnectorNullConfigDefaultsToEmpty(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("null-cfg-%d@example.com", ts), "Null Cfg Org")

	body, _ := json.Marshal(map[string]interface{}{"name": "Null Cfg", "type": "postgres", "config": nil})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("null config: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The OpenSearch use_tls fix must survive get and update round-trips.
func TestConnectorRawConfigRoundTrip(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("config-roundtrip-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Roundtrip Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "TLS OS", "type": "opensearch",
		"config": map[string]interface{}{"host": "localhost", "use_tls": true},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&created)
	connID := created["id"].(string)

	// GET returns the driver-specific field.
	req = httptest.NewRequest("GET", "/api/v1/connectors/"+connID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var fetched map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&fetched)
	if fetched["config"].(map[string]interface{})["use_tls"] != true {
		t.Fatal("GET must return use_tls=true")
	}

	// Update toggles it off and preserves unknown behavior via key merge.
	upd, _ := json.Marshal(map[string]interface{}{
		"config": map[string]interface{}{"use_tls": false},
	})
	req = httptest.NewRequest("PUT", "/api/v1/connectors/"+connID, bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var updated map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&updated)
	cfg := updated["config"].(map[string]interface{})
	if cfg["use_tls"] != false {
		t.Fatalf("update must set use_tls=false, got %v", cfg["use_tls"])
	}
	if cfg["host"] != "localhost" {
		t.Fatalf("update must keep host, got %v", cfg["host"])
	}
}

// The Task 2 review required an end-to-end pin: PUTs echoing the masked ("***")
// or an empty password must not clobber the stored secret. The saved-connector
// test endpoint authenticates with the stored credential, so ok=true proves it
// survived.
func TestUpdateConnectorKeepsStoredSecret(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("keep-secret-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Keep Secret Org")
	connID := createConnector(t, srv, token)

	for _, payload := range []map[string]interface{}{
		{"config": map[string]interface{}{"password": ""}},
		{"config": map[string]interface{}{"password": "***"}},
	} {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("PUT", "/api/v1/connectors/"+connID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest("POST", "/api/v1/connectors/"+connID+"/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test endpoint: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["ok"] != true {
		t.Fatalf("stored password was clobbered; connection test: %v", resp)
	}
}

func TestTestConnectorConfigRejectsNonObject(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("test-cfg-%d@example.com", ts), "Test Cfg Org")

	body, _ := json.Marshal(map[string]interface{}{"type": "opensearch", "config": "not-an-object"})
	req := httptest.NewRequest("POST", "/api/v1/connectors/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with ok=false, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["ok"] != false {
		t.Fatalf("expected ok=false, got %v", resp)
	}
	if resp["error"] != "config must be a JSON object" {
		t.Fatalf("expected object-validation error, got %v", resp)
	}
}
