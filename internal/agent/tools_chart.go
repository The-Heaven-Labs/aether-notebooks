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
			Description: "Turn a cell's table output into a chart. For bar/line/area/scatter: set x_column (categories) and y_columns (values); optional group_by splits into series. Bar supports bar_mode (grouped|stacked|horizontal) for layout. Area supports area_mode (area|stacked) for layout. For pie/donut: x_column or label_column for slices, y_columns for metric. For timeline: time_column, label_column, group_by. For hierarchy_tree: id_column, parent_id_column, metric_columns. For big_number: value_column, optional label/prefix/suffix. For map: x_column=longitude, y_columns[0]=latitude. For sankey: x_column=source, y_columns=[target,value]. For funnel: category_column (stage labels) and value_column (stage values); optional funnel_sort. For heatmap: x_column (x categories), y_axis_column (y categories), value_column (intensity). For histogram: value_column (numeric column to bin); optional bin_count. All charts accept: title, show_labels, show_legend, show_grid, skip_empty, series_colors. Bar/line/area/scatter also accept: mark_lines (array of {value, label, position: 'horizontal'|'vertical', color}) for dashed reference lines.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","line","area","scatter","pie","donut","timeline","hierarchy_tree","big_number","map","sankey","funnel","heatmap","histogram"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"lat_column":{"type":"string","description":"Latitude column (map charts only)"},"lon_column":{"type":"string","description":"Longitude column (map charts only)"},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"value_column":{"type":"string"},"category_column":{"type":"string","description":"Category/label column for funnel charts"},"y_axis_column":{"type":"string","description":"Second category column for heatmap (y-axis categories)"},"funnel_sort":{"type":"string","enum":["ascending","descending","none"],"description":"Sort order for funnel stages"},"bin_count":{"type":"number","description":"Number of bins for histogram"},"bar_mode":{"type":"string","enum":["grouped","stacked","horizontal"],"description":"Bar chart layout: grouped (side-by-side), stacked (totals), horizontal (left-to-right)"},"area_mode":{"type":"string","enum":["area","stacked"],"description":"Area chart layout: area (overlapping), stacked (stacked areas)"},"layout":{"type":"string","enum":["top-down","left-to-right"]},"show_labels":{"type":"boolean"},"show_legend":{"type":"boolean"},"show_grid":{"type":"boolean"},"skip_empty":{"type":"boolean"},"max_label_length":{"type":"number"},"show_connectors":{"type":"boolean"},"show_time_deltas":{"type":"boolean"},"decimal_places":{"type":"number"},"label":{"type":"string"},"prefix":{"type":"string"},"suffix":{"type":"string"},"series_colors":{"type":"object","description":"Map of series names to hex colors. Keys match y_columns (no group_by) or group values (with group_by). E.g. {\"revenue\":\"#ff0000\"}"},"data_zoom":{"type":"boolean"},"smooth":{"type":"boolean"},"connect_nulls":{"type":"boolean"},"bar_width":{"type":"string"},"bar_category_gap":{"type":"string"},"rose_type":{"type":"string","enum":["radius","area"]},"start_angle":{"type":"number"},"pad_angle":{"type":"number"},"node_align":{"type":"string","enum":["justify","left","right"]},"node_width":{"type":"number"},"node_gap":{"type":"number"},"mark_lines":{"type":"array","items":{"type":"object","properties":{"value":{"type":"string"},"label":{"type":"string"},"position":{"type":"string","enum":["horizontal","vertical"]},"color":{"type":"string"}}},"description":"Reference lines for bar/line/area/scatter charts"},"color_column":{"type":"string"},"size_column":{"type":"string"}},"required":["cell_id","chart_type"]}`,
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
			Description: "Modify chart config on an existing cell. Supports the same chart types and parameters as create_chart. For bar/line/area/scatter: x_column, y_columns, group_by. Bar supports bar_mode (grouped|stacked|horizontal) for layout. Area supports area_mode (area|stacked) for layout. For pie/donut: x_column or label_column, y_columns. For timeline: time_column, label_column, group_by. For hierarchy_tree: id_column, parent_id_column, metric_columns. For big_number: value_column, label/prefix/suffix. For map: x_column=longitude, y_columns[0]=latitude. For sankey: x_column=source, y_columns=[target,value]. For funnel: category_column, value_column, funnel_sort. For heatmap: x_column, y_axis_column, value_column. For histogram: value_column, bin_count. All charts accept: title, show_labels, show_legend, show_grid, skip_empty, series_colors. Bar/line/area/scatter also accept: mark_lines (array of {value, label, position: 'horizontal'|'vertical', color}) for dashed reference lines.",
			Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","line","area","scatter","pie","donut","timeline","hierarchy_tree","big_number","map","sankey","funnel","heatmap","histogram"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"lat_column":{"type":"string","description":"Latitude column (map charts only)"},"lon_column":{"type":"string","description":"Longitude column (map charts only)"},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"value_column":{"type":"string"},"category_column":{"type":"string","description":"Category/label column for funnel charts"},"y_axis_column":{"type":"string","description":"Second category column for heatmap (y-axis categories)"},"funnel_sort":{"type":"string","enum":["ascending","descending","none"],"description":"Sort order for funnel stages"},"bin_count":{"type":"number","description":"Number of bins for histogram"},"bar_mode":{"type":"string","enum":["grouped","stacked","horizontal"],"description":"Bar chart layout: grouped (side-by-side), stacked (totals), horizontal (left-to-right)"},"area_mode":{"type":"string","enum":["area","stacked"],"description":"Area chart layout: area (overlapping), stacked (stacked areas)"},"layout":{"type":"string","enum":["top-down","left-to-right"]},"show_labels":{"type":"boolean"},"show_legend":{"type":"boolean"},"show_grid":{"type":"boolean"},"skip_empty":{"type":"boolean"},"max_label_length":{"type":"number"},"show_connectors":{"type":"boolean"},"show_time_deltas":{"type":"boolean"},"decimal_places":{"type":"number"},"label":{"type":"string"},"prefix":{"type":"string"},"suffix":{"type":"string"},"series_colors":{"type":"object","description":"Map of series names to hex colors. Keys match y_columns (no group_by) or group values (with group_by). E.g. {\"revenue\":\"#ff0000\"}"},"data_zoom":{"type":"boolean"},"smooth":{"type":"boolean"},"connect_nulls":{"type":"boolean"},"bar_width":{"type":"string"},"bar_category_gap":{"type":"string"},"rose_type":{"type":"string","enum":["radius","area"]},"start_angle":{"type":"number"},"pad_angle":{"type":"number"},"node_align":{"type":"string","enum":["justify","left","right"]},"node_width":{"type":"number"},"node_gap":{"type":"number"},"mark_lines":{"type":"array","items":{"type":"object","properties":{"value":{"type":"string"},"label":{"type":"string"},"position":{"type":"string","enum":["horizontal","vertical"]},"color":{"type":"string"}}},"description":"Reference lines for bar/line/area/scatter charts"},"color_column":{"type":"string"},"size_column":{"type":"string"}},"required":["cell_id"]}`,
		},
		Handler: makeUpdateChartHandler(db),
	})
}

var chartAllowedFields = map[string]map[string]bool{
	"line": {
			"title": true, "showLegend": true, "showGrid": true,
			"dataZoom": true, "showLabels": true, "smooth": true,
			"connectNulls": true, "seriesColors": true, "xAxis": true, "yAxis": true,
			"groupBy": true, "markLines": true,
		},
		"area": {
			"title": true, "showLegend": true, "showGrid": true,
			"dataZoom": true, "showLabels": true, "smooth": true,
			"connectNulls": true, "seriesColors": true, "xAxis": true, "yAxis": true,
			"groupBy": true, "areaMode": true, "markLines": true,
		},
		"bar": {
			"title": true, "showLegend": true, "showGrid": true,
			"dataZoom": true, "showLabels": true, "seriesColors": true,
			"xAxis": true, "yAxis": true, "barWidth": true, "barCategoryGap": true,
			"groupBy": true, "barMode": true, "markLines": true,
		},
		"scatter": {
			"title": true, "showLegend": true, "showGrid": true,
			"seriesColors": true, "colorColumn": true, "sizeColumn": true,
			"xAxis": true, "yAxis": true, "groupBy": true, "markLines": true,
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
	"funnel": {
		"title": true, "showLabels": true, "categoryColumn": true,
		"valueColumn": true, "xAxis": true, "yAxis": true,
		"funnelSort": true, "suffix": true, "skipEmpty": true,
	},
	"heatmap": {
		"title": true, "showLabels": true, "xAxis": true,
		"yAxisColumn": true, "valueColumn": true, "yAxis": true,
	},
	"histogram": {
		"title": true, "showLabels": true, "showGrid": true,
		"valueColumn": true, "yAxis": true, "binCount": true,
		"seriesColors": true,
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
			CategoryColumn  string              `json:"category_column"`
			YAxisColumn     string              `json:"y_axis_column"`
			FunnelSort      string              `json:"funnel_sort"`
			BinCount        *int                `json:"bin_count"`
			BarMode         string              `json:"bar_mode"`
			AreaMode        string              `json:"area_mode"`
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
			MarkLines       []map[string]any    `json:"mark_lines"`
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
		if req.CategoryColumn != "" {
			chartConfig["categoryColumn"] = req.CategoryColumn
		}
		if req.YAxisColumn != "" {
			chartConfig["yAxisColumn"] = req.YAxisColumn
		}
		if req.FunnelSort != "" {
			chartConfig["funnelSort"] = req.FunnelSort
		}
		if req.BinCount != nil {
			chartConfig["binCount"] = *req.BinCount
		}
		if req.BarMode != "" {
			chartConfig["barMode"] = req.BarMode
		}
		if req.AreaMode != "" {
			chartConfig["areaMode"] = req.AreaMode
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
		if len(req.MarkLines) > 0 {
			chartConfig["markLines"] = req.MarkLines
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
			CategoryColumn  string              `json:"category_column"`
			YAxisColumn     string              `json:"y_axis_column"`
			FunnelSort      string              `json:"funnel_sort"`
			BinCount        *int                `json:"bin_count"`
			BarMode         string              `json:"bar_mode"`
			AreaMode        string              `json:"area_mode"`
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
			MarkLines       []map[string]any    `json:"mark_lines"`
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
		if req.CategoryColumn != "" {
			existingConfig["categoryColumn"] = req.CategoryColumn
		}
		if req.YAxisColumn != "" {
			existingConfig["yAxisColumn"] = req.YAxisColumn
		}
		if req.FunnelSort != "" {
			existingConfig["funnelSort"] = req.FunnelSort
		}
		if req.BinCount != nil {
			existingConfig["binCount"] = *req.BinCount
		}
		if req.BarMode != "" {
			existingConfig["barMode"] = req.BarMode
		}
		if req.AreaMode != "" {
			existingConfig["areaMode"] = req.AreaMode
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
		if len(req.MarkLines) > 0 {
			existingConfig["markLines"] = req.MarkLines
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
