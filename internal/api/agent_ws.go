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
	Type            string   `json:"type"`
	Content         string   `json:"content,omitempty"`
	Answer          string   `json:"answer,omitempty"`
	Command         string   `json:"command,omitempty"`
	LastMessageID   string   `json:"last_message_id,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	ModelConfigID   string   `json:"model_config_id,omitempty"`
	Approved        bool     `json:"approved,omitempty"`
	AdminMode       bool     `json:"admin_mode,omitempty"`
	Images          []string `json:"images,omitempty"`
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

// WebSocket liveness: server pings every wsPingPeriod; a client that misses
// pong responses for wsPongWait is declared dead so its subscriber channel
// stops accumulating dropped events. Every write carries a short deadline so a
// half-open connection cannot wedge the writer goroutine forever.
const (
	wsPingPeriod = 25 * time.Second
	wsPongWait   = 60 * time.Second
	wsWriteWait  = 10 * time.Second
)

// @Summary Agent WebSocket
// @Description WebSocket endpoint for real-time agent chat
// @Tags agents
// @Produce json
// @Param session_id path string true "Session ID"
// @Success 101
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /ws/agents/{session_id} [get]
func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	sess, err := s.agentEngine.SessionStore().GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	allowed, _ := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", sess.AgentID, "view")
	if !allowed {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	// Capture admin mode for this session so the engine can respect per-tool ACLs
	// unless the profile-page "admin mode" toggle is ON.
	if s.agentEngine != nil {
		s.agentEngine.SessionStore().SetAdminMode(sessionID, adminModeFromContext(r.Context()))
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Keepalive: dead (half-open) connections are closed deterministically
	// instead of wedging the writer and filling the stream subscriber channel.
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// Use a background context so in-flight processing isn't cancelled when the
	// WebSocket disconnects (e.g. page navigation). The processing continues and
	// the final result is stored in the DB for the next reconnect to pick up.
	// Deliberately deadline-free: per-message budgets are set where messages are
	// processed. A deadline here would poison every future message once the
	// connection outlives it (all failing instantly with context deadline
	// exceeded until the page is refreshed).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writeChan := make(chan any, 256)
	var wg sync.WaitGroup
	var processWg sync.WaitGroup

	// writeJSON bounds every control write so a dead connection fails fast.
	writeJSON := func(v any) error {
		conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteJSON(v)
	}

	// writeStreamEvent forwards a sequenced stream event, injecting its seq
	// into the wire payload (additive "seq" field — old clients ignore it) so
	// reconnecting clients can drop replayed duplicates.
	writeStreamEvent := func(ev agent.SequencedEvent) error {
		raw, err := json.Marshal(ev.Msg)
		if err != nil {
			return writeJSON(ev.Msg)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return writeJSON(ev.Msg)
		}
		m["seq"] = ev.Seq
		return writeJSON(m)
	}

	// Subscribe to the shared session stream with catch-up buffer. The buffer
	// contains events from any in-flight ProcessMessage that was running when
	// we disconnected (page navigation). These events are replayed on the
	// frontend, giving the user a live-streaming experience for the part of
	// the response they missed. The reconnect_sync (DB query) runs in parallel
	// and replaces messages with the authoritative state, preventing duplicates.
	subChan, unsubscribe := s.agentEngine.SubscribeSession(sessionID, 512, false)

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
	// (slash results, reconnect_sync, errors, etc.). A ping ticker keeps
	// half-open connections from lingering invisibly (see wsPingPeriod).
	pingTicker := time.NewTicker(wsPingPeriod)
	defer pingTicker.Stop()
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
				if err := writeJSON(out); err != nil {
					slog.Debug("ws: write error, writer exiting", "error", err)
					return
				}
			case out, ok := <-sc:
				if !ok {
					sc = nil
					continue
				}
				if err := writeStreamEvent(out); err != nil {
					slog.Debug("ws: write error, writer exiting", "error", err)
					return
				}
			case <-pingTicker.C:
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					slog.Debug("ws: ping error, writer exiting", "error", err)
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
				// Always return all messages — reconnect_sync is the authoritative
				// state and replaces the frontend's message list. Partial (id > $2)
				// responses cause the frontend to lose older messages since it
				// does a full replacement, not an append.
				rows, err := s.db.Pool.Query(ctx, `
					SELECT id, role, content, tool_calls, reasoning_content, image_ids, COALESCE(duration_ms,0), COALESCE(tokens_direct,0), COALESCE(tokens_after,0), created_at, tool_call_id FROM agent_messages
					WHERE session_id = $1 ORDER BY created_at
				`, currentSessionID)
				if err == nil {
					messages := scanAgentMessages(rows)
					if messages != nil {
						_, running := s.sessionCancels.Load(currentSessionID)
						sessionUsage, _ := s.agentEngine.SessionStore().GetUsage(ctx, currentSessionID)
						safeSend(struct {
							Type         string                `json:"type"`
							Messages     []models.AgentMessage `json:"messages"`
							Running      bool                  `json:"running"`
							ServerSeq    uint64                `json:"server_seq"`
							SessionUsage *models.SessionUsage  `json:"session_usage,omitempty"`
						}{Type: "reconnect_sync", Messages: messages, Running: running, ServerSeq: s.agentEngine.StreamLastSeq(currentSessionID), SessionUsage: sessionUsage})
					}
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

			if msg.Type == "set_model_config" {
				s.agentEngine.SetSessionModelConfig(currentSessionID, msg.ModelConfigID)
				slog.Debug("ws: set model config", "session_id", currentSessionID, "model_config_id", msg.ModelConfigID)
				continue
			}

			if msg.Type == "set_admin_mode" {
				s.agentEngine.SessionStore().SetAdminMode(currentSessionID, msg.AdminMode)
				slog.Debug("ws: set admin mode", "session_id", currentSessionID, "admin_mode", msg.AdminMode)
				continue
			}

			if msg.Type == "tool_confirm" {
				s.agentEngine.ResolveToolConfirm(currentSessionID, msg.Approved, msg.Content)
				continue
			}

			if msg.Type == "question_answer" {
				s.agentEngine.ResolveQuestion(currentSessionID, msg.Answer)
				continue
			}

			if msg.Type == "message" {
				mu.Lock()
				busy := processing
				sid := currentSessionID
				mu.Unlock()

				if busy {
					// A turn is running: deliver the follow-up into it instead
					// of dropping it. The engine folds it in at the next turn
					// boundary and announces it on the stream.
					select {
					case s.agentEngine.SteeringChan(sid) <- msg.Content:
						s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "steering_accepted"})
					default:
						// Mailbox full (16 deep): NACK so the client holds the
						// message in its offline queue and retries later.
						// Content is echoed back for the re-queue.
						s.agentEngine.PublishSessionEvent(sid, struct {
							Type    string `json:"type"`
							Content string `json:"content"`
						}{Type: "steering_busy", Content: msg.Content})
					}
					continue
				}

				mu.Lock()
				processing = true
				mu.Unlock()

				slog.Info("ws: processing message", "session_id", sid, "content_len", len(msg.Content), "image_count", len(msg.Images))

				// Capture the page context at message-send time, not the (potentially
				// changed) value when ProcessMessage builds its system prompt (the
				// user may navigate mid-response).
				capturedPageCtx := s.agentEngine.GetPageContext(sid)

				// Run processing in separate goroutine so reader stays free for cancel
				processWg.Add(1)
				go func(content string, images []string, sid string) {
					defer processWg.Done()
					// Per-message deadline: a long-lived connection must not
					// poison future messages once the connection-level budget
					// would have expired. Each message gets a fresh 30 minutes.
					msgCtx, msgCancel := context.WithTimeout(context.Background(), 30*time.Minute)
					mu.Lock()
					currentCancel = msgCancel
					mu.Unlock()
					s.sessionCancels.Store(sid, msgCancel)
					defer s.sessionCancels.Delete(sid)

					// Stream events are published to the SHARED session stream so that
					// any WebSocket connection (including a new one that reconnects after
					// page navigation) receives the real-time output.
					finalText, reasoning, _, events, tokBrk, err := s.agentEngine.ProcessMessage(msgCtx, sid, content, images, nil, s.masterKey, capturedPageCtx,
						func(token string) {
							s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "token", Data: token})
						},
						func(r string) {
							s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "reasoning", Data: r})
						},
						func(toolName, toolID, args, reasoning string, durationMs int) {
							s.agentEngine.PublishSessionEvent(sid, struct {
								Type       string `json:"type"`
								Tool       string `json:"tool"`
								ToolCallID string `json:"tool_call_id"`
								Params     string `json:"params"`
								Reasoning  string `json:"reasoning,omitempty"`
								DurationMs int    `json:"duration_ms"`
							}{Type: "tool_call", Tool: toolName, ToolCallID: toolID, Params: args, Reasoning: reasoning, DurationMs: durationMs})
						},
						func(toolName, toolID, params, result, errMsg string, durationMs int, tokensDirect int) {
							s.agentEngine.PublishSessionEvent(sid, struct {
								Type         string `json:"type"`
								Tool         string `json:"tool"`
								ToolCallID   string `json:"tool_call_id"`
								Params       string `json:"params"`
								Result       string `json:"result"`
								Error        string `json:"error,omitempty"`
								DurationMs   int    `json:"duration_ms"`
								TokensDirect int    `json:"tokens_direct"`
							}{Type: "tool_result", Tool: toolName, ToolCallID: toolID, Params: params, Result: result, Error: errMsg, DurationMs: durationMs, TokensDirect: tokensDirect})
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
									Type         string                `json:"type"`
									Tokens       *agent.TokenBreakdown `json:"tokens"`
									SessionUsage *models.SessionUsage  `json:"session_usage,omitempty"`
								}{Type: "token_update", Tokens: evt.Tokens, SessionUsage: evt.SessionUsage})
							case "llm_retry":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type        string `json:"type"`
									Attempt     int    `json:"attempt"`
									MaxAttempts int    `json:"max_attempts"`
									Error       string `json:"error,omitempty"`
								}{Type: "llm_retry", Attempt: evt.Attempt, MaxAttempts: evt.MaxAttempts, Error: evt.Error})
							case "steering":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type    string `json:"type"`
									Content string `json:"content"`
								}{Type: "steering", Content: evt.Content})
							case "context_compacted":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type    string                `json:"type"`
									Summary string                `json:"summary"`
									Tokens  *agent.TokenBreakdown `json:"tokens"`
								}{Type: "context_compacted", Summary: evt.Summary, Tokens: evt.Tokens})
							case "question":
								s.agentEngine.PublishSessionEvent(sid, struct {
									Type        string `json:"type"`
									Question    string `json:"question"`
									Options     any    `json:"options,omitempty"`
									AllowCustom bool   `json:"allow_custom"`
								}{Type: "question", Question: evt.Question, Options: evt.Options, AllowCustom: evt.AllowCustom})
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
					// done carries the final content so a client that reconnected
					// mid-stream (and missed the tokens) still renders the message.
					// session_usage restores the server-authoritative token meter.
					doneUsage, usageErr := s.agentEngine.SessionStore().GetUsage(context.Background(), sid)
					if usageErr != nil {
						slog.Warn("ws: get session usage", "session_id", sid, "error", usageErr)
						doneUsage = nil
					}
					s.agentEngine.PublishSessionEvent(sid, WSResponse{Type: "done", Data: map[string]any{"content": finalText, "reasoning": reasoning, "tokens": tokBrk, "session_usage": doneUsage}})
					slog.Debug("ws: message done", "session_id", sid, "reasoning_len", len(reasoning))
				}(msg.Content, msg.Images, currentSessionID)
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

func scanAgentMessages(rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
}) []models.AgentMessage {
	messages := make([]models.AgentMessage, 0)
	for rows.Next() {
		var m models.AgentMessage
		var content *string
		var toolCallsJSON []byte
		var reasoning *string
		var imageIDs []string
		var toolCallID *string
		var tokensAfter int
		rows.Scan(&m.ID, &m.Role, &content, &toolCallsJSON, &reasoning, &imageIDs, &m.DurationMs, &m.TokensDirect, &tokensAfter, &m.CreatedAt, &toolCallID)
		if content != nil {
			m.Content = *content
		}
		if reasoning != nil {
			m.ReasoningContent = *reasoning
		}
		if toolCallsJSON != nil {
			json.Unmarshal(toolCallsJSON, &m.ToolCalls)
		}
		m.ToolCallID = toolCallID
		m.ImageIDs = imageIDs
		if tokensAfter > 0 {
			m.TokensAfter = &tokensAfter
		}
		messages = append(messages, m)
	}
	rows.Close()
	return messages
}
