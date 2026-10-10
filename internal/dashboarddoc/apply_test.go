package dashboarddoc

import (
	"math"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
)

// applyTestBase seeds the fixture document the apply tests mutate: two
// widgets, one setting and one variable, so tests can assert that an op leaves
// everything it did not target untouched.
func applyTestBase(t *testing.T) []byte {
	t.Helper()
	connectorID := "conn-1"
	query1 := "SELECT 1"
	query2 := "SELECT 2"
	state, err := Seed(Projection{
		Title:     "Base Dashboard",
		Settings:  map[string]any{"grid_cols": 12},
		Variables: []map[string]any{{"name": "region", "default": "us"}},
		Widgets: map[string]WidgetDoc{
			testUUID(1): {
				ID:          testUUID(1),
				Type:        "chart",
				Layout:      Layout{Row: 0, Col: 0, Width: 6, Height: 4},
				ConnectorID: &connectorID,
				Query:       &query1,
				Language:    "sql",
				Config:      map[string]any{"kind": "bar"},
			},
			testUUID(2): {
				ID:       testUUID(2),
				Type:     "table",
				Layout:   Layout{Row: 0, Col: 6, Width: 6, Height: 4},
				Query:    &query2,
				Language: "sql",
				Config:   map[string]any{},
			},
		},
	})
	require.NoError(t, err)
	return state
}

// mustProject projects state and requires it to be warning-free.
func mustProject(t *testing.T, state []byte) *Projection {
	t.Helper()
	proj, err := Project(state)
	require.NoError(t, err)
	require.Emptyf(t, proj.Warnings, "unexpected warnings: %v", proj.Warnings)
	return proj
}

// widgetQuery returns the widget's query text, failing when it is unset.
func widgetQuery(t *testing.T, w WidgetDoc) string {
	t.Helper()
	require.NotNilf(t, w.Query, "widget %s has no query", w.ID)
	return *w.Query
}

func TestApply_UpsertWidget(t *testing.T) {
	t.Run("adds a widget to an empty state", func(t *testing.T) {
		state, err := UpsertWidget(nil, WidgetDoc{
			ID:       testUUID(1),
			Type:     "table",
			Layout:   Layout{Row: 1, Col: 2, Width: 3, Height: 4},
			Language: "sql",
			Config:   map[string]any{"striped": true},
		})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Len(t, proj.Widgets, 1)
		w := proj.Widgets[testUUID(1)]
		require.Equal(t, "table", w.Type)
		require.Equal(t, Layout{Row: 1, Col: 2, Width: 3, Height: 4}, w.Layout)
		require.Equal(t, "sql", w.Language)
		require.Equal(t, map[string]any{"striped": true}, w.Config)
		require.Nil(t, w.Query)
		require.Nil(t, w.ConnectorID)
	})

	t.Run("adds a widget without disturbing the others", func(t *testing.T) {
		state, err := UpsertWidget(applyTestBase(t), WidgetDoc{
			ID:     testUUID(3),
			Type:   "text",
			Config: map[string]any{"markdown": "# hi"},
		})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Len(t, proj.Widgets, 3)
		require.Equal(t, "Base Dashboard", proj.Title)
		require.Equal(t, map[string]any{"grid_cols": 12}, proj.Settings)
		require.Equal(t, []map[string]any{{"name": "region", "default": "us"}}, proj.Variables)
		require.Equal(t, "chart", proj.Widgets[testUUID(1)].Type)
		require.Equal(t, "table", proj.Widgets[testUUID(2)].Type)
		require.Equal(t, "text", proj.Widgets[testUUID(3)].Type)
	})

	t.Run("replaces an existing widget", func(t *testing.T) {
		state, err := UpsertWidget(applyTestBase(t), WidgetDoc{
			ID:       testUUID(1),
			Type:     "metric",
			Layout:   Layout{Row: 2, Col: 0, Width: 3, Height: 2},
			Language: "",
			Config:   map[string]any{"field": "count"},
		})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Len(t, proj.Widgets, 2)
		w := proj.Widgets[testUUID(1)]
		require.Equal(t, "metric", w.Type)
		require.Equal(t, Layout{Row: 2, Col: 0, Width: 3, Height: 2}, w.Layout)
		require.Equal(t, map[string]any{"field": "count"}, w.Config)
		// Fields the replacement does not carry are reset, not merged.
		require.Nil(t, w.ConnectorID)
		require.Nil(t, w.Query)
		// The other widget is untouched.
		require.Equal(t, "SELECT 2", widgetQuery(t, proj.Widgets[testUUID(2)]))
	})

	t.Run("re-adding a deleted widget revives it", func(t *testing.T) {
		deleted, err := DeleteWidget(applyTestBase(t), testUUID(1))
		require.NoError(t, err)
		state, err := UpsertWidget(deleted, WidgetDoc{
			ID:     testUUID(1),
			Type:   "metric",
			Config: map[string]any{},
		})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Len(t, proj.Widgets, 2)
		require.Equal(t, "metric", proj.Widgets[testUUID(1)].Type)
	})
}

func TestApply_UpsertWidget_ValidatesLikeSeed(t *testing.T) {
	valid := func() WidgetDoc {
		return WidgetDoc{ID: testUUID(1), Type: "table", Config: map[string]any{}}
	}

	cases := []struct {
		name    string
		mutate  func(w *WidgetDoc)
		wantErr string
	}{
		{
			name:    "missing ID",
			mutate:  func(w *WidgetDoc) { w.ID = "" },
			wantErr: "not a valid UUID",
		},
		{
			name:    "non-UUID ID",
			mutate:  func(w *WidgetDoc) { w.ID = "w-1" },
			wantErr: "not a valid UUID",
		},
		{
			name: "connector without query",
			mutate: func(w *WidgetDoc) {
				conn := testUUID(9)
				w.ConnectorID = &conn
			},
			wantErr: "requires a non-empty query",
		},
		{
			name:    "unknown type",
			mutate:  func(w *WidgetDoc) { w.Type = "pie" },
			wantErr: "unknown type",
		},
		{
			name:    "unsupported language",
			mutate:  func(w *WidgetDoc) { w.Language = "python" },
			wantErr: "unsupported language",
		},
		{
			name:    "negative layout",
			mutate:  func(w *WidgetDoc) { w.Layout = Layout{Col: -1} },
			wantErr: "negative layout",
		},
		{
			name:    "non-finite config value",
			mutate:  func(w *WidgetDoc) { w.Config = map[string]any{"n": math.Inf(1)} },
			wantErr: "non-finite",
		},
		{
			name:    "shared type in config",
			mutate:  func(w *WidgetDoc) { w.Config = map[string]any{"xml": crdt.NewYXmlText()} },
			wantErr: "shared type",
		},
		{
			name:    "unmarshalable config",
			mutate:  func(w *WidgetDoc) { w.Config = map[string]any{"fn": func() {}} },
			wantErr: "config",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := valid()
			tc.mutate(&w)
			state, err := UpsertWidget(applyTestBase(t), w)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
			require.Nil(t, state)
		})
	}
}

func TestApply_DeleteWidget(t *testing.T) {
	t.Run("removes the widget and leaves the rest", func(t *testing.T) {
		state, err := DeleteWidget(applyTestBase(t), testUUID(1))
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Len(t, proj.Widgets, 1)
		require.NotContains(t, proj.Widgets, testUUID(1))
		require.Equal(t, "SELECT 2", widgetQuery(t, proj.Widgets[testUUID(2)]))
		require.Equal(t, "Base Dashboard", proj.Title)
	})

	t.Run("missing widget is an error", func(t *testing.T) {
		state, err := DeleteWidget(applyTestBase(t), testUUID(9))
		require.ErrorIs(t, err, ErrWidgetNotFound)
		require.Nil(t, state)
	})

	t.Run("missing widget on an empty document is an error", func(t *testing.T) {
		_, err := DeleteWidget(nil, testUUID(1))
		require.ErrorIs(t, err, ErrWidgetNotFound)
	})

	t.Run("non-UUID widget ID is an error", func(t *testing.T) {
		_, err := DeleteWidget(applyTestBase(t), "w-1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a valid UUID")
	})
}

func TestApply_UpdateLayout(t *testing.T) {
	t.Run("updates only the target widget", func(t *testing.T) {
		state, err := UpdateLayout(applyTestBase(t), testUUID(1), Layout{Row: 3, Col: 1, Width: 5, Height: 2})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, Layout{Row: 3, Col: 1, Width: 5, Height: 2}, proj.Widgets[testUUID(1)].Layout)
		require.Equal(t, Layout{Row: 0, Col: 6, Width: 6, Height: 4}, proj.Widgets[testUUID(2)].Layout)
		require.Equal(t, "SELECT 1", widgetQuery(t, proj.Widgets[testUUID(1)]))
	})

	t.Run("creates the layout map when absent", func(t *testing.T) {
		base := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
			min := crdt.NewMapPrelim()
			min.Set(txn, keyType, "text")
			widgets.Set(txn, testUUID(1), min)
		})
		state, err := UpdateLayout(base, testUUID(1), Layout{Row: 1, Col: 2, Width: 3, Height: 4})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, Layout{Row: 1, Col: 2, Width: 3, Height: 4}, proj.Widgets[testUUID(1)].Layout)
	})

	t.Run("missing widget is an error", func(t *testing.T) {
		state, err := UpdateLayout(applyTestBase(t), testUUID(9), Layout{Row: 1})
		require.ErrorIs(t, err, ErrWidgetNotFound)
		require.Nil(t, state)
	})

	t.Run("negative layout is rejected", func(t *testing.T) {
		_, err := UpdateLayout(applyTestBase(t), testUUID(1), Layout{Height: -1})
		require.Error(t, err)
		require.Contains(t, err.Error(), "negative layout")
	})

	t.Run("wrong layout type is rejected", func(t *testing.T) {
		base := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
			putRawWidget(txn, widgets, testUUID(1), func(txn *crdt.Transaction, w *crdt.YMap) {
				w.Set(txn, keyLayout, "nope")
			})
		})
		_, err := UpdateLayout(base, testUUID(1), Layout{Row: 1})
		require.Error(t, err)
		require.Contains(t, err.Error(), "want Y.Map")
	})
}

func TestApply_SetQuery(t *testing.T) {
	t.Run("replaces the query text", func(t *testing.T) {
		state, err := SetQuery(applyTestBase(t), testUUID(2), "SELECT 42\n-- updated")
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, "SELECT 42\n-- updated", widgetQuery(t, proj.Widgets[testUUID(2)]))
		require.Equal(t, "SELECT 1", widgetQuery(t, proj.Widgets[testUUID(1)]))
	})

	t.Run("empty SQL clears the query", func(t *testing.T) {
		state, err := SetQuery(applyTestBase(t), testUUID(2), "")
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Nil(t, proj.Widgets[testUUID(2)].Query)
	})

	t.Run("clearing a connector widget's SQL projects as a skipped widget", func(t *testing.T) {
		// A connector widget without SQL violates widgets_source_check, so
		// Project skips it with a warning instead of the materializer
		// aborting the store. The other widget is untouched.
		state, err := SetQuery(applyTestBase(t), testUUID(1), "")
		require.NoError(t, err)

		proj, err := Project(state)
		require.NoError(t, err)
		require.NotContains(t, proj.Widgets, testUUID(1))
		require.Len(t, proj.WidgetWarnings, 1)
		require.Contains(t, proj.WidgetWarnings[0], "connector widget has no query")
		require.Equal(t, "SELECT 2", widgetQuery(t, proj.Widgets[testUUID(2)]))
	})

	t.Run("creates the text when absent", func(t *testing.T) {
		base := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
			min := crdt.NewMapPrelim()
			min.Set(txn, keyType, "table")
			min.Set(txn, keyLanguage, "sql")
			widgets.Set(txn, testUUID(1), min)
		})
		state, err := SetQuery(base, testUUID(1), "SELECT 7")
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, "SELECT 7", widgetQuery(t, proj.Widgets[testUUID(1)]))
	})

	t.Run("missing widget is an error", func(t *testing.T) {
		state, err := SetQuery(applyTestBase(t), testUUID(9), "SELECT 1")
		require.ErrorIs(t, err, ErrWidgetNotFound)
		require.Nil(t, state)
	})

	t.Run("wrong query type is rejected", func(t *testing.T) {
		base := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
			putRawWidget(txn, widgets, testUUID(1), func(txn *crdt.Transaction, w *crdt.YMap) {
				w.Set(txn, keyQuery, "SELECT 1")
			})
		})
		_, err := SetQuery(base, testUUID(1), "SELECT 2")
		require.Error(t, err)
		require.Contains(t, err.Error(), "want Y.Text")
	})
}

func TestApply_UpdateMeta(t *testing.T) {
	t.Run("updates title settings and variables", func(t *testing.T) {
		title := "Renamed"
		state, err := UpdateMeta(applyTestBase(t), &title,
			map[string]any{"grid_cols": 6, "public_live": true},
			[]map[string]any{{"name": "country", "default": "us"}},
		)
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, "Renamed", proj.Title)
		require.Equal(t, map[string]any{"grid_cols": 6, "public_live": true}, proj.Settings)
		require.Equal(t, []map[string]any{{"name": "country", "default": "us"}}, proj.Variables)
		require.Len(t, proj.Widgets, 2)
	})

	t.Run("nil arguments leave fields unchanged", func(t *testing.T) {
		state, err := UpdateMeta(applyTestBase(t), nil, nil, nil)
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, "Base Dashboard", proj.Title)
		require.Equal(t, map[string]any{"grid_cols": 12}, proj.Settings)
		require.Equal(t, []map[string]any{{"name": "region", "default": "us"}}, proj.Variables)
	})

	t.Run("non-nil empty settings and variables clear them", func(t *testing.T) {
		state, err := UpdateMeta(applyTestBase(t), nil, map[string]any{}, []map[string]any{})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, map[string]any{}, proj.Settings)
		require.Equal(t, []map[string]any{}, proj.Variables)
		require.Equal(t, "Base Dashboard", proj.Title)
	})

	t.Run("works on an empty document", func(t *testing.T) {
		title := "Fresh"
		state, err := UpdateMeta(nil, &title, map[string]any{"grid_cols": 12}, nil)
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Equal(t, "Fresh", proj.Title)
		require.Equal(t, map[string]any{"grid_cols": 12}, proj.Settings)
		require.Empty(t, proj.Variables)
	})

	t.Run("rejects invalid values before mutating", func(t *testing.T) {
		cases := []struct {
			name      string
			settings  map[string]any
			variables []map[string]any
			wantErr   string
		}{
			{
				name:     "non-finite settings value",
				settings: map[string]any{"x": math.Inf(1)},
				wantErr:  "non-finite",
			},
			{
				name:     "shared type in settings",
				settings: map[string]any{"doc": crdt.New()},
				wantErr:  "shared type",
			},
			{
				name:      "unmarshalable variable value",
				variables: []map[string]any{{"fn": func() {}}},
				wantErr:   "variables[0]",
			},
			{
				name:      "shared type in variables",
				variables: []map[string]any{{"xml": crdt.NewYXmlText()}},
				wantErr:   "shared type",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				state, err := UpdateMeta(applyTestBase(t), nil, tc.settings, tc.variables)
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				require.Nil(t, state)
			})
		}
	})
}

// TestApply_MergeConcurrentChanges pins that ops emit incremental CRDT
// updates: two ops applied to copies of the same base state, merged in either
// order, lose neither change.
func TestApply_MergeConcurrentChanges(t *testing.T) {
	base := applyTestBase(t)

	layoutState, err := UpdateLayout(base, testUUID(1), Layout{Row: 5, Col: 0, Width: 12, Height: 3})
	require.NoError(t, err)
	queryState, err := SetQuery(base, testUUID(2), "SELECT 42")
	require.NoError(t, err)

	orders := [][2][]byte{
		{layoutState, queryState},
		{queryState, layoutState},
	}
	for i, order := range orders {
		doc := crdt.New()
		require.NoError(t, crdt.ApplyUpdateV1(doc, order[0], nil), "merge order %d", i)
		require.NoError(t, crdt.ApplyUpdateV1(doc, order[1], nil), "merge order %d", i)

		proj := mustProject(t, doc.EncodeStateAsUpdate())
		require.Equal(t, Layout{Row: 5, Col: 0, Width: 12, Height: 3},
			proj.Widgets[testUUID(1)].Layout, "merge order %d", i)
		require.Equal(t, "SELECT 42",
			widgetQuery(t, proj.Widgets[testUUID(2)]), "merge order %d", i)
		// Untouched parts of both widgets survive too.
		require.Equal(t, "SELECT 1",
			widgetQuery(t, proj.Widgets[testUUID(1)]), "merge order %d", i)
		require.Equal(t, Layout{Row: 0, Col: 6, Width: 6, Height: 4},
			proj.Widgets[testUUID(2)].Layout, "merge order %d", i)
	}
}

// TestApply_RejectsCorruptState pins that every op reports undecodable state
// instead of panicking or returning garbage.
func TestApply_RejectsCorruptState(t *testing.T) {
	corrupt := []byte("this is not a yjs update")
	title := "x"

	ops := map[string]func() ([]byte, error){
		"UpsertWidget": func() ([]byte, error) {
			return UpsertWidget(corrupt, WidgetDoc{ID: testUUID(1), Type: "table"})
		},
		"DeleteWidget": func() ([]byte, error) { return DeleteWidget(corrupt, testUUID(1)) },
		"UpdateLayout": func() ([]byte, error) { return UpdateLayout(corrupt, testUUID(1), Layout{}) },
		"SetQuery":     func() ([]byte, error) { return SetQuery(corrupt, testUUID(1), "SELECT 1") },
		"UpdateMeta":   func() ([]byte, error) { return UpdateMeta(corrupt, &title, nil, nil) },
	}
	for name, fn := range ops {
		t.Run(name, func(t *testing.T) {
			state, err := fn()
			require.Error(t, err)
			require.Contains(t, err.Error(), "decode dashboard document")
			require.Nil(t, state)
		})
	}
}

// TestCanonicalWidgetIDs pins that UUID spellings are canonicalized on every
// path: Seed stores canonical keys, Project reports canonical IDs, and the
// apply ops address canonical keys, so an uppercase spelling cannot create a
// duplicate document key or a diff mismatch during materialization.
func TestCanonicalWidgetIDs(t *testing.T) {
	const upperID = "AAAAAAAA-BBBB-4CCC-8DDD-000000000001"
	const canonID = "aaaaaaaa-bbbb-4ccc-8ddd-000000000001"

	t.Run("Seed stores the canonical key", func(t *testing.T) {
		state, err := Seed(Projection{
			Widgets: map[string]WidgetDoc{upperID: {ID: upperID, Type: "table"}},
		})
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Contains(t, proj.Widgets, canonID)
		require.Equal(t, canonID, proj.Widgets[canonID].ID)
	})

	t.Run("Project canonicalizes the widget ID", func(t *testing.T) {
		state := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
			putRawWidget(txn, widgets, upperID, nil)
		})

		proj, err := Project(state)
		require.NoError(t, err)
		require.Empty(t, proj.Warnings)
		require.Contains(t, proj.Widgets, canonID)
		require.Equal(t, canonID, proj.Widgets[canonID].ID)
	})

	t.Run("Project warns on duplicate spellings", func(t *testing.T) {
		state := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
			putRawWidget(txn, widgets, upperID, nil)
			putRawWidget(txn, widgets, canonID, nil)
		})

		proj, err := Project(state)
		require.NoError(t, err)
		require.Len(t, proj.Widgets, 1)
		require.Len(t, proj.Warnings, 1)
		require.Contains(t, proj.Warnings[0], "duplicate UUID")
		require.Equal(t, proj.Warnings, proj.WidgetWarnings)
	})

	t.Run("apply ops address the canonical key", func(t *testing.T) {
		state, err := UpsertWidget(nil, WidgetDoc{ID: upperID, Type: "table", Config: map[string]any{}})
		require.NoError(t, err)

		state, err = UpdateLayout(state, upperID, Layout{Row: 1, Col: 2, Width: 3, Height: 4})
		require.NoError(t, err)
		state, err = SetQuery(state, upperID, "SELECT 9")
		require.NoError(t, err)

		proj := mustProject(t, state)
		require.Contains(t, proj.Widgets, canonID)
		require.Equal(t, Layout{Row: 1, Col: 2, Width: 3, Height: 4}, proj.Widgets[canonID].Layout)
		require.Equal(t, "SELECT 9", widgetQuery(t, proj.Widgets[canonID]))

		state, err = DeleteWidget(state, upperID)
		require.NoError(t, err)
		proj = mustProject(t, state)
		require.Empty(t, proj.Widgets)
	})

	t.Run("Seed rejects duplicate spellings", func(t *testing.T) {
		_, err := Seed(Projection{
			Widgets: map[string]WidgetDoc{
				upperID: {ID: upperID, Type: "table"},
				canonID: {ID: canonID, Type: "text"},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate widget ID")
	})
}
