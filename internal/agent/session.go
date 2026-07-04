package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionStore struct {
	pool *pgxpool.Pool
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

func (s *SessionStore) CreateSession(ctx context.Context, agentID, notebookID, userID string, maxTurns int, title *string) (*models.AgentSession, error) {
	session := &models.AgentSession{
		ID:         uuid.New().String(),
		AgentID:    agentID,
		NotebookID: notebookID,
		UserID:     userID,
		MaxTurns:   maxTurns,
		Title:      title,
		CreatedAt:  time.Now(),
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

	return session, nil
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
		INSERT INTO agent_messages (id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, tokens_input, tokens_output, model_calls, duration_ms, image_ids, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, msg.ID, msg.SessionID, msg.Role, msg.Content, msg.ToolCallID, toolCallsJSON, msg.ReasoningContent, msg.TokensInput, msg.TokensOutput, msg.ModelCalls, msg.DurationMs, imageIDs, msg.CreatedAt)
	return err
}

func (s *SessionStore) GetMessages(ctx context.Context, sessionID string) ([]models.AgentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, tokens_input, tokens_output, model_calls, duration_ms, image_ids, created_at
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
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &reasoningContent, &msg.TokensInput, &msg.TokensOutput, &msg.ModelCalls, &msg.DurationMs, &imageIDs, &msg.CreatedAt)
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

func (s *SessionStore) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_messages WHERE session_id = $1`, sessionID).Scan(&count)
	return count, err
}

func (s *SessionStore) GetMessagesWithLimit(ctx context.Context, sessionID string, limit int) ([]models.AgentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, tokens_input, tokens_output, model_calls, duration_ms, image_ids, created_at
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
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &reasoningContent, &msg.TokensInput, &msg.TokensOutput, &msg.ModelCalls, &msg.DurationMs, &imageIDs, &msg.CreatedAt)
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
