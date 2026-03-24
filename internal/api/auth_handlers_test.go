package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
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
	return api.NewServer(db, jwt, auditLogger)
}

func TestRegisterAndLogin(t *testing.T) {
	srv := setupTestServer(t)

	// Use a unique email per test run to avoid conflicts
	email := fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())

	// Register
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "securepass123",
		"name":     "Test User",
		"org_name": "Test Org",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var regResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&regResp)
	if regResp["token"] == nil {
		t.Fatal("register: expected token in response")
	}

	// Login
	body, _ = json.Marshal(map[string]string{
		"email":    email,
		"password": "securepass123",
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var loginResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&loginResp)
	if loginResp["token"] == nil {
		t.Fatal("login: expected token in response")
	}
}
