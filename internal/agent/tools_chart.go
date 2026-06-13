package agent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterChartTools(reg *ToolRegistry, db *pgxpool.Pool) {
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_chart",
			Description: "Turn a cell's table output into a chart",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","stacked_bar","line","area","scatter","pie","donut","timeline","hierarchy_tree"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"layout":{"type":"string","enum":["top-down","left-to-right"]}},"required":["cell_id","chart_type"]}`,
		},
		Handler: makeCreateChartHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_chart",
			Description: "Modify chart config on an existing cell",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string"},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"layout":{"type":"string","enum":["top-down","left-to-right"]}},"required":["cell_id"]}`,
		},
		Handler: makeUpdateChartHandler(db),
	})
}

func makeCreateChartHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID         string   `json:"cell_id"`
			ChartType      string   `json:"chart_type"`
			XColumn        string   `json:"x_column"`
			YColumns       []string `json:"y_columns"`
			Title          string   `json:"title"`
			TimeColumn     string   `json:"time_column"`
			EndTimeColumn  string   `json:"end_time_column"`
			LabelColumn    string   `json:"label_column"`
			GroupBy        string   `json:"group_by"`
			IDColumn       string   `json:"id_column"`
			ParentIDColumn string   `json:"parent_id_column"`
			MetricColumns  []string `json:"metric_columns"`
			Layout         string   `json:"layout"`
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

		chartConfig := map[string]any{
			"chartType":      req.ChartType,
			"xAxis":          req.XColumn,
			"yAxis":          req.YColumns,
			"title":          req.Title,
			"timeColumn":     req.TimeColumn,
			"endTimeColumn":  req.EndTimeColumn,
			"labelColumn":    req.LabelColumn,
			"groupBy":        req.GroupBy,
			"idColumn":       req.IDColumn,
			"parentIdColumn": req.ParentIDColumn,
			"metricColumns":  req.MetricColumns,
			"layout":         req.Layout,
			"created_at":     time.Now().Format(time.RFC3339),
		}

		configJSON, _ := json.Marshal(chartConfig)

		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET metadata = jsonb_set(COALESCE(metadata, '{}'), '{chart}', $1), updated_at = NOW()
			WHERE id = $2
		`, configJSON, req.CellID)
		if err != nil {
			return nil, fmt.Errorf("create chart: %w", err)
		}

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			var updatedMetadata []byte
			db.QueryRow(ctx.Context, `SELECT metadata FROM cells WHERE id = $1`, req.CellID).Scan(&updatedMetadata)
			var metadataMap map[string]any
			if updatedMetadata != nil {
				json.Unmarshal(updatedMetadata, &metadataMap)
			}
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":     "cell_metadata_changed",
				"cell_id":  req.CellID,
				"metadata": metadataMap,
			})
		}

		return map[string]any{"cell_id": req.CellID, "chart_type": req.ChartType}, nil
	}
}

func makeUpdateChartHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID         string   `json:"cell_id"`
			ChartType      string   `json:"chart_type"`
			XColumn        string   `json:"x_column"`
			YColumns       []string `json:"y_columns"`
			Title          string   `json:"title"`
			TimeColumn     string   `json:"time_column"`
			EndTimeColumn  string   `json:"end_time_column"`
			LabelColumn    string   `json:"label_column"`
			GroupBy        string   `json:"group_by"`
			IDColumn       string   `json:"id_column"`
			ParentIDColumn string   `json:"parent_id_column"`
			MetricColumns  []string `json:"metric_columns"`
			Layout         string   `json:"layout"`
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

		var existingConfig map[string]any
		var chartJSON []byte
		err = db.QueryRow(ctx.Context, `SELECT metadata->'chart' FROM cells WHERE id = $1`, req.CellID).Scan(&chartJSON)
		if err == nil && chartJSON != nil {
			json.Unmarshal(chartJSON, &existingConfig)
		}
		if existingConfig == nil {
			existingConfig = make(map[string]any)
		}

		if req.ChartType != "" {
			existingConfig["chartType"] = req.ChartType
		}
		if req.XColumn != "" {
			existingConfig["xAxis"] = req.XColumn
		}
		if req.YColumns != nil {
			existingConfig["yAxis"] = req.YColumns
		}
		if req.Title != "" {
			existingConfig["title"] = req.Title
		}
		if req.TimeColumn != "" {
			existingConfig["timeColumn"] = req.TimeColumn
		}
		if req.EndTimeColumn != "" {
			existingConfig["endTimeColumn"] = req.EndTimeColumn
		}
		if req.LabelColumn != "" {
			existingConfig["labelColumn"] = req.LabelColumn
		}
		if req.GroupBy != "" {
			existingConfig["groupBy"] = req.GroupBy
		}
		if req.IDColumn != "" {
			existingConfig["idColumn"] = req.IDColumn
		}
		if req.ParentIDColumn != "" {
			existingConfig["parentIdColumn"] = req.ParentIDColumn
		}
		if req.MetricColumns != nil {
			existingConfig["metricColumns"] = req.MetricColumns
		}
		if req.Layout != "" {
			existingConfig["layout"] = req.Layout
		}

		configJSON, _ := json.Marshal(existingConfig)
		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET metadata = jsonb_set(COALESCE(metadata, '{}'), '{chart}', $1), updated_at = NOW()
			WHERE id = $2
		`, configJSON, req.CellID)
		if err != nil {
			return nil, fmt.Errorf("update chart: %w", err)
		}

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			var updatedMetadata []byte
			db.QueryRow(ctx.Context, `SELECT metadata FROM cells WHERE id = $1`, req.CellID).Scan(&updatedMetadata)
			var metadataMap map[string]any
			if updatedMetadata != nil {
				json.Unmarshal(updatedMetadata, &metadataMap)
			}
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":     "cell_metadata_changed",
				"cell_id":  req.CellID,
				"metadata": metadataMap,
			})
		}

		return map[string]any{"cell_id": req.CellID}, nil
	}
}
