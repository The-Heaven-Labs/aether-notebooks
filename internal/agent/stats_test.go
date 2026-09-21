package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRollupHourlyStats(t *testing.T) {
	db := setupEngineTestDB(t)
	ctx := context.Background()
	orgID, userID := createEngineTestOrgAndUser(t, db)

	mcID := uuid.New().String()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, context_window, created_by,
			price_per_input_token, price_per_output_token, price_per_cache_read_token)
		VALUES ($1, $2, 'm', 'openai', 'http://x', 'gpt-4', 'k', 128000, $3, 0.15, 0.60, 0.075)
	`, mcID, orgID, userID)
	if err != nil {
		t.Fatalf("model config: %v", err)
	}
	agentID := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO agents (id, org_id, name, description, system_prompt, model_config_id, skill_ids, tool_ids, folder_id, max_turns, created_by, created_at, updated_at)
		VALUES ($1,$2,'Stats Agent','','', $3,'{}','{}',NULL,10,$4,NOW(),NOW())
	`, agentID, orgID, mcID, userID)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	sid := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, created_at)
		VALUES ($1,$2,NULL,$3,10,NOW())
	`, sid, agentID, userID)
	if err != nil {
		t.Fatalf("session: %v", err)
	}

	// Two messages in the current in-progress hour bucket, anchored to the
	// hour start so they cannot straddle a boundary (which would create two
	// bucket rows and break the single-bucket assertions below).
	now := time.Now()
	at := now.Truncate(time.Hour)
	for _, tok := range [][2]int{{100, 50}, {40, 10}} {
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO agent_messages (id, session_id, role, content, tool_calls, tokens_input, tokens_output, tokens_direct, model_calls, duration_ms, created_at)
			VALUES ($1,$2,'assistant','hi','[]',$3,$4,7,2,1500,$5)
		`, uuid.New().String(), sid, tok[0], tok[1], at)
		if err != nil {
			t.Fatalf("message: %v", err)
		}
	}
	// One completed subagent task in the same hour.
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO subagent_tasks (id, parent_session_id, goal, status, result, tokens_input, tokens_output, created_at, completed_at)
		VALUES ($1,$2,'g','completed','{}',30,20,$3,$3)
	`, uuid.New().String(), sid, at)
	if err != nil {
		t.Fatalf("subagent task: %v", err)
	}

	sa := NewStatsAggregator(db.Pool)
	res1, err := sa.RollupHourlyStats(ctx, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	// RowsAffected counts every bucket upserted in the window (the dev DB is
	// shared across tests) — assert on our own bucket row below instead.
	if res1.Rows < 1 {
		t.Fatalf("expected >= 1 bucket row, got %d", res1.Rows)
	}

	var got struct {
		sessions, messages, tin, tout, tdirect, tsub, calls, dur int64
		cost                                                     float64
	}
	err = db.Pool.QueryRow(ctx, `
		SELECT sessions_count, messages_count, tokens_input, tokens_output, tokens_direct,
			tokens_subagent, model_calls, total_duration_ms, est_cost_usd::float8
		FROM agent_stats_hourly
		WHERE agent_id = $1 AND user_id = $2
	`, agentID, userID).Scan(&got.sessions, &got.messages, &got.tin, &got.tout, &got.tdirect, &got.tsub, &got.calls, &got.dur, &got.cost)
	if err != nil {
		t.Fatalf("read bucket: %v", err)
	}
	if got.sessions != 1 || got.messages != 2 {
		t.Fatalf("sessions/messages: got %d/%d", got.sessions, got.messages)
	}
	if got.tin != 140 || got.tout != 60 || got.tdirect != 14 {
		t.Fatalf("tokens: got in=%d out=%d direct=%d", got.tin, got.tout, got.tdirect)
	}
	if got.tsub != 50 {
		t.Fatalf("subagent tokens: got %d", got.tsub)
	}
	if got.calls != 4 || got.dur != 3000 {
		t.Fatalf("calls/duration: got %d/%d", got.calls, got.dur)
	}
	// cost = (140*0.15 + 60*0.60) / 1e6 = 0.000057 (prices are $ per 1M tokens,
	// the unit the model-config UI uses).
	if got.cost < 0.0000569 || got.cost > 0.0000571 {
		t.Fatalf("cost: got %v", got.cost)
	}

	// Idempotent re-run: same values, still one row for our (agent, user).
	_, err = sa.RollupHourlyStats(ctx, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("re-rollup: %v", err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_stats_hourly WHERE agent_id = $1 AND user_id = $2`,
		agentID, userID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected 1 bucket row after re-run, got %d (%v)", count, err)
	}
	var tin2, tout2 int64
	if err := db.Pool.QueryRow(ctx, `SELECT tokens_input, tokens_output FROM agent_stats_hourly WHERE agent_id = $1 AND user_id = $2`,
		agentID, userID).Scan(&tin2, &tout2); err != nil || tin2 != got.tin || tout2 != got.tout {
		t.Fatalf("re-run must keep values stable, got in=%d out=%d (%v)", tin2, tout2, err)
	}

	// Bucket bounds cover the current in-progress hour. BucketTo is the
	// rollup's own truncation of time.Now(); allow for the rollup having
	// crossed the hour boundary after the seed.
	seedBucket := now.UTC().Truncate(time.Hour)
	if !res1.BucketTo.Equal(seedBucket) && !res1.BucketTo.Equal(seedBucket.Add(time.Hour)) {
		t.Fatalf("bucket_to = %v, want %v (or the following hour)", res1.BucketTo, seedBucket)
	}
}
