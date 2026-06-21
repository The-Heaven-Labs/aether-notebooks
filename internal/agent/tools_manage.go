package agent

import (
	"context"
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
			rows.Scan(&id, &title, &createdAt)
			dashboards = append(dashboards, map[string]any{
				"id": id, "title": title, "created_at": createdAt.Format(time.RFC3339),
			})
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
			rows.Scan(&e.ID, &e.SubjectType, &e.SubjectID, &actions)
			json.Unmarshal(actions, &e.Actions)
			entries = append(entries, e)
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
			subjID := e.SubjectID
			if e.SubjectType == "everyone" {
				subjID = nil
			}
			_, err := tx.Exec(ctx.Context, `
				INSERT INTO acl_entries (resource_type, resource_id, org_id, subject_type, subject_id, actions)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, req.ResourceType, req.ResourceID, ctx.OrgID, e.SubjectType, subjID, actionsJSON)
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
			rows.Scan(&cellType, &lang, &src, &pos)
			c.CellType = cellType
			c.Source = src
			c.Metadata.Language = lang
			cells = append(cells, c)
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

		now := time.Now()
		nbID := uuid.New().String()
		_, err := pool.Exec(ctx.Context, `
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
			pool.Exec(ctx.Context, `
				INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
			`, cellID, nbID, cellType, lang, c.Source, i, now)
		}

		_ = ctx.AuditLog("notebook.import", "notebook", nbID)
		return map[string]any{"notebook_id": nbID, "title": req.Title, "cells": len(ipynb.Cells)}, nil
	}
}
