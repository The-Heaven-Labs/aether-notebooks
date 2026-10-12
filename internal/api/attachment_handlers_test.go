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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
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

	inviteBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "role": "non-admin"})
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

	// Register a second user in their own org, then invite them into the admin's org
	viewerEmail := fmt.Sprintf("viewer-%d@example.com", ts)
	registerAndGetToken(t, actx.srv, viewerEmail, "Viewer Own Org")

	// Invite viewer into the admin's org
	inviteBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "role": "non-admin"})
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

// testLogoPNG is a minimal PNG signature + payload; DetectContentType only
// needs the leading bytes to classify it as image/png.
var testLogoPNG = []byte("\x89PNG\r\n\x1a\nlogo-bytes")

// uploadFileTo POSTs a multipart file to path with the given bearer token.
func uploadFileTo(t *testing.T, srv *api.Server, token, path, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// uploadOrgAttachment uploads a logo through POST /org/attachments and returns
// its attachment id.
func uploadOrgAttachment(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	rec := uploadFileTo(t, srv, token, "/api/v1/org/attachments", "logo.png", testLogoPNG)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	attID, _ := resp["id"].(string)
	require.NotEmpty(t, attID)
	return attID
}

// inviteNonAdminMember registers a fresh user, adds them to orgID as a
// non-admin member, and returns a session token scoped to that org.
func inviteNonAdminMember(t *testing.T, srv *api.Server, adminToken, orgID string) string {
	t.Helper()
	email := fmt.Sprintf("member-%d@example.com", time.Now().UnixNano())
	registerAndGetToken(t, srv, email, "Member Own Org")

	body, _ := json.Marshal(map[string]string{"email": email, "role": "non-admin"})
	req := httptest.NewRequest("POST", "/api/v1/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	loginBody, _ := json.Marshal(map[string]string{"email": email, "password": "pass123", "org_id": orgID})
	loginReq := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	srv.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code, loginRec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(loginRec.Body).Decode(&resp))
	token, _ := resp["token"].(string)
	require.NotEmpty(t, token)
	return token
}

func TestOrgAttachmentUpload(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "500")
	srv := setupTestServerWithAttachDir(t)
	orgID, token := createTestOrgAndAdmin(t, srv)

	// An org admin uploads an org-scoped logo (no notebook involved).
	rec := uploadFileTo(t, srv, token, "/api/v1/org/attachments", "logo.png", testLogoPNG)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	attID, _ := resp["id"].(string)
	require.NotEmpty(t, attID)
	assert.Equal(t, "logo.png", resp["filename"])

	// Any org member can read the attachment back by id.
	memberToken := inviteNonAdminMember(t, srv, token, orgID)
	getReq := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	getReq.Header.Set("Authorization", "Bearer "+memberToken)
	getRW := httptest.NewRecorder()
	srv.ServeHTTP(getRW, getReq)
	require.Equal(t, http.StatusOK, getRW.Code, getRW.Body.String())
	assert.Equal(t, string(testLogoPNG), getRW.Body.String())
	assert.Equal(t, "image/png", getRW.Header().Get("Content-Type"))

	// Non-admin members cannot upload org attachments.
	rec = uploadFileTo(t, srv, memberToken, "/api/v1/org/attachments", "evil.png", testLogoPNG)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDashboardLogoAttachment(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "500")
	srv := setupTestServerWithAttachDir(t)
	_, token := createTestOrgAndAdmin(t, srv)

	dashID := createDashWithSettings(t, srv, token, nil)

	// A dashboard editor uploads a logo through the dashboard route.
	rec := uploadFileTo(t, srv, token, "/api/v1/dashboards/"+dashID+"/attachments", "dash-logo.png", testLogoPNG)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var upResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&upResp))
	attID, _ := upResp["id"].(string)
	require.NotEmpty(t, attID)

	getLogo := func() string {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		rw := httptest.NewRecorder()
		srv.ServeHTTP(rw, r)
		require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
		var body struct {
			Settings struct {
				App struct {
					LogoAttachmentID string `json:"logo_attachment_id"`
				} `json:"app"`
			} `json:"settings"`
		}
		require.NoError(t, json.NewDecoder(rw.Body).Decode(&body))
		return body.Settings.App.LogoAttachmentID
	}

	// Referencing the uploaded attachment is accepted and persists.
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID, map[string]any{
		"settings": map[string]any{"app": map[string]any{"logo_attachment_id": attID}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, attID, getLogo())

	// An unknown attachment id is rejected.
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID, map[string]any{
		"settings": map[string]any{"app": map[string]any{"logo_attachment_id": uuid.NewString()}},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// The create path enforces the same invariant.
	badCreateBody, _ := json.Marshal(map[string]any{
		"title":    "Bad Logo",
		"settings": map[string]any{"app": map[string]any{"logo_attachment_id": uuid.NewString()}},
	})
	badCreateReq := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(badCreateBody))
	badCreateReq.Header.Set("Content-Type", "application/json")
	badCreateReq.Header.Set("Authorization", "Bearer "+token)
	badCreateRec := httptest.NewRecorder()
	srv.ServeHTTP(badCreateRec, badCreateReq)
	require.Equal(t, http.StatusBadRequest, badCreateRec.Code, badCreateRec.Body.String())

	// An attachment from another org is rejected too.
	_, foreignToken := createTestOrgAndAdmin(t, srv)
	foreignAtt := uploadOrgAttachment(t, srv, foreignToken)
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID, map[string]any{
		"settings": map[string]any{"app": map[string]any{"logo_attachment_id": foreignAtt}},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// The rejected writes left the stored logo untouched.
	require.Equal(t, attID, getLogo())
}

func TestDashboardGetOrgBranding(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "500")
	srv := setupTestServer(t)
	_, token := createTestOrgAndAdmin(t, srv)
	dashID := createDashWithSettings(t, srv, token, nil)

	getBranding := func() *orgBrandingBody {
		t.Helper()
		rec := versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			OrgBranding *orgBrandingBody `json:"org_branding"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		return body.OrgBranding
	}

	// Unset branding stays null on the dashboard payload.
	require.Nil(t, getBranding())

	// Org branding surfaces on the dashboard read.
	rec := putOrgSettings(t, srv, token, map[string]any{
		"branding": map[string]any{
			"app_name":              "Acme Analytics",
			"accent_color":          "#7c5cff",
			"default_chart_palette": []string{"#7c5cff", "#22c55e"},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := getBranding()
	require.NotNil(t, got)
	assert.Equal(t, "Acme Analytics", got.AppName)
	assert.Equal(t, "#7c5cff", got.AccentColor)
	assert.Equal(t, []string{"#7c5cff", "#22c55e"}, got.DefaultChartPalette)
}

func TestPublicLogo(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "500")
	srv := setupTestServerWithAttachDir(t)
	_, token := createTestOrgAndAdmin(t, srv)

	logoID := uploadOrgAttachment(t, srv, token)
	// Branding carries the logo and rides along on the public payload.
	rec := putOrgSettings(t, srv, token, map[string]any{
		"branding": map[string]any{
			"app_name":           "Acme Analytics",
			"logo_attachment_id": logoID,
			"accent_color":       "#7c5cff",
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	dashID := createDashWithSettings(t, srv, token, nil)
	publicToken := shareDashboard(t, srv, token, dashID)

	// The public token's org serves the logo without auth.
	req := httptest.NewRequest("GET", "/api/v1/public/"+publicToken+"/attachments/"+logoID, nil)
	rw := httptest.NewRecorder()
	srv.ServeHTTP(rw, req)
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.Equal(t, string(testLogoPNG), rw.Body.String())
	assert.Equal(t, "image/png", rw.Header().Get("Content-Type"))

	// The public dashboard payload includes org_branding.
	body := getPublicDashboard(t, srv, publicToken)
	ob, ok := body["org_branding"].(map[string]any)
	require.True(t, ok, "expected org_branding, got %v", body["org_branding"])
	assert.Equal(t, "Acme Analytics", ob["app_name"])

	// Another org's attachment is never served through this token.
	_, foreignToken := createTestOrgAndAdmin(t, srv)
	foreignLogo := uploadOrgAttachment(t, srv, foreignToken)
	req = httptest.NewRequest("GET", "/api/v1/public/"+publicToken+"/attachments/"+foreignLogo, nil)
	rw = httptest.NewRecorder()
	srv.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusNotFound, rw.Code)

	// An unknown attachment is a 404 too.
	req = httptest.NewRequest("GET", "/api/v1/public/"+publicToken+"/attachments/"+uuid.NewString(), nil)
	rw = httptest.NewRecorder()
	srv.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusNotFound, rw.Code)
}
