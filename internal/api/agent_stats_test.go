package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/api"
)

func seedStatsMessages(t *testing.T, srv *api.Server, sessionID string, now time.Time) {
	t.Helper()
	// Anchor every row to the start of the current hour so both messages land
	// in one hourly bucket. Seeding at now and now-1m split them across two
	// buckets whenever the test ran within a minute of an hour boundary, and
	// the hour-grain per-agent endpoint then returned two rows.
	at := now.Truncate(time.Hour)
	for _, tok := range [][3]int{{100, 50, 7}, {40, 10, 3}} {
		_, err := srv.DB().Pool.Exec(context.Background(), `
			INSERT INTO agent_messages (id, session_id, role, content, tool_calls, tokens_input, tokens_output, tokens_direct, model_calls, duration_ms, created_at)
			VALUES ($1,$2,'assistant','hi','[]',$3,$4,$5,1,200,$6)
		`, uuid.New().String(), sessionID, tok[0], tok[1], tok[2], at)
		if err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO subagent_tasks (id, parent_session_id, goal, status, result, tokens_input, tokens_output, created_at, completed_at)
		VALUES ($1,$2,'g','completed','{}',30,20,$3,$3)
	`, uuid.New().String(), sessionID, at)
	if err != nil {
		t.Fatalf("seed subagent task: %v", err)
	}
}

func setupStatsServer(t *testing.T) (*api.Server, string, string, string, string) {
	t.Helper()
	srv := setupTestServer(t)
	email := fmt.Sprintf("stats-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Stats Org")
	nbID := createNotebook(t, srv, token, "Stats NB")
	mcID := createModelConfig(t, srv, token)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)
	seedStatsMessages(t, srv, sessionID, time.Now())
	return srv, token, agentID, sessionID, email
}

func TestAgentStatsRollupEndpoint(t *testing.T) {
	srv, token, _, _, email := setupStatsServer(t)

	t.Run("admin rolls up", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/agents/stats/rollup", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("rollup: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		rolled, _ := resp["rolled_up"].(map[string]any)
		if rolled == nil || rolled["rows"] == nil || rolled["bucket_from"] == nil || rolled["bucket_to"] == nil {
			t.Fatalf("bad rolled_up shape: %v", resp)
		}
		if rows, _ := rolled["rows"].(float64); rows < 1 {
			t.Fatalf("expected >= 1 rolled row, got %v", rolled)
		}

		// Idempotent: second run succeeds with same bucket count.
		req2 := httptest.NewRequest("POST", "/api/v1/agents/stats/rollup", nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("re-rollup: expected 200, got %d", rec2.Code)
		}
	})

	t.Run("audit entry written", func(t *testing.T) {
		var count int
		err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM audit_logs WHERE action='agent_stats.rollup'`).Scan(&count)
		if err != nil || count < 1 {
			t.Fatalf("expected audit entry, count=%d err=%v", count, err)
		}
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		// Demote to viewer and re-login for a fresh role claim.
		var userID, orgID string
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT u.id, m.org_id FROM users u JOIN org_members m ON m.user_id=u.id WHERE u.email=$1`,
			email).Scan(&userID, &orgID); err != nil {
			t.Fatalf("lookup user: %v", err)
		}
		if _, err := srv.DB().Pool.Exec(context.Background(),
			`UPDATE org_members SET role='editor' WHERE user_id=$1 AND org_id=$2`, userID, orgID); err != nil {
			t.Fatalf("demote: %v", err)
		}
		lbody, _ := json.Marshal(map[string]string{"email": email, "password": "pass123", "org_id": orgID})
		lreq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(lbody))
		lreq.Header.Set("Content-Type", "application/json")
		lrec := httptest.NewRecorder()
		srv.ServeHTTP(lrec, lreq)
		if lrec.Code != http.StatusOK {
			t.Fatalf("login: expected 200, got %d: %s", lrec.Code, lrec.Body.String())
		}
		var lresp map[string]any
		json.NewDecoder(lrec.Body).Decode(&lresp)
		viewerToken, _ := lresp["token"].(string)

		vreq := httptest.NewRequest("POST", "/api/v1/agents/stats/rollup", nil)
		vreq.Header.Set("Authorization", "Bearer "+viewerToken)
		vrec := httptest.NewRecorder()
		srv.ServeHTTP(vrec, vreq)
		if vrec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for viewer, got %d: %s", vrec.Code, vrec.Body.String())
		}
	})
}

func TestAgentStatsQueryParams(t *testing.T) {
	srv, token, agentID, _, _ := setupStatsServer(t)

	rollup := func() {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/v1/agents/stats/rollup", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("rollup: %d %s", rec.Code, rec.Body.String())
		}
	}
	rollup()

	get := func(q string) (int, []map[string]any) {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/agents/stats"+q, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var rows []map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&rows)
		return rec.Code, rows
	}

	t.Run("default day grain with names", func(t *testing.T) {
		code, rows := get("")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %v", rows)
		}
		r := rows[0]
		for _, k := range []string{"bucket_start", "agent_id", "agent_name", "user_id", "user_name", "user_email",
			"sessions_count", "messages_count", "tokens_input", "tokens_output", "tokens_direct",
			"tokens_subagent", "model_calls", "total_duration_ms", "est_cost_usd"} {
			if _, ok := r[k]; !ok {
				t.Fatalf("missing field %s in %v", k, r)
			}
		}
		if r["agent_name"] == "" || r["user_email"] == "" {
			t.Fatalf("names must resolve: %v", r)
		}
		if r["messages_count"] != float64(2) || r["tokens_subagent"] != float64(50) {
			t.Fatalf("bad aggregates: %v", r)
		}
	})

	t.Run("hour grain last-hour window", func(t *testing.T) {
		now := time.Now().UTC()
		q := fmt.Sprintf("?granularity=hour&from=%s&to=%s",
			now.Add(-time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))
		code, rows := get(q)
		if code != http.StatusOK || len(rows) != 1 {
			t.Fatalf("expected 1 hourly row, got %d %v", code, rows)
		}
	})

	t.Run("agent filter", func(t *testing.T) {
		code, rows := get("?agent_id=" + agentID)
		if code != http.StatusOK || len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d %v", code, rows)
		}
		code, rows = get("?agent_id=" + uuid.New().String())
		if code != http.StatusOK || len(rows) != 0 {
			t.Fatalf("expected 0 rows, got %d %v", code, rows)
		}
	})

	t.Run("per-agent endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents/"+agentID+"/stats?granularity=hour", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var rows []map[string]any
		json.NewDecoder(rec.Body).Decode(&rows)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %v", rows)
		}
	})

	t.Run("bad params", func(t *testing.T) {
		for _, q := range []string{"?from=nope", "?to=nope", "?granularity=week", "?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z"} {
			req := httptest.NewRequest("GET", "/api/v1/agents/stats"+q, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %q, got %d", q, rec.Code)
			}
		}
	})

	t.Run("user filter", func(t *testing.T) {
		var userID string
		if err := srv.DB().Pool.QueryRow(context.Background(),
			`SELECT id FROM users WHERE email LIKE 'stats-%@example.com' ORDER BY created_at DESC LIMIT 1`).Scan(&userID); err != nil {
			t.Fatalf("user: %v", err)
		}
		code, rows := get("?user_id=" + userID)
		if code != http.StatusOK || len(rows) < 1 {
			t.Fatalf("expected >=1 row, got %d %v", code, rows)
		}
		code, rows = get("?user_id=" + uuid.New().String())
		if code != http.StatusOK || len(rows) != 0 {
			t.Fatalf("expected 0 rows, got %d %v", code, rows)
		}
	})
}
