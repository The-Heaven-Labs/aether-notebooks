package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCellVersioning_FirstSaveCreatesVersion(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("ver1-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "VerOrg1")
	nbID := createNotebook(t, srv, token, "VNB")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	// Update cell source
	body, _ := json.Marshal(map[string]string{"source": "SELECT 2"})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update cell: %d %s", rec.Code, rec.Body.String())
	}

	// Fetch history
	req2 := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get versions: %d %s", rec2.Code, rec2.Body.String())
	}
	var versions []map[string]any
	json.NewDecoder(rec2.Body).Decode(&versions)
	if len(versions) == 0 {
		t.Fatal("expected at least one version")
	}
}

func TestCellVersioning_SmallEditMerges(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("ver2-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "VerOrg2")
	nbID := createNotebook(t, srv, token, "VNB2")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	updateCellSource := func(source string) {
		body, _ := json.Marshal(map[string]string{"source": source})
		req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("update: %d", rec.Code)
		}
	}

	// Two saves with small diff (< 50 chars, < 60s) — should merge into 1 version
	updateCellSource("SELECT 1")
	updateCellSource("SELECT 2") // only 1 char diff

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var versions []map[string]any
	json.NewDecoder(rec.Body).Decode(&versions)
	if len(versions) != 1 {
		t.Fatalf("expected 1 merged version, got %d", len(versions))
	}
	if versions[0]["source"] != "SELECT 2" {
		t.Fatalf("expected merged source 'SELECT 2', got %v", versions[0]["source"])
	}
}

func TestCellVersioning_LargeDiffCreatesNewVersion(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("ver3-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "VerOrg3")
	nbID := createNotebook(t, srv, token, "VNB3")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	updateCellSource := func(source string) {
		body, _ := json.Marshal(map[string]string{"source": source})
		req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}

	// First save
	updateCellSource("SELECT 1")
	// Large diff: 50+ chars changed
	updateCellSource("SELECT id, name, created_at, updated_at FROM users WHERE active = true ORDER BY created_at DESC")

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var versions []map[string]any
	json.NewDecoder(rec.Body).Decode(&versions)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions for large diff, got %d", len(versions))
	}
}

// levenshtein is duplicated here for the test to be self-contained
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[la][lb]
}
func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

var _ = strings.Contains // avoid unused import
