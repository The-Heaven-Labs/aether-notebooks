package dashboarddoc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// White-box test for the content-hash canonicalization. The HTTP-level
// publish/list/restore coverage lives in internal/api
// (dashboard_versions_test.go); this pins the canonical encoding itself.
func TestContentHash(t *testing.T) {
	connectorID := "11111111-1111-4111-8111-111111111111"
	queryA := "SELECT 1 AS x"
	queryB := "SELECT 2 AS x"
	widgetA := SnapshotWidget{
		ID: "22222222-2222-4222-8222-222222222222", Type: "table",
		Layout:      Layout{Row: 0, Col: 0, Width: 6, Height: 6},
		ConnectorID: &connectorID, Query: &queryA, Language: "sql",
		Config: map[string]interface{}{"chart": "bar"},
	}
	widgetB := SnapshotWidget{
		ID: "33333333-3333-4333-8333-333333333333", Type: "text",
		Layout: Layout{Row: 0, Col: 6, Width: 6, Height: 4},
		Config: map[string]interface{}{"text": "hello"},
	}
	settingsA := map[string]any{"grid_cols": 12, "variables": []any{map[string]any{"name": "who"}}}
	settingsB := map[string]any{"variables": []any{map[string]any{"name": "who"}}, "grid_cols": 12}

	base := ContentHash("Dash", settingsA, []SnapshotWidget{widgetA, widgetB})
	require.Len(t, base, 64)
	require.Equal(t, base, ContentHash("Dash", settingsA, []SnapshotWidget{widgetA, widgetB}),
		"the same content must hash identically")
	require.Equal(t, base, ContentHash("Dash", settingsB, []SnapshotWidget{widgetB, widgetA}),
		"settings and widget order must not change the hash")

	changedQuery := widgetA
	changedQuery.Query = &queryB
	require.NotEqual(t, base, ContentHash("Dash", settingsA, []SnapshotWidget{changedQuery, widgetB}),
		"a changed query must change the hash")
	require.NotEqual(t, base, ContentHash("Other", settingsA, []SnapshotWidget{widgetA, widgetB}),
		"a changed title must change the hash")
}
