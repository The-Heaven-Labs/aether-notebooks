package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/pmezard/go-difflib/difflib"
)

const (
	versionMergeMaxDist = 50 // chars
	versionMergeWindow  = 60 * time.Second
)

// upsertCellVersion is called after each cell source save.
// It merges into the latest version if the edit is small and recent,
// otherwise inserts a new version row.
func (s *Server) upsertCellVersion(ctx context.Context, cellID, newSource string, userID string) error {
	var lastID string
	var lastSource string
	var lastCreatedAt time.Time

	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, source, created_at FROM cell_versions
		 WHERE cell_id = $1 ORDER BY created_at DESC LIMIT 1`,
		cellID,
	).Scan(&lastID, &lastSource, &lastCreatedAt)

	if err == pgx.ErrNoRows {
		// No versions yet — create first
		_, err = s.db.Pool.Exec(ctx,
			`INSERT INTO cell_versions (cell_id, source, created_by) VALUES ($1, $2, $3)`,
			cellID, newSource, userID,
		)
		return err
	}
	if err != nil {
		return err
	}

	dist := levenshteinDistance(lastSource, newSource)
	age := time.Since(lastCreatedAt)

	if dist < versionMergeMaxDist && age < versionMergeWindow {
		// Merge: update existing version in place
		_, err = s.db.Pool.Exec(ctx,
			`UPDATE cell_versions SET source = $1, created_by = $2 WHERE id = $3`,
			newSource, userID, lastID,
		)
		return err
	}

	// New version
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO cell_versions (cell_id, source, created_by) VALUES ($1, $2, $3)`,
		cellID, newSource, userID,
	)
	return err
}

func levenshteinDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				m := dp[i-1][j]
				if dp[i][j-1] < m {
					m = dp[i][j-1]
				}
				if dp[i-1][j-1] < m {
					m = dp[i-1][j-1]
				}
				dp[i][j] = 1 + m
			}
		}
	}
	return dp[la][lb]
}

// handleListCellVersions returns all versions for a cell, newest first.
func (s *Server) handleListCellVersions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	ctx := r.Context()

	var exists bool
	s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM cells c JOIN notebooks n ON n.id=c.notebook_id WHERE c.id=$1 AND c.notebook_id=$2 AND n.org_id=$3)`,
		cellID, nbID, claims.OrgID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT cv.id, cv.cell_id, cv.source, cv.created_at, cv.created_by,
		        u.id, u.name, u.email
		 FROM cell_versions cv
		 LEFT JOIN users u ON u.id = cv.created_by
		 WHERE cv.cell_id=$1 ORDER BY cv.created_at DESC`,
		cellID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var versions []models.CellVersion
	for rows.Next() {
		var v models.CellVersion
		var userID, userName, userEmail *string
		if err := rows.Scan(&v.ID, &v.CellID, &v.Source, &v.CreatedAt, &v.CreatedBy, &userID, &userName, &userEmail); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if userID != nil {
			v.User = &models.User{ID: *userID, Name: *userName, Email: *userEmail}
		}
		versions = append(versions, v)
	}
	if versions == nil {
		versions = []models.CellVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}

// handleRestoreCellVersion restores a cell to a previous version source.
func (s *Server) handleRestoreCellVersion(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	versionID := r.PathValue("version_id")
	ctx := r.Context()

	var source string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT cv.source FROM cell_versions cv
		 JOIN cells c ON c.id = cv.cell_id
		 JOIN notebooks n ON n.id = c.notebook_id
		 WHERE cv.id=$1 AND c.id=$2 AND c.notebook_id=$3 AND n.org_id=$4`,
		versionID, cellID, nbID, claims.OrgID,
	).Scan(&source)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Update the cell source
	var cell models.Cell
	var lang, connID *string
	var outputs []byte
	err = s.db.Pool.QueryRow(ctx,
		`UPDATE cells SET source=$1, updated_at=NOW()
		 WHERE id=$2 AND notebook_id=$3
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
		           source_visible, outputs_hidden, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		           created_at, updated_at`,
		source, cellID, nbID,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cell.SourceVisible, &cell.OutputsHidden, &cell.CellCollapsed,
		&cell.Title, &cell.Description, &cell.Slug,
		&cell.CreatedAt, &cell.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	if lang != nil {
		cell.Language = *lang
	}
	if connID != nil {
		cell.ConnectorID = *connID
	}
	json.Unmarshal(outputs, &cell.Outputs)

	// Version the restored source (best-effort — cell is already updated)
	_ = s.upsertCellVersion(ctx, cellID, source, claims.UserID)

	writeJSON(w, http.StatusOK, cell)
}

// Snapshot handlers

type createSnapshotRequest struct {
	Name string `json:"name"`
}



// computeCellDiff computes a line-level diff between old and new cell source.
func computeCellDiff(cellID string, position int, title, oldSource, newSource string) models.CellDiff {
	oldLines := difflib.SplitLines(oldSource)
	newLines := difflib.SplitLines(newSource)

	diff := models.CellDiff{
		CellID:    cellID,
		Position:  position + 1, // 1-indexed for display
		Title:     title,
		OldSource: oldSource,
		NewSource: newSource,
	}

	matcher := difflib.NewMatcher(oldLines, newLines)
	added := 0
	deleted := 0
	oldLineNum := 0
	newLineNum := 0

	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'e':
			// Equal lines — show a few context lines
			for i := op.I1; i < op.I2 && i < op.I1+3; i++ {
				oldLineNum++
				newLineNum++
				diff.DiffLines = append(diff.DiffLines, models.CellDiffLine{
					Type: "ctx", Line: oldLines[i],
					OldNum: oldLineNum, NewNum: newLineNum,
				})
			}
			if op.I2-op.I1 > 3 {
				oldLineNum += op.I2 - op.I1 - 3
				newLineNum += op.I2 - op.I1 - 3
			}
		case 'd':
			for i := op.I1; i < op.I2; i++ {
				oldLineNum++
				deleted++
				diff.DiffLines = append(diff.DiffLines, models.CellDiffLine{
					Type: "del", Line: oldLines[i],
					OldNum: oldLineNum,
				})
			}
		case 'i':
			for i := op.J1; i < op.J2; i++ {
				newLineNum++
				added++
				diff.DiffLines = append(diff.DiffLines, models.CellDiffLine{
					Type: "add", Line: newLines[i],
					NewNum: newLineNum,
				})
			}
		case 'r':
			for i := op.I1; i < op.I2; i++ {
				oldLineNum++
				deleted++
				diff.DiffLines = append(diff.DiffLines, models.CellDiffLine{
					Type: "del", Line: oldLines[i],
					OldNum: oldLineNum,
				})
			}
			for i := op.J1; i < op.J2; i++ {
				newLineNum++
				added++
				diff.DiffLines = append(diff.DiffLines, models.CellDiffLine{
					Type: "add", Line: newLines[i],
					NewNum: newLineNum,
				})
			}
		}
	}

	if added > 0 || deleted > 0 {
		diff.Summary = fmt.Sprintf("+%d/-%d lines", added, deleted)
	}
	return diff
}

// computeSnapshotChanges computes the diff between two snapshots.
func computeSnapshotChanges(prev, curr *models.NotebookSnapshot) *models.SnapshotChanges {
	if prev == nil || curr == nil {
		return nil
	}

	changes := &models.SnapshotChanges{
		TitleChanged: prev.Title != curr.Title,
		OldTitle:     prev.Title,
		NewTitle:     curr.Title,
	}

	prevCells := make(map[string]models.SnapshotCell)
	for _, c := range prev.Cells {
		prevCells[c.ID] = c
	}
	currCells := make(map[string]models.SnapshotCell)
	for _, c := range curr.Cells {
		currCells[c.ID] = c
	}

	prevPositions := make(map[string]int)
	for _, c := range prev.Cells {
		prevPositions[c.ID] = c.Position
	}
	currPositions := make(map[string]int)
	for _, c := range curr.Cells {
		currPositions[c.ID] = c.Position
	}

	for _, c := range curr.Cells {
		if _, ok := prevCells[c.ID]; !ok {
			changes.CellsAdded = append(changes.CellsAdded, models.CellChange{
				CellID: c.ID, Position: c.Position + 1, Title: c.Title,
			})
		} else if prevCells[c.ID].Source != c.Source {
			changes.CellsModified = append(changes.CellsModified, models.CellChange{
				CellID: c.ID, Position: c.Position + 1, Title: c.Title,
			})
			diff := computeCellDiff(c.ID, c.Position, c.Title, prevCells[c.ID].Source, c.Source)
			if len(diff.DiffLines) > 0 {
				changes.CellDiffs = append(changes.CellDiffs, diff)
			}
		}
	}
	for _, c := range prev.Cells {
		if _, ok := currCells[c.ID]; !ok {
			changes.CellsDeleted = append(changes.CellsDeleted, models.CellChange{
				CellID: c.ID, Position: c.Position + 1, Title: c.Title,
			})
		}
	}

	for id, pos := range currPositions {
		if prevPos, ok := prevPositions[id]; ok && prevPos != pos {
			c := currCells[id]
			changes.PositionsChanged = append(changes.PositionsChanged, models.CellChange{
				CellID: c.ID, Position: c.Position + 1, OldPosition: prevPos + 1, Title: c.Title,
			})
		}
	}

	return changes
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	var req createSnapshotRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ctx := r.Context()

	snap, err := agent.CreateNotebookSnapshot(ctx, s.db.Pool, nbID, req.Name, claims.UserID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create snapshot failed")
		return
	}

	writeJSON(w, http.StatusCreated, snap)
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	ctx := r.Context()

	var exists bool
	s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)`, nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT ns.id, ns.notebook_id, ns.name, ns.title, ns.cell_sources, ns.cells,
		        ns.created_by, ns.created_by_name, ns.created_at, ns.auto,
		        u.id, u.name, u.email
		 FROM notebook_snapshots ns
		 LEFT JOIN users u ON u.id = ns.created_by
		 WHERE ns.notebook_id=$1
		 ORDER BY ns.created_at DESC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var snaps []models.NotebookSnapshot
	for rows.Next() {
		var snap models.NotebookSnapshot
		var sourcesOut, cellsOut []byte
		var userName string
		var uID, uName, uEmail *string
		rows.Scan(&snap.ID, &snap.NotebookID, &snap.Name, &snap.Title,
			&sourcesOut, &cellsOut, &snap.CreatedBy, &userName, &snap.CreatedAt, &snap.Auto,
			&uID, &uName, &uEmail)
		json.Unmarshal(sourcesOut, &snap.CellSources)
		if cellsOut != nil {
			json.Unmarshal(cellsOut, &snap.Cells)
		}
		if uID != nil {
			snap.User = &models.User{ID: *uID, Name: *uName, Email: *uEmail}
		} else if userName != "" {
			snap.User = &models.User{Name: userName}
		}
		snaps = append(snaps, snap)
	}
	if snaps == nil {
		snaps = []models.NotebookSnapshot{}
	}

	// Compute changes between consecutive snapshots (they are ordered DESC,
	// so compute between each pair and assign to the newer one)
	for i := 0; i < len(snaps)-1; i++ {
		// snaps[i] is newer (DESC), snaps[i+1] is older
		changes := computeSnapshotChanges(&snaps[i+1], &snaps[i])
		snaps[i].Changes = changes
	}

	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	snapID := r.PathValue("snapshot_id")
	ctx := r.Context()

	onRestored := func(cctx context.Context, cellID, source, userID string) error {
		return s.upsertCellVersion(cctx, cellID, source, userID)
	}

	if err := agent.RestoreNotebookSnapshot(ctx, s.db.Pool, nbID, snapID, claims.OrgID, claims.UserID, onRestored); err != nil {
		if err.Error() == "snapshot not found" {
			writeError(w, http.StatusNotFound, "snapshot not found")
		} else {
			writeError(w, http.StatusInternalServerError, "restore failed")
		}
		return
	}

	_ = s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "snapshot.restore", ResourceType: "notebook", ResourceID: nbID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (s *Server) handleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	snapID := r.PathValue("snapshot_id")
	againstID := r.URL.Query().Get("against")
	ctx := r.Context()

	if againstID == "" {
		writeError(w, http.StatusBadRequest, "against query param is required")
		return
	}

	var currTitle string
	var currCellsJSON []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT ns.title, ns.cells
		 FROM notebook_snapshots ns
		 JOIN notebooks n ON n.id = ns.notebook_id
		 WHERE ns.id=$1 AND ns.notebook_id=$2 AND n.org_id=$3`,
		snapID, nbID, claims.OrgID,
	).Scan(&currTitle, &currCellsJSON)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var prevTitle string
	var prevCellsJSON []byte
	err = s.db.Pool.QueryRow(ctx,
		`SELECT title, cells FROM notebook_snapshots WHERE id=$1 AND notebook_id=$2`,
		againstID, nbID,
	).Scan(&prevTitle, &prevCellsJSON)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "comparison snapshot not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var prevCells, currCells []models.SnapshotCell
	if prevCellsJSON != nil {
		json.Unmarshal(prevCellsJSON, &prevCells)
	}
	if currCellsJSON != nil {
		json.Unmarshal(currCellsJSON, &currCells)
	}

	prev := &models.NotebookSnapshot{Title: prevTitle, Cells: prevCells}
	curr := &models.NotebookSnapshot{Title: currTitle, Cells: currCells}
	changes := computeSnapshotChanges(prev, curr)

	writeJSON(w, http.StatusOK, changes)
}
