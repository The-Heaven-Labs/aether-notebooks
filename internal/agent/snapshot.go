package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateNotebookSnapshot captures the full notebook state and returns the created snapshot.
func CreateNotebookSnapshot(ctx context.Context, pool *pgxpool.Pool, nbID, name string, userID string, auto bool) (*models.NotebookSnapshot, error) {
	var title string
	pool.QueryRow(ctx, `SELECT title FROM notebooks WHERE id=$1`, nbID).Scan(&title)

	rows, err := pool.Query(ctx,
		`SELECT id, type, language, source, position, connector_id, outputs,
		        "limit", source_visible, cell_collapsed, slide_break, metadata,
		        COALESCE(title,''), COALESCE(description,'')
		 FROM cells WHERE notebook_id=$1 ORDER BY position`,
		nbID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cellSources := map[string]string{}
	var cells []models.SnapshotCell
	for rows.Next() {
		var c models.SnapshotCell
		var outputs, metadata []byte
		var lang, connID, cTitle, cDesc *string
		var limit *int
		if err := rows.Scan(&c.ID, &c.Type, &lang, &c.Source, &c.Position, &connID,
			&outputs, &limit, &c.SourceVisible, &c.CellCollapsed, &c.SlideBreak, &metadata,
			&cTitle, &cDesc); err != nil {
			continue
		}
		if lang != nil {
			c.Language = *lang
		}
		if connID != nil {
			c.ConnectorID = *connID
		}
		if cTitle != nil {
			c.Title = *cTitle
		}
		if cDesc != nil {
			c.Description = *cDesc
		}
		c.Outputs = outputs
		c.Metadata = metadata
		c.Limit = limit

		cellSources[c.ID] = c.Source
		cells = append(cells, c)
	}

	sourcesJSON, _ := json.Marshal(cellSources)
	cellsJSON, _ := json.Marshal(cells)

	var userName string
	pool.QueryRow(ctx, `SELECT COALESCE(name, email) FROM users WHERE id=$1`, userID).Scan(&userName)

	var snap models.NotebookSnapshot
	var snapSourcesOut []byte
	err = pool.QueryRow(ctx,
		`INSERT INTO notebook_snapshots (notebook_id, name, title, cell_sources, cells, created_by, created_by_name, auto)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, notebook_id, name, title, cell_sources, cells, created_by, created_by_name, created_at, auto`,
		nbID, name, title, sourcesJSON, cellsJSON, userID, userName, auto,
	).Scan(&snap.ID, &snap.NotebookID, &snap.Name, &snap.Title, &snapSourcesOut, &cellsJSON,
		&snap.CreatedBy, &userName, &snap.CreatedAt, &snap.Auto)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(snapSourcesOut, &snap.CellSources)
	json.Unmarshal(cellsJSON, &snap.Cells)
	snap.User = &models.User{Name: userName}

	return &snap, nil
}

// EnsureAutoSnapshot creates an auto-snapshot if none has been created in the last 5 minutes.
func EnsureAutoSnapshot(ctx context.Context, pool *pgxpool.Pool, nbID, userID string) {
	var recentAuto bool
	pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM notebook_snapshots
			WHERE notebook_id=$1 AND auto=true AND created_at > NOW() - INTERVAL '5 minutes'
		)`, nbID,
	).Scan(&recentAuto)
	if recentAuto {
		return
	}

	name := "Auto-save " + time.Now().Format("2006-01-02 15:04")
	CreateNotebookSnapshot(ctx, pool, nbID, name, userID, true)
}

// RestoreNotebookSnapshot restores a notebook to the state captured in a snapshot.
// It updates the notebook title, creates/missing cells, deletes extra cells.
// The onCellRestored callback, if provided, is called for each restored cell
// (e.g., to record cell versions).
func RestoreNotebookSnapshot(ctx context.Context, pool *pgxpool.Pool, nbID, snapID, orgID, userID string, onCellRestored func(ctx context.Context, cellID, source, userID string) error) error {
	var snapTitle string
	var cellsJSON []byte
	err := pool.QueryRow(ctx,
		`SELECT ns.title, ns.cells
		 FROM notebook_snapshots ns
		 JOIN notebooks n ON n.id = ns.notebook_id
		 WHERE ns.id=$1 AND ns.notebook_id=$2 AND n.org_id=$3`,
		snapID, nbID, orgID,
	).Scan(&snapTitle, &cellsJSON)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("snapshot not found")
	}
	if err != nil {
		return fmt.Errorf("query snapshot: %w", err)
	}
	if cellsJSON == nil {
		return fmt.Errorf("snapshot has no cell data")
	}

	var snapshotCells []models.SnapshotCell
	json.Unmarshal(cellsJSON, &snapshotCells)

	snapCellIDs := make(map[string]bool)
	for _, c := range snapshotCells {
		snapCellIDs[c.ID] = true
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if snapTitle != "" {
		tx.Exec(ctx, `UPDATE notebooks SET title=$1, updated_at=NOW() WHERE id=$2`, snapTitle, nbID)
	}

	existingRows, err := tx.Query(ctx, `SELECT id FROM cells WHERE notebook_id=$1`, nbID)
	if err != nil {
		return fmt.Errorf("query existing cells: %w", err)
	}
	var existingIDs []string
	for existingRows.Next() {
		var id string
		existingRows.Scan(&id)
		existingIDs = append(existingIDs, id)
	}
	existingRows.Close()

	existingSet := make(map[string]bool)
	for _, id := range existingIDs {
		existingSet[id] = true
	}

	for _, id := range existingIDs {
		if !snapCellIDs[id] {
			tx.Exec(ctx, `DELETE FROM cells WHERE id=$1 AND notebook_id=$2`, id, nbID)
		}
	}

	for _, sc := range snapshotCells {
		if existingSet[sc.ID] {
			tx.Exec(ctx, `
				UPDATE cells SET type=$1, language=$2, source=$3, position=$4,
					connector_id=$5, outputs=$6, "limit"=$7,
					source_visible=$8, cell_collapsed=$9, slide_break=$10,
					metadata=$11, title=$12, description=$13,
					agent_updated_at=NULL, updated_at=NOW()
				WHERE id=$14 AND notebook_id=$15`,
				sc.Type, sc.Language, sc.Source, sc.Position,
				sc.ConnectorID, sc.Outputs, sc.Limit,
				sc.SourceVisible, sc.CellCollapsed, sc.SlideBreak,
				sc.Metadata, sc.Title, sc.Description,
				sc.ID, nbID,
			)
		} else {
			tx.Exec(ctx, `
				INSERT INTO cells (id, notebook_id, type, language, source, position,
					connector_id, outputs, "limit", source_visible, cell_collapsed,
					slide_break, metadata, title, description, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NOW(),NOW())`,
				sc.ID, nbID, sc.Type, sc.Language, sc.Source, sc.Position,
				sc.ConnectorID, sc.Outputs, sc.Limit,
				sc.SourceVisible, sc.CellCollapsed, sc.SlideBreak,
				sc.Metadata, sc.Title, sc.Description,
			)
		}

		if onCellRestored != nil {
			onCellRestored(ctx, sc.ID, sc.Source, userID)
		}
	}

	tx.Exec(ctx, `UPDATE notebooks SET updated_at=NOW() WHERE id=$1`, nbID)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit restore: %w", err)
	}

	return nil
}
