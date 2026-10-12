package api

// White-box test for the content-hash canonicalization. It lives in package api
// because dashboardContentHash is unexported; the HTTP-level publish/list/
// restore coverage lives in dashboard_versions_test.go (package api_test).

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/models"
)

func TestDashboardContentHash(t *testing.T) {
	connectorID := "11111111-1111-4111-8111-111111111111"
	queryA := "SELECT 1 AS x"
	queryB := "SELECT 2 AS x"
	widgetA := snapshotWidget{
		ID: "22222222-2222-4222-8222-222222222222", Type: "table",
		Layout:      models.WidgetLayout{Row: 0, Col: 0, Width: 6, Height: 6},
		ConnectorID: &connectorID, Query: &queryA, Language: "sql",
		Config: map[string]interface{}{"chart": "bar"},
	}
	widgetB := snapshotWidget{
		ID: "33333333-3333-4333-8333-333333333333", Type: "text",
		Layout: models.WidgetLayout{Row: 0, Col: 6, Width: 6, Height: 4},
		Config: map[string]interface{}{"text": "hello"},
	}
	settingsA := map[string]any{"grid_cols": 12, "variables": []any{map[string]any{"name": "who"}}}
	settingsB := map[string]any{"variables": []any{map[string]any{"name": "who"}}, "grid_cols": 12}

	base := dashboardContentHash("Dash", settingsA, []snapshotWidget{widgetA, widgetB})
	require.Len(t, base, 64)
	require.Equal(t, base, dashboardContentHash("Dash", settingsA, []snapshotWidget{widgetA, widgetB}),
		"the same content must hash identically")
	require.Equal(t, base, dashboardContentHash("Dash", settingsB, []snapshotWidget{widgetB, widgetA}),
		"settings and widget order must not change the hash")

	changedQuery := widgetA
	changedQuery.Query = &queryB
	require.NotEqual(t, base, dashboardContentHash("Dash", settingsA, []snapshotWidget{changedQuery, widgetB}),
		"a changed query must change the hash")
	require.NotEqual(t, base, dashboardContentHash("Other", settingsA, []snapshotWidget{widgetA, widgetB}),
		"a changed title must change the hash")
}
