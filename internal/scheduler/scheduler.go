// Package scheduler provides cron-based notebook scheduling for automated notebook execution.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/database"
)

type RunFunc func(ctx context.Context, notebookID string, params map[string]string) error

type Scheduler struct {
	db            *database.DB
	runFunc       RunFunc
	stop          chan struct{}
	statsInterval time.Duration
}

func New(db *database.DB, runFunc RunFunc) *Scheduler {
	return &Scheduler{db: db, runFunc: runFunc, stop: make(chan struct{})}
}

// SetStatsRollupInterval configures how often agent usage is rolled up into
// hourly buckets. Non-positive means the 1h default. (Env parsing in
// internal/config floors user input at 5m; the setter takes values as-is so
// tests can use short intervals.)
func (s *Scheduler) SetStatsRollupInterval(d time.Duration) {
	s.statsInterval = d
}

func (s *Scheduler) rollupInterval() time.Duration {
	if s.statsInterval <= 0 {
		return time.Hour
	}
	return s.statsInterval
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
	rollupTicker := time.NewTicker(s.rollupInterval())
	defer rollupTicker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tick()
		case <-rollupTicker.C:
			s.rollupStats()
		}
	}
}

func (s *Scheduler) tick() {
	ctx := context.Background()

	now := time.Now()
	if now.Hour() == 0 && now.Minute() == 0 {
		lockID := int64(989899)
		_, err := s.db.Pool.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID)
		if err == nil {
			s.purgeTrash(ctx)
			s.purgeAuditLogs(ctx)
			s.db.Pool.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)
		}
	}

	for {
		var id, nbID, cronExpr string
		var paramsJSON []byte
		err := s.db.Pool.QueryRow(ctx,
			`UPDATE schedules AS s
			 SET last_run_at = NOW(), next_run_at = GREATEST(NOW(), s.next_run_at + INTERVAL '1 minute'), updated_at = NOW()
			 WHERE s.id = (
				 SELECT s2.id FROM schedules s2
				 WHERE s2.enabled AND s2.next_run_at <= NOW()
				 ORDER BY s2.next_run_at
				 LIMIT 1
				 FOR UPDATE SKIP LOCKED
			 )
			 RETURNING s.id, s.notebook_id, s.cron_expression, s.parameter_overrides`,
		).Scan(&id, &nbID, &cronExpr, &paramsJSON)
		if err != nil {
			if err == pgx.ErrNoRows {
				break
			}
			slog.Warn("scheduler: claim", "error", err)
			break
		}

		var params map[string]string
		json.Unmarshal(paramsJSON, &params)

		if err := s.runFunc(ctx, nbID, params); err != nil {
			slog.Warn("scheduler: run notebook", "notebook_id", nbID, "error", err)
		}
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

func (s *Scheduler) rollupStats() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Two-interval overlap: covers the previous completed hour and the current
	// in-progress hour, healing a missed tick. Non-blocking lock so only one
	// pod rolls up at a time.
	var locked bool
	if err := s.db.Pool.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", int64(989900)).Scan(&locked); err != nil || !locked {
		return
	}
	defer s.db.Pool.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(989900))

	since := time.Now().Add(-2 * s.rollupInterval())
	if _, err := agent.NewStatsAggregator(s.db.Pool).RollupHourlyStats(ctx, since); err != nil {
		slog.Warn("scheduler: agent stats rollup", "error", err)
	}
}

func (s *Scheduler) purgeTrash(ctx context.Context) {
	tables := []string{"notebooks", "connectors", "dashboards", "folders"}
	for _, table := range tables {
		_, err := s.db.Pool.Exec(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '7 days'`, table),
		)
		if err != nil {
			slog.Warn("scheduler: purge trash", "table", table, "error", err)
		}
	}
}

func (s *Scheduler) purgeAuditLogs(ctx context.Context) {
	_, err := s.db.Pool.Exec(ctx, `
		DELETE FROM audit_logs a
		USING orgs o
		WHERE a.org_id = o.id
		AND a.created_at < NOW() - (
			COALESCE((o.settings->>'audit_retention_days')::int, 7)
		) * INTERVAL '1 day'
	`)
	if err != nil {
		slog.Warn("scheduler: purge audit logs (by org retention)", "error", err)
	}
}
