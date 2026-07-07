package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/storage"
)

func setupAuditWithStorage(t *testing.T) *AuditFixtures {
	t.Helper()
	f := SetupAuditTest(t)
	st, err := storage.NewLocalStorage(t.TempDir())
	require.NoError(t, err)
	f.srv.SetStorage(st)
	return f
}

func uploadAsUser(t *testing.T, f *AuditFixtures, userKey, nbID string) (int, string) {
	t.Helper()
	token := f.Tokens[userKey]
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.txt")
	io.WriteString(fw, "test-content")
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func uploadAsUserGetID(t *testing.T, f *AuditFixtures, userKey, nbID string) string {
	t.Helper()
	code, body := uploadAsUser(t, f, userKey, nbID)
	require.Equal(t, http.StatusCreated, code, "upload failed: %s", body)
	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	id, _ := resp["id"].(string)
	require.NotEmpty(t, id)
	return id
}

// ——— UPLOAD ———

func TestAttachment_Upload_Permissions(t *testing.T) {
	t.Parallel()
	f := setupAuditWithStorage(t)

	t.Run("adminA on NoACL — 201 (admin bypass)", func(t *testing.T) {
		code, _ := uploadAsUser(t, f, "adminA", f.OrgA.Notebooks.NoACL)
		require.Equal(t, http.StatusCreated, code)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		code, body := uploadAsUser(t, f, "aliceA", f.OrgA.Notebooks.NoACL)
		t.Logf("aliceA upload to NoACL: %d %s", code, body)
		if code == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA uploaded attachment to NoACL notebook without ACL")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		code, body := uploadAsUser(t, f, "aliceA", f.OrgA.Notebooks.UserACL)
		t.Logf("aliceA upload to UserACL (view-only): %d %s", code, body)
		if code == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA uploaded attachment with only view permission")
		}
	})

	t.Run("bobA on GroupACL — 201 (group edit)", func(t *testing.T) {
		code, body := uploadAsUser(t, f, "bobA", f.OrgA.Notebooks.GroupACL)
		t.Logf("bobA upload to GroupACL: %d %s", code, body)
		require.Equal(t, http.StatusCreated, code)
	})

	t.Run("bobA on NoACL — 403 (no ACL)", func(t *testing.T) {
		code, body := uploadAsUser(t, f, "bobA", f.OrgA.Notebooks.NoACL)
		t.Logf("bobA upload to NoACL: %d %s", code, body)
		if code == http.StatusCreated {
			t.Log("VULNERABILITY: bobA uploaded attachment to NoACL notebook without ACL")
		}
	})

	t.Run("adminB on Org A NoACL — 404 (cross-org)", func(t *testing.T) {
		code, body := uploadAsUser(t, f, "adminB", f.OrgA.Notebooks.NoACL)
		t.Logf("adminB upload to Org A NoACL: %d %s", code, body)
	})
}

// ——— LIST ———

func TestAttachment_List_Permissions(t *testing.T) {
	t.Parallel()
	f := setupAuditWithStorage(t)

	noACLAtt := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.NoACL)
	userACLAtt := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.UserACL)
	_ = noACLAtt
	_ = userACLAtt

	t.Run("adminA on NoACL notebook — 200 (baseline)", func(t *testing.T) {
		code, _ := f.DoRequest(t, "adminA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/attachments", nil)
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("aliceA on NoACL notebook — 403 (fixed)", func(t *testing.T) {
		code, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/attachments", nil)
		require.Equal(t, http.StatusForbidden, code, "aliceA list NoACL attachments: %s", body)
	})

	t.Run("aliceA on UserACL notebook — 200 (has view)", func(t *testing.T) {
		code, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.UserACL+"/attachments", nil)
		require.Equal(t, http.StatusOK, code, "aliceA list UserACL attachments: %s", body)
	})

	t.Run("adminB on Org A NoACL notebook — 403 (cross-org)", func(t *testing.T) {
		code, body := f.DoRequest(t, "adminB", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/attachments", nil)
		t.Logf("adminB list Org A NoACL attachments (cross-org, admin bypass): %d %s", code, body)
	})

	t.Run("eveB on Org A NoACL notebook — 403 (cross-org)", func(t *testing.T) {
		code, body := f.DoRequest(t, "eveB", "GET",
			"/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/attachments", nil)
		require.Equal(t, http.StatusForbidden, code, "eveB list Org A NoACL attachments: %s", body)
	})
}

// ——— GET ———

func TestAttachment_Get_Permissions(t *testing.T) {
	t.Parallel()
	f := setupAuditWithStorage(t)

	noACLAtt := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.NoACL)
	userACLAtt := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.UserACL)

	t.Run("adminA on NoACL attachment — 200 (baseline)", func(t *testing.T) {
		code, body := f.DoRequest(t, "adminA", "GET", "/api/v1/attachments/"+noACLAtt, nil)
		t.Logf("adminA get NoACL attachment: %d %s", code, body)
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("aliceA on NoACL attachment — 404 (no notebook view)", func(t *testing.T) {
		code, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/attachments/"+noACLAtt, nil)
		t.Logf("aliceA get NoACL attachment: %d %s", code, body)
	})

	t.Run("aliceA on UserACL attachment — 200 (has notebook view)", func(t *testing.T) {
		code, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/attachments/"+userACLAtt, nil)
		t.Logf("aliceA get UserACL attachment: %d %s", code, body)
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("adminB on Org A NoACL attachment — 404 (cross-org)", func(t *testing.T) {
		code, body := f.DoRequest(t, "adminB", "GET", "/api/v1/attachments/"+noACLAtt, nil)
		t.Logf("adminB get Org A attachment (cross-org): %d %s", code, body)
	})

	t.Run("eveB on Org A NoACL attachment — 404 (cross-org)", func(t *testing.T) {
		code, body := f.DoRequest(t, "eveB", "GET", "/api/v1/attachments/"+noACLAtt, nil)
		t.Logf("eveB get Org A attachment (cross-org): %d %s", code, body)
	})
}

// ——— DELETE ———

func TestAttachment_Delete_Permissions(t *testing.T) {
	t.Parallel()
	f := setupAuditWithStorage(t)

	noACLAtt := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.NoACL)

	t.Run("adminA on NoACL attachment — 204 (baseline)", func(t *testing.T) {
		code, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/attachments/"+noACLAtt, nil)
		t.Logf("adminA delete NoACL attachment: %d %s", code, body)
		require.Equal(t, http.StatusNoContent, code)
	})

	t.Run("aliceA on NoACL attachment — 403 (fixed)", func(t *testing.T) {
		att := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.NoACL)
		code, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/attachments/"+att, nil)
		require.Equal(t, http.StatusForbidden, code, "aliceA delete NoACL attachment: %s", body)
	})

	t.Run("bobA on GroupACL attachment — 204 (has edit via group)", func(t *testing.T) {
		att := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.GroupACL)
		code, body := f.DoRequest(t, "bobA", "DELETE", "/api/v1/attachments/"+att, nil)
		require.Equal(t, http.StatusNoContent, code, "bobA delete GroupACL attachment: %s", body)
	})

	t.Run("adminB on Org A NoACL attachment — 404 (cross-org)", func(t *testing.T) {
		att := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.NoACL)
		code, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/attachments/"+att, nil)
		t.Logf("adminB delete Org A attachment (cross-org): %d %s", code, body)
	})

	t.Run("eveB on Org A NoACL attachment — 404 (cross-org)", func(t *testing.T) {
		att := uploadAsUserGetID(t, f, "adminA", f.OrgA.Notebooks.NoACL)
		code, body := f.DoRequest(t, "eveB", "DELETE", "/api/v1/attachments/"+att, nil)
		t.Logf("eveB delete Org A attachment (cross-org): %d %s", code, body)
	})
}
