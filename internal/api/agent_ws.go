package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	done := make(chan struct{})

	go func() {
		for {
			select {
			case out := <-writeChan:
				if err := conn.WriteJSON(out); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		defer close(done)
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
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
							json.Unmarshal(toolCallsJSON, &m.ToolCalls)
						}
						messages = append(messages, m)
					}
					rows.Close()
					writeChan <- struct {
						Type     string               `json:"type"`
						Messages []models.AgentMessage `json:"messages"`
					}{Type: "reconnect_sync", Messages: messages}
				}
				continue
			}

			if msg.Type == "message" {
				_, reasoning, _, events, err := s.agentEngine.ProcessMessage(ctx, sessionID, msg.Content, s.agentEngine.GetRegistry().List(), s.masterKey,
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
						case "tasks_updated":
							writeChan <- struct {
								Type string            `json:"type"`
								Data []agent.AgentTask `json:"data"`
							}{Type: "tasks_updated", Data: evt.Tasks}
						}
					},
				)
				if err != nil {
					writeChan <- WSErrorResponse{Type: "error", Message: err.Error()}
					return
				}

				for range events {
				}
				writeChan <- WSResponse{Type: "done", Data: map[string]any{"content": "", "reasoning": reasoning}}
			} else if msg.Type == "slash_command" {
				result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, claims.OrgID, s.masterKey)
				if err != nil {
					writeChan <- WSErrorResponse{Type: "error", Message: err.Error()}
					return
				}
				writeChan <- struct {
					Type    string `json:"type"`
					Command string `json:"command"`
					Data    any    `json:"data"`
				}{Type: "slash_result", Command: msg.Command, Data: result}
			}
		}
	}()

	<-done
}