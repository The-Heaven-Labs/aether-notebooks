package dashboarddoc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reearth/ygo/crdt"
)

// MergeAndStore merges an incoming document update onto the dashboard's stored
// state and materializes the result, all in one transaction.
//
// incoming is the relay's full state for "dashboard:{id}" (PUT
// /internal/dashboard-yjs/{id}). Merging with ApplyUpdateV1 instead of
// overwriting means a relay holding a slightly stale document cannot clobber a
// backend-originated write: CRDT updates are idempotent and commutative, so
// the stored state always contains both sides. When no state is stored yet,
// merging is just applying incoming (the relay's full state already includes
// everything it loaded).
//
// A dashboard that is missing or trashed (deleted_at IS NOT NULL) is a no-op
// returning nil: no state is written — the FK would fail for a purged
// dashboard — and no derived rows are touched, so a stale relay can never
// resurrect a trashed dashboard.
//
// The dashboard row is locked FOR UPDATE for the duration, which serializes
// concurrent stores for the same dashboard (the state read-modify-write is not
// atomic on its own) and keeps a concurrent purge from deleting the row between
// the existence check and the state upsert.
//
// An undecodable incoming update, or an undecodable stored state, returns an
// error and commits nothing: corrupt bytes fail closed rather than being
// silently replaced, and a corrupt document can never partially materialize.
// Project-level and reference-level problems do not fail the store: bad
// widgets are skipped with warnings (see Materialize), and every warning is
// logged once per store.
func MergeAndStore(ctx context.Context, pool *pgxpool.Pool, dashboardID string, incoming []byte) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("merge dashboard document %s: begin: %w", dashboardID, err)
	}
	defer tx.Rollback(ctx)

	var locked string
	err = tx.QueryRow(ctx,
		`SELECT id FROM dashboards WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		dashboardID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("merge dashboard document %s: lock dashboard: %w", dashboardID, err)
	}

	var stored []byte
	err = tx.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`,
		dashboardID).Scan(&stored)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("merge dashboard document %s: read stored state: %w", dashboardID, err)
	}

	doc, err := decodeDoc(stored)
	if err != nil {
		return fmt.Errorf("merge dashboard document %s: decode stored state: %w", dashboardID, err)
	}
	if err := crdt.ApplyUpdateV1(doc, incoming, nil); err != nil {
		return fmt.Errorf("merge dashboard document %s: apply incoming state: %w", dashboardID, err)
	}
	merged := doc.EncodeStateAsUpdate()

	if _, err := tx.Exec(ctx, `
		INSERT INTO dashboard_yjs_documents (dashboard_id, state, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (dashboard_id) DO UPDATE
		SET state = EXCLUDED.state, updated_at = NOW()`,
		dashboardID, merged); err != nil {
		return fmt.Errorf("merge dashboard document %s: store state: %w", dashboardID, err)
	}

	proj, err := Project(merged)
	if err != nil {
		return fmt.Errorf("merge dashboard document %s: project merged state: %w", dashboardID, err)
	}
	materializeWarnings, err := Materialize(ctx, tx, dashboardID, proj)
	if err != nil {
		return fmt.Errorf("merge dashboard document %s: %w", dashboardID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("merge dashboard document %s: commit: %w", dashboardID, err)
	}

	if warnings := append(append([]string{}, proj.Warnings...), materializeWarnings...); len(warnings) > 0 {
		slog.Warn("dashboard document stored with warnings",
			"dashboard_id", dashboardID,
			"count", len(warnings),
			"warnings", warnings)
	}
	return nil
}
