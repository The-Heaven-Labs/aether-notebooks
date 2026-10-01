package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// @Summary Upload agent session attachment
// @Description Upload an image attachment for an agent session (vision support)
// @Tags agents
// @Accept multipart/form-data
// @Produce json
// @Param session_id path string true "Session ID"
// @Param file formData file true "Image file to upload"
// @Success 201 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /agent-sessions/{session_id}/attachments [post]
func (s *Server) handleUploadAgentAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	sessionID := r.PathValue("session_id")
	ctx := r.Context()

	// Resolve the session's agent org: authorization (the owner fallback in
	// checkSessionPermission) is org-agnostic, but the stored row must be
	// keyed to the org that owns the agent, not the caller's token org.
	var sessionOrgID string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT a.org_id
		FROM agent_sessions s
		JOIN agents a ON a.id = s.agent_id
		WHERE s.id = $1
	`, sessionID).Scan(&sessionOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	allowed, _ := s.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, sessionID, "edit")
	if !allowed {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

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

	sniff := make([]byte, 512)
	n, _ := file.Read(sniff)
	sniff = sniff[:n]
	mimeType := http.DetectContentType(sniff)
	reader := io.MultiReader(bytes.NewReader(sniff), file)

	attID := uuid.New().String()
	if err := s.store.Put(attID, reader, header.Size, mimeType); err != nil {
		slog.Error("agent attachment storage put failed", "id", attID, "error", err)
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO session_attachments (id, session_id, org_id, filename, mime_type, size_bytes, storage_path, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, attID, sessionID, sessionOrgID, header.Filename, mimeType, header.Size, attID, claims.UserID)
	if err != nil {
		slog.Error("agent attachment db insert failed", "error", err)
		s.store.Delete(attID)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        attID,
		"filename":  header.Filename,
		"mime_type": mimeType,
		"size":      header.Size,
	})
}

// @Summary Get agent session attachment
// @Description Retrieve an image attachment for an agent session
// @Tags agents
// @Produce application/octet-stream
// @Param id path string true "Attachment ID"
// @Success 200 {file} binary
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /agent-attachments/{id} [get]
func (s *Server) handleGetAgentAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var sessionID, mimeType, filename string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT sa.session_id, sa.mime_type, sa.filename
		FROM session_attachments sa
		JOIN agent_sessions ases ON ases.id = sa.session_id
		JOIN agents a ON a.id = ases.agent_id
		WHERE sa.id = $1 AND a.org_id = $2
	`, attID, claims.OrgID).Scan(&sessionID, &mimeType, &filename)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	allowed, _ := s.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, sessionID, "view")
	if !allowed {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

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
