package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

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

	var resp map[string]interface{}
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

	var listResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&listResp)
	atts := listResp["attachments"].([]interface{})
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
