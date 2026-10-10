package dashboarddoc

import (
	"fmt"
	"math"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
)

// testUUID returns a deterministic, valid UUID for widget map keys. The
// zero-padded decimal suffix keeps lexical order equal to numeric order, so
// warning-order assertions stay readable.
func testUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// buildState builds a document state from raw ygo operations. Tests use it to
// author shapes Seed would never produce (corruption / hand-written docs).
func buildState(t *testing.T, fn func(txn *crdt.Transaction, meta, widgets *crdt.YMap)) []byte {
	t.Helper()
	doc := crdt.New()
	// Root containers must be resolved before Transact (Doc.GetMap locks).
	meta := doc.GetMap(rootMeta)
	widgets := doc.GetMap(rootWidgets)
	doc.Transact(func(txn *crdt.Transaction) {
		fn(txn, meta, widgets)
	})
	return doc.EncodeStateAsUpdate()
}

// putRawWidget stages a valid widget under id, then lets build override or
// delete fields before it is attached.
func putRawWidget(txn *crdt.Transaction, widgets *crdt.YMap, id string, build func(txn *crdt.Transaction, w *crdt.YMap)) {
	w := crdt.NewMapPrelim()
	w.Set(txn, keyType, "table")
	w.Set(txn, keyLanguage, "sql")
	w.Set(txn, keyConfig, "{}")
	layout := crdt.NewMapPrelim()
	layout.Set(txn, keyRow, int64(0))
	layout.Set(txn, keyCol, int64(0))
	layout.Set(txn, keyWidth, int64(6))
	layout.Set(txn, keyHeight, int64(4))
	w.Set(txn, keyLayout, layout)
	query := crdt.NewTextPrelim()
	query.Insert(txn, 0, "SELECT 1", nil)
	w.Set(txn, keyQuery, query)
	if build != nil {
		build(txn, w)
	}
	widgets.Set(txn, id, w)
}

func TestSeedProject_RoundTrip(t *testing.T) {
	connectorID := "conn-1"
	notebookID := "nb-1"
	cellID := "cell-1"
	query := "SELECT region, count(*) FROM sales WHERE region = {{region}} GROUP BY region"

	want := Projection{
		Title: "Revenue Overview",
		Settings: map[string]any{
			"grid_cols":            12,
			"auto_refresh_seconds": 0,
			"query_cache_seconds":  30,
			"public_live":          false,
			"nested": map[string]any{
				"theme":  "dark",
				"levels": []any{1, 2.5},
			},
		},
		Variables: []map[string]any{
			{
				"name":    "region",
				"label":   "Region",
				"type":    "single_select",
				"default": "us",
				"options": []any{
					map[string]any{"label": "United States", "value": "us"},
					map[string]any{"label": "Europe", "value": "eu"},
				},
				"depends_on": []any{"country"},
				"required":   true,
			},
			{
				"name":       "threshold",
				"label":      "Threshold",
				"type":       "number",
				"default":    0.5,
				"options":    []any{},
				"depends_on": []any{},
			},
		},
		Widgets: map[string]WidgetDoc{
			testUUID(1): {
				ID:          testUUID(1),
				Type:        "chart",
				Layout:      Layout{Row: 0, Col: 0, Width: 6, Height: 4},
				ConnectorID: &connectorID,
				Query:       &query,
				Language:    "sql",
				Config:      map[string]any{"kind": "bar", "stacked": true},
			},
			testUUID(2): {
				ID:         testUUID(2),
				Type:       "table",
				Layout:     Layout{Row: 0, Col: 6, Width: 6, Height: 4},
				NotebookID: &notebookID,
				CellID:     &cellID,
				Language:   "sql",
				Config:     map[string]any{},
			},
			testUUID(3): {
				ID:     testUUID(3),
				Type:   "text",
				Layout: Layout{Row: 4, Col: 0, Width: 12, Height: 2},
				Config: map[string]any{"markdown": "# Notes"},
			},
		},
	}

	state, err := Seed(want)
	require.NoError(t, err)
	require.NotEmpty(t, state)

	got, err := Project(state)
	require.NoError(t, err)
	require.Empty(t, got.Warnings)
	require.Equal(t, want, *got)
}

func TestSeedProject_Empty(t *testing.T) {
	empty := &Projection{
		Settings:  map[string]any{},
		Variables: []map[string]any{},
		Widgets:   map[string]WidgetDoc{},
	}

	state, err := Seed(Projection{})
	require.NoError(t, err)
	require.NotEmpty(t, state)

	got, err := Project(state)
	require.NoError(t, err)
	require.Equal(t, empty, got)

	// An absent/empty state behaves like an empty document.
	for _, in := range [][]byte{nil, {}} {
		got, err = Project(in)
		require.NoError(t, err)
		require.Equal(t, empty, got)
	}
}

func TestProject_RejectsCorruptState(t *testing.T) {
	_, err := Project([]byte("this is not a yjs update"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode dashboard document")
}

func TestProject_RejectsWrongRootTypes(t *testing.T) {
	cases := []struct {
		name    string
		build   func(txn *crdt.Transaction, meta, widgets *crdt.YMap)
		wantErr string
	}{
		{
			name:    "title is not a string",
			build:   func(txn *crdt.Transaction, meta, _ *crdt.YMap) { meta.Set(txn, keyTitle, int64(3)) },
			wantErr: "meta.title",
		},
		{
			name:    "settings is not a Y.Map",
			build:   func(txn *crdt.Transaction, meta, _ *crdt.YMap) { meta.Set(txn, keySettings, "nope") },
			wantErr: "meta.settings",
		},
		{
			name:    "variables is not a Y.Array",
			build:   func(txn *crdt.Transaction, meta, _ *crdt.YMap) { meta.Set(txn, keyVariables, "nope") },
			wantErr: "meta.variables",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Project(buildState(t, tc.build))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestProject_SkipsInvalidWidgets(t *testing.T) {
	state := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
		putRawWidget(txn, widgets, testUUID(1), nil)
		putRawWidget(txn, widgets, testUUID(2), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Set(txn, keyType, "pie")
		})
		putRawWidget(txn, widgets, testUUID(3), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Delete(txn, keyType)
		})
		putRawWidget(txn, widgets, testUUID(4), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Set(txn, keyLanguage, "python")
		})
		putRawWidget(txn, widgets, testUUID(5), func(txn *crdt.Transaction, w *crdt.YMap) {
			l := crdt.NewMapPrelim()
			l.Set(txn, keyRow, int64(-1))
			l.Set(txn, keyCol, int64(0))
			l.Set(txn, keyWidth, int64(6))
			l.Set(txn, keyHeight, int64(4))
			w.Set(txn, keyLayout, l)
		})
		putRawWidget(txn, widgets, testUUID(6), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Set(txn, keyLayout, "nope")
		})
		putRawWidget(txn, widgets, testUUID(7), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Set(txn, keyConfig, "{oops")
		})
		putRawWidget(txn, widgets, testUUID(8), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Set(txn, keyConfig, int64(3))
		})
		putRawWidget(txn, widgets, testUUID(9), func(txn *crdt.Transaction, w *crdt.YMap) {
			w.Set(txn, keyQuery, "SELECT 1")
		})
		widgets.Set(txn, testUUID(10), "oops")
		// Valid shape under a non-UUID key: must be skipped, never materialized.
		putRawWidget(txn, widgets, "legacy-widget", nil)

		// Minimal valid widget: no layout/language/query/config at all.
		min := crdt.NewMapPrelim()
		min.Set(txn, keyType, "text")
		widgets.Set(txn, testUUID(11), min)
	})

	got, err := Project(state)
	require.NoError(t, err)

	require.Len(t, got.Widgets, 2)
	require.Contains(t, got.Widgets, testUUID(1))
	require.Contains(t, got.Widgets, testUUID(11))

	wmin := got.Widgets[testUUID(11)]
	require.Equal(t, "text", wmin.Type)
	require.Equal(t, Layout{}, wmin.Layout)
	require.Nil(t, wmin.ConnectorID)
	require.Nil(t, wmin.Query)
	require.Nil(t, wmin.NotebookID)
	require.Nil(t, wmin.CellID)
	require.Equal(t, "", wmin.Language)
	require.Equal(t, map[string]any{}, wmin.Config)

	// One warning per skipped widget, in sorted widget-ID order. UUID keys
	// sort before the non-UUID "legacy-widget".
	wantOrder := []string{
		testUUID(2), testUUID(3), testUUID(4), testUUID(5), testUUID(6),
		testUUID(7), testUUID(8), testUUID(9), testUUID(10), "legacy-widget",
	}
	require.Len(t, got.Warnings, len(wantOrder))
	for i, id := range wantOrder {
		require.Contains(t, got.Warnings[i], id, "warning %d", i)
	}
	require.Contains(t, got.Warnings[len(got.Warnings)-1], "not a valid UUID")
}

func TestProject_SkipsInvalidVariables(t *testing.T) {
	state := buildState(t, func(txn *crdt.Transaction, meta, _ *crdt.YMap) {
		variables := crdt.NewArrayPrelim()
		good := crdt.NewMapPrelim()
		good.Set(txn, "name", "region")
		variables.PushType(txn, good)
		variables.Push(txn, []any{"oops"})
		meta.Set(txn, keyVariables, variables)
	})

	got, err := Project(state)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"name": "region"}}, got.Variables)
	require.Len(t, got.Warnings, 1)
	require.Contains(t, got.Warnings[0], "variables[1]")
}

func TestSeed_RejectsInvalidProjection(t *testing.T) {
	valid := func() Projection {
		return Projection{
			Widgets: map[string]WidgetDoc{
				testUUID(1): {ID: testUUID(1), Type: "table", Config: map[string]any{}},
			},
		}
	}

	cases := []struct {
		name    string
		mutate  func(p *Projection)
		wantErr string
	}{
		{
			name: "unknown widget type",
			mutate: func(p *Projection) {
				p.Widgets[testUUID(1)] = WidgetDoc{ID: testUUID(1), Type: "pie"}
			},
			wantErr: "unknown type",
		},
		{
			name: "unsupported language",
			mutate: func(p *Projection) {
				p.Widgets[testUUID(1)] = WidgetDoc{ID: testUUID(1), Type: "table", Language: "python"}
			},
			wantErr: "unsupported language",
		},
		{
			name: "negative layout",
			mutate: func(p *Projection) {
				p.Widgets[testUUID(1)] = WidgetDoc{ID: testUUID(1), Type: "table", Layout: Layout{Row: -1}}
			},
			wantErr: "negative layout",
		},
		{
			name: "ID disagrees with map key",
			mutate: func(p *Projection) {
				p.Widgets[testUUID(1)] = WidgetDoc{ID: "other", Type: "table"}
			},
			wantErr: "does not match",
		},
		{
			name: "non-UUID widget key",
			mutate: func(p *Projection) {
				p.Widgets = map[string]WidgetDoc{"w-1": {Type: "table"}}
			},
			wantErr: "not a valid UUID",
		},
		{
			name: "empty widget map key",
			mutate: func(p *Projection) {
				p.Widgets = map[string]WidgetDoc{"": {Type: "table"}}
			},
			wantErr: "not a valid UUID",
		},
		{
			name: "unmarshalable config",
			mutate: func(p *Projection) {
				p.Widgets[testUUID(1)] = WidgetDoc{ID: testUUID(1), Type: "table", Config: map[string]any{"ch": make(chan int)}}
			},
			wantErr: "config",
		},
		{
			name: "non-finite settings value",
			mutate: func(p *Projection) {
				p.Settings = map[string]any{"x": math.Inf(1)}
			},
			wantErr: "non-finite",
		},
		{
			name: "unmarshalable variable value",
			mutate: func(p *Projection) {
				p.Variables = []map[string]any{{"fn": func() {}}}
			},
			wantErr: "variables[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			_, err := Seed(p)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSeedProject_EmptyStringsAreUnset(t *testing.T) {
	empty := ""
	state, err := Seed(Projection{
		Widgets: map[string]WidgetDoc{
			testUUID(1): {
				ID:          testUUID(1),
				Type:        "text",
				ConnectorID: &empty,
				Query:       &empty,
				NotebookID:  &empty,
				CellID:      &empty,
			},
		},
	})
	require.NoError(t, err)

	got, err := Project(state)
	require.NoError(t, err)
	require.Empty(t, got.Warnings)
	w := got.Widgets[testUUID(1)]
	require.Nil(t, w.ConnectorID)
	require.Nil(t, w.Query)
	require.Nil(t, w.NotebookID)
	require.Nil(t, w.CellID)
}

func TestProject_NormalizesDecodedNumbers(t *testing.T) {
	state := buildState(t, func(txn *crdt.Transaction, meta, widgets *crdt.YMap) {
		settings := crdt.NewMapPrelim()
		settings.Set(txn, "int", int64(5))
		settings.Set(txn, "float", float64(1.5))
		settings.Set(txn, "big", float64(1e300))
		settings.Set(txn, "list", []any{int64(7), float64(0.25), map[string]any{"n": int64(-2)}})
		meta.Set(txn, keySettings, settings)

		w := crdt.NewMapPrelim()
		w.Set(txn, keyType, "table")
		layout := crdt.NewMapPrelim()
		layout.Set(txn, keyRow, int64(3))
		layout.Set(txn, keyCol, int64(2))
		w.Set(txn, keyLayout, layout)
		widgets.Set(txn, testUUID(1), w)
	})

	got, err := Project(state)
	require.NoError(t, err)
	require.Empty(t, got.Warnings)

	require.Equal(t, int(5), got.Settings["int"])
	require.Equal(t, float64(1.5), got.Settings["float"])
	require.Equal(t, float64(1e300), got.Settings["big"])
	require.Equal(t, []any{int(7), float64(0.25), map[string]any{"n": int(-2)}}, got.Settings["list"])

	require.Equal(t, Layout{Row: 3, Col: 2}, got.Widgets[testUUID(1)].Layout)
}

// TestProject_ConvertsYgoEncodings pins that Project never leaks ygo's
// non-standard encodings or shared types out of a projection.
func TestProject_ConvertsYgoEncodings(t *testing.T) {
	state := buildState(t, func(txn *crdt.Transaction, meta, _ *crdt.YMap) {
		settings := crdt.NewMapPrelim()
		// > int32, within 2^53: decodes as float64.
		settings.Set(txn, "mid", int64(5_000_000_000))
		// > 2^53: decodes as encoding.BigInt.
		settings.Set(txn, "big", int64(1<<53+1))
		// Wire tag 116: raw bytes.
		settings.Set(txn, "bytes", []byte{1, 2, 3})

		sharedMap := crdt.NewMapPrelim()
		sharedMap.Set(txn, "a", int64(1))
		sharedMap.Set(txn, "text", "hello")
		settings.Set(txn, "shared_map", sharedMap)

		sharedArr := crdt.NewArrayPrelim()
		sharedArr.Push(txn, []any{"x", int64(2)})
		settings.Set(txn, "shared_arr", sharedArr)

		sharedText := crdt.NewTextPrelim()
		sharedText.Insert(txn, 0, "nested text", nil)
		settings.Set(txn, "shared_text", sharedText)

		meta.Set(txn, keySettings, settings)
	})

	got, err := Project(state)
	require.NoError(t, err)
	require.Empty(t, got.Warnings)

	require.Equal(t, int(5_000_000_000), got.Settings["mid"])
	require.Equal(t, int64(1<<53+1), got.Settings["big"])
	require.Equal(t, "AQID", got.Settings["bytes"])
	// The nested Y.Map must surface its content, not a ygo type and not {}.
	require.Equal(t, map[string]any{"a": 1, "text": "hello"}, got.Settings["shared_map"])
	require.Equal(t, []any{"x", 2}, got.Settings["shared_arr"])
	require.Equal(t, "nested text", got.Settings["shared_text"])
}

// TestProject_LayoutIntegralFloats covers the wire asymmetry where ints above
// int32 range arrive as float64 (Fix 4).
func TestProject_LayoutIntegralFloats(t *testing.T) {
	state := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
		putRawWidget(txn, widgets, testUUID(1), func(txn *crdt.Transaction, w *crdt.YMap) {
			l := crdt.NewMapPrelim()
			l.Set(txn, keyRow, float64(5_000_000_000))
			l.Set(txn, keyCol, float64(2))
			l.Set(txn, keyWidth, float32(3))
			l.Set(txn, keyHeight, int64(4))
			w.Set(txn, keyLayout, l)
		})
		putRawWidget(txn, widgets, testUUID(2), func(txn *crdt.Transaction, w *crdt.YMap) {
			l := crdt.NewMapPrelim()
			l.Set(txn, keyRow, 1.5)
			w.Set(txn, keyLayout, l)
		})
	})

	got, err := Project(state)
	require.NoError(t, err)
	require.Equal(t, Layout{Row: 5_000_000_000, Col: 2, Width: 3, Height: 4}, got.Widgets[testUUID(1)].Layout)
	require.Len(t, got.Warnings, 1)
	require.Contains(t, got.Warnings[0], "is not an integer")
}

// TestSeedProject_LargeLayoutRoundTrip proves Seed's large int layouts survive
// the float64 wire encoding and project back as ints.
func TestSeedProject_LargeLayoutRoundTrip(t *testing.T) {
	big := Layout{Row: 5_000_000_000, Col: 0, Width: 6, Height: 4}
	state, err := Seed(Projection{
		Widgets: map[string]WidgetDoc{
			testUUID(1): {ID: testUUID(1), Type: "table", Layout: big},
		},
	})
	require.NoError(t, err)

	got, err := Project(state)
	require.NoError(t, err)
	require.Empty(t, got.Warnings)
	require.Equal(t, big, got.Widgets[testUUID(1)].Layout)
}

func TestSeed_AcceptsJSONCompatibleTypes(t *testing.T) {
	type option struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}

	state, err := Seed(Projection{
		Settings: map[string]any{"tags": []string{"a", "b"}},
		Variables: []map[string]any{{
			"name":       "region",
			"depends_on": []string{"country"},
			"options":    []option{{Label: "US", Value: "us"}},
		}},
	})
	require.NoError(t, err)

	got, err := Project(state)
	require.NoError(t, err)
	require.Empty(t, got.Warnings)
	require.Equal(t, []any{"a", "b"}, got.Settings["tags"])
	require.Equal(t, []any{"country"}, got.Variables[0]["depends_on"])
	require.Equal(t, []any{map[string]any{"label": "US", "value": "us"}}, got.Variables[0]["options"])
}
