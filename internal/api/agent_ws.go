package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/heavenlabs/hnb/internal/agent"
	"github.com/heavenlabs/hnb/internal/models"
)

type agentWSHandler struct {
	server *Server
	engine *agent.Engine
}

type WSMessage struct {
	Type          string `json:"type"`
	Content       string `json:"content,omitempty"`
	Command       string `json:"command,omitempty"`
	LastMessageID string `json:"last_message_id,omitempty"`
}

type WSResponse struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type WSErrorResponse struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

var _ = (*websocket.Conn)(nil)

func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, err := s.agentEngine.SessionStore().GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	writeChan := make(chan any, 256)
	var wg sync.WaitGroup

	// Track cancel function for current message processing
	var mu sync.Mutex
	var currentCancel context.CancelFunc
	var processing bool

	wg.Add(1)
	go func() {
		defer wg.Done()
		for out := range writeChan {
			if err := conn.WriteJSON(out); err != nil {
				slog.Debug("ws: write error, writer exiting", "session_id", sessionID, "error", err)
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(writeChan)
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				slog.Debug("ws: read error, reader exiting", "session_id", sessionID, "error", err)
				return
			}

			slog.Debug("ws: received message", "session_id", sessionID, "type", msg.Type, "content_len", len(msg.Content))

			if msg.Type == "cancel" {
				mu.Lock()
				if currentCancel != nil {
					currentCancel()
					currentCancel = nil
				}
				processing = false
				mu.Unlock()
				writeChan <- WSResponse{Type: "cancelled"}
				continue
			}

			if msg.Type == "reconnect" {
				rows, err := s.db.Pool.Query(ctx, `
					SELECT id, role, content, tool_calls FROM agent_messages
					WHERE session_id = $1 AND id > $2 ORDER BY created_at
				`, sessionID, msg.LastMessageID)
				if err == nil {
					var messages []models.AgentMessage
					for rows.Next() {
						var m models.AgentMessage
						var content *string
						var toolCallsJSON []byte
						rows.Scan(&m.ID, &m.Role, &content, &toolCallsJSON)
						if content != nil {
							m.Content = *content
						}
						if toolCallsJSON != nil {
							json.Unmarshal(toolCallsJSON, &m.ToolCalls)
						}
						messages = append(messages, m)
					}
					rows.Close()
					writeChan <- struct {
						Type     string                `json:"type"`
						Messages []models.AgentMessage `json:"messages"`
					}{Type: "reconnect_sync", Messages: messages}
				}
				continue
			}

			if msg.Type == "message" {
				mu.Lock()
				if processing {
					mu.Unlock()
					continue // drop message if already processing
				}
				processing = true
				mu.Unlock()

				slog.Info("ws: processing message", "session_id", sessionID, "content_len", len(msg.Content))

				// Run processing in separate goroutine so reader stays free for cancel
				go func(content string) {
					msgCtx, msgCancel := context.WithCancel(ctx)
					mu.Lock()
					currentCancel = msgCancel
					mu.Unlock()

					_, reasoning, _, events, err := s.agentEngine.ProcessMessage(msgCtx, sessionID, content, s.agentEngine.GetRegistry().List(), s.masterKey,
						func(token string) {
							writeChan <- WSResponse{Type: "token", Data: token}
						},
						func(r string) {
							writeChan <- WSResponse{Type: "reasoning", Data: r}
						},
						func(toolName, toolID, reasoning string) {
							writeChan <- struct {
								Type      string `json:"type"`
								Tool      string `json:"tool"`
								Reasoning string `json:"reasoning,omitempty"`
							}{Type: "tool_call", Tool: toolName, Reasoning: reasoning}
						},
						func(toolName, params, result, errMsg string) {
							writeChan <- struct {
								Type   string `json:"type"`
								Tool   string `json:"tool"`
								Params string `json:"params"`
								Result string `json:"result"`
								Error  string `json:"error,omitempty"`
							}{Type: "tool_result", Tool: toolName, Params: params, Result: result, Error: errMsg}
						},
						func(evt agent.EngineEvent) {
							switch evt.Type {
							case "cell_created":
								writeChan <- struct {
									Type     string `json:"type"`
									CellID   string `json:"cell_id"`
									Position int    `json:"position"`
								}{Type: evt.Type, CellID: evt.CellID, Position: evt.Position}
							case "cell_output":
								writeChan <- struct {
									Type    string `json:"type"`
									CellID  string `json:"cell_id"`
									Outputs any    `json:"outputs"`
								}{Type: evt.Type, CellID: evt.CellID, Outputs: evt.Outputs}
							case "cell_updated":
								writeChan <- struct {
									Type   string `json:"type"`
									CellID string `json:"cell_id"`
									Source string `json:"source,omitempty"`
								}{Type: "cell_updated", CellID: evt.CellID, Source: evt.Source}
							case "tasks_updated":
								writeChan <- struct {
									Type string            `json:"type"`
									Data []agent.AgentTask `json:"data"`
								}{Type: "tasks_updated", Data: evt.Tasks}
							}
						},
					)

					mu.Lock()
					currentCancel = nil
					processing = false
					mu.Unlock()

					if err != nil {
						if msgCtx.Err() == context.Canceled {
							// Cancelled by user - cancelled response already sent
							return
						}
						slog.Error("ws: process message error", "session_id", sessionID, "error", err)
						writeChan <- WSErrorResponse{Type: "error", Message: err.Error()}
						return
					}

					_ = events
					writeChan <- WSResponse{Type: "done", Data: map[string]any{"content": "", "reasoning": reasoning}}
					slog.Debug("ws: message done", "session_id", sessionID, "reasoning_len", len(reasoning))
				}(msg.Content)
			} else if msg.Type == "slash_command" {
				result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, claims.OrgID, s.masterKey)
				if err != nil {
					writeChan <- WSErrorResponse{Type: "error", Message: err.Error()}
					continue
				}
				writeChan <- struct {
					Type    string `json:"type"`
					Command string `json:"command"`
					Data    any    `json:"data"`
				}{Type: "slash_result", Command: msg.Command, Data: result}
			}
		}
	}()

	wg.Wait()
}
