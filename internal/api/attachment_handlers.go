package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// @Summary Upload attachment
// @Description Upload a file attachment to a notebook
// @Tags attachments
// @Accept multipart/form-data
// @Produce json
// @Param notebook_id path string true "Notebook ID"
// @Param file formData file true "File to upload"
// @Success 201 {object} map[string]any
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{notebook_id}/attachments [post]
func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	var exists bool
	if err := s.db.Pool.QueryRow(ctx, "SELECT true FROM notebooks WHERE id = $1 AND org_id = $2", nbID, claims.OrgID).Scan(&exists); err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "write permission required")
		return
	}

	s.uploadAttachment(w, r, claims.OrgID, &nbID, claims.UserID)
}

// @Summary Upload org attachment
// @Description Upload an org-scoped file attachment (app logo)
// @Tags attachments
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "File to upload"
// @Success 201 {object} map[string]any
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /org/attachments [post]
func (s *Server) handleUploadOrgAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	s.uploadAttachment(w, r, claims.OrgID, nil, claims.UserID)
}

// @Summary Upload dashboard attachment
// @Description Upload a file attachment for a dashboard (app logo)
// @Tags attachments
// @Accept multipart/form-data
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param file formData file true "File to upload"
// @Success 201 {object} map[string]any
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/attachments [post]
func (s *Server) handleUploadDashboardAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	// The route's permission middleware already required dashboard edit; this
	// only distinguishes a live dashboard from a trashed or missing one.
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT true FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashID, claims.OrgID).Scan(&exists); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	s.uploadAttachment(w, r, claims.OrgID, nil, claims.UserID)
}

// uploadAttachment parses a multipart upload, stores the bytes, and inserts the
// attachments row, returning its id. notebookID is nil for org-level and
// dashboard logos, which are not tied to a notebook. Callers own the
// existence and permission checks.
func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request, orgID string, notebookID *string, userID string) {
	ctx := r.Context()

	maxBytes := s.maxAttachmentBytes
	if maxBytes == 0 {
		maxBytes = 32 << 20
	}
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	if header.Size > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	// Detect MIME from first 512 bytes
	sniff := make([]byte, 512)
	n, _ := file.Read(sniff)
	sniff = sniff[:n]
	mimeType := http.DetectContentType(sniff)
	reader := io.MultiReader(bytes.NewReader(sniff), file)

	// Insert DB row first to get the UUID, then store bytes keyed by UUID
	var attID string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO attachments (org_id, notebook_id, filename, mime_type, size_bytes, storage_path, created_by)
		 VALUES ($1, $2, $3, $4, 0, $5, $6) RETURNING id`,
		orgID, notebookID, header.Filename, mimeType, "pending", userID,
	).Scan(&attID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Count bytes while storing
	counter := &countingReader{r: reader}
	if err := s.store.Put(attID, counter, header.Size, mimeType); err != nil {
		slog.Error("attachment storage put failed", "id", attID, "error", err)
		s.db.Pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, attID)
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}

	// Update size and storage_path
	if _, err := s.db.Pool.Exec(ctx,
		`UPDATE attachments SET size_bytes = $1, storage_path = $2 WHERE id = $3`,
		counter.n, attID, attID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        attID,
		"filename":  header.Filename,
		"mime_type": mimeType,
		"size":      counter.n,
	})
}

// @Summary Get attachment
// @Description Download a file attachment by ID
// @Tags attachments
// @Produce application/octet-stream
// @Param id path string true "Attachment ID"
// @Success 200 {file} binary
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /attachments/{id} [get]
func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var mimeType, filename, notebookID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT mime_type, filename, COALESCE(notebook_id::text, '') FROM attachments WHERE id = $1 AND org_id = $2`,
		attID, claims.OrgID,
	).Scan(&mimeType, &filename, &notebookID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if notebookID != "" {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", notebookID, "view")
		if err != nil || !ok {
			writeError(w, http.StatusNotFound, "attachment not found")
			return
		}
	}

	s.serveAttachment(w, attID, mimeType, filename)
}

// serveAttachment streams an attachment's stored bytes with its stored content
// type and filename. Callers own access control.
func (s *Server) serveAttachment(w http.ResponseWriter, attID, mimeType, filename string) {
	rc, err := s.store.Get(attID)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	_, _ = io.Copy(w, rc)
}

// errLogoAttachmentNotFound marks a logo reference that does not resolve to an
// attachment in the caller's org.
var errLogoAttachmentNotFound = errors.New("logo attachment not found")

// validateLogoAttachment verifies attachmentID names an attachment in orgID.
// Logos are referenced by attachment id; rejecting unknown ids at write time
// keeps app surfaces from rendering broken images. An empty id is valid (no
// logo) and a malformed id can never match, so it is treated as not found.
func (s *Server) validateLogoAttachment(ctx context.Context, orgID, attachmentID string) error {
	if attachmentID == "" {
		return nil
	}
	if !isValidUUID(attachmentID) {
		return errLogoAttachmentNotFound
	}
	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM attachments WHERE id = $1 AND org_id = $2)`,
		attachmentID, orgID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("logo attachment lookup: %w", err)
	}
	if !exists {
		return errLogoAttachmentNotFound
	}
	return nil
}

// writeLogoAttachmentError maps a validateLogoAttachment error to an HTTP
// response. It reports whether the caller should stop.
func writeLogoAttachmentError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errLogoAttachmentNotFound) {
		writeError(w, http.StatusBadRequest, "logo attachment not found")
		return true
	}
	writeError(w, http.StatusInternalServerError, "logo attachment check failed")
	return true
}

// @Summary List attachments
// @Description List all file attachments for a notebook
// @Tags attachments
// @Produce json
// @Param notebook_id path string true "Notebook ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{notebook_id}/attachments [get]
func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "view"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, filename, mime_type, size_bytes, created_at FROM attachments
		 WHERE notebook_id = $1 AND org_id = $2 ORDER BY created_at DESC`,
		nbID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type att struct {
		ID        string    `json:"id"`
		Filename  string    `json:"filename"`
		MimeType  string    `json:"mime_type"`
		Size      int64     `json:"size"`
		CreatedAt time.Time `json:"created_at"`
	}
	var atts []att
	for rows.Next() {
		var a att
		if err := rows.Scan(&a.ID, &a.Filename, &a.MimeType, &a.Size, &a.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		atts = append(atts, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query iteration failed")
		return
	}
	if atts == nil {
		atts = []att{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": atts})
}

// @Summary Delete attachment
// @Description Delete a file attachment by ID
// @Tags attachments
// @Produce json
// @Param id path string true "Attachment ID"
// @Success 204 "No Content"
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /attachments/{id} [delete]
func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var nbID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(notebook_id::text, '') FROM attachments WHERE id = $1 AND org_id = $2`,
		attID, claims.OrgID,
	).Scan(&nbID)
	if err != nil || nbID == "" {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if _, err := s.db.Pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, attID); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	_ = s.store.Delete(attID)
	w.WriteHeader(http.StatusNoContent)
}

// countingReader wraps an io.Reader and counts bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
