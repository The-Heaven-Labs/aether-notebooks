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

func (s *SessionStore) CreateSession(ctx context.Context, agentID, notebookID, userID string, maxTurns int, title *string, adminMode, autoApproveTools, autoAnswerQuestions bool) (*models.AgentSession, error) {
	session := &models.AgentSession{
		ID:                  uuid.New().String(),
		AgentID:             agentID,
		NotebookID:          notebookID,
		UserID:              userID,
		MaxTurns:            maxTurns,
		Title:               title,
		CreatedAt:           time.Now(),
		AdminMode:           adminMode,
		AutoApproveTools:    autoApproveTools,
		AutoAnswerQuestions: autoAnswerQuestions,
	}

	var nbID *string
	if session.NotebookID != "" {
		nbID = &session.NotebookID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, title, created_at, auto_approve_tools, auto_answer_questions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, session.ID, session.AgentID, nbID, session.UserID, session.MaxTurns, session.Title, session.CreatedAt, session.AutoApproveTools, session.AutoAnswerQuestions)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	if adminMode {
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
		SELECT id, agent_id, notebook_id, user_id, max_turns, ended_at, title, created_at,
			auto_approve_tools, auto_answer_questions,
			context_tokens, context_window, total_input, total_output, total_reasoning,
			total_cache_read, total_model_calls, total_subagent_input, total_subagent_output
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&session.ID, &session.AgentID, &notebookID, &session.UserID, &session.MaxTurns, &endedAt, &title, &session.CreatedAt,
		&session.AutoApproveTools, &session.AutoAnswerQuestions,
		&session.ContextTokens, &session.ContextWindow, &session.TotalInput, &session.TotalOutput, &session.TotalReasoning,
		&session.TotalCacheRead, &session.TotalModelCalls, &session.TotalSubagentInput, &session.TotalSubagentOutput)
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
		INSERT INTO agent_messages (id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, tokens_input, tokens_output, tokens_direct, tokens_after, kept_count, model_calls, duration_ms, image_ids, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, msg.ID, msg.SessionID, msg.Role, msg.Content, msg.ToolCallID, toolCallsJSON, msg.ReasoningContent, msg.TokensInput, msg.TokensOutput, msg.TokensDirect, msg.TokensAfter, msg.KeptCount, msg.ModelCalls, msg.DurationMs, imageIDs, msg.CreatedAt)
	return err
}

func (s *SessionStore) GetMessages(ctx context.Context, sessionID string) ([]models.AgentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, COALESCE(tokens_input,0), COALESCE(tokens_output,0), COALESCE(tokens_direct,0), tokens_after, kept_count, COALESCE(model_calls,0), COALESCE(duration_ms,0), image_ids, created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC, id ASC
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
		var tokensAfter *int
		var keptCount *int
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &reasoningContent, &msg.TokensInput, &msg.TokensOutput, &msg.TokensDirect, &tokensAfter, &keptCount, &msg.ModelCalls, &msg.DurationMs, &imageIDs, &msg.CreatedAt)
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
		msg.TokensAfter = tokensAfter
		msg.KeptCount = keptCount
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

// SessionUsageDelta is the per-call token accounting added to a session's
// running totals. ContextTokens/ContextWindow are point-in-time snapshots:
// values > 0 replace the stored context, 0 leaves it untouched.
type SessionUsageDelta struct {
	Input, Output, Reasoning, CacheRead int64
	ModelCalls                          int
	SubagentInput, SubagentOutput       int64
	ContextTokens                       int64
	ContextWindow                       int
}

// AddUsage accumulates a token delta into the session's persisted usage totals.
func (s *SessionStore) AddUsage(ctx context.Context, sessionID string, d SessionUsageDelta) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions SET
			total_input = total_input + $2,
			total_output = total_output + $3,
			total_reasoning = total_reasoning + $4,
			total_cache_read = total_cache_read + $5,
			total_model_calls = total_model_calls + $6,
			total_subagent_input = total_subagent_input + $7,
			total_subagent_output = total_subagent_output + $8,
			context_tokens = CASE WHEN $9 > 0 THEN $9 ELSE context_tokens END,
			context_window = CASE WHEN $10 > 0 THEN $10 ELSE context_window END
		WHERE id = $1`,
		sessionID, d.Input, d.Output, d.Reasoning, d.CacheRead, d.ModelCalls, d.SubagentInput, d.SubagentOutput, d.ContextTokens, d.ContextWindow)
	return err
}

// GetUsage returns the session's accumulated usage snapshot.
func (s *SessionStore) GetUsage(ctx context.Context, sessionID string) (*models.SessionUsage, error) {
	var u models.SessionUsage
	err := s.pool.QueryRow(ctx, `
		SELECT total_input, total_output, total_reasoning, total_cache_read, total_model_calls,
		       total_subagent_input, total_subagent_output, context_tokens, context_window
		FROM agent_sessions WHERE id = $1`, sessionID).
		Scan(&u.Input, &u.Output, &u.Reasoning, &u.CacheRead, &u.ModelCalls, &u.SubagentInput, &u.SubagentOutput, &u.ContextTokens, &u.ContextWindow)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// applyUsageDelta folds a delta into an in-memory snapshot with the same
// semantics as AddUsage, so live event payloads match persisted totals.
func applyUsageDelta(u *models.SessionUsage, d SessionUsageDelta) {
	u.Input += d.Input
	u.Output += d.Output
	u.Reasoning += d.Reasoning
	u.CacheRead += d.CacheRead
	u.ModelCalls += d.ModelCalls
	u.SubagentInput += d.SubagentInput
	u.SubagentOutput += d.SubagentOutput
	if d.ContextTokens > 0 {
		u.ContextTokens = d.ContextTokens
	}
	if d.ContextWindow > 0 {
		u.ContextWindow = d.ContextWindow
	}
}
