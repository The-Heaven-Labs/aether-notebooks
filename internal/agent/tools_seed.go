package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type BuiltinToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
	HandlerName string
	// Rev is the description/schema revision of this builtin. Seeding upserts
	// the stored row when its seed_rev is older, so changed descriptions and
	// parameters reach existing orgs. Bump the entry (or builtinSeedRev for
	// the whole catalog) when a description or schema changes.
	Rev int
}

// builtinSeedRev is the current revision of the builtin catalog: every entry
// below carries it. Builtin descriptions are platform-managed — a user edit
// to a builtin's text is overwritten when the catalog revision advances.
const builtinSeedRev = 3

var BuiltinTools = []BuiltinToolDef{
	{Name: "notebook_create_notebook", Description: "Create a new notebook", HandlerName: "create_notebook", Rev: builtinSeedRev},
	{Name: "notebook_delete_notebook", Description: "Delete a notebook", HandlerName: "delete_notebook", Rev: builtinSeedRev},
	{Name: "notebook_update_notebook", Description: "Update a notebook", HandlerName: "update_notebook", Rev: builtinSeedRev},
	{Name: "notebook_read_cells", Description: "Read cells from a notebook", HandlerName: "read_cell", Rev: builtinSeedRev},
	{Name: "notebook_create_cell", Description: "Create a new cell (optionally run it immediately with run=true)", HandlerName: "create_cell", Rev: builtinSeedRev},
	{Name: "notebook_update_cell", Description: "Update an existing cell", HandlerName: "update_cell", Rev: builtinSeedRev},
	{Name: "notebook_delete_cell", Description: "Delete a cell", HandlerName: "delete_cell", Rev: builtinSeedRev},
	{Name: "notebook_run_cell", Description: "Run a code cell and get inline results", HandlerName: "run_cell", Rev: builtinSeedRev},
	{Name: "notebook_list_cells", Description: "List all cells in a notebook", HandlerName: "list_cells", Rev: builtinSeedRev},
	{Name: "execute_sql", Description: "Run ad-hoc SQL queries (default 30s timeout; timeout_ms raises it up to the server ceiling — use create_cell with run=true for long queries)", HandlerName: "execute_sql", Rev: builtinSeedRev},
	{Name: "create_dashboard", Description: "Create a new dashboard — an App surface with chapters, filters, and widgets. Returns the dashboard ID.", HandlerName: "create_dashboard", Rev: builtinSeedRev},
	{Name: "list_dashboards", Description: "List dashboards", HandlerName: "list_dashboards", Rev: builtinSeedRev},
	{Name: "delete_dashboard", Description: "Delete a dashboard", HandlerName: "delete_dashboard", Rev: builtinSeedRev},
	{Name: "get_dashboard", Description: "Get a dashboard with its settings (app, chapters/tabs, variables) and widgets. Read this before changing a dashboard.", HandlerName: "get_dashboard", Rev: builtinSeedRev},
	{Name: "create_dashboard_widget", Description: "Add a widget to a dashboard. A widget reads either a notebook cell (notebook_id + cell_id) or a query (connector_id + query); typed chart/metric/table blocks (or raw config) create a fully-configured widget in one call. An html widget (type 'html') is source-less and takes the html block — a script-free HTML/CSS panel. For sandboxed custom components (HTML/CSS/JS + data bindings), use create_custom_widget.", HandlerName: "create_dashboard_widget", Rev: builtinSeedRev},
	{Name: "create_custom_widget", Description: "Add a sandboxed custom component (HTML/CSS/JS + data bindings) to a dashboard, behind the org's custom-code switch. Source-less; bind existing dashboard widgets by id to feed it data through the bridge.", HandlerName: "create_custom_widget", Rev: builtinSeedRev},
	{Name: "update_dashboard_widget", Description: "Update a dashboard widget: position (row, col), size (width, height), type (chart/table/metric/text/html/custom), tab (App chapter), or raw config keys to shallow-merge. For typed chart/metric/table/html/custom config, use set_widget_config.", HandlerName: "update_dashboard_widget", Rev: builtinSeedRev},
	{Name: "set_widget_config", Description: "Set a dashboard widget's typed render config. chart uses create_chart's vocabulary (chart_type, x_column, y_columns, group_by, bar_mode, series_colors, mark_lines, …) and stores the converted chart config; metric sets the metric widget's value_column/aggregation/label/format/comparison/sparkline; table sets per-column number/currency/percent formats (columns: [{name, format}]); html sets an html panel's markup/stylesheet (the widget's type must already be html); custom sets a custom component's bundle (html/css/js) and bindings (the widget's type must already be custom; requires custom_code_enabled). Each block replaces its config section; unrelated keys stay.", HandlerName: "set_widget_config", Rev: builtinSeedRev},
	{Name: "delete_dashboard_widget", Description: "Remove a widget from a dashboard.", HandlerName: "delete_dashboard_widget", Rev: builtinSeedRev},
	{Name: "update_dashboard", Description: "Update a dashboard's title or grid density (grid_cols, default 12). Widget placement lives in set_dashboard_layout.", HandlerName: "update_dashboard", Rev: builtinSeedRev},
	{Name: "update_dashboard_settings", Description: "Update a dashboard's App settings: title, app (title/subtitle/width/show toggles/filter_scope/theme with hex colors), and tabs (App chapters, e.g. [{id, title}]).", HandlerName: "update_dashboard_settings", Rev: builtinSeedRev},
	{Name: "set_dashboard_layout", Description: "Arrange a dashboard's App surface: set tabs, place widgets into tabs (placements), and merge widget appearance (background/padding/align/border).", HandlerName: "set_dashboard_layout", Rev: builtinSeedRev},
	{Name: "publish_dashboard", Description: "Publish the dashboard's current draft as a new immutable version. Viewers see the latest published version until the next publish; review_dashboard lists anything blocking a publish.", HandlerName: "publish_dashboard", Rev: builtinSeedRev},
	{Name: "review_dashboard", Description: "Review a dashboard's App composition (layout, content, surface): returns prioritized findings naming the widget/chapter and the tool call that fixes each, plus the app view/draft-preview links and the public link when the dashboard is shared.", HandlerName: "review_dashboard", Rev: builtinSeedRev},
	{Name: "dashboard_guide", Description: "Read the App Builder guide: widget configs (metric/text/table/chart), chapters and filter scoping, theming tokens and guardrails, publish/version semantics, troubleshooting, and composition guidance. No arguments.", HandlerName: "dashboard_guide", Rev: builtinSeedRev},
	{Name: "share_dashboard", Description: "Generate a public shareable link for a dashboard. Returns the public token and the full URL.", HandlerName: "share_dashboard", Rev: builtinSeedRev},
	{Name: "create_schedule", Description: "Schedule a notebook", HandlerName: "create_schedule", Rev: builtinSeedRev},
	{Name: "delete_schedule", Description: "Delete a schedule", HandlerName: "delete_schedule", Rev: builtinSeedRev},
	{Name: "read_permissions", Description: "Read ACL entries", HandlerName: "read_permissions", Rev: builtinSeedRev},
	{Name: "update_permissions", Description: "Update ACL entries", HandlerName: "update_permissions", Rev: builtinSeedRev},
	{Name: "export_notebook", Description: "Export notebook as ipynb", HandlerName: "export_notebook", Rev: builtinSeedRev},
	{Name: "import_notebook", Description: "Import notebook from ipynb", HandlerName: "import_notebook", Rev: builtinSeedRev},
	{Name: "get_notebook_context", Description: "Get full notebook context", HandlerName: "get_notebook_context", Rev: builtinSeedRev},
	{Name: "list_skills", Description: "List available skills", HandlerName: "list_skills", Rev: builtinSeedRev},
	{Name: "load_skill", Description: "Load a skill's full instructions", HandlerName: "load_skill", Rev: builtinSeedRev},
	{Name: "update_agent", Description: "Update agent configuration", HandlerName: "update_agent", Rev: builtinSeedRev},
	{Name: "create_skill", Description: "Create a new skill", HandlerName: "create_skill", Rev: builtinSeedRev},
	{Name: "update_skill", Description: "Update an existing skill", HandlerName: "update_skill", Rev: builtinSeedRev},
	{Name: "spawn_subagents", Description: "Spawn parallel subagents", HandlerName: "spawn_subagents", Rev: builtinSeedRev},
	{Name: "create_tasks", Description: "Create tasks for subagents", HandlerName: "create_tasks", Rev: builtinSeedRev},
	{Name: "update_task", Description: "Update a task's status", HandlerName: "update_task", Rev: builtinSeedRev},
	{Name: "get_tasks", Description: "Get current tasks", HandlerName: "get_tasks", Rev: builtinSeedRev},
	{Name: "create_chart", Description: "Create a chart from cell output. Types: bar, line, area, scatter, pie, donut, timeline, hierarchy_tree, big_number, map, sankey, funnel, heatmap, histogram. Bar supports bar_mode (grouped|stacked|horizontal) layout. Area supports area_mode (area|stacked) layout. group_by splits data into series (not for pie/donut/big_number/funnel/heatmap/histogram). For heatmap use x_column, y_axis_column, value_column. For funnel use category_column, value_column. For histogram use value_column, bin_count.", HandlerName: "create_chart", Rev: builtinSeedRev},
	{Name: "update_chart", Description: "Update an existing chart's config. Supports same types and params as create_chart. For bar: bar_mode for layout. For area: area_mode for layout. For heatmap: x_column, y_axis_column, value_column. For funnel: category_column, value_column, funnel_sort. For histogram: value_column, bin_count.", HandlerName: "update_chart", Rev: builtinSeedRev},
	{Name: "list_notebooks", Description: "List notebooks", HandlerName: "list_notebooks", Rev: builtinSeedRev},
	{Name: "list_connectors", Description: "List connectors", HandlerName: "list_connectors", Rev: builtinSeedRev},
	{Name: "list_folders", Description: "List folders", HandlerName: "list_folders", Rev: builtinSeedRev},
	{Name: "get_folder_tree", Description: "Get folder tree", HandlerName: "get_folder_tree", Rev: builtinSeedRev},
	{Name: "create_snapshot", Description: "Create a notebook snapshot", HandlerName: "create_snapshot", Rev: builtinSeedRev},
	{Name: "list_snapshots", Description: "List notebook snapshots", HandlerName: "list_snapshots", Rev: builtinSeedRev},
	{Name: "restore_snapshot", Description: "Restore a notebook snapshot", HandlerName: "restore_snapshot", Rev: builtinSeedRev},
	{Name: "list_notebook_parameters", Description: "List a notebook's parameter definitions", HandlerName: "list_notebook_parameters", Rev: builtinSeedRev},
	{Name: "set_notebook_parameters", Description: "Replace a notebook's parameter definitions", HandlerName: "set_notebook_parameters", Rev: builtinSeedRev},
	{Name: "ask_question", Description: "Ask the user a question and wait for their response. Use this when you need a decision, clarification, or input from the user to proceed. You can provide multiple choice options for the user to pick from, or leave it open-ended.", HandlerName: "ask_question", Rev: builtinSeedRev, Schema: map[string]any{"type": "object", "properties": map[string]any{"question": map[string]any{"type": "string", "description": "The question to ask the user"}, "options": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string", "description": "The option title/label"}, "description": map[string]any{"type": "string", "description": "Optional subtitle or explanation"}}, "required": []any{"title"}}, "description": "Optional multiple choice options"}, "allow_custom": map[string]any{"type": "boolean", "description": "Whether to allow the user to type a custom answer (default: true)"}}, "required": []any{"question"}}},
}

// builtinCatalogValues renders the catalog as a SQL VALUES list for the bulk
// upserts: one tuple per builtin, with the schema/config JSON already encoded.
// offset is the number of leading parameters the caller binds before the
// catalog args ($1..$offset), so the tuple placeholders continue from there.
func builtinCatalogValues(offset int) (placeholders []string, args []any) {
	const perTuple = 5
	for i, bt := range BuiltinTools {
		schema := bt.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		schemaJSON, _ := json.Marshal(schema)
		config, _ := json.Marshal(map[string]string{"handler_name": bt.HandlerName})
		base := offset + i*perTuple
		placeholders = append(placeholders, fmt.Sprintf(
			"($%d::text, $%d::text, $%d::jsonb, $%d::jsonb, $%d::int)",
			base+1, base+2, base+3, base+4, base+5))
		args = append(args, bt.Name, bt.Description, string(schemaJSON), string(config), bt.Rev)
	}
	return placeholders, args
}

func SeedBuiltinTools(ctx context.Context, pool *pgxpool.Pool, orgID string) {
	var systemUserID string
	err := pool.QueryRow(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 LIMIT 1`, orgID).Scan(&systemUserID)
	if err != nil {
		return
	}

	// Fast path: every builtin row exists and none is older than the catalog.
	// Two conditions matter — missing rows (a pre-tools org) and stale rows
	// (seeded before a catalog revision bump).
	maxRev := 0
	for _, bt := range BuiltinTools {
		if bt.Rev > maxRev {
			maxRev = bt.Rev
		}
	}
	var count, stale int
	pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE seed_rev < $2)
		FROM tools WHERE org_id = $1 AND type = 'builtin'`, orgID, maxRev).Scan(&count, &stale)
	if count >= len(BuiltinTools) && stale == 0 {
		return
	}

	// One bulk statement per condition (not one per tool): a large dev
	// database can carry tens of thousands of orgs, and per-tool round trips
	// turned startup seeding into millions of queries.
	refreshPlaceholders, refreshArgs := builtinCatalogValues(1)
	insertPlaceholders, insertArgs := builtinCatalogValues(2)

	// Refresh stale rows: builtin descriptions and parameters are
	// platform-managed, so a row older than the catalog's revision is
	// overwritten (a user edit to a builtin is replaced on the next bump).
	if stale > 0 {
		if _, err := pool.Exec(ctx, `
			UPDATE tools SET description = v.description, schema = v.schema, config = v.config,
			                 seed_rev = v.seed_rev, updated_at = NOW()
			FROM (VALUES `+strings.Join(refreshPlaceholders, ",")+`) AS v(name, description, schema, config, seed_rev)
			WHERE tools.org_id = $1 AND tools.type = 'builtin'
			  AND tools.name = v.name AND tools.seed_rev < v.seed_rev`,
			append([]any{orgID}, refreshArgs...)...); err != nil {
			slog.Warn("refresh builtin tools failed", "org", orgID, "error", err)
		}
	}

	// Insert missing rows (a fresh org, or an org seeded before a tool
	// existed), then seed the management ACLs for any builtin row lacking
	// them: all org members can view tools (UI listing); 'use' is not needed
	// — assignment is gated by validateToolAccess, which checks user-level or
	// org-role 'use' permission. Built-ins default to view-only for
	// 'everyone'; admins get full management.
	if count < len(BuiltinTools) {
		if _, err := pool.Exec(ctx, `
			INSERT INTO tools (id, org_id, name, description, type, schema, config, seed_rev, created_by, created_at, updated_at)
			SELECT gen_random_uuid(), $1::uuid, v.name, v.description, 'builtin', v.schema, v.config, v.seed_rev, $2::uuid, NOW(), NOW()
			FROM (VALUES `+strings.Join(insertPlaceholders, ",")+`) AS v(name, description, schema, config, seed_rev)
			ON CONFLICT (org_id, name) DO NOTHING`,
			append([]any{orgID, systemUserID}, insertArgs...)...); err != nil {
			slog.Warn("seed builtin tools failed", "org", orgID, "error", err)
			return
		}
		pool.Exec(ctx, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			SELECT t.org_id, 'tool', t.id, 'org_role', 'everyone', ARRAY['view']
			FROM tools t WHERE t.org_id = $1 AND t.type = 'builtin'
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`, orgID)
		pool.Exec(ctx, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			SELECT t.org_id, 'tool', t.id, 'org_role', 'admin', ARRAY['view','use','edit','delete']
			FROM tools t WHERE t.org_id = $1 AND t.type = 'builtin'
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`, orgID)
	}
}
