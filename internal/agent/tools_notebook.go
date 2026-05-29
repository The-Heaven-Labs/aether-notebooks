package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterNotebookTools(reg *ToolRegistry, db *pgxpool.Pool) {
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "read_cell",
			Description: "Get a cell's source and outputs",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"Cell identifier"}},"required":["cell_id"]}`,
		},
		Handler: makeReadCellHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_cell",
			Description: "Create a new code or text cell",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"},"type":{"type":"string","enum":["code","text"]},"source":{"type":"string"},"position":{"type":"integer"}},"required":["notebook_id","type"]}`,
		},
		Handler: makeCreateCellHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_cell",
			Description: "Change a cell's source or metadata",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"source":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"}},"required":["cell_id"]}`,
		},
		Handler: makeUpdateCellHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "run_cell",
			Description: "Execute a cell's query",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"}},"required":["cell_id"]}`,
		},
		Handler: makeRunCellHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_cells",
			Description: "List all cells in the notebook with summary",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"}},"required":["notebook_id"]}`,
		},
		Handler: makeListCellsHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "move_cell",
			Description: "Reorder a cell",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"new_position":{"type":"integer"}},"required":["cell_id","new_position"]}`,
		},
		Handler: makeMoveCellHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "explore_schema",
			Description: "Explore the database schema for a connector. Lists all tables and their columns with types. Only works for Postgres and ClickHouse connectors.",
			Parameters:  `{"type":"object","properties":{"connector_id":{"type":"string","description":"ID of the connector to explore"}},"required":["connector_id"]}`,
		},
		Handler: makeExploreSchemaHandler(db),
	})
}

func makeReadCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID string `json:"cell_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		if err := ctx.CheckPermission("notebook", notebookID, "view"); err != nil {
			return nil, err
		}

		var cell struct {
			ID       string          `json:"id"`
			Type     string          `json:"type"`
			Language string          `json:"language"`
			Source   string          `json:"source"`
			Outputs  json.RawMessage `json:"outputs"`
			Position int             `json:"position"`
			Title    *string         `json:"title"`
		}

		err = db.QueryRow(ctx.Context, `
			SELECT id, type, language, source, outputs, position, title
			FROM cells WHERE id = $1
		`, req.CellID).Scan(&cell.ID, &cell.Type, &cell.Language, &cell.Source, &cell.Outputs, &cell.Position, &cell.Title)
		if err != nil {
			return nil, fmt.Errorf("read cell: %w", err)
		}

		return cell, nil
	}
}

func makeCreateCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID string `json:"notebook_id"`
			Type       string `json:"type"`
			Source     string `json:"source"`
			Position   int    `json:"position"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if req.NotebookID == "" {
			req.NotebookID = ctx.NotebookID
		}

		if err := ctx.CheckPermission("notebook", req.NotebookID, "edit"); err != nil {
			return nil, err
		}

		cellID := uuid.New().String()
		position := req.Position
		if position <= 0 {
			var maxPos int
			db.QueryRow(ctx.Context, `SELECT COALESCE(MAX(position), -1) FROM cells WHERE notebook_id = $1`, req.NotebookID).Scan(&maxPos)
			position = maxPos + 1
		} else {
			position = position - 1
		}

		language := "sql"
		if req.Type == "text" {
			language = "markdown"
		}

		now := time.Now()
		_, err := db.Exec(ctx.Context, `
			INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		`, cellID, req.NotebookID, req.Type, language, req.Source, position, now)
		if err != nil {
			return nil, fmt.Errorf("create cell: %w", err)
		}

		_ = ctx.AuditLog("cell.create", "cell", cellID)

		ctx.EmitCellCreated(cellID, position+1)

		return map[string]any{"cell_id": cellID, "position": position}, nil
	}
}

func makeUpdateCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID      string `json:"cell_id"`
			Source      string `json:"source"`
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		if err := ctx.CheckPermission("notebook", notebookID, "edit"); err != nil {
			return nil, err
		}

		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET source = COALESCE(NULLIF($2, ''), source),
				title = COALESCE(NULLIF($3, ''), title),
				description = COALESCE(NULLIF($4, ''), description),
				updated_at = NOW()
			WHERE id = $1
		`, req.CellID, req.Source, req.Title, req.Description)
		if err != nil {
			return nil, fmt.Errorf("update cell: %w", err)
		}

		_ = ctx.AuditLog("cell.update", "cell", req.CellID)

		return map[string]any{"cell_id": req.CellID}, nil
	}
}

func makeRunCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID string `json:"cell_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		if err := ctx.CheckPermission("notebook", notebookID, "run"); err != nil {
			return nil, err
		}

		var cell struct {
			ConnectorID *string `json:"connector_id"`
			Language    string  `json:"language"`
			Source      string  `json:"source"`
			Limit       int     `json:"limit"`
		}
		err = db.QueryRow(ctx.Context, `
			SELECT connector_id, language, source, COALESCE(limit, 0) FROM cells WHERE id = $1
		`, req.CellID).Scan(&cell.ConnectorID, &cell.Language, &cell.Source, &cell.Limit)
		if err != nil {
			return nil, fmt.Errorf("get cell: %w", err)
		}

		if cell.ConnectorID == nil || *cell.ConnectorID == "" {
			return nil, fmt.Errorf("cell has no connector assigned")
		}

		var connType models.ConnectorType
		var configEnc []byte
		err = db.QueryRow(ctx.Context,
			`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
			*cell.ConnectorID, ctx.OrgID,
		).Scan(&connType, &configEnc)
		if err != nil {
			return nil, fmt.Errorf("get connector: %w", err)
		}

		if ctx.MasterKey == nil {
			return nil, fmt.Errorf("master key not available")
		}

		plain, err := crypto.Decrypt(configEnc, ctx.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials: %w", err)
		}

		var cfg models.ConnectorConfig
		if err := json.Unmarshal(plain, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		var exec executor.Executor
		switch connType {
		case models.ConnectorPostgres:
			exec, err = executor.NewPostgresExecutor(cfg)
		case models.ConnectorClickHouse:
			exec, err = executor.NewClickHouseExecutor(cfg)
		default:
			return nil, fmt.Errorf("unsupported connector type: %s", connType)
		}
		if err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		defer exec.Close()

		query := cell.Source
		if cell.Limit > 0 && !strings.Contains(strings.ToUpper(query), "LIMIT") {
			query = strings.TrimRight(query, ";") + fmt.Sprintf(" LIMIT %d", cell.Limit)
		}

		result, err := exec.Execute(ctx.Context, query, nil, cell.Limit)
		if err != nil {
			return map[string]any{
				"cell_id": req.CellID,
				"status":  "error",
				"error":   err.Error(),
			}, nil
		}

		_ = ctx.AuditLog("cell.run", "cell", req.CellID)

		return map[string]any{
			"cell_id": req.CellID,
			"status":  "completed",
			"rows":    len(result.Rows),
			"columns": len(result.Columns),
		}, nil
	}
}

func makeListCellsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct{ NotebookID string }
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if req.NotebookID == "" {
			req.NotebookID = ctx.NotebookID
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
			return nil, err
		}

		rows, err := db.Query(ctx.Context, `
			SELECT id, type, language, title, position FROM cells WHERE notebook_id = $1 ORDER BY position
		`, req.NotebookID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var cells []map[string]any
		for rows.Next() {
			var c struct {
				ID       string  `json:"id"`
				Type     string  `json:"type"`
				Language string  `json:"language"`
				Title    *string `json:"title"`
				Position int     `json:"position"`
			}
			if err := rows.Scan(&c.ID, &c.Type, &c.Language, &c.Title, &c.Position); err != nil {
				continue
			}
			title := ""
			if c.Title != nil {
				title = *c.Title
			}
			cells = append(cells, map[string]any{"id": c.ID, "type": c.Type, "language": c.Language, "title": title, "position": c.Position + 1})
		}

		return map[string]any{"cells": cells, "count": len(cells)}, nil
	}
}

func makeMoveCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID      string `json:"cell_id"`
			NewPosition int    `json:"new_position"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		if err := ctx.CheckPermission("notebook", notebookID, "edit"); err != nil {
			return nil, err
		}

		if req.NewPosition > 0 {
			req.NewPosition = req.NewPosition - 1
		}
		_, err = db.Exec(ctx.Context, `UPDATE cells SET position = $1, updated_at = NOW() WHERE id = $2`, req.NewPosition, req.CellID)
		if err != nil {
			return nil, fmt.Errorf("move cell: %w", err)
		}

		_ = ctx.AuditLog("cell.move", "cell", req.CellID)

		return map[string]any{"cell_id": req.CellID, "position": req.NewPosition + 1}, nil
	}
}

func makeExploreSchemaHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ConnectorID string `json:"connector_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if err := ctx.CheckPermission("connector", req.ConnectorID, "view"); err != nil {
			return nil, err
		}

		var connType models.ConnectorType
		var configEnc []byte
		err := db.QueryRow(ctx.Context,
			`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
			req.ConnectorID, ctx.OrgID,
		).Scan(&connType, &configEnc)
		if err != nil {
			return nil, fmt.Errorf("get connector: %w", err)
		}

		if ctx.MasterKey == nil {
			return nil, fmt.Errorf("master key not available")
		}

		plain, err := crypto.Decrypt(configEnc, ctx.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials: %w", err)
		}

		var cfg models.ConnectorConfig
		if err := json.Unmarshal(plain, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		var exec executor.Executor
		switch connType {
		case models.ConnectorPostgres:
			exec, err = executor.NewPostgresExecutor(cfg)
		case models.ConnectorClickHouse:
			exec, err = executor.NewClickHouseExecutor(cfg)
		default:
			return nil, fmt.Errorf("unsupported connector type: %s", connType)
		}
		if err != nil {
			return nil, fmt.Errorf("connect to connector db: %w", err)
		}
		defer exec.Close()

		schema, err := exec.Schema(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("query schema: %w", err)
		}

		var tables []map[string]any
		for _, t := range schema.Tables {
			var columns []map[string]any
			for _, c := range t.Columns {
				columns = append(columns, map[string]any{
					"name": c.Name,
					"type": c.Type,
				})
			}
			fullName := t.Name
			if t.Schema != "" {
				fullName = t.Schema + "." + t.Name
			}
			tables = append(tables, map[string]any{
				"table_name":   fullName,
				"columns":      columns,
				"column_count": len(columns),
			})
		}

		return map[string]any{"tables": tables, "total_tables": len(tables)}, nil
	}
}
