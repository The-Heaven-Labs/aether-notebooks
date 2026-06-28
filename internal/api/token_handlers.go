package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type createTokenRequest struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// @Summary Create a token
// @Description Create a new personal access token
// @Tags tokens
// @Accept json
// @Produce json
// @Param request body object true "Token details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /tokens [post]
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var req createTokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Generate token: aether_tok_ + 32 random bytes hex-encoded
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	rawToken := "aether_tok_" + hex.EncodeToString(rawBytes)

	// Hash the token for storage
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash token")
		return
	}

	ctx := r.Context()
	var id string
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at format (use RFC3339)")
			return
		}
		expiresAt = &t
	}

	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO api_tokens (user_id, org_id, name, token_hash, expires_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		claims.UserID, claims.OrgID, req.Name, string(hash), expiresAt,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         id,
		"name":       req.Name,
		"token":      rawToken,
		"expires_at": expiresAt,
		"created_at": time.Now(),
	})
}

// @Summary List tokens
// @Description List all personal access tokens
// @Tags tokens
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /tokens [get]
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ctx := r.Context()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, last_used_at, expires_at, created_at FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`,
		claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}
	defer rows.Close()

	var tokens []map[string]any
	for rows.Next() {
		var id, name string
		var lastUsed, expires, created *time.Time
		if err := rows.Scan(&id, &name, &lastUsed, &expires, &created); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan token")
			return
		}
		tokens = append(tokens, map[string]any{
			"id": id, "name": name, "last_used_at": lastUsed,
			"expires_at": expires, "created_at": created,
		})
	}
	if tokens == nil {
		tokens = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, tokens)
}

// @Summary Delete a token
// @Description Revoke a personal access token
// @Tags tokens
// @Param id path string true "Token ID"
// @Success 200
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /tokens/{id} [delete]
func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	tokenID := r.PathValue("id")
	ctx := r.Context()
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`,
		tokenID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete token")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
