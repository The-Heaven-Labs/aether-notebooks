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

func (sa *StatsAggregator) RollupDailyStats(ctx context.Context) error {
	yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)

	_, err := sa.pool.Exec(ctx, `
		INSERT INTO agent_stats_daily (date, agent_id, user_id, sessions_count, messages_count, tokens_input, tokens_output)
		SELECT
			$1 as date,
			s.agent_id,
			s.user_id,
			COUNT(DISTINCT s.id) as sessions_count,
			COUNT(m.id) as messages_count,
			COALESCE(SUM(m.tokens_input), 0) as tokens_input,
			COALESCE(SUM(m.tokens_output), 0) as tokens_output
		FROM agent_sessions s
		LEFT JOIN agent_messages m ON m.session_id = s.id
		WHERE s.created_at::date = $1::date
		GROUP BY s.agent_id, s.user_id
		ON CONFLICT (date, agent_id, user_id) DO UPDATE SET
			sessions_count = EXCLUDED.sessions_count,
			messages_count = EXCLUDED.messages_count,
			tokens_input = EXCLUDED.tokens_input,
			tokens_output = EXCLUDED.tokens_output
	`, yesterday)
	return err
}
