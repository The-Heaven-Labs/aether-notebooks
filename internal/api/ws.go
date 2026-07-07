package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsConn wraps a WebSocket connection with a write mutex to prevent
// concurrent writes from multiple goroutines (e.g. subagent broadcasts).
type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsConn) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *wsConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(messageType, data)
}

func (w *wsConn) ReadMessage() (int, []byte, error) { return w.conn.ReadMessage() }
func (w *wsConn) Close() error                      { return w.conn.Close() }

type Hub struct {
	mu           sync.RWMutex
	rooms        map[string]map[*wsConn]bool
	runningCells sync.Map // cellID → notebookID
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*wsConn]bool)}
}

func (h *Hub) Join(notebookID string, wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[notebookID] == nil {
		h.rooms[notebookID] = make(map[*wsConn]bool)
	}
	h.rooms[notebookID][wc] = true
}

func (h *Hub) Leave(notebookID string, wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[notebookID], wc)
	if len(h.rooms[notebookID]) == 0 {
		delete(h.rooms, notebookID)
	}
}

func (h *Hub) SetRunning(cellID, notebookID string) {
	h.runningCells.Store(cellID, notebookID)
}

func (h *Hub) UnsetRunning(cellID string) {
	h.runningCells.Delete(cellID)
}

func (h *Hub) RunningCellsForNotebook(notebookID string) []string {
	var cells []string
	h.runningCells.Range(func(key, value any) bool {
		if value.(string) == notebookID {
			cells = append(cells, key.(string))
		}
		return true
	})
	return cells
}

func (h *Hub) Broadcast(notebookID string, msg interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for wc := range h.rooms[notebookID] {
		if err := wc.WriteMessage(websocket.TextMessage, data); err != nil {
			slog.Warn("ws write", "error", err)
		}
	}
}

func (s *Server) Hub() *Hub {
	return s.hub
}

func (s *Server) userEmail(ctx context.Context, userID string) string {
	var email string
	err := s.db.Pool.QueryRow(ctx, "SELECT email FROM users WHERE id = $1", userID).Scan(&email)
	if err != nil {
		return userID
	}
	return email
}

// @Summary Notebook WebSocket
// @Description WebSocket connection for real-time notebook collaboration
// @Tags websocket
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 101 {object} map[string]any
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/ws/notebooks/{id} [get]
func (s *Server) handleNotebookWS(w http.ResponseWriter, r *http.Request) {
	nbID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	allowed, _ := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "view")
	if !allowed {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	raw, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ws upgrade", "error", err)
		return
	}

	wc := &wsConn{conn: raw}
	s.hub.Join(nbID, wc)

	runningCells := s.hub.RunningCellsForNotebook(nbID)
	if len(runningCells) > 0 {
		wc.WriteJSON(map[string]any{"type": "sync", "running_cells": runningCells})
	}

	defer func() {
		s.hub.Leave(nbID, wc)
		raw.Close()
	}()

	// Read loop — keep connection alive and detect disconnect
	for {
		if _, _, err := raw.ReadMessage(); err != nil {
			break
		}
	}
}
