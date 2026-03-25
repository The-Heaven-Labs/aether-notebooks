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

func createSchedule(t *testing.T, srv interface{ ServeHTTP(http.ResponseWriter, *http.Request) }, token, nbID, cronExpr string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"cron_expression": cronExpr})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/notebooks/%s/schedules", nbID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createSchedule failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

func TestScheduleUpdate(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("sched-update-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Sched Update Org")
	nbID := createNotebook(t, srv, token, "Schedule Test Notebook")

	schedID := createSchedule(t, srv, token, nbID, "0 * * * *")

	// Test 1: disable the schedule (enabled=false) → 200, enabled=false
	t.Run("disable schedule", func(t *testing.T) {
		enabled := false
		body, _ := json.Marshal(map[string]interface{}{"enabled": enabled})
		req := httptest.NewRequest("PUT", "/api/v1/schedules/"+schedID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("disable: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&resp)
		if resp["enabled"] != false {
			t.Errorf("expected enabled=false, got %v", resp["enabled"])
		}
	})

	// Test 2: change cron expression → 200, cron updated
	t.Run("update cron expression", func(t *testing.T) {
		newCron := "30 6 * * 1"
		body, _ := json.Marshal(map[string]interface{}{"cron_expression": newCron})
		req := httptest.NewRequest("PUT", "/api/v1/schedules/"+schedID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update cron: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&resp)
		if resp["cron_expression"] != newCron {
			t.Errorf("expected cron_expression=%q, got %v", newCron, resp["cron_expression"])
		}
	})

	// Test 3: empty body → 400
	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/schedules/"+schedID, bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("empty body: expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Test 4: invalid cron → 400
	t.Run("invalid cron expression", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"cron_expression": "not-a-cron"})
		req := httptest.NewRequest("PUT", "/api/v1/schedules/"+schedID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid cron: expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Test 5: not-found ID → 404
	t.Run("not found", func(t *testing.T) {
		enabled := true
		body, _ := json.Marshal(map[string]interface{}{"enabled": enabled})
		req := httptest.NewRequest("PUT", "/api/v1/schedules/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("not-found: expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
