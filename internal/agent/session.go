package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/models"
)

type SessionStore struct {
	pool       *pgxpool.Pool
	adminModes sync.Map // sessionID -> bool
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

func (s *SessionStore) CreateSession(ctx context.Context, agentID, notebookID, userID string, maxTurns int, title *string, adminMode ...bool) (*models.AgentSession, error) {
	var am bool
	if len(adminMode) > 0 {
		am = adminMode[0]
	}
	session := &models.AgentSession{
		ID:         uuid.New().String(),
		AgentID:    agentID,
		NotebookID: notebookID,
		UserID:     userID,
		MaxTurns:   maxTurns,
		Title:      title,
		CreatedAt:  time.Now(),
		AdminMode:  am,
	}

	var nbID *string
	if session.NotebookID != "" {
		nbID = &session.NotebookID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, title, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, session.ID, session.AgentID, nbID, session.UserID, session.MaxTurns, session.Title, session.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	if am {
		s.adminModes.Store(session.ID, true)
	}

	return session, nil
}

// SetAdminMode stores the transient admin-mode flag for a session.
func (s *SessionStore) SetAdminMode(sessionID string, adminMode bool) {
	if adminMode {
		s.adminModes.Store(sessionID, true)
	} else {
		s.adminModes.Delete(sessionID)
	}
}

// GetAdminMode returns the transient admin-mode flag for a session.
func (s *SessionStore) GetAdminMode(sessionID string) bool {
	if v, ok := s.adminModes.Load(sessionID); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func (s *SessionStore) GetSession(ctx context.Context, sessionID string) (*models.AgentSession, error) {
	var session models.AgentSession
	var endedAt *time.Time
	var title *string
	var notebookID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, notebook_id, user_id, max_turns, ended_at, title, created_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&session.ID, &session.AgentID, &notebookID, &session.UserID, &session.MaxTurns, &endedAt, &title, &session.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	session.EndedAt = endedAt
	session.NotebookID = ""
	if notebookID != nil {
		session.NotebookID = *notebookID
	}
	if title != nil {
		session.Title = title
	}
	if v, ok := s.adminModes.Load(session.ID); ok {
		if b, ok := v.(bool); ok {
			session.AdminMode = b
		}
	}
	return &session, nil
}

func (s *SessionStore) EndSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions SET ended_at = NOW() WHERE id = $1
	`, sessionID)
	return err
}

func (s *SessionStore) AppendMessage(ctx context.Context, msg *models.AgentMessage) error {
	toolCallsJSON, _ := json.Marshal(msg.ToolCalls)
	imageIDs := msg.ImageIDs
	if imageIDs == nil {
		imageIDs = []string{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_messages (id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, tokens_input, tokens_output, tokens_direct, model_calls, duration_ms, image_ids, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, msg.ID, msg.SessionID, msg.Role, msg.Content, msg.ToolCallID, toolCallsJSON, msg.ReasoningContent, msg.TokensInput, msg.TokensOutput, msg.TokensDirect, msg.ModelCalls, msg.DurationMs, imageIDs, msg.CreatedAt)
	return err
}

func (s *SessionStore) GetMessages(ctx context.Context, sessionID string) ([]models.AgentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, COALESCE(tokens_input,0), COALESCE(tokens_output,0), COALESCE(tokens_direct,0), COALESCE(model_calls,0), COALESCE(duration_ms,0), image_ids, created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.AgentMessage
	for rows.Next() {
		var msg models.AgentMessage
		var content *string
		var toolCallID *string
		var toolCallsJSON []byte
		var reasoningContent *string
		var imageIDs []string
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &reasoningContent, &msg.TokensInput, &msg.TokensOutput, &msg.TokensDirect, &msg.ModelCalls, &msg.DurationMs, &imageIDs, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		if content != nil {
			msg.Content = *content
		}
		if reasoningContent != nil {
			msg.ReasoningContent = *reasoningContent
		}
		msg.ToolCallID = toolCallID
		msg.ImageIDs = imageIDs
		if toolCallsJSON != nil {
			json.Unmarshal(toolCallsJSON, &msg.ToolCalls)
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// UpdateMessageToolCall records a tool execution result on the matching tool
// call of an already-persisted assistant message. This makes reconnect_sync
// self-contained: the frontend can render completed tools without joining the
// separate role='tool' result rows.
func (s *SessionStore) UpdateMessageToolCall(ctx context.Context, messageID, callID string, result any, errMsg string, durationMs int) error {
	var toolCallsJSON []byte
	err := s.pool.QueryRow(ctx, `SELECT tool_calls FROM agent_messages WHERE id = $1`, messageID).Scan(&toolCallsJSON)
	if err != nil {
		return fmt.Errorf("load message tool calls: %w", err)
	}
	var calls []models.ToolCall
	if len(toolCallsJSON) > 0 {
		if err := json.Unmarshal(toolCallsJSON, &calls); err != nil {
			return fmt.Errorf("decode tool calls: %w", err)
		}
	}
	found := false
	for i := range calls {
		if calls[i].ID == callID {
			calls[i].Result = result
			if errMsg != "" {
				calls[i].Error = &errMsg
			}
			calls[i].DurationMs = durationMs
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("tool call %s not found in message %s", callID, messageID)
	}
	updated, err := json.Marshal(calls)
	if err != nil {
		return fmt.Errorf("encode tool calls: %w", err)
	}
	_, err = s.pool.Exec(ctx, `UPDATE agent_messages SET tool_calls = $1 WHERE id = $2`, updated, messageID)
	return err
}

func (s *SessionStore) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_messages WHERE session_id = $1`, sessionID).Scan(&count)
	return count, err
}

func (s *SessionStore) GetMessagesWithLimit(ctx context.Context, sessionID string, limit int) ([]models.AgentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, COALESCE(tokens_input,0), COALESCE(tokens_output,0), COALESCE(tokens_direct,0), COALESCE(model_calls,0), COALESCE(duration_ms,0), image_ids, created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.AgentMessage
	for rows.Next() {
		var msg models.AgentMessage
		var content *string
		var toolCallID *string
		var toolCallsJSON []byte
		var reasoningContent *string
		var imageIDs []string
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &reasoningContent, &msg.TokensInput, &msg.TokensOutput, &msg.TokensDirect, &msg.ModelCalls, &msg.DurationMs, &imageIDs, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		if content != nil {
			msg.Content = *content
		}
		if reasoningContent != nil {
			msg.ReasoningContent = *reasoningContent
		}
		msg.ToolCallID = toolCallID
		msg.ImageIDs = imageIDs
		if toolCallsJSON != nil {
			json.Unmarshal(toolCallsJSON, &msg.ToolCalls)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

func (s *SessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agent_sessions WHERE id = $1`, sessionID)
	return err
}

func (s *SessionStore) UpdateTitle(ctx context.Context, sessionID string, title *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions SET title = $1 WHERE id = $2
	`, title, sessionID)
	if err != nil {
		return fmt.Errorf("update title: %w", err)
	}
	return nil
}
