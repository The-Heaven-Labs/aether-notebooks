package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	// Verify notebook belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT true FROM notebooks WHERE id = $1 AND org_id = $2", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	attachDir := s.attachmentDir
	if attachDir == "" {
		attachDir = "./attachments"
	}
	if err := os.MkdirAll(attachDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}

	// Detect MIME from first 512 bytes
	sniff := make([]byte, 512)
	n, _ := file.Read(sniff)
	sniff = sniff[:n]
	mimeType := http.DetectContentType(sniff)
	// Prepend sniffed bytes back for the full copy
	reader := io.MultiReader(bytes.NewReader(sniff), file)

	// Write to a temp file first so we can get the size
	tmpFile, err := os.CreateTemp(attachDir, "upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	tmpPath := tmpFile.Name()

	size, err := io.Copy(tmpFile, reader)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "write error")
		return
	}

	// Insert into DB to get the generated UUID
	var attID string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO attachments (org_id, notebook_id, filename, mime_type, size_bytes, storage_path, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		claims.OrgID, nbID, header.Filename, mimeType, size, tmpPath, claims.UserID,
	).Scan(&attID)
	if err != nil {
		os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Rename temp file to the final path using the attachment ID
	finalPath := filepath.Join(attachDir, attID)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}

	// Update storage_path to the final location
	if _, err := s.db.Pool.Exec(ctx,
		`UPDATE attachments SET storage_path = $1 WHERE id = $2`,
		finalPath, attID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        attID,
		"filename":  header.Filename,
		"mime_type": mimeType,
		"size":      size,
	})
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var storagePath, mimeType, filename string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT storage_path, mime_type, filename FROM attachments WHERE id = $1 AND org_id = $2`,
		attID, claims.OrgID,
	).Scan(&storagePath, &mimeType, &filename)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	f, err := os.Open(storagePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	_, _ = io.Copy(w, f)
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

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
	writeJSON(w, http.StatusOK, map[string]interface{}{"attachments": atts})
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var storagePath string
	err := s.db.Pool.QueryRow(ctx,
		`DELETE FROM attachments WHERE id = $1 AND org_id = $2 RETURNING storage_path`,
		attID, claims.OrgID,
	).Scan(&storagePath)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	os.Remove(storagePath)
	w.WriteHeader(http.StatusNoContent)
}
