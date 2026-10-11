package dashboarddoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
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
// A missing or trashed dashboard is a no-op returning (nil, nil): the liveness
// SELECT matches no row and nothing is touched, so a stale document can never
// resurrect a trashed dashboard or mutate its rows.
//
// Two guards protect the derived rows from corrupt documents (see the package
// documentation for the Project error/warning split):
//
//   - A projection with no widgets but one or more widget warnings refuses to
//     materialize unconditionally. That shape means every widget in the doc
//     was skipped as invalid; treating it as "delete every widget row" would
//     let one corrupt document wipe the dashboard.
//   - A widget-less projection over a dashboard that still has content (a
//     non-empty title, non-empty settings, or widget rows) refuses to
//     materialize when the projection is entirely empty, or when it carries
//     any warnings. The empty case covers wrong-root-kind corruption (ygo's
//     typed root getters project, for example, a "meta" root that is actually
//     a Y.Text as an empty projection without an error); the warnings case
//     keeps a settings/variables-only warning from exempting an empty widget
//     set from the wipe protection. On a dashboard with no content the store
//     proceeds, since there is nothing to wipe.
//
// Per-widget isolation: widgets whose connector/notebook/cell references do
// not exist (or are not UUIDs) are skipped with a warning instead of aborting
// the store on an FK violation. References are scoped to the dashboard's org
// and exclude soft-deleted rows: a document can never materialize a widget
// pointing at another org's data or at a trashed connector/notebook, so the
// derived rows can never become a cross-org read surface. A cell widget's
// (notebook_id, cell_id) pair must also resolve to that cell inside the named
// notebook, mirroring the REST path's pair validation; a mismatched pair is
// skipped like any other dangling reference. The diff then deletes the skipped
// widget's derived row, so a widget that is invalid in the document loses its
// materialized row until the document entry is valid again. Likewise, widgets
// that failed Project validation are absent from the projection and their
// rows are removed.
//
// The widgets diff upserts every surviving widget and deletes the rows whose
// IDs are no longer in it, so deleting every widget through DeleteWidget
// materializes as an empty (not refused) dashboard. created_at is never
// written on conflict: existing rows keep it and new rows default NOW().
//
// The returned warnings are the materialization-only ones (skipped dangling
// references); callers should log them together with Projection.Warnings.
func Materialize(ctx context.Context, tx pgx.Tx, dashboardID string, proj *Projection) ([]string, error) {
	if proj == nil {
		return nil, fmt.Errorf("materialize dashboard %s: nil projection", dashboardID)
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
		return nil, fmt.Errorf("materialize dashboard %s: encode settings: %w", dashboardID, err)
	}

	// Liveness (and current content, for guard 2) comes first so a missing or
	// trashed dashboard is a strict no-op even for a projection the guards
	// below would reject. org_id scopes the reference checks below.
	var currentTitle string
	var currentSettings []byte
	var currentWidgets int
	var orgID string
	err = tx.QueryRow(ctx, `
		SELECT d.title, d.settings, d.org_id, (SELECT COUNT(*) FROM widgets w WHERE w.dashboard_id = d.id)
		FROM dashboards d WHERE d.id = $1 AND d.deleted_at IS NULL`,
		dashboardID).Scan(&currentTitle, &currentSettings, &orgID, &currentWidgets)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("materialize dashboard %s: read dashboard: %w", dashboardID, err)
	}

	// Guard 1: every widget failed validation. Refuse rather than interpret
	// corruption as "delete every widget row". Unconditional: a doc whose only
	// widget entries are invalid cannot be materialized even on a dashboard
	// with no content.
	if len(proj.Widgets) == 0 && len(proj.WidgetWarnings) > 0 {
		return nil, fmt.Errorf(
			"materialize dashboard %s: refusing to materialize: projection has no widgets and %d widget warning(s)",
			dashboardID, len(proj.WidgetWarnings))
	}

	// Guard 2: a widget-less projection over a dashboard that still has
	// content must never be interpreted as "wipe everything". Two shapes
	// qualify: an entirely empty projection (wrong-root-kind corruption), and
	// a warning-tainted projection that yielded no widgets. The second shape
	// matters because guard 1 only covers widget-origin warnings — a
	// settings/variables-only warning must not exempt an empty widget set
	// from the wipe protection.
	rowContent := currentWidgets > 0 || currentTitle != "" || settingsHaveContent(currentSettings)
	if rowContent {
		if projectionIsEmpty(proj) {
			var reasons []string
			if currentTitle != "" {
				reasons = append(reasons, "a title")
			}
			if settingsHaveContent(currentSettings) {
				reasons = append(reasons, "settings")
			}
			if currentWidgets > 0 {
				reasons = append(reasons, fmt.Sprintf("%d widget row(s)", currentWidgets))
			}
			return nil, fmt.Errorf(
				"materialize dashboard %s: refusing to materialize: projection is empty but the dashboard still has %s",
				dashboardID, strings.Join(reasons, ", "))
		}
		if len(proj.Widgets) == 0 && len(proj.Warnings) > 0 {
			return nil, fmt.Errorf(
				"materialize dashboard %s: refusing to materialize: projection has no widgets and %d warning(s) while the dashboard still has content",
				dashboardID, len(proj.Warnings))
		}
	}

	tag, err := tx.Exec(ctx,
		`UPDATE dashboards SET title = $2, settings = $3, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		dashboardID, proj.Title, settingsJSON)
	if err != nil {
		return nil, fmt.Errorf("materialize dashboard %s: update dashboard: %w", dashboardID, err)
	}
	if tag.RowsAffected() == 0 {
		// Trashed between the liveness read and the update: no-op.
		return nil, nil
	}

	existing, cellNotebooks, err := widgetReferencesExist(ctx, tx, orgID, proj.Widgets)
	if err != nil {
		return nil, fmt.Errorf("materialize dashboard %s: validate widget references: %w", dashboardID, err)
	}

	// Sorted iteration keeps warnings (and upserts) deterministic, matching
	// Project's sorted warning order.
	widgetIDs := make([]string, 0, len(proj.Widgets))
	for id := range proj.Widgets {
		widgetIDs = append(widgetIDs, id)
	}
	sort.Strings(widgetIDs)

	var warnings []string
	ids := make([]string, 0, len(widgetIDs))
	for _, id := range widgetIDs {
		w := proj.Widgets[id]
		if missing := danglingWidgetReferences(w, existing, cellNotebooks); len(missing) > 0 {
			warnings = append(warnings, fmt.Sprintf("widget %s: %s", id, strings.Join(missing, "; ")))
			continue
		}
		ids = append(ids, id)

		layoutJSON, err := json.Marshal(w.Layout)
		if err != nil {
			return nil, fmt.Errorf("materialize dashboard %s: widget %s: encode layout: %w", dashboardID, id, err)
		}
		config := w.Config
		if config == nil {
			config = map[string]any{}
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("materialize dashboard %s: widget %s: encode config: %w", dashboardID, id, err)
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
			return nil, fmt.Errorf("materialize dashboard %s: upsert widget %s: %w", dashboardID, id, err)
		}
	}

	// Delete rows that are no longer in the surviving set: widgets skipped by
	// Project validation or by dangling references included. An empty id list
	// makes the predicate true for every row, which is exactly the legitimate
	// delete-all path.
	if _, err := tx.Exec(ctx,
		`DELETE FROM widgets WHERE dashboard_id = $1 AND id <> ALL($2::uuid[])`,
		dashboardID, ids); err != nil {
		return nil, fmt.Errorf("materialize dashboard %s: delete removed widgets: %w", dashboardID, err)
	}

	return warnings, nil
}

// widgetReferencesExist returns the set of existing (kind, canonical UUID)
// pairs among the widgets' connector/notebook/cell references, in one query,
// plus the canonical notebook UUID of each existing cell. References are
// scoped to the dashboard's org and exclude soft-deleted connectors and
// notebooks, so the materializer can never write a derived row that points at
// another org's data or at a trashed row (which the REST read paths would
// otherwise serve). References that do not parse as UUIDs are never queried
// and therefore never reported as existing.
func widgetReferencesExist(ctx context.Context, tx pgx.Tx, orgID string, widgets map[string]WidgetDoc) (map[string]bool, map[string]string, error) {
	connectorIDs := map[string]bool{}
	notebookIDs := map[string]bool{}
	cellIDs := map[string]bool{}
	collect := func(dst map[string]bool, ref *string) {
		if ref == nil {
			return
		}
		if u, err := uuid.Parse(*ref); err == nil {
			dst[u.String()] = true
		}
	}
	for _, w := range widgets {
		collect(connectorIDs, w.ConnectorID)
		collect(notebookIDs, w.NotebookID)
		collect(cellIDs, w.CellID)
	}

	existing := make(map[string]bool, len(connectorIDs)+len(notebookIDs)+len(cellIDs))
	cellNotebooks := make(map[string]string, len(cellIDs))
	if len(connectorIDs) == 0 && len(notebookIDs) == 0 && len(cellIDs) == 0 {
		return existing, cellNotebooks, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT 'connector', id::text, NULL::text FROM connectors
		 WHERE id = ANY($1::uuid[]) AND org_id = $4 AND deleted_at IS NULL
		UNION ALL
		SELECT 'notebook', id::text, NULL::text FROM notebooks
		 WHERE id = ANY($2::uuid[]) AND org_id = $4 AND deleted_at IS NULL
		UNION ALL
		SELECT 'cell', c.id::text, c.notebook_id::text FROM cells c
		 JOIN notebooks n ON n.id = c.notebook_id
		 WHERE c.id = ANY($3::uuid[]) AND n.org_id = $4 AND n.deleted_at IS NULL`,
		uuidKeys(connectorIDs), uuidKeys(notebookIDs), uuidKeys(cellIDs), orgID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		var cellNotebook *string
		if err := rows.Scan(&kind, &id, &cellNotebook); err != nil {
			return nil, nil, err
		}
		existing[refKey(kind, id)] = true
		if kind == "cell" && cellNotebook != nil {
			cellNotebooks[id] = *cellNotebook
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return existing, cellNotebooks, nil
}

// danglingWidgetReferences returns the human-readable list of a widget's
// references that are not valid UUIDs, do not exist, or are not accessible
// from the dashboard's org (missing, another org's, or soft-deleted rows all
// count as "does not exist"). cellNotebooks maps each existing cell to its
// canonical notebook UUID, so a cell widget's (notebook_id, cell_id) pair can
// be required to resolve to that cell inside the named notebook, matching the
// REST path's pair validation.
func danglingWidgetReferences(w WidgetDoc, existing map[string]bool, cellNotebooks map[string]string) []string {
	var missing []string
	check := func(kind string, ref *string) {
		if ref == nil {
			return
		}
		u, err := uuid.Parse(*ref)
		if err != nil {
			missing = append(missing, fmt.Sprintf("%s_id %q is not a valid UUID", kind, *ref))
			return
		}
		if !existing[refKey(kind, u.String())] {
			missing = append(missing, fmt.Sprintf("%s_id %s does not exist", kind, u.String()))
		}
	}
	check("connector", w.ConnectorID)
	check("notebook", w.NotebookID)
	check("cell", w.CellID)

	if w.CellID != nil && w.NotebookID != nil {
		cellUUID, cellErr := uuid.Parse(*w.CellID)
		notebookUUID, notebookErr := uuid.Parse(*w.NotebookID)
		if cellErr == nil && notebookErr == nil {
			if got, ok := cellNotebooks[cellUUID.String()]; ok && got != notebookUUID.String() {
				missing = append(missing, fmt.Sprintf(
					"cell_id %s does not belong to notebook_id %s", cellUUID.String(), notebookUUID.String()))
			}
		}
	}
	return missing
}

func refKey(kind, id string) string { return kind + "\x00" + id }

func uuidKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
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

// settingsHaveContent reports whether a stored dashboards.settings JSONB value
// holds anything beyond an empty variables array, so guard 2 can treat
// {"variables": []} — the shape Materialize itself writes for a variables-less
// document — as empty.
func settingsHaveContent(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		// Unreadable settings still count as content: refuse rather than wipe.
		return true
	}
	for k, v := range settings {
		if k == "variables" {
			if arr, ok := v.([]any); ok && len(arr) == 0 {
				continue
			}
		}
		return true
	}
	return false
}
