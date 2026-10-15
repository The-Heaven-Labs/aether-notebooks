package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetRecent(t *testing.T) {
	srv := setupTestServer(t)

	email := fmt.Sprintf("recent-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Recent Org")

	// Create a notebook so there's something to appear in recent
	_ = createNotebook(t, srv, token, "Recent NB")

	req := httptest.NewRequest("GET", "/api/v1/recent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var items []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one recent item")
	}

	first := items[0]
	if _, ok := first["type"]; !ok {
		t.Error("missing type field")
	}
	if _, ok := first["id"]; !ok {
		t.Error("missing id field")
	}
	if _, ok := first["name"]; !ok {
		t.Error("missing name field")
	}
	if _, ok := first["updated_at"]; !ok {
		t.Error("missing updated_at field")
	}
}

// TestGetRecentBatchVisibility pins the batched view filtering, the
// newest-first order, and the 10-visible cap of GET /recent after the A5
// migration: a hidden newest candidate is skipped without shrinking the cap,
// and only visible items are returned.
func TestGetRecentBatchVisibility(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	const total = 12
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		id := f.insertNotebook(t, fmt.Sprintf("recent-batch-%02d", i), nil, f.otherUserID)
		ids = append(ids, id)
		// i=0 is the newest candidate and stays hidden; every other notebook
		// is granted to the caller with a successively older updated_at, so
		// 11 visible candidates must cap at 10 — skipping the hidden one.
		if i > 0 {
			f.grant(t, "notebook", id, []string{"view"})
		}
		_, err := f.srv.DB().Pool.Exec(context.Background(),
			`UPDATE notebooks SET updated_at = now() - ($1 * interval '1 minute') WHERE id = $2`, i, id)
		require.NoError(t, err)
	}

	status, body := f.get(t, "/api/v1/recent")
	require.Equal(t, http.StatusOK, status, string(body))
	var items []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(body, &items))

	require.Len(t, items, 10, "the visible cap stays 10 even when a newer candidate is hidden")
	for i, item := range items {
		require.Equal(t, "notebook", item.Type)
		require.Equal(t, ids[i+1], item.ID,
			"recent must stay newest-first and skip the hidden newest candidate")
	}
}
