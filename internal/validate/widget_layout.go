package validate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/models"
)

// WidgetLayout checks that a widget layout is within grid bounds and does not
// overlap existing widgets in the same App chapter. The app renders one
// independent grid per chapter, so widgets at identical coordinates in
// different tabs coexist; without tabs the check stays global (today's
// behavior). excludeWidgetID is empty for create, set for update.
func WidgetLayout(ctx context.Context, pool *pgxpool.Pool, dashID string, layout models.WidgetLayout, excludeWidgetID string) error {
	if layout.Col < 0 || layout.Row < 0 || layout.Width <= 0 || layout.Height <= 0 {
		return fmt.Errorf("invalid layout dimensions")
	}

	// Fetch the dashboard's grid_cols setting (default 12) and tab list. An
	// empty or unknown layout.tab falls back to the first tab, mirroring the
	// app surface.
	var gridCols int
	var tabsJSON []byte
	err := pool.QueryRow(ctx,
		`SELECT COALESCE((settings->>'grid_cols')::int, 12), settings->'tabs' FROM dashboards WHERE id=$1`, dashID,
	).Scan(&gridCols, &tabsJSON)
	if err != nil {
		return fmt.Errorf("failed to read dashboard settings")
	}
	if gridCols <= 0 {
		gridCols = 12
	}

	if layout.Col+layout.Width > gridCols {
		return fmt.Errorf("layout exceeds grid columns")
	}

	var tabs []models.DashboardTab
	if len(tabsJSON) > 0 {
		if err := json.Unmarshal(tabsJSON, &tabs); err != nil {
			return fmt.Errorf("failed to read dashboard tabs")
		}
	}
	effectiveTab := effectiveWidgetTab(layout.Tab, tabs)

	// Check for overlap with existing widgets in the same effective chapter.
	rows, err := pool.Query(ctx,
		`SELECT id, layout FROM widgets WHERE dashboard_id=$1`, dashID,
	)
	if err != nil {
		return fmt.Errorf("failed to query existing widgets")
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var existingLayout models.WidgetLayout
		if err := rows.Scan(&id, &existingLayout); err != nil {
			continue
		}
		if excludeWidgetID != "" && id == excludeWidgetID {
			continue
		}
		if len(tabs) > 0 && effectiveWidgetTab(existingLayout.Tab, tabs) != effectiveTab {
			continue
		}
		// Bounding-box overlap: two rectangles overlap when their projections on both axes overlap.
		if layout.Col < existingLayout.Col+existingLayout.Width &&
			layout.Col+layout.Width > existingLayout.Col &&
			layout.Row < existingLayout.Row+existingLayout.Height &&
			layout.Row+layout.Height > existingLayout.Row {
			return fmt.Errorf("widget overlaps existing widget")
		}
	}

	return nil
}

// effectiveWidgetTab resolves a widget's chapter: a known layout.tab is
// itself; an empty or unknown tab falls back to the first tab. Returns ""
// when the dashboard defines no tabs (the global grid).
func effectiveWidgetTab(tab string, tabs []models.DashboardTab) string {
	if len(tabs) == 0 {
		return ""
	}
	if tab != "" {
		for _, t := range tabs {
			if t.ID == tab {
				return tab
			}
		}
	}
	return tabs[0].ID
}
