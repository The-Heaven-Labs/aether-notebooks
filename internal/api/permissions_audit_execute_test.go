package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecuteCell_Vulnerability(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	// Create a connector via adminA (admin role required for connector creation)
	status, connResp := f.DoRequest(t, "adminA", "POST", "/api/v1/connectors", map[string]any{
		"name": "exec-test-connector",
		"type": "postgres",
		"config": map[string]any{
			"host": "localhost", "port": 5432,
			"user": "hnb", "password": "hnb_dev", "database": "hnb",
		},
	})
	require.Equal(t, http.StatusCreated, status, "connector creation failed: %s", connResp)

	var connMap map[string]any
	json.Unmarshal([]byte(connResp), &connMap)
	connID := connMap["id"].(string)

	// Seed connector:use ACL for aliceA on this connector
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'connector', $2::uuid, 'user', $3, ARRAY['use'])`,
		f.OrgA.OrgID, connID, f.UserIDs["aliceA"],
	)
	require.NoError(t, err)

	// Create a code cell in the NoACL notebook with this connector
	status, cellResp := f.DoRequest(t, "adminA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells",
		map[string]any{
			"type": "code", "language": "sql",
			"source": "SELECT 1", "connector_id": connID,
		})
	require.Equal(t, http.StatusCreated, status, "cell creation failed: %s", cellResp)

	var cellMap map[string]any
	json.Unmarshal([]byte(cellResp), &cellMap)
	cellID := cellMap["id"].(string)

	t.Logf("Connector ID: %s, Cell ID: %s, Notebook ID: %s", connID, cellID, f.OrgA.Notebooks.NoACL)

	// ——— Test 1: adminA baseline — admin bypass — should succeed ———
	status, body := f.DoRequest(t, "adminA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+cellID+"/execute", nil)
	t.Logf("adminA executes cell (baseline): %d", status)
	if status != http.StatusOK {
		t.Logf("  body: %s", body)
	}

	// ——— Test 2: aliceA has connector:use but NOT notebook:run — VULNERABILITY ———
	status, body = f.DoRequest(t, "aliceA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+cellID+"/execute", nil)
	t.Logf("aliceA executes cell (connector:use only, no notebook:run): %d", status)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA executed a cell with only connector:use permission — handleExecuteCell checks connector:use but NOT notebook:run")
	}

	// ——— Test 3: adminB cross-org — should be denied ———
	status, body = f.DoRequest(t, "adminB", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+cellID+"/execute", nil)
	t.Logf("adminB executes cell on Org A notebook (cross-org): %d %s", status, body)
	// Expected: 404 (org-scoped query fails for adminB since claims.OrgID is Org B)
	// or 403 (if middleware/intermediate check denies)

	// ——— Test 4: aliceA with explicit notebook:run ACL — should get 200 ———
	_, err = f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['run'])`,
		f.OrgA.OrgID, f.OrgA.Notebooks.NoACL, f.UserIDs["aliceA"],
	)
	require.NoError(t, err)

	status, body = f.DoRequest(t, "aliceA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+cellID+"/execute", nil)
	t.Logf("aliceA executes cell (with notebook:run now): %d", status)
	if status != http.StatusOK {
		t.Logf("  body: %s", body)
	}
}

func TestExecuteCell_NoConnectorUse(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	// Create a connector with NO ACL for aliceA
	status, connResp := f.DoRequest(t, "adminA", "POST", "/api/v1/connectors", map[string]any{
		"name": "exec-test-connector-2",
		"type": "postgres",
		"config": map[string]any{
			"host": "localhost", "port": 5432,
			"user": "hnb", "password": "hnb_dev", "database": "hnb",
		},
	})
	require.Equal(t, http.StatusCreated, status, "connector creation failed: %s", connResp)

	var connMap map[string]any
	json.Unmarshal([]byte(connResp), &connMap)
	connID := connMap["id"].(string)

	// Create a cell with this connector
	status, cellResp := f.DoRequest(t, "adminA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells",
		map[string]any{
			"type": "code", "language": "sql",
			"source": "SELECT 1", "connector_id": connID,
		})
	require.Equal(t, http.StatusCreated, status, "cell creation failed: %s", cellResp)

	var cellMap map[string]any
	json.Unmarshal([]byte(cellResp), &cellMap)
	cellID := cellMap["id"].(string)

	// aliceA has no connector:use and no notebook:run — should be 403
	status, body := f.DoRequest(t, "aliceA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/cells/"+cellID+"/execute", nil)
	t.Logf("aliceA executes cell (no permissions at all): %d", status)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA executed a cell with no connector:use or notebook:run permissions")
		t.Logf("  body: %s", body)
	}
}
