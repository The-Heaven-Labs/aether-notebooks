package agent

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterManageTools(reg *ToolRegistry, pool *pgxpool.Pool) {
	// Dashboard tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_dashboard",
			Description: "Create a new dashboard. Returns the dashboard ID.",
			Parameters:  `{"type":"object","properties":{"title":{"type":"string"},"folder_id":{"type":"string","description":"Optional parent folder"}},"required":["title"]}`,
		},
		Handler: makeCreateDashboardHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_dashboards",
			Description: "List all dashboards in the organization. Returns id, title, and timestamps.",
			Parameters:  `{"type":"object","properties":{}}`,
		},
		Handler: makeListDashboardsHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_dashboard",
			Description: "Delete a dashboard and all its widgets.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makeDeleteDashboardHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_dashboard_widget",
			Description: "Add a widget (chart/table from a notebook cell) to a dashboard. Requires the dashboard ID, notebook ID, cell ID, and layout position. The widget embeds the cell's output.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"notebook_id":{"type":"string"},"cell_id":{"type":"string"},"type":{"type":"string","enum":["chart","table","metric","text"],"description":"Widget type — use 'chart' for cells with chart config, 'table' for table output"},"row":{"type":"number","description":"Row position (0-based)"},"col":{"type":"number","description":"Column position (0-based)"},"width":{"type":"number","description":"Widget width in columns (default 6)"},"height":{"type":"number","description":"Widget height in rows (default 4)"}},"required":["dashboard_id","notebook_id","cell_id","type"]}`,
		},
		Handler: makeCreateDashboardWidgetHandler(pool),
	})

	// Schedule tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_schedule",
			Description: "Schedule a notebook for recurring execution using a cron expression.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"},"cron_expression":{"type":"string","description":"Cron expression like '0 9 * * 1-5' (every weekday at 9am)"}},"required":["notebook_id","cron_expression"]}`,
		},
		Handler: makeCreateScheduleHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_dashboard_widget",
			Description: "Update a dashboard widget's properties: position (row, col), size (width, height), or type (chart/table/text).",
			Parameters:  `{"type":"object","properties":{"widget_id":{"type":"string"},"dashboard_id":{"type":"string"},"row":{"type":"number"},"col":{"type":"number"},"width":{"type":"number"},"height":{"type":"number"},"type":{"type":"string","enum":["chart","table","text"]}},"required":["widget_id","dashboard_id"]}`,
		},
		Handler: makeUpdateDashboardWidgetHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "get_dashboard",
			Description: "Get a dashboard with its widgets. Use this to see the current state of a dashboard before making changes.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makeGetDashboardHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_dashboard_widget",
			Description: "Remove a widget from a dashboard.",
			Parameters:  `{"type":"object","properties":{"widget_id":{"type":"string"},"dashboard_id":{"type":"string"}},"required":["widget_id","dashboard_id"]}`,
		},
		Handler: makeDeleteDashboardWidgetHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_dashboard",
			Description: "Update a dashboard's title or grid settings (e.g., grid_cols: 12 for 12-column grid).",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"title":{"type":"string"},"grid_cols":{"type":"number","description":"Number of columns in the grid layout (default 12)"}},"required":["dashboard_id"]}`,
		},
		Handler: makeUpdateDashboardHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "share_dashboard",
			Description: "Generate a public shareable link for a dashboard. Returns the public token and the full URL.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"}},"required":["dashboard_id"]}`,
		},
		Handler: makeShareDashboardHandler(pool),
	})

	// Schedule tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_schedule",
			Description: "Remove a notebook schedule.",
			Parameters:  `{"type":"object","properties":{"schedule_id":{"type":"string"}},"required":["schedule_id"]}`,
		},
		Handler: makeDeleteScheduleHandler(pool),
	})

	// Permission tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "read_permissions",
			Description: "List ACL entries for a resource (notebook, dashboard, connector, folder). Returns user, group, and everyone entries with their actions.",
			Parameters:  `{"type":"object","properties":{"resource_type":{"type":"string","enum":["notebook","dashboard","connector","folder"]},"resource_id":{"type":"string"}},"required":["resource_type","resource_id"]}`,
		},
		Handler: makeReadPermissionsHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_permissions",
			Description: "Set ACL entries for a resource. Replaces all existing entries. Each entry has subject_type (user/group/everyone), subject_id (user or group ID, omit for everyone), and actions (array like ['view','edit','run','delete']).",
			Parameters:  `{"type":"object","properties":{"resource_type":{"type":"string","enum":["notebook","dashboard","connector","folder"]},"resource_id":{"type":"string"},"entries":{"type":"array","items":{"type":"object","properties":{"subject_type":{"type":"string","enum":["user","group","everyone"]},"subject_id":{"type":"string","description":"User or group ID. Omit for 'everyone'."},"actions":{"type":"array","items":{"type":"string"}}}}}},"required":["resource_type","resource_id","entries"]}`,
		},
		Handler: makeUpdatePermissionsHandler(pool),
	})

	// Export / Import tools
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "export_notebook",
			Description: "Export a notebook as Jupyter .ipynb format. Returns the JSON content that can be downloaded as .ipynb.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"}},"required":["notebook_id"]}`,
		},
		Handler: makeExportNotebookHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "import_notebook",
			Description: "Import a notebook from Jupyter .ipynb JSON content. Creates a new notebook with all cells.",
			Parameters:  `{"type":"object","properties":{"title":{"type":"string","description":"Title for the imported notebook"},"ipynb_json":{"type":"string","description":"The full .ipynb JSON string"},"folder_id":{"type":"string","description":"Optional parent folder ID"}},"required":["title","ipynb_json"]}`,
		},
		Handler: makeImportNotebookHandler(pool),
	})
}

func makeCreateDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Title    string  `json:"title"`
			FolderID *string `json:"folder_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.Title == "" {
			return nil, fmt.Errorf("title is required")
		}
		if req.FolderID != nil && *req.FolderID != "" {
			if err := ctx.CheckPermission("folder", *req.FolderID, "edit"); err != nil {
				return nil, err
			}
		}

		id := uuid.New().String()
		now := time.Now()
		_, err := pool.Exec(ctx.Context, `
			INSERT INTO dashboards (id, org_id, title, settings, created_by, folder_id, created_at, updated_at)
			VALUES ($1, $2, $3, '{}', $4, $5, $6, $6)
		`, id, ctx.OrgID, req.Title, ctx.UserID, req.FolderID, now)
		if err != nil {
			return nil, fmt.Errorf("create dashboard: %w", err)
		}

		_ = ctx.AuditLog("dashboard.create", "dashboard", id)
		return map[string]any{"dashboard_id": id, "title": req.Title}, nil
	}
}

func makeCreateDashboardWidgetHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string `json:"dashboard_id"`
			NotebookID  string `json:"notebook_id"`
			CellID      string `json:"cell_id"`
			Type        string `json:"type"`
			Row         int    `json:"row"`
			Col         int    `json:"col"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.Width == 0 {
			req.Width = 6
		}
		if req.Height == 0 {
			req.Height = 4
		}

		// Check permission on the dashboard
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}

		// Check notebook permissions
		if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
			return nil, err
		}

		layoutJSON, _ := json.Marshal(map[string]any{
			"row":    req.Row,
			"col":    req.Col,
			"width":  req.Width,
			"height": req.Height,
		})

		// Copy the cell's chart config into widget config as fallback
		var cellChart json.RawMessage
		pool.QueryRow(ctx.Context, `SELECT metadata->'chart' FROM cells WHERE id = $1`, req.CellID).Scan(&cellChart)
		widgetConfig := json.RawMessage(`{}`)
		if cellChart != nil && len(cellChart) > 0 && string(cellChart) != "null" {
			widgetConfig = cellChart
		}

		id := uuid.New().String()
		now := time.Now()
		if _, err := pool.Exec(ctx.Context, `
			INSERT INTO widgets (id, dashboard_id, notebook_id, cell_id, type, layout, config, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		`, id, req.DashboardID, req.NotebookID, req.CellID, req.Type, layoutJSON, widgetConfig, now); err != nil {
			return nil, fmt.Errorf("create widget: %w", err)
		}

		_ = ctx.AuditLog("dashboard.add_widget", "dashboard", req.DashboardID)
		return map[string]any{"widget_id": id}, nil
	}
}

func makeUpdateDashboardWidgetHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			WidgetID     string `json:"widget_id"`
			DashboardID  string `json:"dashboard_id"`
			Row          *int   `json:"row"`
			Col          *int   `json:"col"`
			Width        *int   `json:"width"`
			Height       *int   `json:"height"`
			WidgetType   string `json:"type"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}

		// Build layout update
		var layout *string
		if req.Row != nil || req.Col != nil || req.Width != nil || req.Height != nil {
			var layoutJSON []byte
			err := pool.QueryRow(ctx.Context, `SELECT layout FROM widgets WHERE id = $1`, req.WidgetID).Scan(&layoutJSON)
			if err != nil {
				return nil, fmt.Errorf("get current layout: %w", err)
			}
			var cur struct {
				Row, Col, Width, Height int
			}
			if err := json.Unmarshal(layoutJSON, &cur); err != nil {
				return nil, fmt.Errorf("parse current layout: %w", err)
			}
			l := map[string]int{
				"row":    cur.Row,
				"col":    cur.Col,
				"width":  cur.Width,
				"height": cur.Height,
			}
			if req.Row != nil { l["row"] = *req.Row }
			if req.Col != nil { l["col"] = *req.Col }
			if req.Width != nil { l["width"] = *req.Width }
			if req.Height != nil { l["height"] = *req.Height }
			b, _ := json.Marshal(l)
			s := string(b)
			layout = &s
		}

		if layout != nil {
			_, err := pool.Exec(ctx.Context,
				`UPDATE widgets SET layout = $1::jsonb WHERE id = $2 AND dashboard_id = $3`,
				*layout, req.WidgetID, req.DashboardID)
			if err != nil {
				return nil, fmt.Errorf("update widget layout: %w", err)
			}
		}
		if req.WidgetType != "" {
			pool.Exec(ctx.Context,
				`UPDATE widgets SET type = $1 WHERE id = $2 AND dashboard_id = $3`,
				req.WidgetType, req.WidgetID, req.DashboardID)
		}

		return map[string]any{"widget_id": req.WidgetID}, nil
	}
}

func makeGetDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct{ DashboardID string `json:"dashboard_id"` }
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "view"); err != nil {
			return nil, err
		}

		var title string
		var settingsJSON []byte
		err := pool.QueryRow(ctx.Context,
			`SELECT title, COALESCE(settings, '{}') FROM dashboards WHERE id = $1 AND org_id = $2`,
			req.DashboardID, ctx.OrgID,
		).Scan(&title, &settingsJSON)
		if err != nil {
			return nil, fmt.Errorf("dashboard not found: %w", err)
		}

		var settings map[string]any
		json.Unmarshal(settingsJSON, &settings)

		// Fetch widgets
		rows, err := pool.Query(ctx.Context,
			`SELECT id, notebook_id, cell_id, type, layout FROM widgets WHERE dashboard_id = $1 ORDER BY created_at ASC`,
			req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("load widgets: %w", err)
		}
		defer rows.Close()

		var widgets []map[string]any
		for rows.Next() {
			var id, nbID, cellID, wType string
			var layoutJSON []byte
			if err := rows.Scan(&id, &nbID, &cellID, &wType, &layoutJSON); err != nil {
				continue
			}
			var layout map[string]any
			json.Unmarshal(layoutJSON, &layout)
			widgets = append(widgets, map[string]any{
				"id": id, "notebook_id": nbID, "cell_id": cellID,
				"type": wType, "layout": layout,
			})
		}
		if widgets == nil {
			widgets = []map[string]any{}
		}

		return map[string]any{"dashboard_id": req.DashboardID, "title": title, "settings": settings, "widgets": widgets}, nil
	}
}

func makeDeleteDashboardWidgetHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			WidgetID    string `json:"widget_id"`
			DashboardID string `json:"dashboard_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}

		result, err := pool.Exec(ctx.Context,
			`DELETE FROM widgets WHERE id = $1 AND dashboard_id = $2`,
			req.WidgetID, req.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("delete widget: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("widget not found")
		}
		_ = ctx.AuditLog("dashboard.delete_widget", "dashboard", req.DashboardID)
		return map[string]any{"status": "deleted"}, nil
	}
}

func makeUpdateDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			DashboardID string  `json:"dashboard_id"`
			Title       *string `json:"title"`
			GridCols    *int    `json:"grid_cols"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "edit"); err != nil {
			return nil, err
		}

		if req.Title == nil && req.GridCols == nil {
			return nil, fmt.Errorf("nothing to update")
		}

		if req.Title != nil {
			pool.Exec(ctx.Context, `UPDATE dashboards SET title = $1 WHERE id = $2 AND org_id = $3`,
				*req.Title, req.DashboardID, ctx.OrgID)
		}
		if req.GridCols != nil {
			pool.Exec(ctx.Context,
				`UPDATE dashboards SET settings = COALESCE(settings, '{}')::jsonb || jsonb_build_object('grid_cols', $1), updated_at = NOW() WHERE id = $2 AND org_id = $3`,
				*req.GridCols, req.DashboardID, ctx.OrgID)
		}

		_ = ctx.AuditLog("dashboard.update", "dashboard", req.DashboardID)
		return map[string]any{"dashboard_id": req.DashboardID, "status": "updated"}, nil
	}
}

func makeShareDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct{ DashboardID string `json:"dashboard_id"` }
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "view"); err != nil {
			return nil, err
		}

		tokenBytes := make([]byte, 16)
		cryptorand.Read(tokenBytes)
		token := hex.EncodeToString(tokenBytes)

		result, err := pool.Exec(ctx.Context,
			`UPDATE dashboards SET public_token = $1, updated_at = NOW() WHERE id = $2 AND org_id = $3`,
			token, req.DashboardID, ctx.OrgID)
		if err != nil || result.RowsAffected() == 0 {
			return nil, fmt.Errorf("dashboard not found")
		}

		_ = ctx.AuditLog("dashboard.share", "dashboard", req.DashboardID)
		return map[string]any{"public_token": token, "url": "/public/dashboards/" + token}, nil
	}
}

func makeListDashboardsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		rows, err := pool.Query(ctx.Context,
			`SELECT id, title, created_at FROM dashboards WHERE org_id=$1 ORDER BY created_at DESC LIMIT 50`,
			ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("list dashboards: %w", err)
		}
		defer rows.Close()

		var dashboards []map[string]any
		for rows.Next() {
			var id, title string
			var createdAt time.Time
			if err := rows.Scan(&id, &title, &createdAt); err != nil {
				return nil, fmt.Errorf("scan dashboard: %w", err)
			}
			dashboards = append(dashboards, map[string]any{
				"id": id, "title": title, "created_at": createdAt.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list dashboards iter: %w", err)
		}
		if dashboards == nil {
			dashboards = []map[string]any{}
		}
		return map[string]any{"dashboards": dashboards, "count": len(dashboards)}, nil
	}
}

func makeDeleteDashboardHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct{ DashboardID string `json:"dashboard_id"` }
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("dashboard", req.DashboardID, "delete"); err != nil {
			return nil, err
		}
		result, err := pool.Exec(ctx.Context,
			`DELETE FROM dashboards WHERE id=$1 AND org_id=$2`,
			req.DashboardID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("delete dashboard: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("dashboard not found")
		}
		_ = ctx.AuditLog("dashboard.delete", "dashboard", req.DashboardID)
		return map[string]any{"dashboard_id": req.DashboardID, "status": "deleted"}, nil
	}
}

func makeCreateScheduleHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID     string `json:"notebook_id"`
			CronExpression string `json:"cron_expression"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.CronExpression == "" {
			return nil, fmt.Errorf("cron_expression is required")
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "edit"); err != nil {
			return nil, err
		}

		id := uuid.New().String()
		now := time.Now()
		_, err := pool.Exec(ctx.Context, `
			INSERT INTO schedules (id, notebook_id, cron_expression, enabled, created_at, updated_at)
			VALUES ($1, $2, $3, true, $4, $4)
		`, id, req.NotebookID, req.CronExpression, now)
		if err != nil {
			return nil, fmt.Errorf("create schedule: %w", err)
		}

		_ = ctx.AuditLog("schedule.create", "schedule", id)
		return map[string]any{"schedule_id": id, "cron_expression": req.CronExpression}, nil
	}
}

func makeDeleteScheduleHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct{ ScheduleID string `json:"schedule_id"` }
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		var nbID string
		if err := pool.QueryRow(ctx.Context,
			`SELECT notebook_id FROM schedules WHERE id=$1`, req.ScheduleID).Scan(&nbID); err != nil {
			return nil, fmt.Errorf("schedule not found")
		}
		if err := ctx.CheckPermission("notebook", nbID, "edit"); err != nil {
			return nil, err
		}

		result, err := pool.Exec(ctx.Context,
			`DELETE FROM schedules WHERE id=$1 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id=$2)`,
			req.ScheduleID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("delete schedule: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("schedule not found")
		}
		_ = ctx.AuditLog("schedule.delete", "schedule", req.ScheduleID)
		return map[string]any{"schedule_id": req.ScheduleID, "status": "deleted"}, nil
	}
}

func makeReadPermissionsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ResourceType string `json:"resource_type"`
			ResourceID   string `json:"resource_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission(req.ResourceType, req.ResourceID, "manage"); err != nil {
			return nil, err
		}

		rows, err := pool.Query(ctx.Context, `
			SELECT id, subject_type, subject_id, actions FROM acl_entries
			WHERE resource_type=$1 AND resource_id=$2 AND org_id=$3
			ORDER BY subject_type, subject_id
		`, req.ResourceType, req.ResourceID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("query permissions: %w", err)
		}
		defer rows.Close()

		type entry struct {
			ID          string   `json:"id"`
			SubjectType string   `json:"subject_type"`
			SubjectID   *string  `json:"subject_id"`
			Actions     []string `json:"actions"`
		}
		var entries []entry
		for rows.Next() {
			var e entry
			var actions []byte
			if err := rows.Scan(&e.ID, &e.SubjectType, &e.SubjectID, &actions); err != nil {
				return nil, fmt.Errorf("scan entry: %w", err)
			}
			json.Unmarshal(actions, &e.Actions)
			entries = append(entries, e)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read permissions iter: %w", err)
		}
		if entries == nil {
			entries = []entry{}
		}
		return map[string]any{"entries": entries}, nil
	}
}

func makeUpdatePermissionsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ResourceType string `json:"resource_type"`
			ResourceID   string `json:"resource_id"`
			Entries      []struct {
				SubjectType string   `json:"subject_type"`
				SubjectID   *string  `json:"subject_id"`
				Actions     []string `json:"actions"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission(req.ResourceType, req.ResourceID, "manage"); err != nil {
			return nil, err
		}

		tx, err := pool.Begin(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(context.Background())

		tx.Exec(ctx.Context, `DELETE FROM acl_entries WHERE resource_type=$1 AND resource_id=$2 AND org_id=$3`,
			req.ResourceType, req.ResourceID, ctx.OrgID)

		for _, e := range req.Entries {
			actionsJSON, _ := json.Marshal(e.Actions)
			subjType := e.SubjectType
			var subjID *string
			if e.SubjectType == "everyone" {
				subjType = "org_role"
				s := "everyone"
				subjID = &s
			} else {
				subjID = e.SubjectID
			}
			_, err := tx.Exec(ctx.Context, `
				INSERT INTO acl_entries (resource_type, resource_id, org_id, subject_type, subject_id, actions)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, req.ResourceType, req.ResourceID, ctx.OrgID, subjType, subjID, actionsJSON)
			if err != nil {
				return nil, fmt.Errorf("insert entry: %w", err)
			}
		}

		if err := tx.Commit(ctx.Context); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}

		_ = ctx.AuditLog("acl.update", req.ResourceType, req.ResourceID)
		return map[string]any{"status": "updated"}, nil
	}
}

func makeExportNotebookHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct{ NotebookID string `json:"notebook_id"` }
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
			return nil, err
		}

		var title string
		if err := pool.QueryRow(ctx.Context, `SELECT title FROM notebooks WHERE id=$1`, req.NotebookID).Scan(&title); err != nil {
			return nil, fmt.Errorf("notebook not found: %w", err)
		}

		rows, err := pool.Query(ctx.Context,
			`SELECT type, language, source, position FROM cells WHERE notebook_id=$1 ORDER BY position`,
			req.NotebookID)
		if err != nil {
			return nil, fmt.Errorf("query cells: %w", err)
		}
		defer rows.Close()

		type ipynbCell struct {
			CellType string `json:"cell_type"`
			Source   string `json:"source"`
			Metadata struct {
				Language string `json:"language,omitempty"`
			} `json:"metadata"`
		}
		var cells []ipynbCell
		for rows.Next() {
			var c ipynbCell
			var cellType, lang, src string
			var pos int
			if err := rows.Scan(&cellType, &lang, &src, &pos); err != nil {
				return nil, fmt.Errorf("scan cell: %w", err)
			}
			c.CellType = cellType
			c.Source = src
			c.Metadata.Language = lang
			cells = append(cells, c)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("export cells iter: %w", err)
		}

		ipynb := map[string]any{
			"nbformat":       4,
			"nbformat_minor": 5,
			"metadata": map[string]any{
				"title": title,
			},
			"cells": cells,
		}

		return ipynb, nil
	}
}

func makeImportNotebookHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Title     string  `json:"title"`
			IPynbJSON string  `json:"ipynb_json"`
			FolderID  *string `json:"folder_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		var ipynb struct {
			Cells []struct {
				CellType string `json:"cell_type"`
				Source   string `json:"source"`
				Metadata struct {
					Language string `json:"language"`
				} `json:"metadata"`
			} `json:"cells"`
		}
		if err := json.Unmarshal([]byte(req.IPynbJSON), &ipynb); err != nil {
			return nil, fmt.Errorf("invalid ipynb JSON: %w", err)
		}

		if req.FolderID != nil && *req.FolderID != "" {
			if err := ctx.CheckPermission("folder", *req.FolderID, "edit"); err != nil {
				return nil, err
			}
		}

		now := time.Now()
		nbID := uuid.New().String()

		tx, err := pool.Begin(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx.Context)

		_, err = tx.Exec(ctx.Context, `
			INSERT INTO notebooks (id, org_id, title, description, created_by, folder_id, created_at, updated_at)
			VALUES ($1, $2, $3, '', $4, $5, $6, $6)
		`, nbID, ctx.OrgID, req.Title, ctx.UserID, req.FolderID, now)
		if err != nil {
			return nil, fmt.Errorf("create notebook: %w", err)
		}

		for i, c := range ipynb.Cells {
			cellType := c.CellType
			lang := c.Metadata.Language
			if cellType == "code" && lang == "" {
				lang = "sql"
			}
			if cellType == "markdown" && lang == "" {
				lang = "markdown"
			}
			cellID := uuid.New().String()
			if _, err := tx.Exec(ctx.Context, `
				INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
			`, cellID, nbID, cellType, lang, c.Source, i, now); err != nil {
				return nil, fmt.Errorf("create cell %d: %w", i, err)
			}
		}

		if err := tx.Commit(ctx.Context); err != nil {
			return nil, fmt.Errorf("commit import: %w", err)
		}

		_ = ctx.AuditLog("notebook.import", "notebook", nbID)
		return map[string]any{"notebook_id": nbID, "title": req.Title, "cells": len(ipynb.Cells)}, nil
	}
}
