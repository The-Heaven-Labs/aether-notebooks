package validate

import (
	"fmt"
	"strings"
)

// Typed widget-config authoring for the agent tools (`set_widget_config`,
// `create_dashboard_widget`). The tool blocks speak the same snake_case
// vocabulary as `create_chart`; these converters produce the stored shapes the
// dashboard widgets render:
//
//   - chart → camelCase ChartConfig keys at the widget config root
//     (web/src/charts/types.ts is the source of truth; the mapping mirrors
//     internal/agent/tools_chart.go's create_chart handler)
//   - metric → `config.metric` (web/src/utils/metric.ts MetricConfig)
//   - table → `config.table.columns` (web/src/utils/tableConfig.ts)
//
// Converters omit unset fields so a typed block merges into an existing widget
// config without clobbering keys it did not mention. A zero-valued block is a
// no-op: the converter returns no keys at all.

// WidgetChartConfig is the `chart` block of a widget-config tool call.
type WidgetChartConfig struct {
	ChartType      string            `json:"chart_type"`
	XColumn        string            `json:"x_column"`
	YColumns       []string          `json:"y_columns"`
	LatColumn      string            `json:"lat_column"`
	LonColumn      string            `json:"lon_column"`
	Title          string            `json:"title"`
	TimeColumn     string            `json:"time_column"`
	EndTimeColumn  string            `json:"end_time_column"`
	LabelColumn    string            `json:"label_column"`
	GroupBy        string            `json:"group_by"`
	IDColumn       string            `json:"id_column"`
	ParentIDColumn string            `json:"parent_id_column"`
	MetricColumns  []string          `json:"metric_columns"`
	ValueColumn    string            `json:"value_column"`
	CategoryColumn string            `json:"category_column"`
	YAxisColumn    string            `json:"y_axis_column"`
	FunnelSort     string            `json:"funnel_sort"`
	BinCount       *int              `json:"bin_count"`
	BarMode        string            `json:"bar_mode"`
	AreaMode       string            `json:"area_mode"`
	Layout         string            `json:"layout"`
	ShowLabels     *bool             `json:"show_labels"`
	ShowLegend     *bool             `json:"show_legend"`
	ShowGrid       *bool             `json:"show_grid"`
	SkipEmpty      *bool             `json:"skip_empty"`
	MaxLabelLength *int              `json:"max_label_length"`
	ShowConnectors *bool             `json:"show_connectors"`
	ShowTimeDeltas *bool             `json:"show_time_deltas"`
	DecimalPlaces  *int              `json:"decimal_places"`
	Label          string            `json:"label"`
	Prefix         string            `json:"prefix"`
	Suffix         string            `json:"suffix"`
	SeriesColors   map[string]string `json:"series_colors"`
	DataZoom       *bool             `json:"data_zoom"`
	Smooth         *bool             `json:"smooth"`
	ConnectNulls   *bool             `json:"connect_nulls"`
	BarWidth       string            `json:"bar_width"`
	BarCategoryGap string            `json:"bar_category_gap"`
	RoseType       string            `json:"rose_type"`
	StartAngle     *int              `json:"start_angle"`
	PadAngle       *int              `json:"pad_angle"`
	NodeAlign      string            `json:"node_align"`
	NodeWidth      *int              `json:"node_width"`
	NodeGap        *int              `json:"node_gap"`
	ColorColumn    string            `json:"color_column"`
	SizeColumn     string            `json:"size_column"`
	MarkLines      []WidgetMarkLine  `json:"mark_lines"`
}

// WidgetMarkLine is one dashed reference line on an axis-based chart.
type WidgetMarkLine struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Position string `json:"position"` // horizontal | vertical
	Color    string `json:"color"`
}

// WidgetMetricConfig is the `metric` block of a widget-config tool call.
type WidgetMetricConfig struct {
	ValueColumn string                  `json:"value_column"`
	Aggregation string                  `json:"aggregation"`
	Label       string                  `json:"label"`
	Format      *WidgetValueFormat      `json:"format"`
	Comparison  *WidgetMetricComparison `json:"comparison"`
	Sparkline   *WidgetMetricSparkline  `json:"sparkline"`
}

// WidgetMetricComparison compares the metric against a second column.
type WidgetMetricComparison struct {
	Column string `json:"column"`
	Mode   string `json:"mode"` // delta | percent
}

// WidgetMetricSparkline plots a trend line under the metric value.
type WidgetMetricSparkline struct {
	XColumn string `json:"x_column"`
	YColumn string `json:"y_column"`
}

// WidgetValueFormat is a number/currency/percent format shared by metric
// values and table columns.
type WidgetValueFormat struct {
	Style    string `json:"style"`    // number | currency | percent
	Currency string `json:"currency"` // 3-letter code, e.g. USD
	Decimals *int   `json:"decimals"` // 0..6
	Compact  *bool  `json:"compact"`
}

// WidgetTableConfig is the `table` block of a widget-config tool call.
type WidgetTableConfig struct {
	Columns []WidgetTableColumn `json:"columns"`
}

// WidgetTableColumn is one per-column format entry.
type WidgetTableColumn struct {
	Name   string             `json:"name"`
	Format *WidgetValueFormat `json:"format"`
}

var widgetChartTypes = []string{
	"bar", "line", "area", "scatter", "pie", "donut", "timeline",
	"hierarchy_tree", "big_number", "map", "sankey", "funnel", "heatmap", "histogram",
}

var widgetMetricAggregations = []string{
	"none", "sum", "avg", "min", "max", "median", "count", "count_distinct", "first",
}

var widgetFormatStyles = []string{"number", "currency", "percent"}

func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(quoted, ", ")
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// ValidateWidgetChart checks a `chart` block: the chart type is supported and
// every value that would render incorrectly (missing required columns, unknown
// enum members, malformed colors, broken mark lines) is rejected with an
// actionable error.
func ValidateWidgetChart(cfg WidgetChartConfig) error {
	if cfg.ChartType == "" {
		return fmt.Errorf("chart_type is required")
	}
	if !containsString(widgetChartTypes, cfg.ChartType) {
		return fmt.Errorf("chart type %q is not supported; use one of %s", cfg.ChartType, quotedList(widgetChartTypes))
	}

	switch cfg.ChartType {
	case "map":
		if cfg.LatColumn == "" || cfg.LonColumn == "" {
			return fmt.Errorf("map charts need lat_column and lon_column (x_column/y_columns are derived from them)")
		}
	case "sankey":
		if cfg.XColumn == "" {
			return fmt.Errorf("sankey charts need x_column (source node)")
		}
		if len(cfg.YColumns) < 2 {
			return fmt.Errorf("sankey charts need two y_columns ([target, value])")
		}
	case "funnel":
		if cfg.CategoryColumn == "" {
			return fmt.Errorf("funnel charts need category_column (stage labels)")
		}
		if cfg.ValueColumn == "" {
			return fmt.Errorf("funnel charts need value_column (stage values)")
		}
	case "histogram":
		if cfg.ValueColumn == "" {
			return fmt.Errorf("histogram charts need value_column (numeric column to bin)")
		}
	}

	enums := []struct {
		field  string
		value  string
		values []string
	}{
		{"bar_mode", cfg.BarMode, []string{"grouped", "stacked", "horizontal"}},
		{"area_mode", cfg.AreaMode, []string{"area", "stacked"}},
		{"layout", cfg.Layout, []string{"top-down", "left-to-right"}},
		{"funnel_sort", cfg.FunnelSort, []string{"ascending", "descending", "none"}},
		{"rose_type", cfg.RoseType, []string{"radius", "area"}},
		{"node_align", cfg.NodeAlign, []string{"justify", "left", "right"}},
	}
	for _, e := range enums {
		if e.value != "" && !containsString(e.values, e.value) {
			return fmt.Errorf("%s must be one of %s", e.field, quotedList(e.values))
		}
	}

	for name, color := range cfg.SeriesColors {
		if !IsHexColor(color) {
			return fmt.Errorf("series_colors[%q] must be a hex color like #ff0000", name)
		}
	}

	for i, line := range cfg.MarkLines {
		if line.Value == "" {
			return fmt.Errorf("mark_lines[%d].value is required", i)
		}
		if line.Position != "horizontal" && line.Position != "vertical" {
			return fmt.Errorf(`mark_lines[%d].position must be "horizontal" or "vertical"`, i)
		}
		if line.Color != "" && !IsHexColor(line.Color) {
			return fmt.Errorf("mark_lines[%d].color must be a hex color like #ff0000", i)
		}
	}
	return nil
}

// ToStoredWidgetChart converts a validated `chart` block into the camelCase
// ChartConfig keys stored at the widget config root. Unset fields are omitted;
// a zero block produces no keys.
func ToStoredWidgetChart(cfg WidgetChartConfig) map[string]any {
	if cfg.ChartType == "" {
		return map[string]any{}
	}
	// The key names and conditional shape mirror create_chart's handler
	// (internal/agent/tools_chart.go); web/src/charts/types.ts is the
	// stored-shape source of truth.
	out := map[string]any{"chartType": cfg.ChartType}
	setString := func(key, value string) {
		if value != "" {
			out[key] = value
		}
	}
	setBool := func(key string, value *bool) {
		if value != nil {
			out[key] = *value
		}
	}
	setInt := func(key string, value *int) {
		if value != nil {
			out[key] = *value
		}
	}
	setString("xAxis", cfg.XColumn)
	if len(cfg.YColumns) > 0 {
		out["yAxis"] = cfg.YColumns
	}
	setString("title", cfg.Title)
	setString("timeColumn", cfg.TimeColumn)
	setString("endTimeColumn", cfg.EndTimeColumn)
	setString("labelColumn", cfg.LabelColumn)
	setString("idColumn", cfg.IDColumn)
	setString("parentIdColumn", cfg.ParentIDColumn)
	if len(cfg.MetricColumns) > 0 {
		out["metricColumns"] = cfg.MetricColumns
	}
	setString("layout", cfg.Layout)
	setString("valueColumn", cfg.ValueColumn)
	setString("categoryColumn", cfg.CategoryColumn)
	setString("yAxisColumn", cfg.YAxisColumn)
	setString("funnelSort", cfg.FunnelSort)
	setInt("binCount", cfg.BinCount)
	setString("barMode", cfg.BarMode)
	setString("areaMode", cfg.AreaMode)
	setString("groupBy", cfg.GroupBy)
	setBool("showLabels", cfg.ShowLabels)
	setBool("showLegend", cfg.ShowLegend)
	setBool("showGrid", cfg.ShowGrid)
	setBool("skipEmpty", cfg.SkipEmpty)
	setInt("maxLabelLength", cfg.MaxLabelLength)
	setBool("showConnectors", cfg.ShowConnectors)
	setBool("showTimeDeltas", cfg.ShowTimeDeltas)
	setInt("decimalPlaces", cfg.DecimalPlaces)
	setString("label", cfg.Label)
	setString("prefix", cfg.Prefix)
	setString("suffix", cfg.Suffix)
	if len(cfg.SeriesColors) > 0 {
		out["seriesColors"] = cfg.SeriesColors
	}
	setBool("dataZoom", cfg.DataZoom)
	setBool("smooth", cfg.Smooth)
	setBool("connectNulls", cfg.ConnectNulls)
	setString("barWidth", cfg.BarWidth)
	setString("barCategoryGap", cfg.BarCategoryGap)
	setString("roseType", cfg.RoseType)
	setInt("startAngle", cfg.StartAngle)
	setInt("padAngle", cfg.PadAngle)
	setString("nodeAlign", cfg.NodeAlign)
	setInt("nodeWidth", cfg.NodeWidth)
	setInt("nodeGap", cfg.NodeGap)
	setString("colorColumn", cfg.ColorColumn)
	setString("sizeColumn", cfg.SizeColumn)
	if len(cfg.MarkLines) > 0 {
		lines := make([]map[string]any, 0, len(cfg.MarkLines))
		for _, line := range cfg.MarkLines {
			entry := map[string]any{"value": line.Value, "position": line.Position}
			if line.Label != "" {
				entry["label"] = line.Label
			}
			if line.Color != "" {
				entry["color"] = line.Color
			}
			lines = append(lines, entry)
		}
		out["markLines"] = lines
	}
	// Map charts carry their coordinates through the shared axis keys.
	if cfg.ChartType == "map" {
		out["xAxis"] = cfg.LonColumn
		out["yAxis"] = []string{cfg.LatColumn}
	}
	return out
}

// ValidateWidgetMetric checks a `metric` block: aggregation and format enums,
// bounded decimals, a comparison that names its column, and a sparkline with
// both coordinates.
func ValidateWidgetMetric(cfg WidgetMetricConfig) error {
	if cfg.Aggregation != "" && !containsString(widgetMetricAggregations, cfg.Aggregation) {
		return fmt.Errorf("metric aggregation %q is not supported; use one of %s", cfg.Aggregation, quotedList(widgetMetricAggregations))
	}
	if cfg.Format != nil {
		if err := validateWidgetValueFormat(cfg.Format); err != nil {
			return fmt.Errorf("metric %w", err)
		}
	}
	if cfg.Comparison != nil {
		if cfg.Comparison.Column == "" {
			return fmt.Errorf("metric comparison needs a column (the second column to compare against)")
		}
		if cfg.Comparison.Mode != "" && cfg.Comparison.Mode != "delta" && cfg.Comparison.Mode != "percent" {
			return fmt.Errorf(`metric comparison mode must be "delta" or "percent"`)
		}
	}
	if cfg.Sparkline != nil {
		if cfg.Sparkline.XColumn == "" || cfg.Sparkline.YColumn == "" {
			return fmt.Errorf("metric sparkline needs x_column and y_column")
		}
	}
	return nil
}

// ToStoredWidgetMetric converts a validated `metric` block into the stored
// `config.metric` shape (snake_case, per web/src/utils/metric.ts). Unset
// fields are omitted; a zero block produces no keys.
func ToStoredWidgetMetric(cfg WidgetMetricConfig) map[string]any {
	out := map[string]any{}
	setString := func(key, value string) {
		if value != "" {
			out[key] = value
		}
	}
	setString("value_column", cfg.ValueColumn)
	setString("aggregation", cfg.Aggregation)
	setString("label", cfg.Label)
	if cfg.Format != nil {
		out["format"] = storedWidgetValueFormat(cfg.Format)
	}
	if cfg.Comparison != nil {
		comparison := map[string]any{"column": cfg.Comparison.Column}
		if cfg.Comparison.Mode != "" {
			comparison["mode"] = cfg.Comparison.Mode
		}
		out["comparison"] = comparison
	}
	if cfg.Sparkline != nil {
		out["sparkline"] = map[string]any{
			"x_column": cfg.Sparkline.XColumn,
			"y_column": cfg.Sparkline.YColumn,
		}
	}
	return out
}

// ValidateWidgetTable checks a `table` block: at least one named column and
// valid per-column formats.
func ValidateWidgetTable(cfg WidgetTableConfig) error {
	if len(cfg.Columns) == 0 {
		return fmt.Errorf("table needs at least one column")
	}
	for i, column := range cfg.Columns {
		if column.Name == "" {
			return fmt.Errorf("table column %d needs a name", i)
		}
		if column.Format != nil {
			if err := validateWidgetValueFormat(column.Format); err != nil {
				return fmt.Errorf("table column %q %w", column.Name, err)
			}
		}
	}
	return nil
}

// ToStoredWidgetTable converts a validated `table` block into the stored
// `config.table` shape (`columns` entries with a `format` spec). Columns
// without a format are stored bare; a zero block produces no keys.
func ToStoredWidgetTable(cfg WidgetTableConfig) map[string]any {
	if len(cfg.Columns) == 0 {
		return map[string]any{}
	}
	columns := make([]map[string]any, 0, len(cfg.Columns))
	for _, column := range cfg.Columns {
		entry := map[string]any{"name": column.Name}
		if column.Format != nil {
			entry["format"] = storedWidgetValueFormat(column.Format)
		}
		columns = append(columns, entry)
	}
	return map[string]any{"columns": columns}
}

// validateWidgetValueFormat is the shared number/currency/percent check.
func validateWidgetValueFormat(format *WidgetValueFormat) error {
	if format.Style != "" && !containsString(widgetFormatStyles, format.Style) {
		return fmt.Errorf("format style must be one of %s", quotedList(widgetFormatStyles))
	}
	if format.Currency != "" && !isCurrencyCode(format.Currency) {
		return fmt.Errorf("format currency must be a 3-letter code like USD")
	}
	if format.Decimals != nil && (*format.Decimals < 0 || *format.Decimals > 6) {
		return fmt.Errorf("format decimals must be between 0 and 6")
	}
	return nil
}

// storedWidgetValueFormat normalizes a format into the stored keys
// (web/src/utils/valueFormat.ts): currency codes are uppercased, unset fields
// are omitted.
func storedWidgetValueFormat(format *WidgetValueFormat) map[string]any {
	out := map[string]any{}
	if format.Style != "" {
		out["style"] = format.Style
	}
	if format.Currency != "" {
		out["currency"] = strings.ToUpper(format.Currency)
	}
	if format.Decimals != nil {
		out["decimals"] = *format.Decimals
	}
	if format.Compact != nil {
		out["compact"] = *format.Compact
	}
	return out
}

// isCurrencyCode reports whether s looks like an ISO 4217 code (three ASCII
// letters); the stored form is uppercased.
func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}
