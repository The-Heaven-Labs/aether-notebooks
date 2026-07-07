package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// @Summary Get Yjs document
// @Description Returns the Yjs document state for a notebook (internal relay endpoint)
// @Tags internal
// @Produce octet-stream
// @Param notebook_id path string true "Notebook ID"
// @Success 200 {string} binary
// @Failure 500 {object} map[string]string
// @Router /internal/yjs/{notebook_id} [get]
func (s *Server) handleInternalYjsGet(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	_, err := s.jwt.Validate(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	var state []byte
	err = s.db.Pool.QueryRow(ctx,
		`SELECT state FROM yjs_documents WHERE notebook_id = $1`,
		nbID,
	).Scan(&state)
	if err == pgx.ErrNoRows {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(state)
}

// @Summary Update Yjs document
// @Description Stores the Yjs document state for a notebook (internal relay endpoint)
// @Tags internal
// @Accept octet-stream
// @Param notebook_id path string true "Notebook ID"
// @Success 204 {string} string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /internal/yjs/{notebook_id} [put]
func (s *Server) handleInternalYjsPut(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	_, err := s.jwt.Validate(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	state, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO yjs_documents (notebook_id, state)
		 VALUES ($1, $2)
		 ON CONFLICT (notebook_id) DO UPDATE SET state = $2, updated_at = NOW()`,
		nbID, state,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary Validate authentication
// @Description Validates a JWT token and returns user info (internal relay endpoint)
// @Tags internal
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /internal/auth/validate [get]
func (s *Server) handleInternalAuthValidate(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	claims, err := s.jwt.Validate(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id": claims.UserID,
		"org_id":  claims.OrgID,
		"role":    claims.Role,
	})
}
