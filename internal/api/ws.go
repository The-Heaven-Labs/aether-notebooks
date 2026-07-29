package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
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

type runningCellInfo struct {
	NotebookID string    `json:"notebook_id"`
	StartedAt  time.Time `json:"started_at"`
}

type Hub struct {
	mu           sync.RWMutex
	rooms        map[string]map[*wsConn]bool
	runningCells sync.Map // cellID → runningCellInfo
	rdb          *redis.Client
	pubsub       *redis.PubSub
	stopCh       chan struct{}
}

func NewHub(rdb *redis.Client) *Hub {
	h := &Hub{
		rooms:  make(map[string]map[*wsConn]bool),
		rdb:    rdb,
		stopCh: make(chan struct{}),
	}
	if rdb != nil {
		h.pubsub = rdb.Subscribe(context.Background())
		go h.redisListener()
	}
	h.loadRunningCellsFromRedis()
	return h
}

func (h *Hub) Join(notebookID string, wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[notebookID] == nil {
		h.rooms[notebookID] = make(map[*wsConn]bool)
		if h.rdb != nil {
			h.pubsub.Subscribe(context.Background(), "ws:notebook:"+notebookID)
		}
	}
	h.rooms[notebookID][wc] = true
}

func (h *Hub) Leave(notebookID string, wc *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[notebookID], wc)
	if len(h.rooms[notebookID]) == 0 {
		delete(h.rooms, notebookID)
		if h.rdb != nil {
			h.pubsub.Unsubscribe(context.Background(), "ws:notebook:"+notebookID)
		}
	}
}

func (h *Hub) redisListener() {
	for {
		ch := h.pubsub.Channel()
		for {
			select {
			case msg := <-ch:
				if msg == nil {
					goto reconnect
				}
				notebookID := strings.TrimPrefix(msg.Channel, "ws:notebook:")
				h.mu.RLock()
				conns := h.rooms[notebookID]
				h.mu.RUnlock()
				for wc := range conns {
					wc.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
				}
			case <-h.stopCh:
				h.pubsub.Close()
				return
			}
		}
	reconnect:
		h.pubsub.Close()
		time.Sleep(time.Second)
		h.mu.RLock()
		rooms := make([]string, 0, len(h.rooms))
		for r := range h.rooms {
			rooms = append(rooms, r)
		}
		h.mu.RUnlock()
		h.pubsub = h.rdb.Subscribe(context.Background())
		for _, r := range rooms {
			h.pubsub.Subscribe(context.Background(), "ws:notebook:"+r)
		}
	}
}

func (h *Hub) SetRunning(cellID, notebookID string, startedAt time.Time) {
	info := runningCellInfo{NotebookID: notebookID, StartedAt: startedAt}
	h.runningCells.Store(cellID, info)
	if h.rdb != nil {
		data, _ := json.Marshal(info)
		h.rdb.Set(context.Background(), "cell:running:"+cellID, data, 5*time.Minute)
	}
}

func (h *Hub) UnsetRunning(cellID string) {
	h.runningCells.Delete(cellID)
	if h.rdb != nil {
		h.rdb.Del(context.Background(), "cell:running:"+cellID)
	}
}

func (h *Hub) RunningCellsForNotebook(notebookID string) []map[string]any {
	var cells []map[string]any
	h.runningCells.Range(func(key, value any) bool {
		info := value.(runningCellInfo)
		if info.NotebookID == notebookID {
			cells = append(cells, map[string]any{
				"cell_id":    key.(string),
				"started_at": info.StartedAt,
			})
		}
		return true
	})
	return cells
}

func (h *Hub) loadRunningCellsFromRedis() {
	if h.rdb == nil {
		return
	}
	ctx := context.Background()
	keys, err := h.rdb.Keys(ctx, "cell:running:*").Result()
	if err != nil {
		return
	}
	for _, key := range keys {
		data, err := h.rdb.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var info runningCellInfo
		if err := json.Unmarshal([]byte(data), &info); err != nil {
			continue
		}
		cellID := strings.TrimPrefix(key, "cell:running:")
		h.runningCells.Store(cellID, info)
	}
}

func (h *Hub) Broadcast(notebookID string, msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	if h.rdb != nil {
		h.rdb.Publish(context.Background(), "ws:notebook:"+notebookID, data)
		return
	}

	// Without Redis, deliver locally only.
	h.mu.RLock()
	defer h.mu.RUnlock()
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
