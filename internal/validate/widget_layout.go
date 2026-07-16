package validate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/models"
)

// WidgetLayout checks that a widget layout is within grid bounds and does not
// overlap existing widgets. excludeWidgetID is empty for create, set for update.
func WidgetLayout(ctx context.Context, pool *pgxpool.Pool, dashID string, layout models.WidgetLayout, excludeWidgetID string) error {
	if layout.Col < 0 || layout.Row < 0 || layout.Width <= 0 || layout.Height <= 0 {
		return fmt.Errorf("invalid layout dimensions")
	}

	// Fetch the dashboard's grid_cols setting (default 12).
	var gridCols int
	err := pool.QueryRow(ctx,
		`SELECT COALESCE((settings->>'grid_cols')::int, 12) FROM dashboards WHERE id=$1`, dashID,
	).Scan(&gridCols)
	if err != nil {
		return fmt.Errorf("failed to read dashboard settings")
	}
	if gridCols <= 0 {
		gridCols = 12
	}

	if layout.Col+layout.Width > gridCols {
		return fmt.Errorf("layout exceeds grid columns")
	}

	// Check for overlap with existing widgets.
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
