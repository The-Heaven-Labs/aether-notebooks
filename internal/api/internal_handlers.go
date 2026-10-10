package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
)

// validateInternalToken validates a session/internal JWT. OAuth access
// tokens (ClientID set) are bound to the MCP endpoint and must never be
// accepted by the relay's internal endpoints.
func (s *Server) validateInternalToken(token string) (*auth.Claims, error) {
	claims, err := s.jwt.Validate(token)
	if err != nil {
		return nil, err
	}
	if claims.ClientID != "" {
		return nil, errors.New("oauth access tokens are not valid for internal endpoints")
	}
	return claims, nil
}

// requireInternalToken validates the request's bearer token for an internal
// relay endpoint and returns its claims. On failure it writes a 401 response
// and returns ok=false, so callers simply return.
func (s *Server) requireInternalToken(w http.ResponseWriter, r *http.Request) (*auth.Claims, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return nil, false
	}
	claims, err := s.validateInternalToken(strings.TrimPrefix(authHeader, "Bearer "))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return nil, false
	}
	return claims, true
}

// @Summary Get Yjs document
// @Description Returns the Yjs document state for a notebook (internal relay endpoint)
// @Tags internal
// @Produce octet-stream
// @Param notebook_id path string true "Notebook ID"
// @Success 200 {string} binary
// @Failure 500 {object} map[string]string
// @Router /internal/yjs/{notebook_id} [get]
func (s *Server) handleInternalYjsGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInternalToken(w, r); !ok {
		return
	}

	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	var state []byte
	err := s.db.Pool.QueryRow(ctx,
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
	if _, ok := s.requireInternalToken(w, r); !ok {
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

// @Summary Get dashboard Yjs document
// @Description Returns the Yjs document state for a dashboard, lazily seeding it from the dashboards/widgets rows on first access (internal relay endpoint)
// @Tags internal
// @Produce octet-stream
// @Param dashboard_id path string true "Dashboard ID"
// @Success 200 {string} binary
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /internal/dashboard-yjs/{dashboard_id} [get]
func (s *Server) handleInternalDashboardYjsGet(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.requireInternalToken(w, r)
	if !ok {
		return
	}

	dashID := r.PathValue("dashboard_id")

	// The relay presents the connecting user's session token, so the load is
	// scoped to that user's org: a dashboard whose org_id differs is treated
	// as missing (404), exactly like an unknown or trashed one, and can never
	// be read or seeded with another org's token. loadOrSeedDashboardDoc locks
	// the dashboards row FOR UPDATE exactly like MergeAndStore, so a lazy seed
	// and a concurrent store serialize on the same row.
	state, err := s.loadOrSeedDashboardDoc(r.Context(), claims.OrgID, dashID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		slog.Error("internal dashboard yjs: load or seed", "dashboard_id", dashID, "error", err)
		writeError(w, http.StatusInternalServerError, "seed failed")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(state)
}

// @Summary Update dashboard Yjs document
// @Description Merges and stores the Yjs document state for a dashboard, validating the store actor's dashboard edit / notebook view rights (internal relay endpoint)
// @Tags internal
// @Accept octet-stream
// @Param dashboard_id path string true "Dashboard ID"
// @Success 204 {string} string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /internal/dashboard-yjs/{dashboard_id} [put]
func (s *Server) handleInternalDashboardYjsPut(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.requireInternalToken(w, r)
	if !ok {
		return
	}

	dashID := r.PathValue("dashboard_id")
	ctx := r.Context()

	state, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	if len(state) == 0 {
		writeError(w, http.StatusBadRequest, "empty body")
		return
	}

	// MergeAndStore treats a missing or trashed dashboard as a silent no-op
	// (so a stale relay can never resurrect one); the endpoint must answer 404
	// explicitly, so check liveness first. The org filter makes a cross-org
	// dashboard ID indistinguishable from a missing one, exactly like the
	// authorize endpoint.
	var locked string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashID, claims.OrgID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// The validator runs inside the merge transaction: the actor must hold
	// dashboard edit for any content change and notebook view for every
	// added/changed widget cell reference. A denied store is a 403 the relay
	// abandons (no retry loop); idempotent stores skip the checks so a viewer
	// token can flush an unchanged document.
	validate := func(vctx context.Context, tx pgx.Tx, stored, incoming []byte) error {
		return s.validateDashboardDocStore(vctx, tx, claims, dashID, stored, incoming)
	}
	if err := dashboarddoc.MergeAndStoreValidated(ctx, s.db.Pool, dashID, state, validate); err != nil {
		if errors.Is(err, dashboarddoc.ErrStoreForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		slog.Error("internal dashboard yjs: merge and store", "dashboard_id", dashID, "error", err)
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
	claims, ok := s.requireInternalToken(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id": claims.UserID,
		"org_id":  claims.OrgID,
		"role":    claims.Role,
	})
}

// collabAuthorizeRequest is the body of POST /internal/collab/authorize.
type collabAuthorizeRequest struct {
	DocumentName string `json:"document_name"`
}

// @Summary Authorize a collaborative document
// @Description Decides whether the caller may edit the dashboard backing a collaborative document; viewers get can_edit=false (internal relay endpoint)
// @Tags internal
// @Accept json
// @Produce json
// @Param body body collabAuthorizeRequest true "Document name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /internal/collab/authorize [post]
func (s *Server) handleInternalCollabAuthorize(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.requireInternalToken(w, r)
	if !ok {
		return
	}

	var req collabAuthorizeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only dashboard documents are collaborative today; notebook documents have
	// their own Yjs path and must not be authorized through this endpoint.
	const prefix = "dashboard:"
	if !strings.HasPrefix(req.DocumentName, prefix) {
		writeError(w, http.StatusBadRequest, "unsupported document name")
		return
	}
	dashUUID, err := uuid.Parse(strings.TrimPrefix(req.DocumentName, prefix))
	if err != nil {
		writeError(w, http.StatusBadRequest, "malformed dashboard id")
		return
	}
	dashID := dashUUID.String()

	ctx := r.Context()

	// Missing, trashed, and cross-org dashboards are indistinguishable to the
	// caller: all answer 404, so the endpoint never confirms a dashboard exists
	// outside the caller's org.
	var found string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashID, claims.OrgID,
	).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	canEdit, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !canEdit {
		// view_with_data does not imply view in the ACL resolver, so a
		// data-only viewer must be checked explicitly (mirrors handleGetDashboard).
		canView, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !canView {
			canView, err = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
			if err != nil {
				writeError(w, http.StatusInternalServerError, "permission check failed")
				return
			}
		}
		if !canView {
			writeError(w, http.StatusForbidden, "access denied")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document_id": dashID,
		"can_edit":    canEdit,
	})
}
