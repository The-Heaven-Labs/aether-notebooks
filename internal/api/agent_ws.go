package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/models"
)

type agentWSHandler struct {
	server *Server
	engine *agent.Engine
}

type WSMessage struct {
	Type            string `json:"type"`
	Content         string `json:"content,omitempty"`
	Command         string `json:"command,omitempty"`
	LastMessageID   string `json:"last_message_id,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Approved        bool   `json:"approved,omitempty"`
	PageContext     *struct {
		Type  string `json:"type"`
		ID    string `json:"id,omitempty"`
		Title string `json:"title,omitempty"`
	} `json:"page_context,omitempty"`
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

	// Use a background context so in-flight processing isn't cancelled when the
	// WebSocket disconnects (e.g. page navigation). The processing continues and
	// the final result is stored in the DB for the next reconnect to pick up.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	writeChan := make(chan any, 256)
	var wg sync.WaitGroup
	var processWg sync.WaitGroup

	// Subscribe to the shared session stream with catch-up buffer. The buffer
	// contains events from any in-flight ProcessMessage that was running when
	// we disconnected (page navigation). These events are replayed on the
	// frontend, giving the user a live-streaming experience for the part of
	// the response they missed. The reconnect_sync (DB query) runs in parallel
	// and replaces messages with the authoritative state, preventing duplicates.
	subChan, unsubscribe := s.agentEngine.SubscribeSession(sessionID, 512, true)

	// Track cancel function for current message processing
	var mu sync.Mutex
	var currentCancel context.CancelFunc
	var processing bool

	// currentSessionID can be updated when rate-limit auto-continuation creates a new session
	currentSessionID := sessionID

	// safeSend sends a control message to writeChan without blocking.
	safeSend := func(msg any) {
		select {
		case writeChan <- msg:
		default:
		}
	}

	// Writer goroutine reads from both the control channel (writeChan) and the
	// shared session stream (subChan). The session stream carries real-time
	// tokens/events from in-flight processing; writeChan carries control messages
	// (slash results, reconnect_sync, errors, etc.).
	wg.Add(1)
	go func() {
		defer wg.Done()
		wc, sc := writeChan, subChan
		for wc != nil || sc != nil {
			select {
			case out, ok := <-wc:
				if !ok {
					wc = nil
					continue
				}
				if err := conn.WriteJSON(out); err != nil {
					slog.Debug("ws: write error, writer exiting", "error", err)
					return
				}
			case out, ok := <-sc:
				if !ok {
					sc = nil
					continue
				}
				if err := conn.WriteJSON(out); err != nil {
					slog.Debug("ws: write error, writer exiting", "error", err)
					return
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				slog.Debug("ws: read error, reader exiting", "session_id", currentSessionID, "error", err)
				return
			}

			slog.Debug("ws: received message", "session_id", currentSessionID, "type", msg.Type, "content_len", len(msg.Content))

			if msg.Type == "cancel" {
				// Cancel via session-level map (handles reconnected connections)
				if cancel, ok := s.sessionCancels.LoadAndDelete(currentSessionID); ok {
					cancel.(context.CancelFunc)()
				}
				mu.Lock()
				if currentCancel != nil {
					currentCancel()
					currentCancel = nil
				}
				processing = false
				mu.Unlock()
				safeSend(WSResponse{Type: "cancelled"})
				continue
			}

			if msg.Type == "reconnect" {
				rows, err := s.db.Pool.Query(ctx, `
					SELECT id, role, content, tool_calls, created_at FROM agent_messages
					WHERE session_id = $1 AND id > $2 ORDER BY created_at
				`, currentSessionID, msg.LastMessageID)
				if err == nil {
					var messages []models.AgentMessage
					for rows.Next() {
						var m models.AgentMessage
						var content *string
						var toolCallsJSON []byte
						rows.Scan(&m.ID, &m.Role, &content, &toolCallsJSON, &m.CreatedAt)
						if content != nil {
							m.Content = *content
						}
						if toolCallsJSON != nil {
							json.Unmarshal(toolCallsJSON, &m.ToolCalls)
						}
						messages = append(messages, m)
					}
					rows.Close()
					safeSend(struct {
						Type     string                `json:"type"`
						Messages []models.AgentMessage `json:"messages"`
					}{Type: "reconnect_sync", Messages: messages})
				}
				continue
			}

			if msg.Type == "set_reasoning_effort" {
				s.agentEngine.SetReasoningEffort(currentSessionID, msg.ReasoningEffort)
				slog.Debug("ws: set reasoning effort", "session_id", currentSessionID, "effort", msg.ReasoningEffort)
				continue
			}

			if msg.Type == "set_page_context" {
				if msg.PageContext != nil {
					pc := &agent.PageContextInfo{
						Type:  msg.PageContext.Type,
						ID:    msg.PageContext.ID,
						Title: msg.PageContext.Title,
					}
					s.agentEngine.SetPageContext(currentSessionID, pc)
					slog.Debug("ws: set page context", "session_id", currentSessionID, "type", pc.Type, "id", pc.ID)
				} else {
					s.agentEngine.SetPageContext(currentSessionID, nil)
				}
				continue
			}

			if msg.Type == "tool_confirm" {
				s.agentEngine.ResolveToolConfirm(currentSessionID, msg.Approved, msg.Content)
				continue
			}

			if msg.Type == "message" {
				mu.Lock()
				if processing {
					mu.Unlock()
					continue // drop message if already processing
				}
				processing = true
				sid := currentSessionID
				mu.Unlock()

				slog.Info("ws: processing message", "session_id", sid, "content_len", len(msg.Content))

				// Capture the page context at message-send time, not the (potentially
				// changed) value when ProcessMessage builds its system prompt (the
				// user may navigate mid-response).
				capturedPageCtx := s.agentEngine.GetPageContext(sid)

				// Run processing in separate goroutine so reader stays free for cancel
				processWg.Add(1)
				go func(content string, sid string) {
					defer processWg.Done()
					msgCtx, msgCancel := context.WithCancel(ctx)
					mu.Lock()
					currentCancel = msgCancel
					mu.Unlock()
					s.sessionCancels.Store(sid, msgCancel)
					defer s.sessionCancels.Delete(sid)

					// Stream events are published to the SHARED session stream so that
					// any WebSocket connection (including a new one that reconnects after
					// page navigation) receives the real-time output.
					_, reasoning, _, events, tokBrk, err := s.agentEngine.ProcessMessage(msgCtx, sid, content, s.agentEngine.GetRegistry().List(), s.masterKey, capturedPageCtx,
						func(token string) {
							s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "token", Data: token})
						},
						func(r string) {
							s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "reasoning", Data: r})
						},
						func(toolName, toolID, reasoning string) {
							s.agentEngine.PublishSessionEvent(sid, struct {
								Type      string `json:"type"`
								Tool      string `json:"tool"`
								Reasoning string `json:"reasoning,omitempty"`
							}{Type: "tool_call", Tool: toolName, Reasoning: reasoning})
						},
						func(toolName, params, result, errMsg string) {
							s.agentEngine.PublishSessionEvent(sid, struct {
								Type   string `json:"type"`
								Tool   string `json:"tool"`
								Params string `json:"params"`
								Result string `json:"result"`
								Error  string `json:"error,omitempty"`
							}{Type: "tool_result", Tool: toolName, Params: params, Result: result, Error: errMsg})
						},
						func(evt agent.EngineEvent) {
							switch evt.Type {
							case "cell_created":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type     string `json:"type"`
									CellID   string `json:"cell_id"`
									Position int    `json:"position"`
								}{Type: evt.Type, CellID: evt.CellID, Position: evt.Position})
							case "cell_output":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type    string `json:"type"`
									CellID  string `json:"cell_id"`
									Outputs any    `json:"outputs"`
								}{Type: evt.Type, CellID: evt.CellID, Outputs: evt.Outputs})
							case "cell_updated":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type   string `json:"type"`
									CellID string `json:"cell_id"`
									Source string `json:"source,omitempty"`
								}{Type: "cell_updated", CellID: evt.CellID, Source: evt.Source})
							case "tasks_updated":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type string            `json:"type"`
									Data []agent.AgentTask `json:"data"`
								}{Type: "tasks_updated", Data: evt.Tasks})
							case "tool_confirm_required":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type          string `json:"type"`
									ToolName      string `json:"tool_name"`
									ToolArgs      string `json:"tool_args"`
									CurrentSource string `json:"current_source,omitempty"`
								}{Type: "tool_confirm_required", ToolName: evt.ToolName, ToolArgs: evt.ToolArgs, CurrentSource: evt.Source})
							case "token_update":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type   string              `json:"type"`
									Tokens *agent.TokenBreakdown `json:"tokens"`
								}{Type: "token_update", Tokens: evt.Tokens})
							}
						},
					)

					mu.Lock()
					currentCancel = nil
					processing = false
					mu.Unlock()

					if err != nil {
						if msgCtx.Err() == context.Canceled {
							return
						}
						slog.Error("ws: process message error", "session_id", sid, "error", err)
						s.agentEngine.PublishSessionEvent(sid, WSErrorResponse{Type: "error", Message: err.Error()})
						return
					}

					_ = events
					s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "done", Data: map[string]any{"content": "", "reasoning": reasoning, "tokens": tokBrk}})
					slog.Debug("ws: message done", "session_id", sid, "reasoning_len", len(reasoning))
				}(msg.Content, currentSessionID)
			} else if msg.Type == "slash_command" {
				result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, claims.OrgID, s.masterKey)
				if err != nil {
					safeSend(WSErrorResponse{Type: "error", Message: err.Error()})
					continue
				}
				safeSend(struct {
					Type    string `json:"type"`
					Command string `json:"command"`
					Data    any    `json:"data"`
				}{Type: "slash_result", Command: msg.Command, Data: result})
			}
		}
	}()

	// Wait for reader + writer to finish (WS disconnected, e.g. page navigation)
	wg.Wait()

	// Unsubscribe from the shared session stream. Any in-flight ProcessMessage
	// continues running (it publishes to the stream); if a new WebSocket connects
	// for the same session, it subscribes and receives the catch-up buffer.
	unsubscribe()

	// Wait for in-flight LLM processing to finish (response still continues in
	// background; the DB gets the complete result via AppendMessage).
	processWg.Wait()

	// No more sends to writeChan from control messages.
	close(writeChan)
}
