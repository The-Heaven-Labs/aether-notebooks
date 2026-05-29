package scheduler

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/heavenlabs/hnb/internal/database"
	"github.com/robfig/cron/v3"
)

type RunFunc func(ctx context.Context, notebookID string, params map[string]string) error

type Scheduler struct {
	db      *database.DB
	runFunc RunFunc
	stop    chan struct{}
}

func New(db *database.DB, runFunc RunFunc) *Scheduler {
	return &Scheduler{db: db, runFunc: runFunc, stop: make(chan struct{})}
}

func (s *Scheduler) Start() {
	go s.loop()
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	ctx := context.Background()

	now := time.Now()
	if now.Hour() == 0 && now.Minute() == 0 {
		s.runAgentStatsRollup(ctx)
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, cron_expression, parameter_overrides
		 FROM schedules WHERE enabled = TRUE AND next_run_at <= NOW()`)
	if err != nil {
		log.Printf("scheduler: query: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, nbID, cronExpr string
		var paramsJSON []byte
		if err := rows.Scan(&id, &nbID, &cronExpr, &paramsJSON); err != nil {
			log.Printf("scheduler: scan: %v", err)
			continue
		}

		var params map[string]string
		json.Unmarshal(paramsJSON, &params)

		if err := s.runFunc(ctx, nbID, params); err != nil {
			log.Printf("scheduler: run notebook %s: %v", nbID, err)
		}

		next, _ := NextRun(cronExpr)
		s.db.Pool.Exec(ctx,
			`UPDATE schedules SET last_run_at = NOW(), next_run_at = $1, updated_at = NOW() WHERE id = $2`,
			next, id,
		)
	}
}

func NextRun(cronExpr string) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(time.Now()), nil
}

func (s *Scheduler) runAgentStatsRollup(ctx context.Context) {
	yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)

	_, err := s.db.Pool.Exec(ctx, `
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
	if err != nil {
		log.Printf("scheduler: agent stats rollup: %v", err)
	}
}
