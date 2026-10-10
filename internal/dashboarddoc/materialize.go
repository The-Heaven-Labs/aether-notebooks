package dashboarddoc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Materialize writes a projection into the derived dashboards/widgets rows.
//
// It is the second half of MergeAndStore and runs inside the caller's
// transaction, so a materialization failure rolls back the document state
// write as well. Postgres is a derived read cache (mirroring cells.source):
// GET/list/CLI/agent reads and validateWidgetLayout all keep reading these
// rows, and trash/purge keep operating on them.
//
// A missing or trashed dashboard is a no-op: the dashboards UPDATE matches no
// row and nothing else is touched, so a stale document can never resurrect a
// trashed dashboard or mutate its rows.
//
// Two guards protect the widget diff from corrupt documents (see the package
// documentation for the Project error/warning split):
//
//   - A projection with no widgets but one or more warnings refuses to
//     materialize. That shape means every widget in the doc was skipped as
//     invalid; treating it as "delete every widget row" would let one corrupt
//     document wipe the dashboard.
//   - An entirely empty projection (no title, settings, variables, widgets, or
//     warnings) refuses to materialize while the dashboard still has widget
//     rows. ygo's typed root getters project a wrong-root-kind document (for
//     example a "meta" root that is actually a Y.Text) as an empty projection
//     without an error, and a legitimate empty dashboard keeps its title and
//     settings, so this cannot trigger on normal use.
//
// The widgets diff upserts every projected widget and deletes the rows whose
// IDs are no longer in the projection, so deleting every widget through
// DeleteWidget materializes as an empty (not refused) dashboard. created_at is
// never written on conflict: existing rows keep it and new rows default NOW().
func Materialize(ctx context.Context, tx pgx.Tx, dashboardID string, proj *Projection) error {
	if proj == nil {
		return fmt.Errorf("materialize dashboard %s: nil projection", dashboardID)
	}

	// dashboards.settings.variables is the storage shape the REST layer reads
	// (models.DashboardSettings.Variables); the doc keeps variables as an
	// ordered Y.Array separate from the settings map.
	settings := make(map[string]any, len(proj.Settings)+1)
	for k, v := range proj.Settings {
		if k == "variables" {
			continue
		}
		settings[k] = v
	}
	variables := proj.Variables
	if variables == nil {
		variables = []map[string]any{}
	}
	settings["variables"] = variables
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("materialize dashboard %s: encode settings: %w", dashboardID, err)
	}

	// The liveness check comes first so a missing or trashed dashboard is a
	// strict no-op even for a projection the guards below would reject.
	tag, err := tx.Exec(ctx,
		`UPDATE dashboards SET title = $2, settings = $3, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		dashboardID, proj.Title, settingsJSON)
	if err != nil {
		return fmt.Errorf("materialize dashboard %s: update dashboard: %w", dashboardID, err)
	}
	if tag.RowsAffected() == 0 {
		// Unknown or trashed dashboard: no-op, never resurrect.
		return nil
	}

	// Guard 1: every widget failed validation. Refuse rather than interpret
	// corruption as "delete every widget row".
	if len(proj.Widgets) == 0 && len(proj.Warnings) > 0 {
		return fmt.Errorf(
			"materialize dashboard %s: refusing to materialize: projection has no widgets and %d warning(s)",
			dashboardID, len(proj.Warnings))
	}

	// Guard 2: entirely empty projection against a dashboard that still has
	// widget rows. Refuse rather than interpret corruption as "delete all".
	if projectionIsEmpty(proj) {
		var rows int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashboardID).Scan(&rows); err != nil {
			return fmt.Errorf("materialize dashboard %s: count widgets: %w", dashboardID, err)
		}
		if rows > 0 {
			return fmt.Errorf(
				"materialize dashboard %s: refusing to materialize: projection is empty but %d widget row(s) exist",
				dashboardID, rows)
		}
	}

	ids := make([]string, 0, len(proj.Widgets))
	for id, w := range proj.Widgets {
		ids = append(ids, id)

		layoutJSON, err := json.Marshal(w.Layout)
		if err != nil {
			return fmt.Errorf("materialize dashboard %s: widget %s: encode layout: %w", dashboardID, id, err)
		}
		config := w.Config
		if config == nil {
			config = map[string]any{}
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("materialize dashboard %s: widget %s: encode config: %w", dashboardID, id, err)
		}

		// created_at is deliberately absent from both sides of the upsert:
		// new rows take the column default (NOW()) and existing rows keep
		// their original timestamp.
		if _, err := tx.Exec(ctx, `
			INSERT INTO widgets
				(id, dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE SET
				dashboard_id = EXCLUDED.dashboard_id,
				notebook_id  = EXCLUDED.notebook_id,
				cell_id      = EXCLUDED.cell_id,
				connector_id = EXCLUDED.connector_id,
				query        = EXCLUDED.query,
				language     = EXCLUDED.language,
				type         = EXCLUDED.type,
				layout       = EXCLUDED.layout,
				config       = EXCLUDED.config,
				updated_at   = NOW()`,
			id, dashboardID, w.NotebookID, w.CellID, w.ConnectorID, w.Query, w.Language, w.Type,
			layoutJSON, configJSON); err != nil {
			return fmt.Errorf("materialize dashboard %s: upsert widget %s: %w", dashboardID, id, err)
		}
	}

	// Delete rows that are no longer in the projection. An empty id list
	// makes the predicate true for every row, which is exactly the
	// legitimate delete-all path.
	if _, err := tx.Exec(ctx,
		`DELETE FROM widgets WHERE dashboard_id = $1 AND id <> ALL($2::uuid[])`,
		dashboardID, ids); err != nil {
		return fmt.Errorf("materialize dashboard %s: delete removed widgets: %w", dashboardID, err)
	}

	return nil
}

// projectionIsEmpty reports whether proj carries no dashboard content at all.
// A legitimate empty dashboard keeps its title and settings, so this is only
// true for a document that failed to project (or was never written).
func projectionIsEmpty(proj *Projection) bool {
	return proj.Title == "" &&
		len(proj.Settings) == 0 &&
		len(proj.Variables) == 0 &&
		len(proj.Widgets) == 0 &&
		len(proj.Warnings) == 0
}
