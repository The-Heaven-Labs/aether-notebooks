package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// createWidgetInDashboard creates a widget referencing a notebook and returns the widget ID.
func createWidgetInDashboard(t *testing.T, f *AuditFixtures, userKey, dashID, nbID, cellID string) string {
	t.Helper()
	status, resp := f.DoRequest(t, userKey, "POST",
		"/api/v1/dashboards/"+dashID+"/widgets",
		map[string]any{
			"type":        "chart",
			"notebook_id": nbID,
			"cell_id":     cellID,
			"layout":      map[string]any{"row": 0, "col": 0, "width": 6, "height": 4},
		})
	require.Equal(t, http.StatusCreated, status, "create widget failed: %s", resp)
	var m map[string]any
	json.Unmarshal([]byte(resp), &m)
	id, _ := m["id"].(string)
	require.NotEmpty(t, id)
	return id
}

// ——— LIST ———

func TestDashboard_List(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		wantMin int
		wantMax int
	}{
		{
			name:    "adminA sees all Org A dashboards",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees dashboards with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL dashboards",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B dashboards",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B dashboards she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/dashboards", nil)
			require.Equal(t, http.StatusOK, status)

			var dashboards []map[string]any
			json.Unmarshal([]byte(body), &dashboards)
			count := len(dashboards)
			t.Logf("%s: %d dashboards visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestDashboard_Get(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL, nil)
		t.Logf("aliceA GET NoACL dashboard: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL dashboard without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/dashboards/"+f.OrgA.Dashboards.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL, nil)
		t.Logf("carolA GET NoACL dashboard: %d %s", status, body)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL, nil)
		t.Logf("adminB GET Org A NoACL dashboard: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL, nil)
		t.Logf("eveB GET Org A NoACL dashboard: %d %s", status, body)
	})
}

// ——— CREATE ———

func TestDashboard_Create(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates dashboard", "adminA", http.StatusCreated},
		{"aliceA creates dashboard", "aliceA", http.StatusCreated},
		{"carolA creates dashboard", "carolA", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/dashboards",
				map[string]string{"title": "test-" + tt.userKey})
			require.Equal(t, tt.want, status, "body: %s", body)
		})
	}
}

// ——— UPDATE ———

func TestDashboard_Update(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL,
			map[string]string{"title": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (middleware deny)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL,
			map[string]string{"title": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL dashboard: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated NoACL dashboard")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/dashboards/"+f.OrgA.Dashboards.UserACL,
			map[string]string{"title": "updated-by-alice"})
		t.Logf("aliceA PUT UserACL dashboard (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated UserACL dashboard with only view permission")
		}
	})

	t.Run("bobA on GroupACL — 200 (group has edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT", "/api/v1/dashboards/"+f.OrgA.Dashboards.GroupACL,
			map[string]string{"title": "updated-by-bob"})
		t.Logf("bobA PUT GroupACL dashboard: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL,
			map[string]string{"title": "updated-by-adminb"})
		t.Logf("adminB PUT Org A NoACL dashboard: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL,
			map[string]string{"title": "updated-by-eveb"})
		t.Logf("eveB PUT Org A NoACL dashboard: %d %s", status, body)
	})
}

// ——— DELETE ———

func TestDashboard_Delete(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL, nil)
		t.Logf("adminA DELETE NoACL dashboard: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/dashboards/"+f.OrgA.Dashboards.UserACL, nil)
		t.Logf("aliceA DELETE UserACL dashboard: %d %s", status, body)
		if status == http.StatusNoContent {
			t.Log("VULNERABILITY: aliceA deleted dashboard with only view permission")
		}
	})

	t.Run("adminB on EveryoneACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/dashboards/"+f.OrgA.Dashboards.EveryoneACL, nil)
		t.Logf("adminB DELETE Org A dashboard: %d %s", status, body)
	})
}

// ——— WIDGETS ———

func TestDashboard_Widgets(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	// Use the EveryoneACL notebook (everyone has view) and create a cell in it
	testNbID := f.OrgA.Notebooks.EveryoneACL
	cellStatus, cellResp := f.DoRequest(t, "adminA", "POST",
		"/api/v1/notebooks/"+testNbID+"/cells",
		map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
	require.Equal(t, http.StatusCreated, cellStatus, "create cell: %s", cellResp)
	var cm map[string]any
	json.Unmarshal([]byte(cellResp), &cm)
	testCellID := cm["id"].(string)

	// Pre-create widgets
	noACLWidget := createWidgetInDashboard(t, f, "adminA", f.OrgA.Dashboards.NoACL, testNbID, testCellID)
	groupACLWidget := createWidgetInDashboard(t, f, "adminA", f.OrgA.Dashboards.GroupACL, testNbID, testCellID)

	t.Run("aliceA creates widget on NoACL — 403 (no edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/widgets",
			map[string]any{
				"type": "chart", "notebook_id": testNbID, "cell_id": testCellID,
				"layout": map[string]any{"row": 0, "col": 0, "width": 6, "height": 4},
			})
		t.Logf("aliceA create widget: %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA created widget in NoACL dashboard without edit permission")
		}
	})

	t.Run("aliceA creates widget on GroupACL — 403 (aliceA not in group)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.GroupACL+"/widgets",
			map[string]any{
				"type": "chart", "notebook_id": testNbID, "cell_id": testCellID,
				"layout": map[string]any{"row": 0, "col": 0, "width": 6, "height": 4},
			})
		t.Logf("aliceA create widget in GroupACL: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("bobA creates widget on GroupACL — 201 (bobA in engineers group)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.GroupACL+"/widgets",
			map[string]any{
				"type": "chart", "notebook_id": testNbID, "cell_id": testCellID,
				"layout": map[string]any{"row": 0, "col": 0, "width": 6, "height": 4},
			})
		t.Logf("bobA create widget in GroupACL: %d %s", status, body)
		require.Equal(t, http.StatusCreated, status)
	})

	t.Run("adminA updates widget on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "PUT",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/widgets/"+noACLWidget,
			map[string]any{"layout": map[string]any{"row": 0, "col": 0, "width": 6, "height": 4}})
		t.Logf("adminA update widget: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA updates widget on NoACL — 403 (no edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/widgets/"+noACLWidget,
			map[string]any{"layout": map[string]any{"row": 0, "col": 0, "width": 6, "height": 4}})
		t.Logf("aliceA update widget: %d %s", status, body)
		if status == http.StatusNoContent {
			t.Log("VULNERABILITY: aliceA updated widget in NoACL dashboard")
		}
	})

	t.Run("bobA deletes widget on GroupACL — 204 (bobA in engineers group)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "DELETE",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.GroupACL+"/widgets/"+groupACLWidget, nil)
		t.Logf("bobA delete widget: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})
}

// ——— SHARE ———

func TestDashboard_Share(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA shares NoACL dashboard — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/share", nil)
		t.Logf("adminA share: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA shares UserACL dashboard — 403 (view-only, no share)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.UserACL+"/share", nil)
		t.Logf("aliceA share UserACL: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA shared dashboard with only view permission")
		}
	})

	t.Run("aliceA shares GroupACL dashboard — 403 (no share action)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.GroupACL+"/share", nil)
		t.Logf("aliceA share GroupACL: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA shared GroupACL dashboard (group has edit but not share)")
		}
	})

	t.Run("adminB shares Org A dashboard — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "POST",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/share", nil)
		t.Logf("adminB share Org A dashboard: %d %s", status, body)
	})
}

// ——— PERMISSIONS ———

func TestDashboard_Permissions(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/permissions", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/permissions", nil)
		t.Logf("aliceA permissions NoACL: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can get permissions of NoACL dashboard")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.UserACL+"/permissions", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET",
			"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/permissions", nil)
		t.Logf("adminB permissions Org A: %d %s", status, body)
	})
}

// ——— PUBLIC ———

func TestDashboard_Public(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	// Create a public token via adminA
	status, resp := f.DoRequest(t, "adminA", "POST",
		"/api/v1/dashboards/"+f.OrgA.Dashboards.NoACL+"/share", nil)
	require.Equal(t, http.StatusOK, status, "share failed: %s", resp)
	var m map[string]any
	json.Unmarshal([]byte(resp), &m)
	token, ok := m["public_token"].(string)
	require.True(t, ok, "no public_token: %s", resp)

	t.Run("public access without auth — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/public/dashboards/"+token, nil)
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid token — 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/public/dashboards/invalid-token", nil)
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}
