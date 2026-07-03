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
			Description: "Turn a cell's table output into a chart. For axis-based charts (bar, line, area, scatter), series are derived from y_columns. When group_by is set, one series is created per unique group value (cross-product with y_columns if multiple). Use series_colors to set colors per series. For map charts, use lat_column and lon_column instead of x_column/y_columns.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","stacked_bar","line","area","scatter","pie","donut","timeline","hierarchy_tree","big_number","map","sankey"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"lat_column":{"type":"string","description":"Latitude column (map charts only)"},"lon_column":{"type":"string","description":"Longitude column (map charts only)"},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"value_column":{"type":"string"},"layout":{"type":"string","enum":["top-down","left-to-right"]},"show_labels":{"type":"boolean"},"show_legend":{"type":"boolean"},"show_grid":{"type":"boolean"},"skip_empty":{"type":"boolean"},"max_label_length":{"type":"number"},"show_connectors":{"type":"boolean"},"show_time_deltas":{"type":"boolean"},"decimal_places":{"type":"number"},"label":{"type":"string"},"prefix":{"type":"string"},"suffix":{"type":"string"},"series_colors":{"type":"object","description":"Map of series names to hex colors. Keys match y_columns (no group_by) or group values (with group_by). E.g. {\"revenue\":\"#ff0000\"}"},"data_zoom":{"type":"boolean"},"smooth":{"type":"boolean"},"connect_nulls":{"type":"boolean"},"bar_width":{"type":"string"},"bar_category_gap":{"type":"string"},"rose_type":{"type":"string","enum":["radius","area"]},"start_angle":{"type":"number"},"pad_angle":{"type":"number"},"node_align":{"type":"string","enum":["justify","left","right"]},"node_width":{"type":"number"},"node_gap":{"type":"number"},"color_column":{"type":"string"},"size_column":{"type":"string"}},"required":["cell_id","chart_type"]}`,
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
			Description: "Modify chart config on an existing cell. For axis-based charts (bar, line, area, scatter), series are derived from y_columns. When group_by is set, one series is created per unique group value (cross-product with y_columns if multiple). Colors in series_colors keys match y_columns (no group_by) or group values (with group_by). For timeline, series_colors keys match group values. For map charts, use lat_column and lon_column instead of x_column/y_columns.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","stacked_bar","line","area","scatter","pie","donut","timeline","hierarchy_tree","big_number","map","sankey"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"lat_column":{"type":"string","description":"Latitude column (map charts only)"},"lon_column":{"type":"string","description":"Longitude column (map charts only)"},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"value_column":{"type":"string"},"layout":{"type":"string","enum":["top-down","left-to-right"]},"show_labels":{"type":"boolean"},"show_legend":{"type":"boolean"},"show_grid":{"type":"boolean"},"skip_empty":{"type":"boolean"},"max_label_length":{"type":"number"},"show_connectors":{"type":"boolean"},"show_time_deltas":{"type":"boolean"},"decimal_places":{"type":"number"},"label":{"type":"string"},"prefix":{"type":"string"},"suffix":{"type":"string"},"series_colors":{"type":"object","description":"Map of series names to hex colors. Keys match y_columns (no group_by) or group values (with group_by). E.g. {\"revenue\":\"#ff0000\"}"},"data_zoom":{"type":"boolean"},"smooth":{"type":"boolean"},"connect_nulls":{"type":"boolean"},"bar_width":{"type":"string"},"bar_category_gap":{"type":"string"},"rose_type":{"type":"string","enum":["radius","area"]},"start_angle":{"type":"number"},"pad_angle":{"type":"number"},"node_align":{"type":"string","enum":["justify","left","right"]},"node_width":{"type":"number"},"node_gap":{"type":"number"},"color_column":{"type":"string"},"size_column":{"type":"string"}},"required":["cell_id"]}`,
		},
		Handler: makeUpdateChartHandler(db),
	})
}

var chartAllowedFields = map[string]map[string]bool{
	"line": {
		"title": true, "showLegend": true, "showGrid": true,
		"dataZoom": true, "showLabels": true, "smooth": true,
		"connectNulls": true, "seriesColors": true, "xAxis": true, "yAxis": true,
		"groupBy": true,
	},
	"area": {
		"title": true, "showLegend": true, "showGrid": true,
		"dataZoom": true, "showLabels": true, "smooth": true,
		"connectNulls": true, "seriesColors": true, "xAxis": true, "yAxis": true,
		"groupBy": true,
	},
	"bar": {
		"title": true, "showLegend": true, "showGrid": true,
		"dataZoom": true, "showLabels": true, "seriesColors": true,
		"xAxis": true, "yAxis": true, "barWidth": true, "barCategoryGap": true,
		"groupBy": true,
	},
	"stacked_bar": {
		"title": true, "showLegend": true, "showGrid": true,
		"dataZoom": true, "showLabels": true, "seriesColors": true,
		"xAxis": true, "yAxis": true, "barWidth": true, "barCategoryGap": true,
		"groupBy": true,
	},
	"scatter": {
		"title": true, "showLegend": true, "showGrid": true,
		"seriesColors": true, "colorColumn": true, "sizeColumn": true,
		"xAxis": true, "yAxis": true, "groupBy": true,
	},
	"pie": {
		"title": true, "showLegend": true, "showLabels": true,
		"labelColumn": true, "seriesColors": true, "xAxis": true, "yAxis": true,
		"roseType": true, "startAngle": true, "padAngle": true,
	},
	"donut": {
		"title": true, "showLegend": true, "showLabels": true,
		"labelColumn": true, "seriesColors": true, "xAxis": true, "yAxis": true,
		"roseType": true, "startAngle": true, "padAngle": true,
	},
	"timeline": {
		"title": true, "showLegend": true, "showGrid": true,
		"showLabels": true, "showConnectors": true, "showTimeDeltas": true,
		"maxLabelLength": true, "timeColumn": true, "endTimeColumn": true,
		"labelColumn": true, "groupBy": true, "seriesColors": true,
	},
	"hierarchy_tree": {
		"title": true, "seriesColors": true, "idColumn": true,
		"parentIdColumn": true, "labelColumn": true, "metricColumns": true,
		"layout": true,
	},
	"sankey": {
		"title": true, "seriesColors": true, "xAxis": true, "yAxis": true,
		"nodeAlign": true, "nodeWidth": true, "nodeGap": true,
	},
	"map": {
		"title": true, "showLabels": true, "seriesColors": true,
		"labelColumn": true, "xAxis": true, "yAxis": true,
	},
	"big_number": {
		"valueColumn": true, "label": true, "prefix": true, "suffix": true,
		"decimalPlaces": true, "skipEmpty": true, "seriesColors": true,
	},
}

func filterChartConfig(config map[string]any, chartType string) []string {
	var warnings []string
	allowed, ok := chartAllowedFields[chartType]
	if !ok {
		return warnings
	}
	for k := range config {
		if k == "chartType" || k == "created_at" {
			continue
		}
		if !allowed[k] {
			delete(config, k)
			warnings = append(warnings, fmt.Sprintf("field %q ignored — not supported by chart type %q", k, chartType))
		}
	}

	return warnings
}

func makeCreateChartHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID          string              `json:"cell_id"`
			ChartType       string              `json:"chart_type"`
			XColumn         string              `json:"x_column"`
			YColumns        []string            `json:"y_columns"`
			LatColumn       string              `json:"lat_column"`
			LonColumn       string              `json:"lon_column"`
			Title           string              `json:"title"`
			TimeColumn      string              `json:"time_column"`
			EndTimeColumn   string              `json:"end_time_column"`
			LabelColumn     string              `json:"label_column"`
			GroupBy         string              `json:"group_by"`
			IDColumn        string              `json:"id_column"`
			ParentIDColumn  string              `json:"parent_id_column"`
			MetricColumns   []string            `json:"metric_columns"`
			ValueColumn     string              `json:"value_column"`
			Layout          string              `json:"layout"`
			ShowLabels      *bool               `json:"show_labels"`
			ShowLegend      *bool               `json:"show_legend"`
			ShowGrid        *bool               `json:"show_grid"`
			SkipEmpty       *bool               `json:"skip_empty"`
			MaxLabelLength  *int                `json:"max_label_length"`
			ShowConnectors  *bool               `json:"show_connectors"`
			ShowTimeDeltas  *bool               `json:"show_time_deltas"`
			DecimalPlaces   *int                `json:"decimal_places"`
			ChartLabel      string              `json:"label"`
			Prefix          string              `json:"prefix"`
			Suffix          string              `json:"suffix"`
			SeriesColors    map[string]string   `json:"series_colors"`
			DataZoom        *bool               `json:"data_zoom"`
			Smooth          *bool               `json:"smooth"`
			ConnectNulls    *bool               `json:"connect_nulls"`
			BarWidth        string              `json:"bar_width"`
			BarCategoryGap  string              `json:"bar_category_gap"`
			RoseType        string              `json:"rose_type"`
			StartAngle      *int                `json:"start_angle"`
			PadAngle        *int                `json:"pad_angle"`
			NodeAlign       string              `json:"node_align"`
			NodeWidth       *int                `json:"node_width"`
			NodeGap         *int                `json:"node_gap"`
			ColorColumn     string              `json:"color_column"`
			SizeColumn      string              `json:"size_column"`
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
		req.CellID = resolved.ID

		chartConfig := map[string]any{
			"chartType":      req.ChartType,
			"xAxis":          req.XColumn,
			"yAxis":          req.YColumns,
			"title":          req.Title,
			"timeColumn":     req.TimeColumn,
			"endTimeColumn":  req.EndTimeColumn,
			"labelColumn":    req.LabelColumn,
			"idColumn":       req.IDColumn,
			"parentIdColumn": req.ParentIDColumn,
			"metricColumns":  req.MetricColumns,
			"layout":         req.Layout,
			"created_at":     time.Now().Format(time.RFC3339),
		}
		if req.ValueColumn != "" {
			chartConfig["valueColumn"] = req.ValueColumn
		}
		if req.GroupBy != "" {
			chartConfig["groupBy"] = req.GroupBy
		}
		if req.ShowLabels != nil {
			chartConfig["showLabels"] = *req.ShowLabels
		}
		if req.ShowLegend != nil {
			chartConfig["showLegend"] = *req.ShowLegend
		}
		if req.ShowGrid != nil {
			chartConfig["showGrid"] = *req.ShowGrid
		}
		if req.SkipEmpty != nil {
			chartConfig["skipEmpty"] = *req.SkipEmpty
		}
		if req.MaxLabelLength != nil {
			chartConfig["maxLabelLength"] = *req.MaxLabelLength
		}
		if req.ShowConnectors != nil {
			chartConfig["showConnectors"] = *req.ShowConnectors
		}
		if req.ShowTimeDeltas != nil {
			chartConfig["showTimeDeltas"] = *req.ShowTimeDeltas
		}
		if req.DecimalPlaces != nil {
			chartConfig["decimalPlaces"] = *req.DecimalPlaces
		}
		if req.ChartLabel != "" {
			chartConfig["label"] = req.ChartLabel
		}
		if req.Prefix != "" {
			chartConfig["prefix"] = req.Prefix
		}
		if req.Suffix != "" {
			chartConfig["suffix"] = req.Suffix
		}
		if req.SeriesColors != nil {
			chartConfig["seriesColors"] = req.SeriesColors
		}
		if req.DataZoom != nil {
			chartConfig["dataZoom"] = *req.DataZoom
		}
		if req.Smooth != nil {
			chartConfig["smooth"] = *req.Smooth
		}
		if req.ConnectNulls != nil {
			chartConfig["connectNulls"] = *req.ConnectNulls
		}
		if req.BarWidth != "" {
			chartConfig["barWidth"] = req.BarWidth
		}
		if req.BarCategoryGap != "" {
			chartConfig["barCategoryGap"] = req.BarCategoryGap
		}
		if req.RoseType != "" {
			chartConfig["roseType"] = req.RoseType
		}
		if req.StartAngle != nil {
			chartConfig["startAngle"] = *req.StartAngle
		}
		if req.PadAngle != nil {
			chartConfig["padAngle"] = *req.PadAngle
		}
		if req.NodeAlign != "" {
			chartConfig["nodeAlign"] = req.NodeAlign
		}
		if req.NodeWidth != nil {
			chartConfig["nodeWidth"] = *req.NodeWidth
		}
		if req.NodeGap != nil {
			chartConfig["nodeGap"] = *req.NodeGap
		}
		if req.ColorColumn != "" {
			chartConfig["colorColumn"] = req.ColorColumn
		}
		if req.SizeColumn != "" {
			chartConfig["sizeColumn"] = req.SizeColumn
		}
		if req.LatColumn != "" {
			chartConfig["yAxis"] = []string{req.LatColumn}
		}
		if req.LonColumn != "" {
			chartConfig["xAxis"] = req.LonColumn
		}

		warnings := filterChartConfig(chartConfig, req.ChartType)

		configJSON, _ := json.Marshal(chartConfig)

		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET metadata = jsonb_set(COALESCE(metadata, '{}'), '{chart}', $1), updated_at = NOW()
			WHERE id = $2
		`, configJSON, req.CellID)
		if err != nil {
			return nil, fmt.Errorf("create chart: %w", err)
		}

		// Notify agent panel to scroll
		ctx.EmitCellUpdated(req.CellID, "")

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			var updatedMetadata []byte
			db.QueryRow(ctx.Context, `SELECT metadata FROM cells WHERE id = $1`, req.CellID).Scan(&updatedMetadata)
			var metadataMap map[string]any
			if updatedMetadata != nil {
				json.Unmarshal(updatedMetadata, &metadataMap)
			}
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":       "cell_metadata_changed",
				"cell_id":    req.CellID,
				"metadata":   metadataMap,
				"user_email": "agent@aether",
			})
		}

		result := map[string]any{"cell_id": req.CellID, "chart_type": req.ChartType}
		if len(warnings) > 0 {
			result["config_warnings"] = warnings
		}
		return result, nil
	}
}

func makeUpdateChartHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID          string              `json:"cell_id"`
			ChartType       string              `json:"chart_type"`
			XColumn         string              `json:"x_column"`
			YColumns        []string            `json:"y_columns"`
			LatColumn       string              `json:"lat_column"`
			LonColumn       string              `json:"lon_column"`
			Title           string              `json:"title"`
			TimeColumn      string              `json:"time_column"`
			EndTimeColumn   string              `json:"end_time_column"`
			LabelColumn     string              `json:"label_column"`
			GroupBy         string              `json:"group_by"`
			IDColumn        string              `json:"id_column"`
			ParentIDColumn  string              `json:"parent_id_column"`
			MetricColumns   []string            `json:"metric_columns"`
			ValueColumn     string              `json:"value_column"`
			Layout          string              `json:"layout"`
			ShowLabels      *bool               `json:"show_labels"`
			ShowLegend      *bool               `json:"show_legend"`
			ShowGrid        *bool               `json:"show_grid"`
			SkipEmpty       *bool               `json:"skip_empty"`
			MaxLabelLength  *int                `json:"max_label_length"`
			ShowConnectors  *bool               `json:"show_connectors"`
			ShowTimeDeltas  *bool               `json:"show_time_deltas"`
			DecimalPlaces   *int                `json:"decimal_places"`
			ChartLabel      string              `json:"label"`
			Prefix          string              `json:"prefix"`
			Suffix          string              `json:"suffix"`
			SeriesColors    map[string]string   `json:"series_colors"`
			DataZoom        *bool               `json:"data_zoom"`
			Smooth          *bool               `json:"smooth"`
			ConnectNulls    *bool               `json:"connect_nulls"`
			BarWidth        string              `json:"bar_width"`
			BarCategoryGap  string              `json:"bar_category_gap"`
			RoseType        string              `json:"rose_type"`
			StartAngle      *int                `json:"start_angle"`
			PadAngle        *int                `json:"pad_angle"`
			NodeAlign       string              `json:"node_align"`
			NodeWidth       *int                `json:"node_width"`
			NodeGap         *int                `json:"node_gap"`
			ColorColumn     string              `json:"color_column"`
			SizeColumn      string              `json:"size_column"`
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

		var existingConfig map[string]any
		var chartJSON []byte
		err = db.QueryRow(ctx.Context, `SELECT metadata->'chart' FROM cells WHERE id = $1`, cellID).Scan(&chartJSON)
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
		if req.ValueColumn != "" {
			existingConfig["valueColumn"] = req.ValueColumn
		}
		if req.Layout != "" {
			existingConfig["layout"] = req.Layout
		}
		if req.ShowLabels != nil {
			existingConfig["showLabels"] = *req.ShowLabels
		}
		if req.ShowLegend != nil {
			existingConfig["showLegend"] = *req.ShowLegend
		}
		if req.ShowGrid != nil {
			existingConfig["showGrid"] = *req.ShowGrid
		}
		if req.SkipEmpty != nil {
			existingConfig["skipEmpty"] = *req.SkipEmpty
		}
		if req.MaxLabelLength != nil {
			existingConfig["maxLabelLength"] = *req.MaxLabelLength
		}
		if req.ShowConnectors != nil {
			existingConfig["showConnectors"] = *req.ShowConnectors
		}
		if req.ShowTimeDeltas != nil {
			existingConfig["showTimeDeltas"] = *req.ShowTimeDeltas
		}
		if req.DecimalPlaces != nil {
			existingConfig["decimalPlaces"] = *req.DecimalPlaces
		}
		if req.ChartLabel != "" {
			existingConfig["label"] = req.ChartLabel
		}
		if req.Prefix != "" {
			existingConfig["prefix"] = req.Prefix
		}
		if req.Suffix != "" {
			existingConfig["suffix"] = req.Suffix
		}
		if req.SeriesColors != nil {
			existingConfig["seriesColors"] = req.SeriesColors
		}
		if req.DataZoom != nil {
			existingConfig["dataZoom"] = *req.DataZoom
		}
		if req.Smooth != nil {
			existingConfig["smooth"] = *req.Smooth
		}
		if req.ConnectNulls != nil {
			existingConfig["connectNulls"] = *req.ConnectNulls
		}
		if req.BarWidth != "" {
			existingConfig["barWidth"] = req.BarWidth
		}
		if req.BarCategoryGap != "" {
			existingConfig["barCategoryGap"] = req.BarCategoryGap
		}
		if req.RoseType != "" {
			existingConfig["roseType"] = req.RoseType
		}
		if req.StartAngle != nil {
			existingConfig["startAngle"] = *req.StartAngle
		}
		if req.PadAngle != nil {
			existingConfig["padAngle"] = *req.PadAngle
		}
		if req.NodeAlign != "" {
			existingConfig["nodeAlign"] = req.NodeAlign
		}
		if req.NodeWidth != nil {
			existingConfig["nodeWidth"] = *req.NodeWidth
		}
		if req.NodeGap != nil {
			existingConfig["nodeGap"] = *req.NodeGap
		}
		if req.ColorColumn != "" {
			existingConfig["colorColumn"] = req.ColorColumn
		}
		if req.SizeColumn != "" {
			existingConfig["sizeColumn"] = req.SizeColumn
		}
		if req.LatColumn != "" {
			existingConfig["yAxis"] = []string{req.LatColumn}
		}
		if req.LonColumn != "" {
			existingConfig["xAxis"] = req.LonColumn
		}

		var warnings []string
		ct, _ := existingConfig["chartType"].(string)
		if ct != "" {
			warnings = filterChartConfig(existingConfig, ct)
		}

		configJSON, _ := json.Marshal(existingConfig)
		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET metadata = jsonb_set(COALESCE(metadata, '{}'), '{chart}', $1), updated_at = NOW()
			WHERE id = $2
		`, configJSON, cellID)
		if err != nil {
			return nil, fmt.Errorf("update chart: %w", err)
		}

		// Notify agent panel to scroll
		ctx.EmitCellUpdated(cellID, "")

		// Broadcast to all notebook viewers via WebSocket
		if ctx.BroadcastFunc != nil {
			var updatedMetadata []byte
			db.QueryRow(ctx.Context, `SELECT metadata FROM cells WHERE id = $1`, cellID).Scan(&updatedMetadata)
			var metadataMap map[string]any
			if updatedMetadata != nil {
				json.Unmarshal(updatedMetadata, &metadataMap)
			}
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":       "cell_metadata_changed",
				"cell_id":    cellID,
				"metadata":   metadataMap,
				"user_email": "agent@aether",
			})
		}

		result := map[string]any{"cell_id": cellID}
		if len(warnings) > 0 {
			result["config_warnings"] = warnings
		}
		return result, nil
	}
}
