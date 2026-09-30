package agent_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
)

// NewEngine must wire the Redis client into Engine.rdb; otherwise session
// reasoning effort, model config and page context stay process-local despite a
// Redis client being passed in.
func TestEngineSharesRedisStateAcrossInstances(t *testing.T) {
	url := os.Getenv("AETHER_REDIS_URL")
	if url == "" {
		t.Skip("AETHER_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	require.NoError(t, err)
	rdb := redis.NewClient(opt)
	require.NoError(t, rdb.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = rdb.Close() })

	db := setupTestDB(t)
	ctx := context.Background()
	sessionID := "engine-" + uuid.NewString()
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = rdb.Del(cctx, "agent:reasoning:"+sessionID).Err()
	})

	engineA := agent.NewEngine(ctx, db.Pool, rdb)
	engineB := agent.NewEngine(ctx, db.Pool, rdb)

	engineA.SetReasoningEffort(sessionID, "high")
	require.Equal(t, "high", engineB.GetReasoningEffort(sessionID))
}
