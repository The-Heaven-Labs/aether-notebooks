package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RateLimiter struct {
	sessionStore *SessionStore
	pool         *pgxpool.Pool
}

func NewRateLimiter(pool *pgxpool.Pool) *RateLimiter {
	return &RateLimiter{
		sessionStore: NewSessionStore(pool),
		pool:         pool,
	}
}

func (rl *RateLimiter) CheckAndUpdateTokens(ctx context.Context, sessionID string, tokensIn, tokensOut int) (bool, error) {
	messageCount, err := rl.sessionStore.GetMessageCount(ctx, sessionID)
	if err != nil {
		return false, err
	}

	var maxTurns, maxTokens int
	err = rl.pool.QueryRow(ctx, `
		SELECT max_turns, max_tokens FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&maxTurns, &maxTokens)
	if err != nil {
		return false, err
	}

	if messageCount >= maxTurns {
		return false, nil
	}

	var totalTokens int
	rl.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(tokens_input + tokens_output), 0) FROM agent_messages WHERE session_id = $1
	`, sessionID).Scan(&totalTokens)

	if totalTokens >= maxTokens {
		return false, nil
	}

	return true, nil
}

func (rl *RateLimiter) CreateSummarizedSession(ctx context.Context, sessionID string, summary string, masterKey []byte) (string, error) {
	oldSession, err := rl.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	newSession, err := rl.sessionStore.CreateSession(ctx, oldSession.AgentID, oldSession.NotebookID, oldSession.UserID, oldSession.MaxTurns, oldSession.MaxTokens, nil)
	if err != nil {
		return "", err
	}

	msgID := uuid.New().String()
	err = rl.sessionStore.AppendMessage(ctx, &models.AgentMessage{
		ID:        msgID,
		SessionID: newSession.ID,
		Role:      "user",
		Content:   "Previous conversation summary: " + summary,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return "", err
	}

	rl.sessionStore.EndSession(ctx, sessionID)

	return newSession.ID, nil
}

func (rl *RateLimiter) GetSessionStats(ctx context.Context, sessionID string) (messageCount int, totalTokens int, err error) {
	messageCount, err = rl.sessionStore.GetMessageCount(ctx, sessionID)
	if err != nil {
		return
	}

	rl.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(tokens_input + tokens_output), 0) FROM agent_messages WHERE session_id = $1
	`, sessionID).Scan(&totalTokens)

	return
}
