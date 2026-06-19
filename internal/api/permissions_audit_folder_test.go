package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ROOT CONTENTS ———

func TestFolder_List(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA lists root contents — sees all Org A root folders", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET", "/api/v1/folders", nil)
		require.Equal(t, http.StatusOK, status)

		var contents map[string]any
		json.Unmarshal([]byte(body), &contents)
		folders := contents["folders"].([]any)
		t.Logf("adminA sees %d root folders", len(folders))
		require.GreaterOrEqual(t, len(folders), 4)
	})

	t.Run("aliceA lists root contents — sees only accessible folders", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/folders", nil)
		require.Equal(t, http.StatusOK, status)

		var contents map[string]any
		json.Unmarshal([]byte(body), &contents)
		folders := contents["folders"].([]any)
		t.Logf("aliceA sees %d root folders", len(folders))
		require.GreaterOrEqual(t, len(folders), 1)
	})

	t.Run("carolA lists root contents — sees nothing (deny-by-default)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/folders", nil)
		require.Equal(t, http.StatusOK, status)

		var contents map[string]any
		json.Unmarshal([]byte(body), &contents)
		folders := contents["folders"].([]any)
		t.Logf("carolA sees %d root folders", len(folders))
		// carolA has no ACLs and owns no folders
	})

	t.Run("adminB lists root contents — sees nothing from Org A (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/folders", nil)
		require.Equal(t, http.StatusOK, status)

		var contents map[string]any
		json.Unmarshal([]byte(body), &contents)
		folders := contents["folders"].([]any)
		t.Logf("adminB sees %d root folders", len(folders))
	})
}

// ——— GET (VIEW) ———

func TestFolder_Get(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL folder — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/folders/"+f.OrgA.Folders.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL folder — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/folders/"+f.OrgA.Folders.NoACL, nil)
		t.Logf("aliceA GET NoACL folder: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL folder without ACL entry")
		}
	})

	t.Run("aliceA on UserACL folder — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/folders/"+f.OrgA.Folders.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL folder — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/folders/"+f.OrgA.Folders.NoACL, nil)
		t.Logf("carolA GET NoACL folder: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: carolA can view NoACL folder without ACL entry")
		}
	})

	t.Run("adminB on NoACL folder — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/folders/"+f.OrgA.Folders.NoACL, nil)
		t.Logf("adminB GET Org A NoACL folder (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL folder — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/folders/"+f.OrgA.Folders.NoACL, nil)
		t.Logf("eveB GET Org A NoACL folder (cross-org): %d %s", status, body)
	})
}

// ——— CREATE ———

func TestFolder_Create(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates folder", "adminA", http.StatusCreated},
		{"aliceA creates folder", "aliceA", http.StatusCreated},
		{"carolA creates folder", "carolA", http.StatusCreated},
		{"adminB creates folder", "adminB", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/folders",
				map[string]string{"name": "test-" + tt.userKey})
			require.Equal(t, tt.want, status, "body: %s", body)
		})
	}
}

// ——— UPDATE ———

func TestFolder_Update(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL folder — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/folders/"+f.OrgA.Folders.NoACL,
			map[string]string{"name": "renamed-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL folder — 403 (middleware deny)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/folders/"+f.OrgA.Folders.NoACL,
			map[string]string{"name": "renamed-by-alice"})
		t.Logf("aliceA PUT NoACL folder: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated NoACL folder (requirePermission middleware allowed)")
		}
	})

	t.Run("aliceA on UserACL folder — 403 (view-only ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/folders/"+f.OrgA.Folders.UserACL,
			map[string]string{"name": "renamed-by-alice"})
		t.Logf("aliceA PUT UserACL folder (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated UserACL folder with only view permission")
		}
	})

	t.Run("adminB on NoACL folder — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/folders/"+f.OrgA.Folders.NoACL,
			map[string]string{"name": "renamed-by-adminb"})
		t.Logf("adminB PUT Org A NoACL folder (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL folder — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/folders/"+f.OrgA.Folders.NoACL,
			map[string]string{"name": "renamed-by-eveb"})
		t.Logf("eveB PUT Org A NoACL folder (cross-org): %d %s", status, body)
	})
}

// ——— DELETE ———

func TestFolder_Delete(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	// Create dedicated folders for delete tests (adminA owns them)
	var deleteFolders []string
	for i := 0; i < 3; i++ {
		status, resp := f.DoRequest(t, "adminA", "POST", "/api/v1/folders",
			map[string]string{"name": "delete-test-folder"})
		require.Equal(t, http.StatusCreated, status)
		var m map[string]any
		json.Unmarshal([]byte(resp), &m)
		deleteFolders = append(deleteFolders, m["id"].(string))
	}

	// Seed UserACL on second folder for aliceA
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view'])`,
		f.OrgA.OrgID, deleteFolders[1], f.UserIDs["aliceA"],
	)
	require.NoError(t, err)

	t.Run("adminA on NoACL folder — 204 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "DELETE", "/api/v1/folders/"+deleteFolders[0], nil)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL folder — 403 (view-only, no delete)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/folders/"+deleteFolders[1], nil)
		t.Logf("aliceA DELETE UserACL folder (view-only): %d %s", status, body)
		if status == http.StatusNoContent || status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA deleted folder with only view permission")
		}
	})

	t.Run("adminB on NoACL folder — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/folders/"+deleteFolders[2], nil)
		t.Logf("adminB DELETE Org A NoACL folder (cross-org): %d %s", status, body)
	})

	t.Run("eveB on NoACL folder — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "DELETE", "/api/v1/folders/"+deleteFolders[2], nil)
		t.Logf("eveB DELETE Org A NoACL folder (cross-org): %d %s", status, body)
	})
}

// ——— ANCESTORS ———

func TestFolder_Ancestors(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL folder — 200 (baseline)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET",
			"/api/v1/folders/"+f.OrgA.Folders.NoACL+"/ancestors", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL folder — no permission check (vulnerability)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/folders/"+f.OrgA.Folders.NoACL+"/ancestors", nil)
		t.Logf("aliceA GET ancestors of NoACL folder: %d", status)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view ancestors of NoACL folder — handleGetFolderAncestors has no permission check")
			t.Logf("  body: %s", body)
		}
	})

	t.Run("aliceA on UserACL folder — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/folders/"+f.OrgA.Folders.UserACL+"/ancestors", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL folder — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET",
			"/api/v1/folders/"+f.OrgA.Folders.NoACL+"/ancestors", nil)
		t.Logf("adminB GET ancestors of Org A folder (cross-org): %d %s", status, body)
	})
}

// ——— ENSURE HOME FOLDER ———

func TestFolder_EnsureHome(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA ensures home folder", "adminA", http.StatusOK},
		{"aliceA ensures home folder", "aliceA", http.StatusOK},
		{"carolA ensures home folder", "carolA", http.StatusOK},
		{"adminB ensures home folder", "adminB", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/users/me/home", nil)
			t.Logf("%s ensures home folder: %d", tt.userKey, status)
			// Should be 200 (existing) or 201 (created) — no permission check
			if status != http.StatusOK && status != http.StatusCreated {
				t.Logf("  unexpected status: %s", body)
			}
		})
	}
}

// ——— FOLDER HIERARCHY INHERITANCE ———

func TestFolder_HierarchyInheritance(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)
	ctx := context.Background()

	// 1. Create a parent folder (adminA)
	status, resp := f.DoRequest(t, "adminA", "POST", "/api/v1/folders",
		map[string]string{"name": "hierarchy-parent"})
	require.Equal(t, http.StatusCreated, status)
	var m map[string]any
	json.Unmarshal([]byte(resp), &m)
	parentID := m["id"].(string)

	// 2. Seed ACL on parent: aliceA gets view access on this folder
	_, err := f.srv.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view'])`,
		f.OrgA.OrgID, parentID, f.UserIDs["aliceA"],
	)
	require.NoError(t, err)

	// 3. Create a child folder under parent
	status, resp = f.DoRequest(t, "adminA", "POST", "/api/v1/folders",
		map[string]any{"name": "hierarchy-child", "parent_id": parentID})
	require.Equal(t, http.StatusCreated, status)
	json.Unmarshal([]byte(resp), &m)
	childID := m["id"].(string)

	// 4. Create a notebook inside child folder
	status, resp = f.DoRequest(t, "adminA", "POST", "/api/v1/notebooks",
		map[string]any{"title": "nb-in-child", "folder_id": childID})
	require.Equal(t, http.StatusCreated, status)
	json.Unmarshal([]byte(resp), &m)
	nbInChild := m["id"].(string)

	// 5. Create a notebook outside the hierarchy (no folder)
	status, resp = f.DoRequest(t, "adminA", "POST", "/api/v1/notebooks",
		map[string]string{"title": "nb-outside"})
	require.Equal(t, http.StatusCreated, status)
	json.Unmarshal([]byte(resp), &m)
	nbOutside := m["id"].(string)

	// — Test: aliceA can view notebook inside child folder (inherited from parent ACL) —
	status, body := f.DoRequest(t, "aliceA", "GET",
		"/api/v1/notebooks/"+nbInChild, nil)
	t.Logf("aliceA GET notebook in child folder (inherited ACL): %d", status)
	if status != http.StatusOK {
		t.Logf("  body: %s", body)
	}
	require.Equal(t, http.StatusOK, status,
		"aliceA should inherit view permission from parent folder ACL")

	// — Test: aliceA cannot view notebook outside hierarchy —
	status, body = f.DoRequest(t, "aliceA", "GET",
		"/api/v1/notebooks/"+nbOutside, nil)
	t.Logf("aliceA GET notebook outside hierarchy: %d", status)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA can view a notebook with no ACL and no folder — no inheritance chain")
		t.Logf("  body: %s", body)
	}
}

// ——— BOB A GROUP ACL ON FOLDER ———

func TestFolder_GroupACL(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("bobA gets GroupACL folder — 200 (group view+edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "GET",
			"/api/v1/folders/"+f.OrgA.Folders.GroupACL, nil)
		require.Equal(t, http.StatusOK, status,
			"bobA should have view access via engineers group")
	})

	t.Run("bobA updates GroupACL folder — 200 (group edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "PUT",
			"/api/v1/folders/"+f.OrgA.Folders.GroupACL,
			map[string]string{"name": "group-renamed-by-bob"})
		require.Equal(t, http.StatusOK, status,
			"bobA should have edit access via engineers group")
	})
}
