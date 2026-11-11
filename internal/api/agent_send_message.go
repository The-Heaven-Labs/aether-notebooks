package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/the-heaven-labs/aether/internal/agent"
)

// sendMessageRequest is the POST /sessions/{session_id}/messages request body.
type sendMessageRequest struct {
	Message string `json:"message"`
}

// handleSendMessage enqueues a chat message for an existing agent session.
// Processing runs in the background (fire-and-forget) and the result is
// persisted to agent_messages, retrievable via GET /sessions/{id}/messages.
func (h *agentHandlers) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var exists bool
	if err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM agent_sessions WHERE id = $1)
	`, sessionID).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	allowed, err := h.server.checkSessionPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, sessionID, "edit")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req sendMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	engine := h.server.agentEngine
	if engine == nil {
		writeError(w, http.StatusInternalServerError, "agent engine not configured")
		return
	}

	capturedPageCtx := engine.GetPageContext(sessionID)
	masterKey := h.server.masterKey

	// Sync the request's admin mode into the session store so the turn runs with
	// the same privileges that authorized it (mirrors handleCreateSession/handleAgentWS).
	engine.SessionStore().SetAdminMode(sessionID, adminModeFromContext(r.Context()))

	// Fire-and-forget: the turn keeps running after the response is sent, on a
	// background context, and the final result lands in agent_messages. This is
	// the REST equivalent of the WebSocket disconnect-continues behavior.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		_, _, _, _, _, err := engine.ProcessMessage(
			ctx, sessionID, req.Message, nil, nil, masterKey, capturedPageCtx,
			func(string) {},
			func(string) {},
			func(string, string, string, string, int) {},
			func(string, string, string, string, string, int, int) {},
			func(agent.EngineEvent) {},
		)
		if err != nil {
			if ctx.Err() == context.Canceled {
				return
			}
			slog.Error("send message: agent processing failed", "session_id", sessionID, "error", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"session_id": sessionID,
		"status":     "processing",
	})
}
