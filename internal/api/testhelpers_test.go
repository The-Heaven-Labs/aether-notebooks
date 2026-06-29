package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/cache"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/storage"
	"github.com/stretchr/testify/require"
)

const testOrgID = "00000000-0000-0000-0000-000000000001"
const testUserID = "00000000-0000-0000-0000-000000000002"

var testJWT = auth.NewJWTIssuer("test-secret", 15*time.Minute)

// withAdminClaims attaches an admin JWT token to the request Authorization header.
func withAdminClaims(r *http.Request, orgID string) *http.Request {
	token, err := testJWT.Issue(testUserID, orgID, "admin")
	if err != nil {
		panic("withAdminClaims: " + err.Error())
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

// withPlatformAdminClaims attaches a platform-admin JWT token to the request Authorization header.
func withPlatformAdminClaims(r *http.Request) *http.Request {
	token, err := testJWT.IssuePlatformAdmin(testUserID, testOrgID, "admin")
	if err != nil {
		panic("withPlatformAdminClaims: " + err.Error())
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func setupTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	redisURL := os.Getenv("AETHER_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	c, err := cache.New(redisURL)
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("cache ping: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func setupTestServer(t *testing.T) *api.Server {
	t.Helper()
	db := setupTestDB(t)
	jwt := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	auditLogger := audit.NewLogger(db)
	key := crypto.DeriveKey("test-master-key-for-tests-only!")
	redisCache := setupTestCache(t)
	srv := api.NewServer(db, jwt, auditLogger, key, redisCache)
	srv.SetMaxAttachmentBytes(10 << 20) // 10 MB default for tests
	return srv
}

func registerAndGetToken(t *testing.T, srv *api.Server, email, orgName string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "pass123", "name": "Test",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("register failed: %d %s", rec.Code, rec.Body.String())
	}
	var regResp map[string]any
	json.NewDecoder(rec.Body).Decode(&regResp)
	onboardingToken, ok := regResp["onboarding_token"].(string)
	if !ok {
		t.Fatalf("register: expected onboarding_token in response, got %v", regResp)
	}

	// Create org using onboarding token
	uniqueOrg := fmt.Sprintf("%s %d", orgName, time.Now().UnixNano())
	orgBody, _ := json.Marshal(map[string]string{"org_name": uniqueOrg})
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewReader(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgRec := httptest.NewRecorder()
	srv.ServeHTTP(orgRec, orgReq)
	if orgRec.Code != 201 {
		t.Fatalf("org create failed: %d %s", orgRec.Code, orgRec.Body.String())
	}
	var orgResp map[string]any
	json.NewDecoder(orgRec.Body).Decode(&orgResp)
	token, ok := orgResp["token"].(string)
	if !ok {
		t.Fatalf("org create: expected token in response, got %v", orgResp)
	}
	return token
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
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

func createConnector(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name": "Test DB",
		"type": "postgres",
		"config": map[string]any{
			"host": "localhost", "port": 5432,
			"user": "aether", "password": "aether_dev", "database": "aether",
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
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

// withEditorClaims attaches an editor JWT token to the request Authorization header.
func withEditorClaims(r *http.Request, orgID string) *http.Request {
	token, err := testJWT.Issue(testUserID, orgID, "editor")
	if err != nil {
		panic("withEditorClaims: " + err.Error())
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

// setupTestServerWithAttachDir creates a test server with a temporary attachment directory.
func setupTestServerWithAttachDir(t *testing.T) *api.Server {
	t.Helper()
	s := setupTestServer(t)
	st, err := storage.NewLocalStorage(t.TempDir())
	require.NoError(t, err)
	s.SetStorage(st)
	return s
}

// attachTestContext holds server + auth token for attachment tests.
type attachTestContext struct {
	srv   *api.Server
	token string
}

// setupAttachTestContext creates a server with attach dir + a registered user and returns both.
func setupAttachTestContext(t *testing.T) attachTestContext {
	t.Helper()
	s := setupTestServerWithAttachDir(t)
	email := fmt.Sprintf("attach-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, s, email, "Attach Org")
	return attachTestContext{srv: s, token: token}
}

// createTestNotebook creates a notebook via the API using the given bearer token and returns its ID.
func createTestNotebook(t *testing.T, s http.Handler, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": "Test Notebook"})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createTestNotebook failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

// uploadTestAttachment uploads a small test file to a notebook and returns the attachment ID.
func uploadTestAttachment(t *testing.T, s http.Handler, token, nbID string) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.txt")
	io.WriteString(fw, "test-content")
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("uploadTestAttachment failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

func createCell(t *testing.T, srv *api.Server, token, nbID, lang, source, connID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
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
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}
