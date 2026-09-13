package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/models"
)

func createSessionTestAgent(t *testing.T, pool *pgxpool.Pool, orgID, userID string) string {
	t.Helper()
	agentID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, max_turns, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Session Test Agent', '', '', '{}', '{}', 10, $3, NOW(), NOW())
	`, agentID, orgID, userID)
	require.NoError(t, err)
	return agentID
}

func TestSessionStoreTokensAfterRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	store := agent.NewSessionStore(db.Pool)
	ctx := context.Background()
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil)
	require.NoError(t, err)

	base := time.Now()
	require.NoError(t, store.AppendMessage(ctx, &models.AgentMessage{
		ID:        uuid.New().String(),
		SessionID: session.ID,
		Role:      "user",
		Content:   "hello",
		CreatedAt: base,
	}))

	after := 400
	require.NoError(t, store.AppendMessage(ctx, &models.AgentMessage{
		ID:          uuid.New().String(),
		SessionID:   session.ID,
		Role:        "compaction",
		Content:     "summary of earlier history",
		TokensAfter: &after,
		CreatedAt:   base.Add(time.Second),
	}))

	messages, err := store.GetMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	require.Nil(t, messages[0].TokensAfter)
	require.NotNil(t, messages[1].TokensAfter)
	require.Equal(t, 400, *messages[1].TokensAfter)
}

func TestSessionStoreGetMessagesOrderByIDAsc(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	store := agent.NewSessionStore(db.Pool)
	ctx := context.Background()
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil)
	require.NoError(t, err)

	// Canonical UUID text ordering matches Postgres uuid comparison, so keep
	// unique random ids but control which one is lexicographically smaller.
	smallerID, largerID := uuid.New().String(), uuid.New().String()
	if smallerID > largerID {
		smallerID, largerID = largerID, smallerID
	}
	ts := time.Now().UTC()

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO agent_messages (id, session_id, role, content, created_at)
		VALUES ($1, $3, 'assistant', 'larger id inserted first', $2)
	`, largerID, ts, session.ID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO agent_messages (id, session_id, role, content, created_at)
		VALUES ($1, $3, 'user', 'smaller id inserted second', $2)
	`, smallerID, ts, session.ID)
	require.NoError(t, err)

	messages, err := store.GetMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, smallerID, messages[0].ID)
	require.Equal(t, largerID, messages[1].ID)
}
