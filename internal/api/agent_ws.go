package api

import (
	"context"
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

	writeChan := make(chan string, 100)
	done := make(chan struct{})
	droppedTokens := 0

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case token := <-writeChan:
				if err := conn.WriteJSON(WSResponse{Type: "token", Data: token}); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				close(done)
				return
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
							// tool_calls JSON already stored, just pass through
						}
						messages = append(messages, m)
					}
					rows.Close()
					conn.WriteJSON(WSResponse{Type: "reconnect_sync", Data: map[string]any{"messages": messages}})
				}
				continue
			}

			if msg.Type == "message" {
				resp, reasoning, _, events, err := s.agentEngine.ProcessMessage(ctx, sessionID, msg.Content, s.agentEngine.GetRegistry().List(), s.masterKey,
					func(token string) {
						select {
						case writeChan <- token:
						default:
						}
					},
					func(r string) {
						conn.WriteJSON(WSResponse{Type: "reasoning", Data: r})
					},
					func(toolName, toolID, reasoning string) {
						conn.WriteJSON(struct {
							Type      string `json:"type"`
							Tool      string `json:"tool"`
							Reasoning string `json:"reasoning,omitempty"`
						}{Type: "tool_call", Tool: toolName, Reasoning: reasoning})
					},
					func(toolName, params, result, errMsg string) {
						conn.WriteJSON(struct {
							Type   string `json:"type"`
							Tool   string `json:"tool"`
							Params string `json:"params"`
							Result string `json:"result"`
							Error  string `json:"error,omitempty"`
						}{Type: "tool_result", Tool: toolName, Params: params, Result: result, Error: errMsg})
					},
				)
				if err != nil {
					conn.WriteJSON(WSErrorResponse{Type: "error", Message: err.Error()})
					return
				}

				for _, evt := range events {
					switch evt.Type {
					case "cell_created":
						conn.WriteJSON(struct {
							Type     string `json:"type"`
							CellID   string `json:"cell_id"`
							Position int    `json:"position"`
						}{Type: evt.Type, CellID: evt.CellID, Position: evt.Position})
					case "tasks_updated":
						conn.WriteJSON(struct {
							Type string            `json:"type"`
							Data []agent.AgentTask `json:"data"`
						}{Type: "tasks_updated", Data: evt.Tasks})
					}
				}
				select {
				case writeChan <- resp:
				default:
					droppedTokens++
					conn.WriteJSON(WSResponse{Type: "backpressure_warning", Data: map[string]any{"dropped_tokens": droppedTokens}})
				}
				conn.WriteJSON(WSResponse{Type: "done", Data: map[string]any{"content": resp, "reasoning": reasoning}})
			} else if msg.Type == "slash_command" {
				result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, s.masterKey)
				if err != nil {
					conn.WriteJSON(WSErrorResponse{Type: "error", Message: err.Error()})
					return
				}
				conn.WriteJSON(WSResponse{Type: "slash_result", Data: map[string]any{"command": msg.Command, "data": result}})
			}
		}
	}()

	wg.Wait()
}

func (s *Server) handleAgentWSWithUpgrader(upgrader websocket.Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		writeChan := make(chan string, 100)
		done := make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for {
				select {
				case token := <-writeChan:
					if err := conn.WriteJSON(WSResponse{Type: "token", Data: token}); err != nil {
						return
					}
				case <-done:
					return
				}
			}
		}()

		go func() {
			defer wg.Done()
			for {
				var msg WSMessage
				if err := conn.ReadJSON(&msg); err != nil {
					close(done)
					return
				}

				if msg.Type == "message" {
					resp, reasoning, _, events, err := s.agentEngine.ProcessMessage(ctx, sessionID, msg.Content, s.agentEngine.GetRegistry().List(), s.masterKey,
						func(token string) {
							select {
							case writeChan <- token:
							default:
							}
						},
						func(r string) {
							conn.WriteJSON(WSResponse{Type: "reasoning", Data: r})
						},
						func(toolName, toolID, reasoning string) {
							conn.WriteJSON(struct {
								Type      string `json:"type"`
								Tool      string `json:"tool"`
								Reasoning string `json:"reasoning,omitempty"`
							}{Type: "tool_call", Tool: toolName, Reasoning: reasoning})
						},
						func(toolName, params, result, errMsg string) {
							conn.WriteJSON(struct {
								Type   string `json:"type"`
								Tool   string `json:"tool"`
								Params string `json:"params"`
								Result string `json:"result"`
								Error  string `json:"error,omitempty"`
							}{Type: "tool_result", Tool: toolName, Params: params, Result: result, Error: errMsg})
						},
					)
					if err != nil {
						conn.WriteJSON(WSErrorResponse{Type: "error", Message: err.Error()})
						return
					}

					for _, evt := range events {
						switch evt.Type {
						case "cell_created":
							conn.WriteJSON(struct {
								Type     string `json:"type"`
								CellID   string `json:"cell_id"`
								Position int    `json:"position"`
							}{Type: evt.Type, CellID: evt.CellID, Position: evt.Position})
						case "tasks_updated":
							conn.WriteJSON(struct {
								Type string            `json:"type"`
								Data []agent.AgentTask `json:"data"`
							}{Type: "tasks_updated", Data: evt.Tasks})
						}
					}
					writeChan <- resp
					conn.WriteJSON(WSResponse{Type: "done", Data: map[string]any{"content": resp, "reasoning": reasoning}})
				} else if msg.Type == "slash_command" {
					result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, s.masterKey)
					if err != nil {
						conn.WriteJSON(WSErrorResponse{Type: "error", Message: err.Error()})
						return
					}
					conn.WriteJSON(WSResponse{Type: "slash_result", Data: map[string]any{"command": msg.Command, "data": result}})
				}
			}
		}()

		wg.Wait()
	}
}
