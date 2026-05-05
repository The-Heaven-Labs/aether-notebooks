package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachmentUploadAndGet(t *testing.T) {
	ctx := setupAttachTestContext(t)
	nbID := createTestNotebook(t, ctx.srv, ctx.token)

	// Upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.png")
	io.WriteString(fw, "fake-png-data")
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+ctx.token)
	rw := httptest.NewRecorder()
	ctx.srv.ServeHTTP(rw, req)
	require.Equal(t, http.StatusCreated, rw.Code)

	var resp map[string]any
	json.NewDecoder(rw.Body).Decode(&resp)
	attID := resp["id"].(string)
	assert.NotEmpty(t, attID)
	assert.Equal(t, "test.png", resp["filename"])

	// Get
	getReq := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	getReq.Header.Set("Authorization", "Bearer "+ctx.token)
	getRW := httptest.NewRecorder()
	ctx.srv.ServeHTTP(getRW, getReq)
	assert.Equal(t, http.StatusOK, getRW.Code)
	assert.Equal(t, "fake-png-data", getRW.Body.String())
}

func TestAttachmentList(t *testing.T) {
	ctx := setupAttachTestContext(t)
	nbID := createTestNotebook(t, ctx.srv, ctx.token)

	// Upload two attachments
	uploadTestAttachment(t, ctx.srv, ctx.token, nbID)
	uploadTestAttachment(t, ctx.srv, ctx.token, nbID)

	// List
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID+"/attachments", nil)
	req.Header.Set("Authorization", "Bearer "+ctx.token)
	w := httptest.NewRecorder()
	ctx.srv.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp map[string]any
	json.NewDecoder(w.Body).Decode(&listResp)
	atts := listResp["attachments"].([]any)
	assert.Len(t, atts, 2)
}

func TestAttachmentDelete(t *testing.T) {
	ctx := setupAttachTestContext(t)
	nbID := createTestNotebook(t, ctx.srv, ctx.token)
	attID := uploadTestAttachment(t, ctx.srv, ctx.token, nbID)

	req := httptest.NewRequest("DELETE", "/api/v1/attachments/"+attID, nil)
	req.Header.Set("Authorization", "Bearer "+ctx.token)
	w := httptest.NewRecorder()
	ctx.srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Subsequent GET returns 404
	getReq := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	getReq.Header.Set("Authorization", "Bearer "+ctx.token)
	getRW := httptest.NewRecorder()
	ctx.srv.ServeHTTP(getRW, getReq)
	assert.Equal(t, http.StatusNotFound, getRW.Code)
}

func TestAttachmentNotFound(t *testing.T) {
	ctx := setupAttachTestContext(t)

	req := httptest.NewRequest("GET", "/api/v1/attachments/00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("Authorization", "Bearer "+ctx.token)
	w := httptest.NewRecorder()
	ctx.srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAttachmentUploadNotebookNotFound(t *testing.T) {
	ctx := setupAttachTestContext(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.txt")
	io.WriteString(fw, "data")
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/00000000-0000-0000-0000-000000000000/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+ctx.token)
	w := httptest.NewRecorder()
	ctx.srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAttachmentGetRequiresOrgMembership(t *testing.T) {
	ctx := setupAttachTestContext(t)
	nbID := createTestNotebook(t, ctx.srv, ctx.token)
	attID := uploadTestAttachment(t, ctx.srv, ctx.token, nbID)

	// Second user in a different org — must not see the attachment
	token2 := registerAndGetToken(t, ctx.srv, fmt.Sprintf("other-%d@example.com", time.Now().UnixNano()), "Other Org")

	req := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	req.Header.Set("Authorization", "Bearer "+token2)
	w := httptest.NewRecorder()
	ctx.srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAttachmentGetDeniedWhenNotebookACLRestrictsViewer(t *testing.T) {
	ts := time.Now().UnixNano()
	actx := setupAttachTestContext(t)

	// Create notebook as admin and capture org_id
	nbBody, _ := json.Marshal(map[string]string{"title": "ACL Restricted NB"})
	nbReq := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(nbBody))
	nbReq.Header.Set("Content-Type", "application/json")
	nbReq.Header.Set("Authorization", "Bearer "+actx.token)
	nbRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(nbRec, nbReq)
	require.Equal(t, http.StatusCreated, nbRec.Code)
	var nbResp map[string]any
	json.NewDecoder(nbRec.Body).Decode(&nbResp)
	nbID := nbResp["id"].(string)
	orgID := nbResp["org_id"].(string)

	// Get admin user ID for ACL entry
	meReq := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+actx.token)
	meRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(meRec, meReq)
	require.Equal(t, http.StatusOK, meRec.Code)
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	adminID := me["id"].(string)

	// Upload attachment as admin
	attID := uploadTestAttachment(t, actx.srv, actx.token, nbID)

	// Set ACL on notebook so only admin can view (explicit user entry, no org-wide grant)
	aclBody, _ := json.Marshal(map[string]any{
		"entries": []map[string]any{
			{"subject_type": "user", "subject_id": adminID, "actions": []string{"view", "edit", "delete"}},
		},
	})
	aclReq := httptest.NewRequest("PUT", "/api/v1/acl/notebook/"+nbID, bytes.NewReader(aclBody))
	aclReq.Header.Set("Content-Type", "application/json")
	aclReq.Header.Set("Authorization", "Bearer "+actx.token)
	aclRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(aclRec, aclReq)
	require.Equal(t, http.StatusOK, aclRec.Code)

	// Register second user and invite them to the same org as viewer
	viewerEmail := fmt.Sprintf("viewer-acl-%d@example.com", ts)
	registerAndGetToken(t, actx.srv, viewerEmail, "Viewer Own Org2")

	inviteBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "role": "viewer"})
	inviteReq := httptest.NewRequest("POST", "/api/v1/members", bytes.NewReader(inviteBody))
	inviteReq.Header.Set("Content-Type", "application/json")
	inviteReq.Header.Set("Authorization", "Bearer "+actx.token)
	inviteRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(inviteRec, inviteReq)
	require.Equal(t, http.StatusNoContent, inviteRec.Code)

	// Login as viewer scoped to the admin's org
	loginBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "password": "pass123", "org_id": orgID})
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)
	var loginResp map[string]any
	json.NewDecoder(loginRec.Body).Decode(&loginResp)
	viewerToken := loginResp["token"].(string)
	require.NotEmpty(t, viewerToken)

	// Viewer tries to GET the attachment — should receive 404 (no notebook read access)
	req := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	w := httptest.NewRecorder()
	actx.srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAttachmentUploadTooLarge(t *testing.T) {
	ctx := setupAttachTestContext(t)
	nbID := createTestNotebook(t, ctx.srv, ctx.token)

	// Set a very small limit so any file exceeds it
	ctx.srv.SetMaxAttachmentBytes(1)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.png")
	io.WriteString(fw, "fake-png-data")
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+ctx.token)
	rw := httptest.NewRecorder()
	ctx.srv.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rw.Code)
}

func TestAttachmentUploadRequiresWritePermission(t *testing.T) {
	ts := time.Now().UnixNano()
	actx := setupAttachTestContext(t)

	// Create notebook and capture the org_id from the response
	nbBody, _ := json.Marshal(map[string]string{"title": "Perm Test NB"})
	nbReq := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(nbBody))
	nbReq.Header.Set("Content-Type", "application/json")
	nbReq.Header.Set("Authorization", "Bearer "+actx.token)
	nbRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(nbRec, nbReq)
	require.Equal(t, http.StatusCreated, nbRec.Code)
	var nbResp map[string]any
	json.NewDecoder(nbRec.Body).Decode(&nbResp)
	nbID := nbResp["id"].(string)
	orgID := nbResp["org_id"].(string)

	// Register a second user in their own org, then invite them as viewer into the admin's org
	viewerEmail := fmt.Sprintf("viewer-%d@example.com", ts)
	registerAndGetToken(t, actx.srv, viewerEmail, "Viewer Own Org")

	// Invite viewer into the admin's org as "viewer"
	inviteBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "role": "viewer"})
	inviteReq := httptest.NewRequest("POST", "/api/v1/members", bytes.NewReader(inviteBody))
	inviteReq.Header.Set("Content-Type", "application/json")
	inviteReq.Header.Set("Authorization", "Bearer "+actx.token)
	inviteRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(inviteRec, inviteReq)
	require.Equal(t, http.StatusNoContent, inviteRec.Code)

	// Login as viewer, specifying the admin's org so token is scoped to that org
	loginBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "password": "pass123", "org_id": orgID})
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	actx.srv.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)
	var loginResp map[string]any
	json.NewDecoder(loginRec.Body).Decode(&loginResp)
	viewerToken := loginResp["token"].(string)
	require.NotEmpty(t, viewerToken, "could not get viewer token")

	// Try to upload as viewer — should be forbidden
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.txt")
	io.WriteString(fw, "data")
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	w := httptest.NewRecorder()
	actx.srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
