package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

func RegisterNotebookTools(reg *ToolRegistry, db *pgxpool.Pool) {
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_notebook",
			Description: "Create a new notebook. Use this to start a new analysis project. The notebook will get the org's default connector if one exists.",
			Parameters:  `{"type":"object","properties":{"title":{"type":"string","description":"Notebook title"},"description":{"type":"string","description":"Optional description"},"folder_id":{"type":"string","description":"Optional parent folder ID to place this notebook in"}},"required":["title"]}`,
		},
		Handler: makeCreateNotebookHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_notebook",
			Description: "Delete a notebook and all its cells. This cannot be undone — use with care.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string","description":"ID of the notebook to delete"}},"required":["notebook_id"]}`,
		},
		Handler:         makeDeleteNotebookHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_notebook",
			Description: "Update a notebook's title, description, or default connector.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"connector_id":{"type":"string"}},"required":["notebook_id"]}`,
		},
		Handler:         makeUpdateNotebookHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "read_cell",
			Description: "Get a cell's complete information including source, outputs, type, language, connector, and metadata",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"The cell's UUID (from list_cells output, not the positional number)"}},"required":["cell_id"]}`,
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
			Description: "Create a new cell. Use type 'code' with language 'sql' for database queries, or type 'text' with language 'markdown' for documentation and notes.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"},"type":{"type":"string","enum":["code","text"],"description":"Cell type: 'code' for executable queries, 'text' for markdown documentation"},"language":{"type":"string","enum":["sql","markdown"],"description":"Cell language. Defaults to 'sql' for code cells, 'markdown' for text cells. Currently only SQL and markdown are supported."},"source":{"type":"string"},"connector_id":{"type":"string","description":"The ID of the connector to assign to this cell. Required for code cells if the notebook has no default connector."},"position":{"type":"integer"}},"required":["notebook_id","type"]}`,
		},
		Handler:         makeCreateCellHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_cell",
			Description: "Change a cell's source, title, description, connector, or other properties",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"The cell's UUID (from list_cells output, NOT the position number)"},"source":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"connector_id":{"type":"string","description":"The ID of the connector to assign to this cell"}},"required":["cell_id"]}`,
		},
		Handler:         makeUpdateCellHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "run_cell",
			Description: "Execute a code cell's SQL query against the database connector. Only works on cells with type 'code' and language 'sql'. Returns tabular results. Skips re-running if cell already has results (use force=true to override). Use for SELECT, SHOW, DESCRIBE queries.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"The cell's UUID (from list_cells output, not the position number)"},"force":{"type":"boolean","description":"Set to true to re-run even if the cell already has results"}},"required":["cell_id"]}`,
		},
		Handler:         makeRunCellHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_cells",
			Description: "List all cells in the notebook showing their UUID (id), type (code/text), language (sql/markdown), position, and title. Use the UUID from the id field for other cell operations.",
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
			Description: "Move a cell to a new 1-based position. Cells between the old and new position shift by 1 to make room.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"The cell's UUID (the id field from list_cells, not the position number)"},"new_position":{"type":"integer"}},"required":["cell_id","new_position"]}`,
		},
		Handler:         makeMoveCellHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "swap_cells",
			Description: "Swap the positions of two cells. Useful for reordering — two swaps can move any cell anywhere without needing to understand position cascading.",
			Parameters:  `{"type":"object","properties":{"cell_id_a":{"type":"string","description":"UUID of the first cell"},"cell_id_b":{"type":"string","description":"UUID of the second cell"}},"required":["cell_id_a","cell_id_b"]}`,
		},
		Handler:         makeSwapCellsHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "execute_sql",
			Description: "Run an ad-hoc SQL query on a database connector (30s timeout). Use this for quick queries only. For long-running queries, use create_cell + run_cell instead. Returns up to 1000 rows. For SELECT, SHOW, DESCRIBE queries.",
			Parameters:  `{"type":"object","properties":{"connector_id":{"type":"string","description":"ID of the connector to query"},"query":{"type":"string","description":"The SQL query to execute"},"limit":{"type":"integer","description":"Max rows to return (default 1000)"}},"required":["connector_id","query"]}`,
		},
		Handler:         makeExecuteSQLHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "explore_schema",
			Description: "Explore the database schema for a connector. Returns all tables/indices and their columns with types. Use this to understand what data is available before writing queries.",
			Parameters:  `{"type":"object","properties":{"connector_id":{"type":"string","description":"ID of the connector to explore"}},"required":["connector_id"]}`,
		},
		Handler: makeExploreSchemaHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "delete_cell",
			Description: "Delete a cell from a notebook. Use this to clean up cells that are no longer needed.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"The cell's UUID (from list_cells output, not the position number)"}},"required":["cell_id"]}`,
		},
		Handler:         makeDeleteCellHandler(db),
		ConfirmRequired: true,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "get_notebook_context",
			Description: "Get the full content of a notebook including all cell UUIDs, sources and optionally outputs. Use this to understand the complete notebook structure and content.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string","description":"The notebook ID to read"},"max_cells":{"type":"integer","description":"Maximum number of cells to return (default 50)"},"include_outputs":{"type":"boolean","description":"Include cell outputs (default false, truncates to first 10 rows if true)"}},"required":["notebook_id"]}`,
		},
		Handler: makeGetNotebookContextHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_snapshot",
			Description: "Create a manual snapshot (version history checkpoint) of the current notebook state. Use this before making destructive changes.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string","description":"Notebook ID (defaults to current)"},"name":{"type":"string","description":"A descriptive name for this snapshot"}},"required":["name"]}`,
		},
		Handler: makeCreateSnapshotHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_snapshots",
			Description: "List all version history checkpoints (snapshots) for a notebook. Shows date, who created it, name, and what changed.",
			Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string","description":"Notebook ID (defaults to current)"}},"required":[]}`,
		},
		Handler: makeListSnapshotsHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "restore_snapshot",
			Description: "Restore a notebook to a previous version history checkpoint. This will restore the notebook title, all cell sources, types, positions, and metadata to the state captured in the snapshot.",
			Parameters:  `{"type":"object","properties":{"snapshot_id":{"type":"string","description":"ID of the snapshot to restore to"}},"required":["snapshot_id"]}`,
		},
		Handler: makeRestoreSnapshotHandler(db),
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

		resolved, err := ctx.ResolveCell(req.CellID)
		if err != nil {
			return nil, err
		}
		if err := ctx.CheckPermission("notebook", resolved.NotebookID, "view"); err != nil {
			return nil, err
		}

		var cell struct {
			ID            string          `json:"id"`
			NotebookID    string          `json:"notebook_id"`
			Position      int             `json:"position"`
			Type          string          `json:"type"`
			Language      *string         `json:"language"`
			ConnectorID   *string         `json:"connector_id"`
			Source        string          `json:"source"`
			Outputs       json.RawMessage `json:"outputs"`
			Limit         *int            `json:"limit"`
			CreatedAt     time.Time       `json:"created_at"`
			UpdatedAt     time.Time       `json:"updated_at"`
			SourceVisible bool            `json:"source_visible"`
			CellCollapsed bool            `json:"cell_collapsed"`
			Title         *string         `json:"title"`
			Description   *string         `json:"description"`
			Slug          *string         `json:"slug"`
			Parameters    json.RawMessage `json:"parameters"`
			SlideBreak    bool            `json:"slide_break"`
			Metadata      json.RawMessage `json:"metadata"`
		}

		err = db.QueryRow(ctx.Context, `
			SELECT id, notebook_id, position, type, language, connector_id, source, outputs, 
			       "limit", created_at, updated_at, source_visible, cell_collapsed, title, 
			       description, slug, parameters, slide_break, metadata
			FROM cells WHERE id = $1
		`, resolved.ID).Scan(
			&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &cell.Language,
			&cell.ConnectorID, &cell.Source, &cell.Outputs, &cell.Limit,
			&cell.CreatedAt, &cell.UpdatedAt, &cell.SourceVisible, &cell.CellCollapsed,
			&cell.Title, &cell.Description, &cell.Slug, &cell.Parameters,
			&cell.SlideBreak, &cell.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("read cell: %w", err)
		}

		// Build a chart/output summary to help the agent assess chart health
		summary := map[string]any{}
		var groupByCol string
		if cell.Metadata != nil {
			var meta struct {
				Chart map[string]any `json:"chart"`
			}
			if json.Unmarshal(cell.Metadata, &meta) == nil && meta.Chart != nil {
				summary["chart_type"] = meta.Chart["chartType"]
				summary["chart_title"] = meta.Chart["title"]
				if yAxis, ok := meta.Chart["yAxis"].([]any); ok {
					var series []string
					for _, y := range yAxis {
						if s, ok := y.(string); ok {
							series = append(series, s)
						}
					}
					summary["configured_series"] = series
				}
				if xCol, ok := meta.Chart["xAxis"].(string); ok {
					summary["x_axis"] = xCol
				}
				if gCol, ok := meta.Chart["groupBy"].(string); ok {
					summary["group_by"] = gCol
					groupByCol = gCol
				}
			}
		}
		if cell.Outputs != nil {
			var outputs []struct {
				Type string `json:"type"`
				Data struct {
					Columns []string         `json:"columns"`
					Rows    []map[string]any `json:"rows"`
				} `json:"data"`
			}
			if json.Unmarshal(cell.Outputs, &outputs) == nil {
				for _, o := range outputs {
					if o.Type == "table" {
						summary["data_columns"] = o.Data.Columns
						summary["data_rows"] = len(o.Data.Rows)
						if len(o.Data.Rows) > 0 {
							rowStr, _ := json.Marshal(o.Data.Rows[0])
							summary["sample_row"] = string(rowStr)
						}
						// Extract actual series names from groupBy column
						if groupByCol != "" && len(o.Data.Rows) > 0 {
							seen := map[string]bool{}
							var names []string
							for _, row := range o.Data.Rows {
								if v, ok := row[groupByCol]; ok {
									s := fmt.Sprintf("%v", v)
									if !seen[s] {
										seen[s] = true
										names = append(names, s)
									}
								}
							}
							if len(names) > 0 {
								summary["data_series"] = names
							}
						}
						break
					}
				}
			}
		}
		cellWithSummary := map[string]any{
			"id":             cell.ID,
			"notebook_id":    cell.NotebookID,
			"position":       cell.Position + 1,
			"type":           cell.Type,
			"language":       cell.Language,
			"connector_id":   cell.ConnectorID,
			"source":         cell.Source,
			"outputs":        cell.Outputs,
			"limit":          cell.Limit,
			"created_at":     cell.CreatedAt,
			"updated_at":     cell.UpdatedAt,
			"source_visible": cell.SourceVisible,
			"cell_collapsed": cell.CellCollapsed,
			"title":          cell.Title,
			"description":    cell.Description,
			"slug":           cell.Slug,
			"parameters":     cell.Parameters,
			"slide_break":    cell.SlideBreak,
			"metadata":       cell.Metadata,
			"chart_summary":  summary,
		}
		return cellWithSummary, nil
	}
}

func makeCreateCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID  string `json:"notebook_id"`
			Type        string `json:"type"`
			Language    string `json:"language"`
			Source      string `json:"source"`
			ConnectorID string `json:"connector_id"`
			Position    int    `json:"position"`
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
			if err := db.QueryRow(ctx.Context, `SELECT COALESCE(MAX(position), -1) FROM cells WHERE notebook_id = $1`, req.NotebookID).Scan(&maxPos); err != nil {
				maxPos = -1
			}
			position = maxPos + 1
		} else {
			position = position - 1
			tx, err := db.Begin(ctx.Context)
			if err != nil {
				return nil, fmt.Errorf("begin tx: %w", err)
			}
			defer tx.Rollback(ctx.Context)

			if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -position - 1 WHERE notebook_id = $1 AND position >= $2`, req.NotebookID, position); err != nil {
				return nil, fmt.Errorf("shift cells: %w", err)
			}
			if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -position WHERE notebook_id = $1 AND position < 0`, req.NotebookID); err != nil {
				return nil, fmt.Errorf("shift cells back: %w", err)
			}

			if err := tx.Commit(ctx.Context); err != nil {
				return nil, fmt.Errorf("commit shift: %w", err)
			}
		}

		language := req.Language
		if language == "" {
			language = "sql"
			if req.Type == "text" {
				language = "markdown"
			}
		}

		var connID *string
		if req.ConnectorID != "" {
			connID = &req.ConnectorID
		}

		now := time.Now()
		_, err := db.Exec(ctx.Context, `
			INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		`, cellID, req.NotebookID, req.Type, language, connID, req.Source, position, now)
		if err != nil {
			return nil, fmt.Errorf("create cell: %w", err)
		}

		_ = ctx.AuditLog("cell.create", "cell", cellID)

		ctx.EmitCellCreated(cellID, position+1)

		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(req.NotebookID, map[string]any{
				"type": "cell_created",
				"cell": map[string]any{
					"id":             cellID,
					"notebook_id":    req.NotebookID,
					"position":       position,
					"type":           req.Type,
					"language":       language,
					"source":         req.Source,
					"outputs":        []models.Output{},
					"source_visible": true,
					"outputs_hidden": false,
					"cell_collapsed": false,
					"slide_break":    false,
					"created_at":     now,
					"updated_at":     now,
				},
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"cell_id": cellID, "position": position + 1}, nil
	}
}

func makeUpdateCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID      string `json:"cell_id"`
			Source      string `json:"source"`
			Title       string `json:"title"`
			Description string `json:"description"`
			ConnectorID string `json:"connector_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		resolved, err := ctx.ResolveCell(req.CellID)
		if err != nil {
			return nil, err
		}
		if err := ctx.CheckPermission("notebook", resolved.NotebookID, "edit"); err != nil {
			return nil, err
		}
		notebookID := resolved.NotebookID
		cellID := resolved.ID

		// 1. Update Yjs document (source of truth) if source is changing
		if req.Source != "" {
			if err := UpdateCellInYjs(ctx.Context, db, notebookID, cellID, req.Source); err != nil {
				return nil, fmt.Errorf("update yjs: %w", err)
			}
		}

		// 2. Update database cache (for API queries, search, exports)
		var connID *string
		if req.ConnectorID != "" {
			connID = &req.ConnectorID
		}
		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET source = COALESCE(NULLIF($2, ''), source),
				title = COALESCE(NULLIF($3, ''), title),
				description = COALESCE(NULLIF($4, ''), description),
				connector_id = COALESCE($5, connector_id),
				agent_updated_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, cellID, req.Source, req.Title, req.Description, connID)
		if err != nil {
			return nil, fmt.Errorf("update cache: %w", err)
		}

		_ = ctx.AuditLog("cell.update", "cell", cellID)

		// Notify agent panel via event
		ctx.EmitCellUpdated(cellID, req.Source)

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":       "cell_updated",
				"cell_id":    cellID,
				"source":     req.Source,
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"cell_id": cellID}, nil
	}
}

func makeRunCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		startTime := time.Now()

		var req struct {
			CellID string `json:"cell_id"`
			Force  bool   `json:"force"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		resolved, err := ctx.ResolveCell(req.CellID)
		if err != nil {
			return nil, err
		}
		if err := ctx.CheckPermission("notebook", resolved.NotebookID, "run"); err != nil {
			return nil, err
		}
		notebookID := resolved.NotebookID
		cellID := resolved.ID

		// Check if cell already has results
		if !req.Force {
			var hasOutputs bool
			db.QueryRow(ctx.Context, `SELECT outputs IS NOT NULL AND outputs != '[]'::jsonb FROM cells WHERE id = $1`, cellID).Scan(&hasOutputs)
			if hasOutputs {
				return map[string]any{"cell_id": cellID, "status": "skipped", "reason": "cell already has results, use force=true to re-run"}, nil
			}
		}

		var cell struct {
			ConnectorID *string `json:"connector_id"`
			Language    string  `json:"language"`
			Source      string  `json:"source"`
			Limit       int     `json:"limit"`
		}
		err = db.QueryRow(ctx.Context, `
			SELECT connector_id, language, source, COALESCE("limit", 0) FROM cells WHERE id = $1
		`, cellID).Scan(&cell.ConnectorID, &cell.Language, &cell.Source, &cell.Limit)
		if err != nil {
			return nil, fmt.Errorf("get cell: %w", err)
		}

		if cell.ConnectorID == nil || *cell.ConnectorID == "" {
			var nbConnID *string
			if err := db.QueryRow(ctx.Context, "SELECT connector_id FROM notebooks WHERE id = $1", notebookID).Scan(&nbConnID); err != nil && err != pgx.ErrNoRows {
				return nil, fmt.Errorf("get notebook connector: %w", err)
			}
			if nbConnID != nil && *nbConnID != "" {
				cell.ConnectorID = nbConnID
			}
		}
		if cell.ConnectorID == nil || *cell.ConnectorID == "" {
			return nil, fmt.Errorf("cell has no connector assigned; set one with create_cell or update_cell")
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

		driver, ok := executor.GetDriver(connType)
		if !ok {
			return nil, fmt.Errorf("unsupported connector type: %s", connType)
		}
		exec, err := driver.NewExecutor(plain)
		if err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		defer exec.Close()

		query := executor.ApplyLimit(cell.Source, cell.Limit)

		result, err := exec.Execute(ctx.Context, query, nil, cell.Limit)
		if err != nil {
			errTotalTimeMs := time.Since(startTime).Milliseconds()
			errOutput := models.Output{Type: "error", Data: map[string]string{"message": err.Error()}}
			outJSON, _ := json.Marshal([]models.Output{errOutput})
			db.Exec(ctx.Context, "UPDATE cells SET outputs = $1, duration_ms = $2, updated_at = NOW() WHERE id = $3", outJSON, errTotalTimeMs, cellID)
			ctx.EmitCellOutput(cellID, []models.Output{errOutput})
			if ctx.BroadcastFunc != nil {
				ctx.BroadcastFunc(notebookID, map[string]any{
					"type":       "cell_output",
					"cell_id":    cellID,
					"outputs":    []models.Output{errOutput},
					"user_email": "agent@aether",
				})
			}

			return map[string]any{
				"cell_id":       cellID,
				"status":        "error",
				"error":         err.Error(),
				"total_time_ms": errTotalTimeMs,
			}, nil
		}

		totalTimeMs := time.Since(startTime).Milliseconds()

		tableOutput := models.Output{Type: "table", Data: result}
		outputs := []models.Output{tableOutput}
		outJSON, _ := json.Marshal(outputs)
		db.Exec(ctx.Context, "UPDATE cells SET outputs = $1, duration_ms = $2, updated_at = NOW() WHERE id = $3", outJSON, totalTimeMs, cellID)
		ctx.EmitCellOutput(cellID, outputs)
		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":          "cell_output",
				"cell_id":       cellID,
				"outputs":       outputs,
				"user_email":    "agent@aether",
				"total_time_ms": totalTimeMs,
			})
		}

		_ = ctx.AuditLog("cell.run", "cell", cellID)

		return map[string]any{
			"cell_id":       cellID,
			"status":        "completed",
			"rows":          len(result.Rows),
			"columns":       len(result.Columns),
			"total_time_ms": totalTimeMs,
		}, nil
	}
}

func makeListCellsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID string `json:"notebook_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if req.NotebookID == "" {
			req.NotebookID = ctx.NotebookID
		}
		if req.NotebookID == "" {
			return nil, fmt.Errorf("notebook_id is required")
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
			return nil, err
		}

		rows, err := db.Query(ctx.Context, `
			SELECT id, type, language, title, position, COALESCE(metadata->'chart', '{}') FROM cells WHERE notebook_id = $1 ORDER BY position
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
			var metadata json.RawMessage
			if err := rows.Scan(&c.ID, &c.Type, &c.Language, &c.Title, &c.Position, &metadata); err != nil {
				return nil, fmt.Errorf("scan cell: %w", err)
			}
			title := ""
			if c.Title != nil {
				title = *c.Title
			}
			cellMap := map[string]any{"id": c.ID, "type": c.Type, "language": c.Language, "title": title, "position": c.Position + 1}
			// Include chart metadata if present (helps agent check if cell has chart config)
			if metadata != nil && string(metadata) != "{}" {
				var chartMeta map[string]any
				if json.Unmarshal(metadata, &chartMeta) == nil {
					if chartType, ok := chartMeta["chartType"].(string); ok {
						cellMap["chart"] = map[string]any{"chartType": chartType}
					}
				}
			}
			cells = append(cells, cellMap)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list cells iter: %w", err)
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

		resolved, err := ctx.ResolveCell(req.CellID)
		if err != nil {
			return nil, err
		}
		if err := ctx.CheckPermission("notebook", resolved.NotebookID, "edit"); err != nil {
			return nil, err
		}
		notebookID := resolved.NotebookID
		cellID := resolved.ID

		if req.NewPosition <= 0 {
			return nil, fmt.Errorf("new_position must be >= 1")
		}
		newPos := req.NewPosition - 1

		tx, err := db.Begin(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx.Context)

		var oldPos int
		if err := tx.QueryRow(ctx.Context, `SELECT position FROM cells WHERE id=$1 AND notebook_id=$2`,
			cellID, notebookID).Scan(&oldPos); err != nil {
			return nil, fmt.Errorf("get cell position: %w", err)
		}

		if newPos == oldPos {
			return map[string]any{"cell_id": cellID, "position": req.NewPosition, "status": "no change"}, nil
		}

		// Move source cell to a negative position that won't collide with the
		// negative-intermediate shift pattern. -(oldPos+1) is always outside the
		// range of shifted intermediate values for both increment and decrement.
		if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = $1 WHERE id = $2`,
			-(oldPos + 1), cellID); err != nil {
			return nil, fmt.Errorf("remove cell: %w", err)
		}

		if newPos > oldPos {
			// Shift cells in (oldPos, newPos] down by 1 using negative-intermediate pattern
			// decrement: position -> position - 1
			if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -(position + 1) WHERE notebook_id = $1 AND position > $2 AND position <= $3 AND id != $4`,
				notebookID, oldPos, newPos, cellID); err != nil {
				return nil, fmt.Errorf("shift cells down: %w", err)
			}
			if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -position - 2 WHERE notebook_id = $1 AND position < 0 AND id != $2`,
				notebookID, cellID); err != nil {
				return nil, fmt.Errorf("shift cells down back: %w", err)
			}
		} else {
			// Shift cells in [newPos, oldPos) up by 1 using negative-intermediate pattern
			// increment: position -> position + 1
			if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -position - 1 WHERE notebook_id = $1 AND position >= $2 AND position < $3 AND id != $4`,
				notebookID, newPos, oldPos, cellID); err != nil {
				return nil, fmt.Errorf("shift cells up: %w", err)
			}
			if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -position WHERE notebook_id = $1 AND position < 0 AND id != $2`,
				notebookID, cellID); err != nil {
				return nil, fmt.Errorf("shift cells up back: %w", err)
			}
		}

		if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = $1, updated_at = NOW() WHERE id = $2`, newPos, cellID); err != nil {
			return nil, fmt.Errorf("set cell position: %w", err)
		}

		if err := tx.Commit(ctx.Context); err != nil {
			return nil, fmt.Errorf("commit move: %w", err)
		}

		_ = ctx.AuditLog("cell.move", "cell", cellID)

		ctx.EmitCellUpdated(cellID, "")

		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":       "notebook_refresh",
				"reason":     "cell_moved",
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"cell_id": cellID, "position": req.NewPosition, "status": "moved"}, nil
	}
}

func makeSwapCellsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellA string `json:"cell_id_a"`
			CellB string `json:"cell_id_b"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.CellA == "" || req.CellB == "" {
			return nil, fmt.Errorf("cell_id_a and cell_id_b are required")
		}
		if req.CellA == req.CellB {
			return nil, fmt.Errorf("cannot swap a cell with itself")
		}

		resolvedA, err := ctx.ResolveCell(req.CellA)
		if err != nil {
			return nil, fmt.Errorf("cell A: %w", err)
		}
		resolvedB, err := ctx.ResolveCell(req.CellB)
		if err != nil {
			return nil, fmt.Errorf("cell B: %w", err)
		}
		if resolvedA.NotebookID != resolvedB.NotebookID {
			return nil, fmt.Errorf("cells must be in the same notebook")
		}
		if err := ctx.CheckPermission("notebook", resolvedA.NotebookID, "edit"); err != nil {
			return nil, err
		}
		notebookID := resolvedA.NotebookID
		cellA := resolvedA.ID
		cellB := resolvedB.ID

		tx, err := db.Begin(ctx.Context)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx.Context)

		var posA, posB int
		if err := tx.QueryRow(ctx.Context, `SELECT position FROM cells WHERE id=$1 AND notebook_id=$2`, cellA, notebookID).Scan(&posA); err != nil {
			return nil, fmt.Errorf("get cell A position: %w", err)
		}
		if err := tx.QueryRow(ctx.Context, `SELECT position FROM cells WHERE id=$1 AND notebook_id=$2`, cellB, notebookID).Scan(&posB); err != nil {
			return nil, fmt.Errorf("get cell B position: %w", err)
		}

		if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = -(position + 1), updated_at = NOW() WHERE id = $1`, cellA); err != nil {
			return nil, fmt.Errorf("move cell A aside: %w", err)
		}
		if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = $1, updated_at = NOW() WHERE id = $2`, posA, cellB); err != nil {
			return nil, fmt.Errorf("set cell B position: %w", err)
		}
		if _, err := tx.Exec(ctx.Context, `UPDATE cells SET position = $1, updated_at = NOW() WHERE id = $2`, posB, cellA); err != nil {
			return nil, fmt.Errorf("set cell A position: %w", err)
		}

		if err := tx.Commit(ctx.Context); err != nil {
			return nil, fmt.Errorf("commit swap: %w", err)
		}

		_ = ctx.AuditLog("cell.swap", "cell", cellA)
		_ = ctx.AuditLog("cell.swap", "cell", cellB)

		ctx.EmitCellUpdated(cellA, "")

		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":       "notebook_refresh",
				"reason":     "cells_swapped",
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"cell_id_a": cellA, "cell_id_b": cellB, "status": "swapped"}, nil
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

		driver, ok := executor.GetDriver(connType)
		if !ok {
			return nil, fmt.Errorf("unsupported connector type: %s", connType)
		}
		exec, err := driver.NewExecutor(plain)
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
				col := map[string]any{
					"name": c.Name,
					"type": c.Type,
				}
				if c.Description != "" {
					col["description"] = c.Description
				}
				columns = append(columns, col)
			}
			fullName := t.Name
			if t.Schema != "" {
				fullName = t.Schema + "." + t.Name
			}
			table := map[string]any{
				"table_name":   fullName,
				"columns":      columns,
				"column_count": len(columns),
			}
			if t.Description != "" {
				table["description"] = t.Description
			}
			tables = append(tables, table)
		}

		return map[string]any{"tables": tables, "total_tables": len(tables)}, nil
	}
}

func makeDeleteCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID string `json:"cell_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		resolved, err := ctx.ResolveCell(req.CellID)
		if err != nil {
			return nil, err
		}
		if err := ctx.CheckPermission("notebook", resolved.NotebookID, "edit"); err != nil {
			return nil, err
		}
		notebookID := resolved.NotebookID
		cellID := resolved.ID

		// Auto-snapshot before destructive action
		go EnsureAutoSnapshot(context.Background(), db, notebookID, ctx.UserID, ctx.OrgID)

		result, err := db.Exec(ctx.Context,
			`DELETE FROM cells WHERE id = $1 AND notebook_id = $2
			 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $3)`,
			cellID, notebookID, ctx.OrgID,
		)
		if err != nil {
			return nil, fmt.Errorf("delete cell: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("cell not found")
		}

		// Touch notebook timestamp
		if _, err := db.Exec(ctx.Context, `UPDATE notebooks SET updated_at = NOW() WHERE id = $1`, notebookID); err != nil {
			slog.Warn("touch notebook timestamp", "error", err)
		}

		_ = ctx.AuditLog("cell.delete", "cell", cellID)

		ctx.EmitCellDeleted(cellID)

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":       "cell_deleted",
				"cell_id":    cellID,
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"cell_id": cellID, "status": "deleted"}, nil
	}
}

func makeGetNotebookContextHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var params struct {
			NotebookID     string `json:"notebook_id"`
			MaxCells       int    `json:"max_cells"`
			IncludeOutputs bool   `json:"include_outputs"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if params.NotebookID == "" {
			params.NotebookID = ctx.NotebookID
		}
		if params.MaxCells <= 0 {
			params.MaxCells = 50
		}
		if params.MaxCells > 50 {
			params.MaxCells = 50
		}

		if err := ctx.CheckPermission("notebook", params.NotebookID, "view"); err != nil {
			return nil, err
		}

		// Get notebook info
		var title string
		err := db.QueryRow(ctx.Context, `SELECT title FROM notebooks WHERE id = $1`, params.NotebookID).Scan(&title)
		if err != nil {
			return nil, fmt.Errorf("notebook not found: %w", err)
		}

		// Get cells
		rows, err := db.Query(ctx.Context, `SELECT c.id, c.type, c.language, c.source, c.position, COALESCE(c.metadata->'chart'->>'chartType', '') FROM cells c WHERE notebook_id = $1 ORDER BY position LIMIT $2`, params.NotebookID, params.MaxCells)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var result strings.Builder
		result.WriteString(fmt.Sprintf("Notebook: %q\n", title))
		result.WriteString(fmt.Sprintf("Cell link format: /notebooks/%s#cell-{cell_id}\n", params.NotebookID))
		result.WriteString(fmt.Sprintf("Cells (showing up to %d):\n\n", params.MaxCells))

		cellNum := 0
		for rows.Next() {
			cellNum++
			var id, cellType, lang, source, chartType string
			var pos int
			if err := rows.Scan(&id, &cellType, &lang, &source, &pos, &chartType); err != nil {
				return nil, fmt.Errorf("scan cell: %w", err)
			}

			header := fmt.Sprintf("--- Cell %d (%s, %s, id=%s) ---\n", cellNum, cellType, lang, id)
			if chartType != "" {
				header = fmt.Sprintf("--- Cell %d (%s, %s, chart=%s, id=%s) ---\n", cellNum, cellType, lang, chartType, id)
			}
			result.WriteString(header)
			if len(source) > 2000 {
				result.WriteString(source[:2000] + "\n... (truncated)\n\n")
			} else {
				result.WriteString(source + "\n\n")
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list cells iter: %w", err)
		}

		// Check if there are more cells
		var totalCount int
		if err := db.QueryRow(ctx.Context, `SELECT COUNT(*) FROM cells WHERE notebook_id = $1`, params.NotebookID).Scan(&totalCount); err != nil {
			slog.Warn("count cells", "error", err)
		}
		if totalCount > params.MaxCells {
			result.WriteString(fmt.Sprintf("\n... and %d more cells (truncated)\n", totalCount-params.MaxCells))
		}

		return map[string]string{"content": result.String()}, nil
	}
}

func makeCreateSnapshotHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID string `json:"notebook_id"`
			Name       string `json:"name"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.Name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if req.NotebookID == "" {
			req.NotebookID = ctx.NotebookID
		}
		if req.NotebookID == "" {
			return nil, fmt.Errorf("notebook_id is required")
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "edit"); err != nil {
			return nil, err
		}

		snap, err := CreateNotebookSnapshot(ctx.Context, db, req.NotebookID, ctx.OrgID, req.Name, ctx.UserID, false)
		if err != nil {
			return nil, fmt.Errorf("create snapshot: %w", err)
		}

		_ = ctx.AuditLog("snapshot.create", "notebook", req.NotebookID)

		return map[string]any{
			"snapshot_id": snap.ID,
			"name":        snap.Name,
			"created_at":  snap.CreatedAt,
			"cell_count":  len(snap.Cells),
		}, nil
	}
}

func makeListSnapshotsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID string `json:"notebook_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.NotebookID == "" {
			req.NotebookID = ctx.NotebookID
		}
		if req.NotebookID == "" {
			return nil, fmt.Errorf("notebook_id is required")
		}
		if err := ctx.CheckPermission("notebook", req.NotebookID, "view"); err != nil {
			return nil, err
		}

		rows, err := db.Query(ctx.Context,
			`SELECT ns.id, ns.name, ns.title, ns.created_by_name, ns.created_at, ns.auto,
			        u.id, u.name, u.email
			 FROM notebook_snapshots ns
			 LEFT JOIN users u ON u.id = ns.created_by
			 WHERE ns.notebook_id=$1
			 ORDER BY ns.created_at DESC`,
			req.NotebookID,
		)
		if err != nil {
			return nil, fmt.Errorf("list snapshots: %w", err)
		}
		defer rows.Close()

		var snapshots []map[string]any
		for rows.Next() {
			var id, name, title, createdByName string
			var createdAt time.Time
			var auto bool
			var uID, uName, uEmail *string
			if err := rows.Scan(&id, &name, &title, &createdByName, &createdAt, &auto,
				&uID, &uName, &uEmail); err != nil {
				return nil, fmt.Errorf("scan snapshot: %w", err)
			}
			userName := createdByName
			if uName != nil && *uName != "" {
				userName = *uName
			}
			snapshots = append(snapshots, map[string]any{
				"id":         id,
				"name":       name,
				"title":      title,
				"created_by": userName,
				"created_at": createdAt.Format(time.RFC3339),
				"auto":       auto,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list snapshots iter: %w", err)
		}
		if snapshots == nil {
			snapshots = []map[string]any{}
		}

		return map[string]any{"snapshots": snapshots, "count": len(snapshots)}, nil
	}
}

func makeRestoreSnapshotHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			SnapshotID string `json:"snapshot_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.SnapshotID == "" {
			return nil, fmt.Errorf("snapshot_id is required")
		}

		// Get notebook ID from the snapshot
		var nbID string
		err := db.QueryRow(ctx.Context,
			`SELECT notebook_id FROM notebook_snapshots WHERE id=$1`, req.SnapshotID,
		).Scan(&nbID)
		if err != nil {
			return nil, fmt.Errorf("snapshot not found")
		}

		if err := ctx.CheckPermission("notebook", nbID, "edit"); err != nil {
			return nil, err
		}

		if err := RestoreNotebookSnapshot(ctx.Context, db, nbID, req.SnapshotID, ctx.OrgID, ctx.UserID, nil); err != nil {
			return nil, fmt.Errorf("restore snapshot: %w", err)
		}

		_ = ctx.AuditLog("snapshot.restore", "notebook", nbID)

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(nbID, map[string]any{
				"type":       "notebook_refresh",
				"reason":     "snapshot_restore",
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"notebook_id": nbID, "status": "restored"}, nil
	}
}

func makeDeleteNotebookHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID string `json:"notebook_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.NotebookID == "" {
			return nil, fmt.Errorf("notebook_id is required")
		}

		if err := ctx.CheckPermission("notebook", req.NotebookID, "delete"); err != nil {
			return nil, err
		}

		// Auto-snapshot before destructive action
		go EnsureAutoSnapshot(context.Background(), db, req.NotebookID, ctx.UserID, ctx.OrgID)

		result, err := db.Exec(ctx.Context,
			`DELETE FROM notebooks WHERE id = $1 AND org_id = $2`,
			req.NotebookID, ctx.OrgID,
		)
		if err != nil {
			return nil, fmt.Errorf("delete notebook: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, fmt.Errorf("notebook not found")
		}

		_ = ctx.AuditLog("notebook.delete", "notebook", req.NotebookID)

		return map[string]any{"notebook_id": req.NotebookID, "status": "deleted"}, nil
	}
}

func makeUpdateNotebookHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			NotebookID  string  `json:"notebook_id"`
			Title       *string `json:"title"`
			Description *string `json:"description"`
			ConnectorID *string `json:"connector_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.NotebookID == "" {
			return nil, fmt.Errorf("notebook_id is required")
		}

		if err := ctx.CheckPermission("notebook", req.NotebookID, "edit"); err != nil {
			return nil, err
		}

		_, err := db.Exec(ctx.Context, `
			UPDATE notebooks SET
				title = COALESCE($2, title),
				description = COALESCE($3, description),
				connector_id = COALESCE($4, connector_id),
				updated_at = NOW()
			WHERE id = $1 AND org_id = $5
		`, req.NotebookID, req.Title, req.Description, req.ConnectorID, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("update notebook: %w", err)
		}

		_ = ctx.AuditLog("notebook.update", "notebook", req.NotebookID)

		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(req.NotebookID, map[string]any{
				"type":       "notebook_refresh",
				"reason":     "notebook_updated",
				"user_email": "agent@aether",
			})
		}

		return map[string]any{"notebook_id": req.NotebookID, "status": "updated"}, nil
	}
}

func makeCreateNotebookHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Title       string  `json:"title"`
			Description string  `json:"description"`
			FolderID    *string `json:"folder_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.Title == "" {
			return nil, fmt.Errorf("title is required")
		}

		now := time.Now()
		id := uuid.New().String()

		var folderID *string
		if req.FolderID != nil && *req.FolderID != "" {
			folderID = req.FolderID
		}

		_, err := db.Exec(ctx.Context, `
			INSERT INTO notebooks (id, org_id, title, description, created_by, folder_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		`, id, ctx.OrgID, req.Title, req.Description, ctx.UserID, folderID, now)
		if err != nil {
			return nil, fmt.Errorf("create notebook: %w", err)
		}

		// Set default connector if one exists
		var defaultID string
		if err := db.QueryRow(ctx.Context,
			`SELECT id FROM connectors WHERE org_id=$1 AND is_default=true LIMIT 1`, ctx.OrgID,
		).Scan(&defaultID); err == nil {
			if _, err := db.Exec(ctx.Context, `UPDATE notebooks SET connector_id=$1 WHERE id=$2`, defaultID, id); err != nil {
				slog.Warn("set default connector", "notebook_id", id, "error", err)
			}
		}

		_, aclErr := db.Exec(ctx.Context, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view','run','edit','share','delete','create']),
			       ($1, 'notebook', $2::uuid, 'org_role', 'admin', ARRAY['view','run','edit','share','delete','create'])
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
			ctx.OrgID, id, ctx.UserID,
		)
		if aclErr != nil {
			slog.Warn("seed ACL entries for notebook", "notebook_id", id, "error", aclErr)
		}

		_ = ctx.AuditLog("notebook.create", "notebook", id)

		return map[string]any{
			"notebook_id": id,
			"title":       req.Title,
			"description": req.Description,
		}, nil
	}
}
