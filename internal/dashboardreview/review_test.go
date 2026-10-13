package dashboardreview

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
)

// cleanTableConfig keeps fixture widgets free of content findings.
var cleanTableConfig = map[string]any{
	"table": map[string]any{"columns": []any{map[string]any{"name": "x", "format": map[string]any{"style": "number"}}}},
}

func tab(id, title string) map[string]any { return map[string]any{"id": id, "title": title} }

func widget(id, wtype string, layout dashboarddoc.Layout, config map[string]any) dashboarddoc.WidgetDoc {
	if config == nil {
		config = cleanTableConfig
	}
	return dashboarddoc.WidgetDoc{ID: id, Type: wtype, Layout: layout, Language: "sql", Config: config}
}

func projection(title string, settings map[string]any, widgets ...dashboarddoc.WidgetDoc) *dashboarddoc.Projection {
	if settings == nil {
		settings = map[string]any{}
	}
	proj := &dashboarddoc.Projection{
		Title:    title,
		Settings: settings,
		Widgets:  map[string]dashboarddoc.WidgetDoc{},
	}
	for _, w := range widgets {
		proj.Widgets[w.ID] = w
	}
	return proj
}

func messages(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Message)
	}
	return out
}

func strPtr(s string) *string { return &s }

func filterFindings(findings []Finding, substr string) []Finding {
	var out []Finding
	for _, f := range findings {
		if contains(f.Message, substr) {
			out = append(out, f)
		}
	}
	return out
}

// requireFinding asserts one finding's distinctive message is present and
// returns it for field-level assertions.
func requireFinding(t *testing.T, findings []Finding, substr string) Finding {
	t.Helper()
	for _, f := range findings {
		if contains(f.Message, substr) {
			return f
		}
	}
	t.Fatalf("no finding contains %q; got %v", substr, messages(findings))
	return Finding{}
}

func requireNoFinding(t *testing.T, findings []Finding, substr string) {
	t.Helper()
	for _, f := range findings {
		if contains(f.Message, substr) {
			t.Fatalf("unexpected finding %q; got %v", substr, messages(findings))
		}
	}
}

func TestReviewLayout(t *testing.T) {
	tabs := []any{tab("overview", "Overview"), tab("orders", "Orders")}

	t.Run("overlap in the same chapter", func(t *testing.T) {
		proj := projection("Board", map[string]any{"tabs": tabs},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6, Tab: "overview"}, nil),
			widget("w2", "table", dashboarddoc.Layout{Row: 3, Col: 3, Width: 6, Height: 6, Tab: "overview"}, nil),
			// Identical coordinates in another chapter are fine: each
			// chapter is its own grid.
			widget("w3", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6, Tab: "orders"}, nil),
		)
		findings := Review(proj, Options{})
		f := requireFinding(t, findings, "overlap")
		require.Equal(t, SeverityError, f.Severity)
		require.Equal(t, "layout", f.Category)
		require.Equal(t, "overview", f.ChapterID)
		require.NotEmpty(t, f.WidgetID)
		require.Equal(t, "set_dashboard_layout", f.FixTool)
		require.Len(t, filterFindings(findings, "overlap"), 1)
	})

	t.Run("out of bounds", func(t *testing.T) {
		proj := projection("Board", map[string]any{"grid_cols": float64(12)},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 8, Width: 6, Height: 6}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "12-column grid")
		require.Equal(t, SeverityError, f.Severity)
		require.Equal(t, "w1", f.WidgetID)
	})

	t.Run("invalid dimensions", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "table", dashboarddoc.Layout{Row: -1, Col: 0, Width: 0, Height: 6}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "dimensions")
		require.Equal(t, SeverityError, f.Severity)
	})

	t.Run("dead gap above the first row", func(t *testing.T) {
		proj := projection("Board", map[string]any{"tabs": tabs},
			widget("w1", "table", dashboarddoc.Layout{Row: 4, Col: 0, Width: 6, Height: 6, Tab: "overview"}, nil),
			widget("w2", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6, Tab: "orders"}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "starts at row 4")
		require.Equal(t, SeverityInfo, f.Severity)
		require.Equal(t, "overview", f.ChapterID)
		requireNoFinding(t, Review(proj, Options{}), "orders")
	})

	t.Run("empty chapter", func(t *testing.T) {
		proj := projection("Board", map[string]any{"tabs": tabs},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6, Tab: "overview"}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "no widgets")
		require.Equal(t, "orders", f.ChapterID)
		require.Equal(t, SeverityWarning, f.Severity)
	})

	t.Run("widget with no tab when tabs exist", func(t *testing.T) {
		proj := projection("Board", map[string]any{"tabs": tabs},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil),
			widget("w2", "table", dashboarddoc.Layout{Row: 0, Col: 6, Width: 6, Height: 6, Tab: "orders"}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "no chapter")
		require.Equal(t, SeverityInfo, f.Severity)
		require.Equal(t, "w1", f.WidgetID)
		require.Equal(t, "overview", f.ChapterID, "the fallback chapter is named")
	})

	t.Run("widget naming a removed tab", func(t *testing.T) {
		proj := projection("Board", map[string]any{"tabs": tabs},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6, Tab: "deleted"}, nil),
			widget("w2", "table", dashboarddoc.Layout{Row: 0, Col: 6, Width: 6, Height: 6, Tab: "orders"}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "deleted")
		require.Equal(t, SeverityWarning, f.Severity)
		require.Equal(t, "w1", f.WidgetID)
	})

	t.Run("chapter balance", func(t *testing.T) {
		tabs3 := []any{tab("a", "A"), tab("b", "B"), tab("c", "C")}
		widgets := []dashboarddoc.WidgetDoc{}
		for i := 0; i < 6; i++ {
			widgets = append(widgets, widget(
				"a"+string(rune('0'+i)), "table",
				dashboarddoc.Layout{Row: i, Col: 0, Width: 2, Height: 1, Tab: "a"}, nil))
		}
		widgets = append(widgets,
			widget("b1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 2, Height: 1, Tab: "b"}, nil),
			widget("c1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 2, Height: 1, Tab: "c"}, nil),
		)
		proj := projection("Board", map[string]any{"tabs": tabs3}, widgets...)
		f := requireFinding(t, Review(proj, Options{}), "median")
		require.Equal(t, "a", f.ChapterID)
		require.Equal(t, SeverityInfo, f.Severity)
	})
}

func TestReviewContent(t *testing.T) {
	t.Run("metric without a label", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "metric", dashboarddoc.Layout{Row: 0, Col: 0, Width: 4, Height: 4},
				map[string]any{"metric": map[string]any{"value_column": "revenue", "aggregation": "sum"}}),
		)
		f := requireFinding(t, Review(proj, Options{}), "label")
		require.Equal(t, "content", f.Category)
		require.Equal(t, "set_widget_config", f.FixTool)
		require.Equal(t, "w1", f.WidgetID)
	})

	t.Run("metric without a value column", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "metric", dashboarddoc.Layout{Row: 0, Col: 0, Width: 4, Height: 4},
				map[string]any{"metric": map[string]any{"aggregation": "sum", "label": "Total"}}),
		)
		f := requireFinding(t, Review(proj, Options{}), "value column")
		require.Equal(t, SeverityWarning, f.Severity)
	})

	t.Run("aggregation none is not a finding", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "metric", dashboarddoc.Layout{Row: 0, Col: 0, Width: 4, Height: 4},
				map[string]any{"metric": map[string]any{"value_column": "revenue", "aggregation": "none", "label": "Latest"}}),
		)
		requireNoFinding(t, Review(proj, Options{}), "aggregation")
	})

	t.Run("table without column formats", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, map[string]any{}),
		)
		f := requireFinding(t, Review(proj, Options{}), "column formats")
		require.Equal(t, SeverityInfo, f.Severity)
	})

	t.Run("empty text panel", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "text", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6},
				map[string]any{"text": map[string]any{"markdown": "   "}}),
		)
		f := requireFinding(t, Review(proj, Options{}), "empty")
		require.Equal(t, SeverityWarning, f.Severity)
	})

	t.Run("text panel without a heading", func(t *testing.T) {
		proj := projection("Board", nil,
			widget("w1", "text", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6},
				map[string]any{"text": map[string]any{"markdown": "Just prose, no heading."}}),
		)
		f := requireFinding(t, Review(proj, Options{}), "heading")
		require.Equal(t, SeverityInfo, f.Severity)

		withHeading := projection("Board", nil,
			widget("w1", "text", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6},
				map[string]any{"text": map[string]any{"markdown": "## Events\n\nProse."}}),
		)
		requireNoFinding(t, Review(withHeading, Options{}), "heading")
	})
}

func TestReviewSurface(t *testing.T) {
	t.Run("missing app title", func(t *testing.T) {
		proj := projection("Revenue review", map[string]any{"app": map[string]any{}},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil),
		)
		f := requireFinding(t, Review(proj, Options{}), "no title")
		require.Equal(t, "surface", f.Category)
		require.Equal(t, "update_dashboard_settings", f.FixTool)

		titled := projection("Revenue review", map[string]any{"app": map[string]any{"title": "Q3 Revenue"}},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil),
		)
		requireNoFinding(t, Review(titled, Options{}), "no title")
	})

	t.Run("theme contrast failures", func(t *testing.T) {
		bad := projection("Board", map[string]any{"app": map[string]any{
			"title": "Board",
			"theme": map[string]any{"text": "#999999", "background": "#ffffff", "accent": "#ffff00"},
		}}, widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil))
		findings := Review(bad, Options{})
		requireFinding(t, findings, "text")
		requireFinding(t, findings, "accent")

		good := projection("Board", map[string]any{"app": map[string]any{
			"title": "Board",
			"theme": map[string]any{"text": "#111111", "background": "#ffffff", "accent": "#7c5cff"},
		}}, widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil))
		requireNoFinding(t, Review(good, Options{}), "contrast")
	})

	t.Run("unused variable under chapter scoping", func(t *testing.T) {
		proj := projection("Board", map[string]any{
			"app": map[string]any{"title": "Board", "filter_scope": "tab"},
		}, widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil))
		proj.Widgets["w1"] = dashboarddoc.WidgetDoc{
			ID: "w1", Type: "table", Layout: dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6},
			Language: "sql", Query: strPtr("SELECT * FROM t WHERE r = {{used}}"), Config: cleanTableConfig,
		}
		proj.Variables = []map[string]any{
			{"name": "used", "type": "text"},
			{"name": "dead", "type": "text"},
			{"name": "upstream", "type": "text"},
		}
		// `used` pulls `upstream` in through its options query: the closure
		// keeps feeding filters visible, so only `dead` is unreachable.
		proj.Variables[0]["options"] = map[string]any{
			"mode": "query", "query": map[string]any{"connector_id": "c1", "sql": "SELECT {{upstream}}"},
		}
		f := requireFinding(t, Review(proj, Options{}), "dead")
		require.Equal(t, SeverityInfo, f.Severity)
		requireNoFinding(t, Review(proj, Options{}), "upstream")

		// Without chapter scoping every filter shows in the strip: no finding.
		unscoped := projection("Board", map[string]any{"app": map[string]any{"title": "Board"}},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil))
		unscoped.Variables = []map[string]any{{"name": "dead", "type": "text"}}
		requireNoFinding(t, Review(unscoped, Options{}), "dead")
	})

	t.Run("unpublished changes", func(t *testing.T) {
		proj := projection("Board", map[string]any{"app": map[string]any{"title": "Board"}},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil))
		f := requireFinding(t, Review(proj, Options{PublishedHash: "v1", DraftHash: "v2"}), "unpublished")
		require.Equal(t, SeverityInfo, f.Severity)
		require.Equal(t, "publish_dashboard", f.FixTool)
		requireNoFinding(t, Review(proj, Options{PublishedHash: "v1", DraftHash: "v1"}), "unpublished")
		requireNoFinding(t, Review(proj, Options{}), "unpublished")
	})

	t.Run("projection warnings", func(t *testing.T) {
		proj := projection("Board", map[string]any{"app": map[string]any{"title": "Board"}},
			widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, nil))
		proj.Warnings = []string{"widget w1 has an invalid layout"}
		f := requireFinding(t, Review(proj, Options{}), "invalid layout")
		require.Equal(t, SeverityError, f.Severity)
		require.Equal(t, "surface", f.Category)
	})
}

func TestReviewOrdering(t *testing.T) {
	tabs := []any{tab("overview", "Overview"), tab("orders", "Orders")}
	proj := projection("Board", map[string]any{"tabs": tabs},
		// error: overlap in overview
		widget("w1", "table", dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6, Tab: "overview"}, nil),
		widget("w2", "table", dashboarddoc.Layout{Row: 3, Col: 3, Width: 6, Height: 6, Tab: "overview"}, nil),
		// warning: orders is empty
		// info: app has no title
	)
	findings := Review(proj, Options{})
	require.GreaterOrEqual(t, len(findings), 3)

	ranks := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
	for i := 1; i < len(findings); i++ {
		prev, cur := findings[i-1], findings[i]
		require.LessOrEqual(t, ranks[prev.Severity], ranks[cur.Severity],
			"findings must be sorted by severity: %v", messages(findings))
		if prev.Severity == cur.Severity {
			require.LessOrEqual(t, prev.ChapterID, cur.ChapterID,
				"same-severity findings sort by chapter: %v", messages(findings))
		}
	}
}
