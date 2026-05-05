package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOrphanAttachments(t *testing.T) {
	actx := setupAttachTestContext(t)
	srv := actx.srv
	token := actx.token

	nbID := createTestNotebook(t, srv, token)

	referencedID := uploadTestAttachment(t, srv, token, nbID)
	orphanID := uploadTestAttachment(t, srv, token, nbID)

	_, err := srv.DB().Pool.Exec(context.Background(),
		`UPDATE attachments SET created_at = NOW() - INTERVAL '20 minutes' WHERE id = ANY($1)`,
		[]string{referencedID, orphanID},
	)
	require.NoError(t, err)

	cellSource := fmt.Sprintf(`<img src="/api/v1/attachments/%s" />`, referencedID)
	body, _ := json.Marshal(map[string]any{"type": "text", "source": cellSource})
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create cell failed: %s", rec.Body.String())

	require.NoError(t, srv.CleanupOrphanAttachments(context.Background()))

	getRef := httptest.NewRequest("GET", "/api/v1/attachments/"+referencedID, nil)
	getRef.Header.Set("Authorization", "Bearer "+token)
	refRec := httptest.NewRecorder()
	srv.ServeHTTP(refRec, getRef)
	assert.Equal(t, http.StatusOK, refRec.Code, "referenced attachment should survive cleanup")

	getOrphan := httptest.NewRequest("GET", "/api/v1/attachments/"+orphanID, nil)
	getOrphan.Header.Set("Authorization", "Bearer "+token)
	orphanRec := httptest.NewRecorder()
	srv.ServeHTTP(orphanRec, getOrphan)
	assert.Equal(t, http.StatusNotFound, orphanRec.Code, "orphaned attachment should be deleted")
}

func TestCleanupOrphanAttachments_RespectsGracePeriod(t *testing.T) {
	actx := setupAttachTestContext(t)
	srv := actx.srv
	token := actx.token

	nbID := createTestNotebook(t, srv, token)
	recentID := uploadTestAttachment(t, srv, token, nbID)

	require.NoError(t, srv.CleanupOrphanAttachments(context.Background()))

	getReq := httptest.NewRequest("GET", "/api/v1/attachments/"+recentID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, getReq)
	assert.Equal(t, http.StatusOK, rec.Code, "recently uploaded attachment should not be cleaned up")
}
