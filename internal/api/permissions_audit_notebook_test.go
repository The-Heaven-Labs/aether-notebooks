package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ———

func TestNotebook_List(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name     string
		userKey  string
		wantMin  int
		wantMax  int
	}{
		{
			name:    "adminA sees all Org A notebooks",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees notebooks with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL notebooks",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B notebooks",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B notebooks she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/notebooks", nil)
			require.Equal(t, http.StatusOK, status)

			var notebooks []map[string]any
			json.Unmarshal([]byte(body), &notebooks)
			count := len(notebooks)
			t.Logf("%s: %d notebooks visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin, "%s should see at least %d notebooks", tt.userKey, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax, "%s should see at most %d notebooks", tt.userKey, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestNotebook_Get(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL, nil)
		t.Logf("aliceA GET NoACL notebook: %d", status)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL notebook without ACL entry")
			t.Logf("  body: %s", body)
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL, nil)
		t.Logf("carolA GET NoACL notebook: %d", status)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: carolA can view NoACL notebook without ACL entry")
			t.Logf("  body: %s", body)
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL, nil)
		t.Logf("adminB GET Org A NoACL notebook (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL, nil)
		t.Logf("eveB GET Org A NoACL notebook (cross-org): %d %s", status, body)
	})
}

// ——— CREATE ———

func TestNotebook_Create(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates notebook", "adminA", http.StatusCreated},
		{"aliceA creates notebook", "aliceA", http.StatusCreated},
		{"carolA creates notebook", "carolA", http.StatusCreated},
		{"adminB creates notebook", "adminB", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/notebooks",
				map[string]string{"title": "test-" + tt.userKey})
			require.Equal(t, tt.want, status, "body: %s", body)
		})
	}
}

// ——— UPDATE ———

func TestNotebook_Update(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL,
			map[string]string{"title": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (middleware deny)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL,
			map[string]string{"title": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL notebook: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated NoACL notebook (requirePermission middleware allowed)")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL,
			map[string]string{"title": "updated-by-alice"})
		t.Logf("aliceA PUT UserACL notebook (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated UserACL notebook with only view permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org handler scoping)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL,
			map[string]string{"title": "updated-by-adminb"})
		t.Logf("adminB PUT Org A NoACL notebook (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL,
			map[string]string{"title": "updated-by-eveb"})
		t.Logf("eveB PUT Org A NoACL notebook (cross-org): %d %s", status, body)
	})
}

// ——— DELETE ———

func TestNotebook_Delete(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	// Create dedicated notebooks for delete tests to avoid affecting other tests
	var deleteNbs []string
	for _, label := range []string{"delete-noacl", "delete-useracl", "delete-noacl-cross"} {
		status, resp := f.DoRequest(t, "adminA", "POST", "/api/v1/notebooks",
			map[string]string{"title": label + "-" + f.OrgA.OrgID[:8]})
		require.Equal(t, http.StatusCreated, status, "create failed: %s", resp)
		var m map[string]any
		json.Unmarshal([]byte(resp), &m)
		deleteNbs = append(deleteNbs, m["id"].(string))
	}

	// Seed UserACL on second notebook for aliceA
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view'])`,
		f.OrgA.OrgID, deleteNbs[1], f.UserIDs["aliceA"],
	)
	require.NoError(t, err)

	t.Run("adminA on NoACL — 204 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "DELETE", "/api/v1/notebooks/"+deleteNbs[0], nil)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (view-only, no delete)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/notebooks/"+deleteNbs[1], nil)
		t.Logf("aliceA DELETE UserACL notebook (view-only): %d %s", status, body)
		if status == http.StatusNoContent || status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA deleted notebook with only view permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/notebooks/"+deleteNbs[2], nil)
		t.Logf("adminB DELETE Org A NoACL notebook (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "DELETE", "/api/v1/notebooks/"+deleteNbs[2], nil)
		t.Logf("eveB DELETE Org A NoACL notebook (cross-org): %d %s", status, body)
	})
}

// ——— PERMISSIONS ———

func TestNotebook_Permissions(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/permissions", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/permissions", nil)
		t.Logf("aliceA GET permissions NoACL notebook: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can get permissions of NoACL notebook without ACL")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/permissions", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/permissions", nil)
		t.Logf("adminB GET permissions Org A notebook (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/permissions", nil)
		t.Logf("eveB GET permissions Org A notebook (cross-org): %d %s", status, body)
	})
}

// ——— EXPORT ———

func TestNotebook_Export(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (baseline)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/export", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — no permission check (vulnerability)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/export", nil)
		t.Logf("aliceA exports NoACL notebook: %d", status)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA exported a notebook with no ACL entry — handleExportNotebook has no permission check, only org scoping")
			t.Logf("  body (truncated): %.100s", body)
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/export", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/export", nil)
		t.Logf("adminB exports Org A notebook (cross-org): %d %s", status, body)
	})
}

// ——— CELL OPERATIONS ———

func TestNotebook_CreateCell(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 201 (baseline)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells",
			map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
		require.Equal(t, http.StatusCreated, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells",
			map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
		t.Logf("aliceA creates cell in NoACL notebook: %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA created a cell in a notebook with no ACL")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only, no create)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/cells",
			map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
		t.Logf("aliceA creates cell in UserACL notebook (view-only): %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA created a cell in a notebook with only view permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org handler scoping)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells",
			map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
		t.Logf("adminB creates cell in Org A notebook (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells",
			map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
		t.Logf("eveB creates cell in Org A notebook (cross-org): %d %s", status, body)
	})
}

func TestNotebook_UpdateCell(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	// Create cells in the various notebooks using adminA
	noACLCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.NoACL)
	userACLCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.UserACL)

	t.Run("adminA on NoACL — 200 (baseline)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell,
			map[string]any{"source": "SELECT 2"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell,
			map[string]any{"source": "SELECT 999"})
		t.Logf("aliceA updates cell in NoACL notebook: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated a cell in a notebook with no ACL")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only, no edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/cells/"+userACLCell,
			map[string]any{"source": "SELECT 999"})
		t.Logf("aliceA updates cell in UserACL notebook (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated a cell in a notebook with only view permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell,
			map[string]any{"source": "SELECT 999"})
		t.Logf("adminB updates cell in Org A notebook (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell,
			map[string]any{"source": "SELECT 999"})
		t.Logf("eveB updates cell in Org A notebook (cross-org): %d %s", status, body)
	})
}

func TestNotebook_DeleteCell(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	noACLCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.NoACL)
	userACLCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.UserACL)

	t.Run("adminA on NoACL — 204 (baseline)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "DELETE",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell, nil)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (view-only, no edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/cells/"+userACLCell, nil)
		t.Logf("aliceA deletes cell in UserACL notebook (view-only): %d %s", status, body)
		if status == http.StatusNoContent || status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA deleted a cell in a notebook with only view permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		// Need a fresh cell since adminA already deleted noACLCell
		c := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.NoACL)
		status, body := f.DoRequest(t, "adminB", "DELETE",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+c, nil)
		t.Logf("adminB deletes cell in Org A notebook (cross-org): %d %s", status, body)
	})
}

func TestNotebook_DuplicateCell(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	noACLCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.NoACL)
	userACLCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.UserACL)

	t.Run("adminA on NoACL — 201 (baseline)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell+"/duplicate", nil)
		require.Equal(t, http.StatusCreated, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell+"/duplicate", nil)
		t.Logf("aliceA duplicates cell in NoACL notebook: %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA duplicated a cell in a notebook with no ACL")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only, no create)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/cells/"+userACLCell+"/duplicate", nil)
		t.Logf("aliceA duplicates cell in UserACL notebook (view-only): %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA duplicated a cell in a notebook with only view permission")
		}
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "POST",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+noACLCell+"/duplicate", nil)
		t.Logf("adminB duplicates cell in Org A notebook (cross-org): %d %s", status, body)
	})
}

func TestNotebook_Cell_Groups_And_EveryoneACL(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	// bobA is in the "engineers" group which has view+edit on GroupACL notebook
	groupCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.GroupACL)

	t.Run("bobA updates cell in GroupACL — 200 (group has edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.GroupACL+"/cells/"+groupCell,
			map[string]any{"source": "SELECT 42"})
		t.Logf("bobA updates cell in GroupACL notebook: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	// aliceA has view-only on UserACL, but EveryoneACL gives everyone view
	everyoneCell := createCellInNotebook(t, f, "adminA", f.OrgA.Notebooks.EveryoneACL)

	t.Run("carolA reads everyone-ACL notebook cell — 200 (everyone has view)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.EveryoneACL, nil)
		t.Logf("carolA GET EveryoneACL notebook: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA edits cell in EveryoneACL (view-only) — 403", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "PUT",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.EveryoneACL+"/cells/"+everyoneCell,
			map[string]any{"source": "SELECT 7"})
		t.Logf("carolA updates cell in EveryoneACL notebook: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: carolA edited a cell in an everyone-ACL notebook (should be view-only)")
		}
	})
}

// ——— HELPERS ———

// createCellInNotebook creates a simple code cell using adminA and returns the cell ID.
func createCellInNotebook(t *testing.T, f *AuditFixtures, userKey, nbID string) string {
	t.Helper()
	status, resp := f.DoRequest(t, userKey, "POST",
		"/api/v1/notebooks/"+nbID+"/cells",
		map[string]any{"type": "code", "language": "sql", "source": "SELECT 1"})
	require.Equal(t, http.StatusCreated, status, "create cell failed: %s", resp)
	var m map[string]any
	json.Unmarshal([]byte(resp), &m)
	id, _ := m["id"].(string)
	require.NotEmpty(t, id)
	return id
}
