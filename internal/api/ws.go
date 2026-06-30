package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*websocket.Conn]bool
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*websocket.Conn]bool)}
}

func (h *Hub) Join(notebookID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[notebookID] == nil {
		h.rooms[notebookID] = make(map[*websocket.Conn]bool)
	}
	h.rooms[notebookID][conn] = true
}

func (h *Hub) Leave(notebookID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[notebookID], conn)
	if len(h.rooms[notebookID]) == 0 {
		delete(h.rooms, notebookID)
	}
}

func (h *Hub) Broadcast(notebookID string, msg interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for conn := range h.rooms[notebookID] {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("ws write: %v", err)
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}

	s.hub.Join(nbID, conn)
	defer func() {
		s.hub.Leave(nbID, conn)
		conn.Close()
	}()

	// Read loop — keep connection alive and detect disconnect
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
