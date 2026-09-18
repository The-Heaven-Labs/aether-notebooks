package agent

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type StatsAggregator struct {
	pool *pgxpool.Pool
}

func NewStatsAggregator(pool *pgxpool.Pool) *StatsAggregator {
	return &StatsAggregator{pool: pool}
}

// RollupHourlyStatsResult summarizes one hourly-rollup run.
type RollupHourlyStatsResult struct {
	BucketFrom time.Time `json:"bucket_from"`
	BucketTo   time.Time `json:"bucket_to"`
	Rows       int64     `json:"rows"`
}

// RollupHourlyStats upserts per-hour usage buckets for all activity since the
// given time. Each run covers the previous completed hour and the current
// in-progress hour (and anything newer), so in-progress buckets self-correct
// until the hour closes and repeat runs are idempotent.
//
// Sources: agent_messages (input/output/direct tokens, model calls, duration,
// sessions) joined through agent_sessions, plus subagent_tasks (subagent
// tokens, attributed to the triggering user). Cost uses the agent's CURRENT
// model-config prices at rollup time — per-message price snapshots are out of
// scope. Prices are dollars per 1M tokens (the unit the model-config UI and
// the session token panel use), so token counts are divided by 1e6.
// agent_stats_daily is left untouched for history.
func (sa *StatsAggregator) RollupHourlyStats(ctx context.Context, since time.Time) (*RollupHourlyStatsResult, error) {
	tag, err := sa.pool.Exec(ctx, `
		WITH msg AS (
			SELECT date_trunc('hour', m.created_at) AS bucket,
				s.agent_id AS agent_id,
				s.user_id AS user_id,
				COUNT(*) AS messages,
				COUNT(DISTINCT s.id) AS sessions,
				COALESCE(SUM(m.tokens_input), 0) AS tin,
				COALESCE(SUM(m.tokens_output), 0) AS tout,
				COALESCE(SUM(m.tokens_direct), 0) AS tdirect,
				COALESCE(SUM(m.model_calls), 0) AS calls,
				COALESCE(SUM(m.duration_ms), 0) AS dur
			FROM agent_messages m
			JOIN agent_sessions s ON s.id = m.session_id
			WHERE m.created_at >= $1
			GROUP BY 1, 2, 3
		),
		sub AS (
			SELECT date_trunc('hour', COALESCE(st.completed_at, st.created_at)) AS bucket,
				s.agent_id AS agent_id,
				s.user_id AS user_id,
				COALESCE(SUM(st.tokens_input), 0) + COALESCE(SUM(st.tokens_output), 0) AS tsub
			FROM subagent_tasks st
			JOIN agent_sessions s ON s.id = st.parent_session_id
			WHERE COALESCE(st.completed_at, st.created_at) >= $1
			GROUP BY 1, 2, 3
		),
		price AS (
			SELECT a.id AS agent_id,
				COALESCE(mc.price_per_input_token, 0) AS pin,
				COALESCE(mc.price_per_output_token, 0) AS pout
			FROM agents a
			LEFT JOIN model_configs mc ON mc.id = a.model_config_id
		)
		INSERT INTO agent_stats_hourly
			(bucket_start, agent_id, user_id, sessions_count, messages_count,
			 tokens_input, tokens_output, tokens_direct, tokens_subagent,
			 model_calls, total_duration_ms, est_cost_usd)
		SELECT
			b.bucket, b.agent_id, b.user_id,
			COALESCE(m.sessions, 0), COALESCE(m.messages, 0),
			COALESCE(m.tin, 0), COALESCE(m.tout, 0), COALESCE(m.tdirect, 0),
			COALESCE(s.tsub, 0), COALESCE(m.calls, 0), COALESCE(m.dur, 0),
			(COALESCE(m.tin, 0) * p.pin + COALESCE(m.tout, 0) * p.pout) / 1000000.0
		FROM (
			SELECT bucket, agent_id, user_id FROM msg
			UNION
			SELECT bucket, agent_id, user_id FROM sub
		) b
		LEFT JOIN msg m USING (bucket, agent_id, user_id)
		LEFT JOIN sub s USING (bucket, agent_id, user_id)
		LEFT JOIN price p ON p.agent_id = b.agent_id
		ON CONFLICT (bucket_start, agent_id, user_id) DO UPDATE SET
			sessions_count = EXCLUDED.sessions_count,
			messages_count = EXCLUDED.messages_count,
			tokens_input = EXCLUDED.tokens_input,
			tokens_output = EXCLUDED.tokens_output,
			tokens_direct = EXCLUDED.tokens_direct,
			tokens_subagent = EXCLUDED.tokens_subagent,
			model_calls = EXCLUDED.model_calls,
			total_duration_ms = EXCLUDED.total_duration_ms,
			est_cost_usd = EXCLUDED.est_cost_usd
	`, since)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &RollupHourlyStatsResult{
		BucketFrom: since.UTC().Truncate(time.Hour),
		BucketTo:   now.Truncate(time.Hour),
		Rows:       tag.RowsAffected(),
	}, nil
}
