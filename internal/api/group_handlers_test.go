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

	"github.com/stretchr/testify/require"
)

func TestGroupCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Org")

	// Reserved display name is rejected on create, case-insensitively
	for _, label := range []string{"Everyone", "everyone", " EVERYONE "} {
		reservedBody, _ := json.Marshal(map[string]any{"name": "Reserved Label", "display_name": label})
		reservedReq := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(reservedBody))
		reservedReq.Header.Set("Content-Type", "application/json")
		reservedReq.Header.Set("Authorization", "Bearer "+token)
		reservedRec := httptest.NewRecorder()
		srv.ServeHTTP(reservedRec, reservedReq)
		if reservedRec.Code != http.StatusBadRequest {
			t.Fatalf("create with display_name %q: expected 400, got %d: %s", label, reservedRec.Code, reservedRec.Body.String())
		}
	}

	// Create group
	body, _ := json.Marshal(map[string]any{"name": "Analytics", "display_name": "Analytics Label"})
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var g map[string]any
	json.NewDecoder(rec.Body).Decode(&g)
	groupID := g["id"].(string)
	if g["display_name"] != "Analytics Label" {
		t.Fatalf("expected create to return display_name, got %v", g["display_name"])
	}
	if g["source"] != "manual" {
		t.Fatalf("expected create to return source=manual, got %v", g["source"])
	}

	// List groups
	req2 := httptest.NewRequest("GET", "/api/v1/groups", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list groups: expected 200, got %d", rec2.Code)
	}
	var groups []any
	json.NewDecoder(rec2.Body).Decode(&groups)
	if len(groups) == 0 {
		t.Error("expected at least one group")
	}
	var listed map[string]any
	for _, raw := range groups {
		if m, ok := raw.(map[string]any); ok && m["id"] == groupID {
			listed = m
		}
	}
	if listed == nil {
		t.Fatal("created group missing from list")
	}
	if listed["display_name"] != "Analytics Label" {
		t.Fatalf("expected list to return display_name, got %v", listed["display_name"])
	}
	if listed["source"] != "manual" {
		t.Fatalf("expected list to return source=manual, got %v", listed["source"])
	}

	// Get current user ID
	meReq := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	srv.ServeHTTP(meRec, meReq)
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	userID := me["id"].(string)

	// Add member
	memberBody, _ := json.Marshal(map[string]string{"user_id": userID})
	req3 := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/members", bytes.NewReader(memberBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// The ?member=me variant must expose the same source contract.
	myGroupsReq := httptest.NewRequest("GET", "/api/v1/groups?member=me", nil)
	myGroupsReq.Header.Set("Authorization", "Bearer "+token)
	myGroupsRec := httptest.NewRecorder()
	srv.ServeHTTP(myGroupsRec, myGroupsReq)
	if myGroupsRec.Code != http.StatusOK {
		t.Fatalf("list my groups: expected 200, got %d: %s", myGroupsRec.Code, myGroupsRec.Body.String())
	}
	var myGroups []map[string]any
	json.NewDecoder(myGroupsRec.Body).Decode(&myGroups)
	myFound := false
	for _, g := range myGroups {
		if g["id"] == groupID {
			myFound = true
			if g["source"] != "manual" {
				t.Fatalf("expected ?member=me list to return source=manual, got %v", g["source"])
			}
		}
	}
	if !myFound {
		t.Fatal("created group missing from ?member=me list")
	}

	// List members
	req3b := httptest.NewRequest("GET", "/api/v1/groups/"+groupID+"/members", nil)
	req3b.Header.Set("Authorization", "Bearer "+token)
	rec3b := httptest.NewRecorder()
	srv.ServeHTTP(rec3b, req3b)
	if rec3b.Code != http.StatusOK {
		t.Fatalf("list members: expected 200, got %d: %s", rec3b.Code, rec3b.Body.String())
	}
	var members []any
	json.NewDecoder(rec3b.Body).Decode(&members)
	if len(members) != 1 {
		t.Errorf("expected 1 member, got %d", len(members))
	}

	// Remove member
	req4 := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID+"/members/"+userID, nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d: %s", rec4.Code, rec4.Body.String())
	}

	// Rename group
	renameBody, _ := json.Marshal(map[string]string{"name": "Data Analytics"})
	req5 := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(renameBody))
	req5.Header.Set("Content-Type", "application/json")
	req5.Header.Set("Authorization", "Bearer "+token)
	rec5 := httptest.NewRecorder()
	srv.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec5.Code, rec5.Body.String())
	}

	// Display names: set on update, rendered alongside the identity name
	labelBody, _ := json.Marshal(map[string]any{"name": "Analytics", "display_name": "Data Analysts Infra"})
	labelReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(labelBody))
	labelReq.Header.Set("Content-Type", "application/json")
	labelReq.Header.Set("Authorization", "Bearer "+token)
	labelRec := httptest.NewRecorder()
	srv.ServeHTTP(labelRec, labelReq)
	if labelRec.Code != http.StatusOK {
		t.Fatalf("set display name: expected 200, got %d: %s", labelRec.Code, labelRec.Body.String())
	}
	var labeled map[string]any
	json.NewDecoder(labelRec.Body).Decode(&labeled)
	if labeled["display_name"] != "Data Analysts Infra" {
		t.Fatalf("expected display_name to be set, got %v", labeled["display_name"])
	}
	if labeled["name"] != "Analytics" {
		t.Fatalf("expected name unchanged, got %v", labeled["name"])
	}
	if labeled["source"] != "manual" {
		t.Fatalf("expected update to return source=manual, got %v", labeled["source"])
	}

	// Reserved display name is rejected on update, case-insensitively
	for _, label := range []string{"everyone", "EvErYoNe"} {
		reservedUpdateBody, _ := json.Marshal(map[string]any{"display_name": label})
		reservedUpdateReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(reservedUpdateBody))
		reservedUpdateReq.Header.Set("Content-Type", "application/json")
		reservedUpdateReq.Header.Set("Authorization", "Bearer "+token)
		reservedUpdateRec := httptest.NewRecorder()
		srv.ServeHTTP(reservedUpdateRec, reservedUpdateReq)
		if reservedUpdateRec.Code != http.StatusBadRequest {
			t.Fatalf("update with display_name %q: expected 400, got %d: %s", label, reservedUpdateRec.Code, reservedUpdateRec.Body.String())
		}
	}

	// Whitespace-only label clears to NULL
	spaceBody, _ := json.Marshal(map[string]any{"display_name": "   "})
	spaceReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(spaceBody))
	spaceReq.Header.Set("Content-Type", "application/json")
	spaceReq.Header.Set("Authorization", "Bearer "+token)
	spaceRec := httptest.NewRecorder()
	srv.ServeHTTP(spaceRec, spaceReq)
	if spaceRec.Code != http.StatusOK {
		t.Fatalf("whitespace display name: expected 200, got %d: %s", spaceRec.Code, spaceRec.Body.String())
	}
	var spaced map[string]any
	json.NewDecoder(spaceRec.Body).Decode(&spaced)
	if spaced["display_name"] != nil {
		t.Fatalf("expected whitespace display_name cleared to null, got %v", spaced["display_name"])
	}

	// Label-only update keeps the name
	clearBody, _ := json.Marshal(map[string]any{"display_name": ""})
	clearReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(clearBody))
	clearReq.Header.Set("Content-Type", "application/json")
	clearReq.Header.Set("Authorization", "Bearer "+token)
	clearRec := httptest.NewRecorder()
	srv.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear display name: expected 200, got %d: %s", clearRec.Code, clearRec.Body.String())
	}
	var cleared map[string]any
	json.NewDecoder(clearRec.Body).Decode(&cleared)
	if cleared["display_name"] != nil {
		t.Fatalf("expected cleared display_name null, got %v", cleared["display_name"])
	}
	if cleared["name"] != "Analytics" {
		t.Fatalf("expected name kept on label-only update, got %v", cleared["name"])
	}

	// Delete group
	req6 := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID, nil)
	req6.Header.Set("Authorization", "Bearer "+token)
	rec6 := httptest.NewRecorder()
	srv.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusNoContent {
		t.Fatalf("delete group: expected 204, got %d: %s", rec6.Code, rec6.Body.String())
	}
}

func TestDeleteGroupSourceGuards(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-source-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Source Org")
	ctx := context.Background()

	create := func(name string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": name})
		req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create group: %d %s", rec.Code, rec.Body.String())
		}
		var g map[string]any
		json.NewDecoder(rec.Body).Decode(&g)
		if g["source"] != "manual" {
			t.Fatalf("created group source = %v, want manual", g["source"])
		}
		return g["id"].(string)
	}

	del := func(id, query string) int {
		t.Helper()
		req := httptest.NewRequest("DELETE", "/api/v1/groups/"+id+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	// SSO group: blocked without force, deletable with force.
	ssoID := create("SSO Managed")
	_, err := srv.DB().Pool.Exec(ctx, `UPDATE groups SET source='sso' WHERE id=$1`, ssoID)
	require.NoError(t, err)
	if code := del(ssoID, ""); code != http.StatusBadRequest {
		t.Fatalf("sso delete without force: got %d, want 400", code)
	}
	if code := del(ssoID, "?force=true"); code != http.StatusNoContent {
		t.Fatalf("sso delete with force: got %d, want 204", code)
	}
	var forcedAudits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.delete.forced' AND resource_id=$1`, ssoID,
	).Scan(&forcedAudits))
	require.Equal(t, 1, forcedAudits)

	// System group: force cannot delete it.
	systemID := create("System Managed")
	_, err = srv.DB().Pool.Exec(ctx, `UPDATE groups SET source='system' WHERE id=$1`, systemID)
	require.NoError(t, err)
	if code := del(systemID, "?force=true"); code != http.StatusBadRequest {
		t.Fatalf("system delete with force: got %d, want 400", code)
	}
	var systemExists bool
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM groups WHERE id=$1)`, systemID,
	).Scan(&systemExists))
	require.True(t, systemExists, "blocked system delete must not remove the row")

	// Unknown group: 404, with and without force.
	if code := del("00000000-0000-0000-0000-000000000000", ""); code != http.StatusNotFound {
		t.Fatalf("unknown group delete: got %d, want 404", code)
	}
	if code := del("00000000-0000-0000-0000-000000000000", "?force=true"); code != http.StatusNotFound {
		t.Fatalf("unknown group delete with force: got %d, want 404", code)
	}

	// Manual group: unchanged behavior.
	manualID := create("Manual Group")
	if code := del(manualID, ""); code != http.StatusNoContent {
		t.Fatalf("manual delete: got %d, want 204", code)
	}
}

func TestUpdateEveryoneGroupRejectsNameAndLabel(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("everyone-label-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Everyone Label Org")

	listReq := httptest.NewRequest("GET", "/api/v1/groups", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	srv.ServeHTTP(listRec, listReq)
	var groups []map[string]any
	json.NewDecoder(listRec.Body).Decode(&groups)
	var everyoneID string
	for _, g := range groups {
		if g["name"] == "Everyone" {
			everyoneID = g["id"].(string)
		}
	}
	if everyoneID == "" {
		t.Fatal("expected the org's Everyone group")
	}

	labelBody, _ := json.Marshal(map[string]any{"display_name": "All Staff"})
	labelReq := httptest.NewRequest("PUT", "/api/v1/groups/"+everyoneID, bytes.NewReader(labelBody))
	labelReq.Header.Set("Content-Type", "application/json")
	labelReq.Header.Set("Authorization", "Bearer "+token)
	labelRec := httptest.NewRecorder()
	srv.ServeHTTP(labelRec, labelReq)
	if labelRec.Code != http.StatusBadRequest {
		t.Fatalf("label on Everyone: expected 400, got %d: %s", labelRec.Code, labelRec.Body.String())
	}

	renameBody, _ := json.Marshal(map[string]any{"name": "Staff"})
	renameReq := httptest.NewRequest("PUT", "/api/v1/groups/"+everyoneID, bytes.NewReader(renameBody))
	renameReq.Header.Set("Content-Type", "application/json")
	renameReq.Header.Set("Authorization", "Bearer "+token)
	renameRec := httptest.NewRecorder()
	srv.ServeHTTP(renameRec, renameReq)
	if renameRec.Code != http.StatusBadRequest {
		t.Fatalf("rename Everyone: expected 400, got %d: %s", renameRec.Code, renameRec.Body.String())
	}
}

func TestUpdateSSOGroupRenameRequiresConfirmation(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-sso-rename-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group SSO Rename Org")
	ctx := context.Background()

	createBody, _ := json.Marshal(map[string]any{"name": "aether-analysts"})
	createReq := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created map[string]any
	require.NoError(t, json.NewDecoder(createRec.Body).Decode(&created))
	groupID := created["id"].(string)

	_, err := srv.DB().Pool.Exec(ctx, `UPDATE groups SET source='sso' WHERE id=$1`, groupID)
	require.NoError(t, err)

	put := func(body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var decoded map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&decoded)
		return rec, decoded
	}

	// Rename without confirm_name: 409 and the name stays put.
	rec, _ := put(map[string]any{"name": "renamed"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var dbName string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, groupID).Scan(&dbName))
	require.Equal(t, "aether-analysts", dbName)

	// Wrong confirm_name: still 409.
	rec, _ = put(map[string]any{"name": "renamed", "confirm_name": "not-the-name"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// A case-only change is still an identity change and needs confirmation.
	rec, _ = put(map[string]any{"name": "AETHER-ANALYSTS"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// Correct confirm_name: acknowledged rename.
	rec, decoded := put(map[string]any{"name": "renamed", "confirm_name": "aether-analysts"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "renamed", decoded["name"])
	var renameAudits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.sso.rename' AND resource_id=$1`, groupID,
	).Scan(&renameAudits))
	require.Equal(t, 1, renameAudits)
	var oldName, newName string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT metadata->>'old_name', metadata->>'new_name' FROM audit_logs WHERE action='group.sso.rename' AND resource_id=$1`,
		groupID,
	).Scan(&oldName, &newName))
	require.Equal(t, "aether-analysts", oldName)
	require.Equal(t, "renamed", newName)

	// Display-only update needs no confirmation and stays a plain group.update.
	rec, decoded = put(map[string]any{"display_name": "Data Analysts"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "renamed", decoded["name"])
	require.Equal(t, "Data Analysts", decoded["display_name"])
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.sso.rename' AND resource_id=$1`, groupID,
	).Scan(&renameAudits))
	require.Equal(t, 1, renameAudits)
	var updateAudits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.update' AND resource_id=$1`, groupID,
	).Scan(&updateAudits))
	require.Equal(t, 1, updateAudits)
}

func TestUpdateGroupDuplicateNameConflict(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-duplicate-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Duplicate Org")
	ctx := context.Background()

	create := func(name string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": name})
		req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var g map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&g))
		return g["id"].(string)
	}

	create("Alpha")
	secondID := create("Beta")

	payload, _ := json.Marshal(map[string]any{"name": "Alpha"})
	req := httptest.NewRequest("PUT", "/api/v1/groups/"+secondID, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	var dbName string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, secondID).Scan(&dbName))
	require.Equal(t, "Beta", dbName)
}
