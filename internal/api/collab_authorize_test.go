package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// collabAuthorize posts a document name to the internal authorize endpoint. An
// empty token sends no Authorization header at all.
func collabAuthorize(t *testing.T, srv *api.Server, token, documentName string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"document_name": documentName})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/internal/collab/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func decodeCollabAuthorize(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

// TestCollabAuthorizePermissionMatrix pins the relay's read-write decision:
// edit → can_edit true, view or view_with_data → can_edit false, and no ACL at
// all → 403.
func TestCollabAuthorizePermissionMatrix(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	ownerToken := registerAndGetToken(t, srv, fmt.Sprintf("collab-auth-owner-%d@example.com", ts), "Collab Auth Org")
	dashID := createDashWithSettings(t, srv, ownerToken, nil)

	var orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	// A viewer with `view` only.
	viewerID := insertUser(t, srv, fmt.Sprintf("collab-auth-view-%d@example.com", ts), "Viewer")
	addOrgMember(t, srv, orgID, viewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "non-admin")

	// A data viewer with `view_with_data` only — the resolver does not treat it
	// as implying `view`, so the endpoint's fallback check must admit it.
	dataViewerID := insertUser(t, srv, fmt.Sprintf("collab-auth-data-%d@example.com", ts), "Data Viewer")
	addOrgMember(t, srv, orgID, dataViewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", dataViewerID, "view_with_data")
	dataViewerToken := issueToken(t, dataViewerID, orgID, "non-admin")

	// An org member with no ACL on the dashboard at all.
	outsiderID := insertUser(t, srv, fmt.Sprintf("collab-auth-none-%d@example.com", ts), "Outsider")
	addOrgMember(t, srv, orgID, outsiderID, "non-admin")
	outsiderToken := issueToken(t, outsiderID, orgID, "non-admin")

	// A second org admin with no ACL row on the dashboard: internal routes
	// never run the auth middleware, so they cannot carry admin mode and the
	// org-admin bypass must not apply here.
	adminID := insertUser(t, srv, fmt.Sprintf("collab-auth-admin-%d@example.com", ts), "Org Admin")
	addOrgMember(t, srv, orgID, adminID, "non-admin")
	_, err := srv.DB().Pool.Exec(ctx,
		`UPDATE org_members SET role = 'admin' WHERE org_id = $1 AND user_id = $2`, orgID, adminID)
	require.NoError(t, err)
	adminToken := issueToken(t, adminID, orgID, "admin")

	t.Run("owner can edit", func(t *testing.T) {
		rec := collabAuthorize(t, srv, ownerToken, "dashboard:"+dashID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		resp := decodeCollabAuthorize(t, rec)
		require.Equal(t, true, resp["can_edit"])
		require.Equal(t, dashID, resp["document_id"])
	})

	t.Run("viewer cannot edit", func(t *testing.T) {
		rec := collabAuthorize(t, srv, viewerToken, "dashboard:"+dashID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		resp := decodeCollabAuthorize(t, rec)
		require.Equal(t, false, resp["can_edit"])
		require.Equal(t, dashID, resp["document_id"])
	})

	t.Run("view_with_data viewer cannot edit", func(t *testing.T) {
		rec := collabAuthorize(t, srv, dataViewerToken, "dashboard:"+dashID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		resp := decodeCollabAuthorize(t, rec)
		require.Equal(t, false, resp["can_edit"])
		require.Equal(t, dashID, resp["document_id"])
	})

	t.Run("no access is forbidden", func(t *testing.T) {
		rec := collabAuthorize(t, srv, outsiderToken, "dashboard:"+dashID)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("org admin without ACL is forbidden", func(t *testing.T) {
		rec := collabAuthorize(t, srv, adminToken, "dashboard:"+dashID)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("document id is canonicalized", func(t *testing.T) {
		rec := collabAuthorize(t, srv, ownerToken, "dashboard:"+strings.ToUpper(dashID))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		resp := decodeCollabAuthorize(t, rec)
		require.Equal(t, true, resp["can_edit"])
		require.Equal(t, dashID, resp["document_id"])
	})
}

// TestCollabAuthorizeTrashedAndCrossOrg pins that unknown, trashed, and
// cross-org dashboards are all indistinguishable 404s.
func TestCollabAuthorizeTrashedAndCrossOrg(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("collab-auth-trash-%d@example.com", ts), "Collab Trash Org")
	dashID := createDashWithSettings(t, srv, token, nil)

	t.Run("unknown dashboard", func(t *testing.T) {
		rec := collabAuthorize(t, srv, token, "dashboard:"+uuid.NewString())
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("trashed dashboard", func(t *testing.T) {
		code, _ := doRequest(t, srv, token, "DELETE", "/api/v1/dashboards/"+dashID, nil)
		require.Equal(t, http.StatusNoContent, code)

		rec := collabAuthorize(t, srv, token, "dashboard:"+dashID)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("cross-org dashboard", func(t *testing.T) {
		otherToken := registerAndGetToken(t, srv, fmt.Sprintf("collab-auth-other-%d@example.com", ts), "Collab Other Org")
		otherDashID := createDashWithSettings(t, srv, otherToken, nil)

		rec := collabAuthorize(t, srv, token, "dashboard:"+otherDashID)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		// The owning org still authorizes its own document.
		rec = collabAuthorize(t, srv, otherToken, "dashboard:"+otherDashID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}

// TestCollabAuthorizeMalformedDocumentNames pins 400 for anything that is not a
// dashboard document with a parseable UUID.
func TestCollabAuthorizeMalformedDocumentNames(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("collab-auth-bad-%d@example.com", ts), "Collab Bad Org")

	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"notebook document", "notebook:" + uuid.NewString()},
		{"bare uuid", uuid.NewString()},
		{"dashboard non-uuid", "dashboard:not-a-uuid"},
		{"dashboard empty id", "dashboard:"},
		{"empty name", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := collabAuthorize(t, srv, token, tc.doc)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestCollabAuthorizeRequiresInternalToken(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"missing", ""},
		{"invalid", "invalid.token.here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := collabAuthorize(t, srv, tc.token, "dashboard:"+uuid.NewString())
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}
