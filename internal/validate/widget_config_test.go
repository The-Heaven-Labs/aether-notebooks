package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(n int) *int    { return &n }

// One minimal-but-valid config per supported chart type.
func validChartFixtures() map[string]WidgetChartConfig {
	return map[string]WidgetChartConfig{
		"bar":            {ChartType: "bar", XColumn: "day", YColumns: []string{"revenue"}},
		"line":           {ChartType: "line", XColumn: "day", YColumns: []string{"revenue"}},
		"area":           {ChartType: "area", XColumn: "day", YColumns: []string{"revenue"}},
		"scatter":        {ChartType: "scatter", XColumn: "x", YColumns: []string{"y"}},
		"pie":            {ChartType: "pie", XColumn: "label", YColumns: []string{"revenue"}},
		"donut":          {ChartType: "donut", XColumn: "label", YColumns: []string{"revenue"}},
		"timeline":       {ChartType: "timeline", TimeColumn: "ts", LabelColumn: "event"},
		"hierarchy_tree": {ChartType: "hierarchy_tree", IDColumn: "id", ParentIDColumn: "parent"},
		"big_number":     {ChartType: "big_number", ValueColumn: "revenue"},
		"map":            {ChartType: "map", LatColumn: "lat", LonColumn: "lon"},
		"sankey":         {ChartType: "sankey", XColumn: "source", YColumns: []string{"target", "value"}},
		"funnel":         {ChartType: "funnel", CategoryColumn: "stage", ValueColumn: "count"},
		"heatmap":        {ChartType: "heatmap", XColumn: "x", YAxisColumn: "y", ValueColumn: "intensity"},
		"histogram":      {ChartType: "histogram", ValueColumn: "revenue"},
	}
}

func TestValidateWidgetChartTypes(t *testing.T) {
	for name, cfg := range validChartFixtures() {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, ValidateWidgetChart(cfg))
		})
	}

	require.ErrorContains(t, ValidateWidgetChart(WidgetChartConfig{ChartType: "waffle"}),
		"not supported")
	require.ErrorContains(t, ValidateWidgetChart(WidgetChartConfig{}), "chart_type is required")
}

func TestValidateWidgetChartRequirements(t *testing.T) {
	cases := []struct {
		name string
		cfg  WidgetChartConfig
		want string
	}{
		{"map needs lat_column", WidgetChartConfig{ChartType: "map", LonColumn: "lon"}, "lat_column"},
		{"map needs lon_column", WidgetChartConfig{ChartType: "map", LatColumn: "lat"}, "lon_column"},
		{"sankey needs two y_columns", WidgetChartConfig{ChartType: "sankey", XColumn: "src", YColumns: []string{"dst"}}, "y_columns"},
		{"sankey needs x_column", WidgetChartConfig{ChartType: "sankey", YColumns: []string{"dst", "v"}}, "x_column"},
		{"funnel needs category_column", WidgetChartConfig{ChartType: "funnel", ValueColumn: "v"}, "category_column"},
		{"funnel needs value_column", WidgetChartConfig{ChartType: "funnel", CategoryColumn: "stage"}, "value_column"},
		{"histogram needs value_column", WidgetChartConfig{ChartType: "histogram"}, "value_column"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorContains(t, ValidateWidgetChart(tc.cfg), tc.want)
		})
	}
}

func TestValidateWidgetChartEnums(t *testing.T) {
	base := func() WidgetChartConfig {
		return WidgetChartConfig{ChartType: "bar", XColumn: "day", YColumns: []string{"revenue"}}
	}

	bad := []struct {
		name   string
		mutate func(*WidgetChartConfig)
		want   string
	}{
		{"bar_mode", func(c *WidgetChartConfig) { c.BarMode = "stacked3d" }, "bar_mode"},
		{"area_mode", func(c *WidgetChartConfig) { c.AreaMode = "fill" }, "area_mode"},
		{"layout", func(c *WidgetChartConfig) { c.Layout = "bottom-up" }, "layout"},
		{"funnel_sort", func(c *WidgetChartConfig) { c.FunnelSort = "random" }, "funnel_sort"},
		{"rose_type", func(c *WidgetChartConfig) { c.RoseType = "petal" }, "rose_type"},
		{"node_align", func(c *WidgetChartConfig) { c.NodeAlign = "center" }, "node_align"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg)
			require.ErrorContains(t, ValidateWidgetChart(cfg), tc.want)
		})
	}

	valid := []func(*WidgetChartConfig){
		func(c *WidgetChartConfig) { c.BarMode = "grouped" },
		func(c *WidgetChartConfig) { c.BarMode = "stacked" },
		func(c *WidgetChartConfig) { c.BarMode = "horizontal" },
		func(c *WidgetChartConfig) { c.AreaMode = "area" },
		func(c *WidgetChartConfig) { c.AreaMode = "stacked" },
		func(c *WidgetChartConfig) { c.Layout = "top-down" },
		func(c *WidgetChartConfig) { c.Layout = "left-to-right" },
		func(c *WidgetChartConfig) { c.FunnelSort = "ascending" },
		func(c *WidgetChartConfig) { c.FunnelSort = "descending" },
		func(c *WidgetChartConfig) { c.FunnelSort = "none" },
		func(c *WidgetChartConfig) { c.RoseType = "radius" },
		func(c *WidgetChartConfig) { c.RoseType = "area" },
		func(c *WidgetChartConfig) { c.NodeAlign = "justify" },
		func(c *WidgetChartConfig) { c.NodeAlign = "left" },
		func(c *WidgetChartConfig) { c.NodeAlign = "right" },
	}
	for i, mutate := range valid {
		cfg := base()
		mutate(&cfg)
		require.NoError(t, ValidateWidgetChart(cfg), "valid enum case %d", i)
	}
}

func TestValidateWidgetChartSeriesColors(t *testing.T) {
	cfg := WidgetChartConfig{ChartType: "bar", XColumn: "day", YColumns: []string{"revenue"}}
	cfg.SeriesColors = map[string]string{"revenue": "#ff0000", "cost": "#0f0"}
	require.NoError(t, ValidateWidgetChart(cfg))

	cfg.SeriesColors = map[string]string{"revenue": "red"}
	require.ErrorContains(t, ValidateWidgetChart(cfg), "series_colors")
}

func TestValidateWidgetChartMarkLines(t *testing.T) {
	cfg := WidgetChartConfig{ChartType: "line", XColumn: "day", YColumns: []string{"revenue"}}
	cfg.MarkLines = []WidgetMarkLine{{Value: "100", Label: "Target", Position: "horizontal", Color: "#00ff00"}}
	require.NoError(t, ValidateWidgetChart(cfg))

	cfg.MarkLines = []WidgetMarkLine{{Label: "Target", Position: "horizontal"}}
	require.ErrorContains(t, ValidateWidgetChart(cfg), "mark_lines[0].value")

	cfg.MarkLines = []WidgetMarkLine{{Value: "100", Position: "diagonal"}}
	require.ErrorContains(t, ValidateWidgetChart(cfg), "mark_lines[0].position")

	cfg.MarkLines = []WidgetMarkLine{{Value: "100", Position: "vertical", Color: "green"}}
	require.ErrorContains(t, ValidateWidgetChart(cfg), "mark_lines[0].color")
}

func TestToStoredWidgetChart(t *testing.T) {
	cfg := WidgetChartConfig{
		ChartType:      "bar",
		XColumn:        "day",
		YColumns:       []string{"revenue", "cost"},
		Title:          "Revenue",
		GroupBy:        "region",
		BarMode:        "stacked",
		BarWidth:       "20%",
		BarCategoryGap: "30%",
		ShowLegend:     boolPtr(false),
		ShowLabels:     boolPtr(true),
		ShowGrid:       boolPtr(false),
		SkipEmpty:      boolPtr(true),
		SeriesColors:   map[string]string{"revenue": "#ff0000"},
		DataZoom:       boolPtr(true),
		Smooth:         boolPtr(false),
		ConnectNulls:   boolPtr(true),
		DecimalPlaces:  intPtr(2),
		Label:          "Total",
		Prefix:         "$",
		Suffix:         "k",
		MarkLines:      []WidgetMarkLine{{Value: "100", Label: "Target", Position: "horizontal", Color: "#00ff00"}},
	}
	require.NoError(t, ValidateWidgetChart(cfg))

	require.Equal(t, map[string]any{
		"chartType":      "bar",
		"xAxis":          "day",
		"yAxis":          []string{"revenue", "cost"},
		"title":          "Revenue",
		"groupBy":        "region",
		"barMode":        "stacked",
		"barWidth":       "20%",
		"barCategoryGap": "30%",
		"showLegend":     false,
		"showLabels":     true,
		"showGrid":       false,
		"skipEmpty":      true,
		"seriesColors":   map[string]string{"revenue": "#ff0000"},
		"dataZoom":       true,
		"smooth":         false,
		"connectNulls":   true,
		"decimalPlaces":  2,
		"label":          "Total",
		"prefix":         "$",
		"suffix":         "k",
		"markLines":      []map[string]any{{"value": "100", "label": "Target", "position": "horizontal", "color": "#00ff00"}},
	}, ToStoredWidgetChart(cfg))
}

func TestToStoredWidgetChartMapColumns(t *testing.T) {
	cfg := WidgetChartConfig{ChartType: "map", LatColumn: "lat", LonColumn: "lon"}
	require.NoError(t, ValidateWidgetChart(cfg))
	require.Equal(t, map[string]any{
		"chartType": "map",
		"xAxis":     "lon",
		"yAxis":     []string{"lat"},
	}, ToStoredWidgetChart(cfg))
}

func TestToStoredWidgetChartOmitsUnset(t *testing.T) {
	stored := ToStoredWidgetChart(WidgetChartConfig{ChartType: "histogram", ValueColumn: "v"})
	require.Equal(t, map[string]any{"chartType": "histogram", "valueColumn": "v"}, stored)
	require.NotContains(t, stored, "title")
	require.NotContains(t, stored, "xAxis")
	require.NotContains(t, stored, "yAxis")
}

func TestValidateWidgetMetric(t *testing.T) {
	for _, agg := range []string{"", "none", "sum", "avg", "min", "max", "median", "count", "count_distinct", "first"} {
		require.NoError(t, ValidateWidgetMetric(WidgetMetricConfig{ValueColumn: "v", Aggregation: agg}), "aggregation %q", agg)
	}
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Aggregation: "total"}), "aggregation")

	for _, style := range []string{"", "number", "currency", "percent"} {
		cfg := WidgetMetricConfig{Format: &WidgetValueFormat{Style: style}}
		require.NoError(t, ValidateWidgetMetric(cfg), "style %q", style)
	}
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Format: &WidgetValueFormat{Style: "money"}}), "style")

	for _, decimals := range []int{0, 3, 6} {
		cfg := WidgetMetricConfig{Format: &WidgetValueFormat{Decimals: intPtr(decimals)}}
		require.NoError(t, ValidateWidgetMetric(cfg), "decimals %d", decimals)
	}
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Format: &WidgetValueFormat{Decimals: intPtr(7)}}), "decimals")
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Format: &WidgetValueFormat{Decimals: intPtr(-1)}}), "decimals")

	require.NoError(t, ValidateWidgetMetric(WidgetMetricConfig{Format: &WidgetValueFormat{Currency: "usd"}}))
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Format: &WidgetValueFormat{Currency: "EURO"}}), "currency")
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Format: &WidgetValueFormat{Currency: "#fff"}}), "currency")

	require.NoError(t, ValidateWidgetMetric(WidgetMetricConfig{Comparison: &WidgetMetricComparison{Column: "prev", Mode: "delta"}}))
	require.NoError(t, ValidateWidgetMetric(WidgetMetricConfig{Comparison: &WidgetMetricComparison{Column: "prev", Mode: "percent"}}))
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Comparison: &WidgetMetricComparison{Mode: "delta"}}), "comparison")
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Comparison: &WidgetMetricComparison{Column: "prev", Mode: "ratio"}}), "mode")

	require.NoError(t, ValidateWidgetMetric(WidgetMetricConfig{Sparkline: &WidgetMetricSparkline{XColumn: "day", YColumn: "revenue"}}))
	require.ErrorContains(t, ValidateWidgetMetric(WidgetMetricConfig{Sparkline: &WidgetMetricSparkline{XColumn: "day"}}), "sparkline")
}

func TestToStoredWidgetMetric(t *testing.T) {
	cfg := WidgetMetricConfig{
		ValueColumn: "revenue",
		Aggregation: "sum",
		Label:       "Revenue",
		Format:      &WidgetValueFormat{Style: "currency", Currency: "usd", Decimals: intPtr(2), Compact: boolPtr(true)},
		Comparison:  &WidgetMetricComparison{Column: "prev_revenue", Mode: "percent"},
		Sparkline:   &WidgetMetricSparkline{XColumn: "day", YColumn: "revenue"},
	}
	require.NoError(t, ValidateWidgetMetric(cfg))

	require.Equal(t, map[string]any{
		"value_column": "revenue",
		"aggregation":  "sum",
		"label":        "Revenue",
		"format": map[string]any{
			"style":    "currency",
			"currency": "USD",
			"decimals": 2,
			"compact":  true,
		},
		"comparison": map[string]any{"column": "prev_revenue", "mode": "percent"},
		"sparkline":  map[string]any{"x_column": "day", "y_column": "revenue"},
	}, ToStoredWidgetMetric(cfg))
}

func TestValidateWidgetTable(t *testing.T) {
	require.ErrorContains(t, ValidateWidgetTable(WidgetTableConfig{}), "at least one column")
	require.ErrorContains(t, ValidateWidgetTable(WidgetTableConfig{
		Columns: []WidgetTableColumn{{}},
	}), "name")
	require.ErrorContains(t, ValidateWidgetTable(WidgetTableConfig{
		Columns: []WidgetTableColumn{{Name: "revenue", Format: &WidgetValueFormat{Style: "money"}}},
	}), "style")
	require.ErrorContains(t, ValidateWidgetTable(WidgetTableConfig{
		Columns: []WidgetTableColumn{{Name: "revenue", Format: &WidgetValueFormat{Currency: "EURO"}}},
	}), "currency")

	require.NoError(t, ValidateWidgetTable(WidgetTableConfig{
		Columns: []WidgetTableColumn{
			{Name: "revenue", Format: &WidgetValueFormat{Style: "currency", Currency: "usd", Decimals: intPtr(2)}},
			{Name: "notes"},
		},
	}))
}

func TestToStoredWidgetTable(t *testing.T) {
	cfg := WidgetTableConfig{Columns: []WidgetTableColumn{
		{Name: "revenue", Format: &WidgetValueFormat{Style: "currency", Currency: "usd", Decimals: intPtr(2)}},
		{Name: "margin", Format: &WidgetValueFormat{Style: "percent", Decimals: intPtr(1), Compact: boolPtr(false)}},
		{Name: "notes"},
	}}
	require.NoError(t, ValidateWidgetTable(cfg))

	require.Equal(t, map[string]any{"columns": []map[string]any{
		{"name": "revenue", "format": map[string]any{"style": "currency", "currency": "USD", "decimals": 2}},
		{"name": "margin", "format": map[string]any{"style": "percent", "decimals": 1, "compact": false}},
		{"name": "notes"},
	}}, ToStoredWidgetTable(cfg))
}

func TestWidgetConfigEmptyBlocksAreNoOps(t *testing.T) {
	require.Empty(t, ToStoredWidgetChart(WidgetChartConfig{}))
	require.Empty(t, ToStoredWidgetMetric(WidgetMetricConfig{}))
	require.Empty(t, ToStoredWidgetTable(WidgetTableConfig{}))
}

func TestValidateWidgetHTML(t *testing.T) {
	// Valid content passes.
	require.NoError(t, ValidateWidgetHTML(WidgetHTMLConfig{
		HTML: `<div class="stat">42</div>`,
		CSS:  `.stat { font-size: 28px; }`,
	}))
	// Both empty is allowed (a no-op block).
	require.NoError(t, ValidateWidgetHTML(WidgetHTMLConfig{}))

	// Oversize html / css are rejected with actionable messages.
	err := ValidateWidgetHTML(WidgetHTMLConfig{HTML: strings.Repeat("x", maxHTMLPanelBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "256 KiB")

	err = ValidateWidgetHTML(WidgetHTMLConfig{CSS: strings.Repeat("x", maxHTMLPanelCSSBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "64 KiB")

	// The converter omits empty keys and keeps set ones verbatim.
	require.Equal(t, map[string]any{}, ToStoredWidgetHTML(WidgetHTMLConfig{}))
	stored := ToStoredWidgetHTML(WidgetHTMLConfig{HTML: "<p>hi</p>", CSS: "p{}"})
	require.Equal(t, "<p>hi</p>", stored["html"])
	require.Equal(t, "p{}", stored["css"])
}
