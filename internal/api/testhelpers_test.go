package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/database"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func setupTestServer(t *testing.T) *api.Server {
	t.Helper()
	db := setupTestDB(t)
	jwt := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	auditLogger := audit.NewLogger(db)
	key := crypto.DeriveKey("test-master-key-for-tests-only!")
	return api.NewServer(db, jwt, auditLogger, key)
}

func registerAndGetToken(t *testing.T, srv *api.Server, email, orgName string) string {
	t.Helper()
	// Make slug unique by appending a timestamp
	uniqueOrg := fmt.Sprintf("%s %d", orgName, time.Now().UnixNano())
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "pass123", "name": "Test", "org_name": uniqueOrg,
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("register failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["token"].(string)
}

func createNotebook(t *testing.T, srv *api.Server, token, title string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createNotebook failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

func createConnector(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name": "Test DB",
		"type": "postgres",
		"config": map[string]interface{}{
			"host": "localhost", "port": 5432,
			"user": "hnb", "password": "hnb_dev", "database": "hnb",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createConnector failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

func createCell(t *testing.T, srv *api.Server, token, nbID, lang, source, connID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"type":         "code",
		"language":     lang,
		"source":       source,
		"connector_id": connID,
	})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/notebooks/%s/cells", nbID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createCell failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}
