package api_test

import (
	"net/http"
	"testing"
)

// TestSchedule_Create_NoPermission: aliceA has no ACL on NoACL notebook.
// Currently no permission check — should be 403, likely returns 201 (VULNERABILITY).
func TestSchedule_Create_NoPermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	status, body := f.DoRequest(t, "aliceA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules",
		map[string]string{"cron_expression": "0 0 * * *"})
	t.Logf("aliceA creates schedule on no-ACL notebook: %d %s", status, body)
	if status == http.StatusCreated {
		t.Log("VULNERABILITY: aliceA created a schedule on a notebook with no ACL entry (deny-by-default bypass)")
	}
}

// TestSchedule_Create_ViewOnly: aliceA has view-only on UserACL notebook.
// Creating a schedule should require edit permission.
func TestSchedule_Create_ViewOnly(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	status, body := f.DoRequest(t, "aliceA", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/schedules",
		map[string]string{"cron_expression": "0 0 * * *"})
	t.Logf("aliceA creates schedule on view-only notebook: %d %s", status, body)
	if status == http.StatusCreated {
		t.Log("VULNERABILITY: aliceA created a schedule on a notebook where she only has view permission")
	}
}

// TestSchedule_Create_CrossOrg: adminB tries to create a schedule on an Org A notebook.
// Should be denied — cross-org isolation.
func TestSchedule_Create_CrossOrg(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	status, body := f.DoRequest(t, "adminB", "POST",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules",
		map[string]string{"cron_expression": "0 0 * * *"})
	t.Logf("adminB creates schedule on Org A notebook: %d %s", status, body)
	if status == http.StatusCreated {
		t.Log("VULNERABILITY: adminB created a schedule on a cross-org notebook (org scoping failed)")
	}
}

// TestSchedule_List_NoPermission: aliceA lists schedules on NoACL notebook — should be denied.
func TestSchedule_List_NoPermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	// adminA baseline
	status, body := f.DoRequest(t, "adminA", "GET",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules", nil)
	t.Logf("adminA lists schedules on no-ACL notebook (baseline): %d %s", status, body)

	// aliceA — no ACL
	status, body = f.DoRequest(t, "aliceA", "GET",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules", nil)
	t.Logf("aliceA lists schedules on no-ACL notebook: %d %s", status, body)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA listed schedules on a notebook with no ACL entry")
	}
}

// TestSchedule_List_CrossOrg: adminB lists schedules on Org A notebook — should be denied.
func TestSchedule_List_CrossOrg(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	status, body := f.DoRequest(t, "adminB", "GET",
		"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules", nil)
	t.Logf("adminB lists schedules on Org A notebook: %d %s", status, body)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: adminB listed schedules on a cross-org notebook")
	}
}

// TestSchedule_Get_NoPermission: aliceA tries to GET a schedule on a notebook she has
// view-only access to. The schedule handler has no permission check — should be 403.
func TestSchedule_Get_NoPermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.UserACL, "0 0 * * *")

	status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/schedules/"+schedID, nil)
	t.Logf("aliceA gets schedule (view-only notebook): %d %s", status, body)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA fetched a schedule on a view-only notebook (should require edit)")
	}
}

// TestSchedule_Get_CrossOrg: adminB tries to GET an Org A schedule — should be 404 (org scoped).
func TestSchedule_Get_CrossOrg(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.NoACL, "0 0 * * *")

	status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/schedules/"+schedID, nil)
	t.Logf("adminB gets Org A schedule (cross-org): %d %s", status, body)
}

// TestSchedule_Update_NoPermission: aliceA tries to update a schedule on a view-only notebook.
func TestSchedule_Update_NoPermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.UserACL, "0 0 * * *")

	status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/schedules/"+schedID,
		map[string]any{"enabled": false})
	t.Logf("aliceA updates schedule (view-only notebook): %d %s", status, body)
	if status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA updated a schedule on a view-only notebook")
	}
}

// TestSchedule_Update_CrossOrg: adminB tries to update an Org A schedule.
func TestSchedule_Update_CrossOrg(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.NoACL, "0 0 * * *")

	status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/schedules/"+schedID,
		map[string]any{"enabled": false})
	t.Logf("adminB updates Org A schedule (cross-org): %d %s", status, body)
}

// TestSchedule_Delete_NoPermission: aliceA tries to delete a schedule on a view-only notebook.
func TestSchedule_Delete_NoPermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.UserACL, "0 0 * * *")

	status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/schedules/"+schedID, nil)
	t.Logf("aliceA deletes schedule (view-only notebook): %d %s", status, body)
	if status == http.StatusNoContent || status == http.StatusOK {
		t.Log("VULNERABILITY: aliceA deleted a schedule on a view-only notebook")
	}
}

// TestSchedule_Delete_CrossOrg: adminB tries to delete an Org A schedule.
func TestSchedule_Delete_CrossOrg(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.NoACL, "0 0 * * *")

	status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/schedules/"+schedID, nil)
	t.Logf("adminB deletes Org A schedule (cross-org): %d %s", status, body)
}

// TestSchedule_Update_AdminBaseline: adminA can update their own org's schedule.
func TestSchedule_Update_AdminBaseline(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.NoACL, "0 0 * * *")

	status, body := f.DoRequest(t, "adminA", "PUT", "/api/v1/schedules/"+schedID,
		map[string]any{"cron_expression": "30 6 * * 1"})
	t.Logf("adminA updates own schedule (baseline): %d %s", status, body)
}

// TestSchedule_Delete_AdminBaseline: adminA can delete their own org's schedule.
func TestSchedule_Delete_AdminBaseline(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	schedID := createSchedule(t, f.srv, f.Tokens["adminA"], f.OrgA.Notebooks.NoACL, "0 0 * * *")

	status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/schedules/"+schedID, nil)
	t.Logf("adminA deletes own schedule (baseline): %d %s", status, body)
}
