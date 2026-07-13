package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BuiltinToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
	HandlerName string
}

var BuiltinTools = []BuiltinToolDef{
	{Name: "notebook_create_notebook", Description: "Create a new notebook", HandlerName: "create_notebook"},
	{Name: "notebook_delete_notebook", Description: "Delete a notebook", HandlerName: "delete_notebook"},
	{Name: "notebook_update_notebook", Description: "Update a notebook", HandlerName: "update_notebook"},
	{Name: "notebook_read_cells", Description: "Read cells from a notebook", HandlerName: "read_cell"},
	{Name: "notebook_create_cell", Description: "Create a new cell", HandlerName: "create_cell"},
	{Name: "notebook_update_cell", Description: "Update an existing cell", HandlerName: "update_cell"},
	{Name: "notebook_delete_cell", Description: "Delete a cell", HandlerName: "delete_cell"},
	{Name: "notebook_run_cell", Description: "Run a code cell", HandlerName: "run_cell"},
	{Name: "notebook_list_cells", Description: "List all cells in a notebook", HandlerName: "list_cells"},
	{Name: "execute_sql", Description: "Run ad-hoc SQL queries (30s timeout — use create_cell + run_cell for long queries)", HandlerName: "execute_sql"},
	{Name: "create_dashboard", Description: "Create a dashboard", HandlerName: "create_dashboard"},
	{Name: "list_dashboards", Description: "List dashboards", HandlerName: "list_dashboards"},
	{Name: "delete_dashboard", Description: "Delete a dashboard", HandlerName: "delete_dashboard"},
	{Name: "create_schedule", Description: "Schedule a notebook", HandlerName: "create_schedule"},
	{Name: "delete_schedule", Description: "Delete a schedule", HandlerName: "delete_schedule"},
	{Name: "read_permissions", Description: "Read ACL entries", HandlerName: "read_permissions"},
	{Name: "update_permissions", Description: "Update ACL entries", HandlerName: "update_permissions"},
	{Name: "export_notebook", Description: "Export notebook as ipynb", HandlerName: "export_notebook"},
	{Name: "import_notebook", Description: "Import notebook from ipynb", HandlerName: "import_notebook"},
	{Name: "get_notebook_context", Description: "Get full notebook context", HandlerName: "get_notebook_context"},
	{Name: "list_skills", Description: "List available skills", HandlerName: "list_skills"},
	{Name: "load_skill", Description: "Load a skill's full instructions", HandlerName: "load_skill"},
	{Name: "update_agent", Description: "Update agent configuration", HandlerName: "update_agent"},
	{Name: "create_skill", Description: "Create a new skill", HandlerName: "create_skill"},
	{Name: "update_skill", Description: "Update an existing skill", HandlerName: "update_skill"},
	{Name: "spawn_subagents", Description: "Spawn parallel subagents", HandlerName: "spawn_subagents"},
	{Name: "create_tasks", Description: "Create tasks for subagents", HandlerName: "create_tasks"},
	{Name: "update_task", Description: "Update a task's status", HandlerName: "update_task"},
	{Name: "get_tasks", Description: "Get current tasks", HandlerName: "get_tasks"},
	{Name: "create_chart", Description: "Create a chart from cell output. Types: bar, line, area, scatter, pie, donut, timeline, hierarchy_tree, big_number, map, sankey, funnel, heatmap, histogram. Bar supports bar_mode (grouped|stacked|horizontal) layout. Area supports area_mode (area|stacked) layout. group_by splits data into series (not for pie/donut/big_number/funnel/heatmap/histogram). For heatmap use x_column, y_axis_column, value_column. For funnel use category_column, value_column. For histogram use value_column, bin_count.", HandlerName: "create_chart"},
	{Name: "update_chart", Description: "Update an existing chart's config. Supports same types and params as create_chart. For bar: bar_mode for layout. For area: area_mode for layout. For heatmap: x_column, y_axis_column, value_column. For funnel: category_column, value_column, funnel_sort. For histogram: value_column, bin_count.", HandlerName: "update_chart"},
	{Name: "list_notebooks", Description: "List notebooks", HandlerName: "list_notebooks"},
	{Name: "list_connectors", Description: "List connectors", HandlerName: "list_connectors"},
	{Name: "list_folders", Description: "List folders", HandlerName: "list_folders"},
	{Name: "get_folder_tree", Description: "Get folder tree", HandlerName: "get_folder_tree"},
	{Name: "create_snapshot", Description: "Create a notebook snapshot", HandlerName: "create_snapshot"},
	{Name: "list_snapshots", Description: "List notebook snapshots", HandlerName: "list_snapshots"},
	{Name: "restore_snapshot", Description: "Restore a notebook snapshot", HandlerName: "restore_snapshot"},
	{Name: "ask_question", Description: "Ask the user a question and wait for their response. Use this when you need a decision, clarification, or input from the user to proceed. You can provide multiple choice options for the user to pick from, or leave it open-ended.", HandlerName: "ask_question", Schema: map[string]any{"type": "object", "properties": map[string]any{"question": map[string]any{"type": "string", "description": "The question to ask the user"}, "options": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string", "description": "The option title/label"}, "description": map[string]any{"type": "string", "description": "Optional subtitle or explanation"}}, "required": []any{"title"}}, "description": "Optional multiple choice options"}, "allow_custom": map[string]any{"type": "boolean", "description": "Whether to allow the user to type a custom answer (default: true)"}}, "required": []any{"question"}}},
}

func SeedBuiltinTools(ctx context.Context, pool *pgxpool.Pool, orgID string) {
	var systemUserID string
	err := pool.QueryRow(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 LIMIT 1`, orgID).Scan(&systemUserID)
	if err != nil {
		return
	}

	// Check if already seeded for this org
	var count int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM tools WHERE org_id = $1 AND type = 'builtin'`, orgID).Scan(&count)
	if count >= len(BuiltinTools) {
		return
	}

	for _, bt := range BuiltinTools {
		schema := bt.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		schemaJSON, _ := json.Marshal(schema)
		config, _ := json.Marshal(map[string]string{"handler_name": bt.HandlerName})
		toolID := uuid.New().String()
		_, err := pool.Exec(ctx, `
			INSERT INTO tools (id, org_id, name, description, type, schema, config, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'builtin', $5, $6, $7, NOW(), NOW())
			ON CONFLICT (org_id, name) DO NOTHING`,
			toolID, orgID, bt.Name, bt.Description, string(schemaJSON), string(config), systemUserID)
		if err != nil {
			slog.Warn("seed builtin tool failed", "tool", bt.Name, "error", err)
			continue
		}
		// Seed ACL so all org members can view and use built-in tools
		pool.Exec(ctx, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'tool', $2::uuid, 'org_role', 'everyone', ARRAY['view','use'])
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
			orgID, toolID)
	}
}
