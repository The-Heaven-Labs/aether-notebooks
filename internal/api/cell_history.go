package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

const (
	versionMergeMaxDist = 50 // chars
	versionMergeWindow  = 60 * time.Second
)

// upsertCellVersion is called after each cell source save.
// It merges into the latest version if the edit is small and recent,
// otherwise inserts a new version row.
func (s *Server) upsertCellVersion(ctx context.Context, cellID, newSource string) error {
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
			`INSERT INTO cell_versions (cell_id, source) VALUES ($1, $2)`,
			cellID, newSource,
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
			`UPDATE cell_versions SET source = $1 WHERE id = $2`,
			newSource, lastID,
		)
		return err
	}

	// New version
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO cell_versions (cell_id, source) VALUES ($1, $2)`,
		cellID, newSource,
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
		`SELECT id, cell_id, source, created_at FROM cell_versions WHERE cell_id=$1 ORDER BY created_at DESC`,
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
		if err := rows.Scan(&v.ID, &v.CellID, &v.Source, &v.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
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
		           source_visible, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		           created_at, updated_at`,
		source, cellID, nbID,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cell.SourceVisible, &cell.CellCollapsed,
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
	_ = s.upsertCellVersion(ctx, cellID, source)

	writeJSON(w, http.StatusOK, cell)
}

// Snapshot handlers

type createSnapshotRequest struct {
	Name string `json:"name"`
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

	// Collect all cell sources
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, source FROM cells WHERE notebook_id=$1 ORDER BY position`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query cells failed")
		return
	}
	defer rows.Close()

	cellSources := map[string]string{}
	for rows.Next() {
		var id, src string
		rows.Scan(&id, &src)
		cellSources[id] = src
	}

	sourcesJSON, _ := json.Marshal(cellSources)

	var snap models.NotebookSnapshot
	var sourcesOut []byte
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebook_snapshots (notebook_id, name, cell_sources, created_by)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, notebook_id, name, cell_sources, created_by, created_at`,
		nbID, req.Name, sourcesJSON, claims.UserID,
	).Scan(&snap.ID, &snap.NotebookID, &snap.Name, &sourcesOut, &snap.CreatedBy, &snap.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create snapshot failed")
		return
	}
	json.Unmarshal(sourcesOut, &snap.CellSources)
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
		`SELECT id, notebook_id, name, cell_sources, created_by, created_at
		 FROM notebook_snapshots WHERE notebook_id=$1 ORDER BY created_at DESC`,
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
		var sourcesOut []byte
		rows.Scan(&snap.ID, &snap.NotebookID, &snap.Name, &sourcesOut, &snap.CreatedBy, &snap.CreatedAt)
		json.Unmarshal(sourcesOut, &snap.CellSources)
		snaps = append(snaps, snap)
	}
	if snaps == nil {
		snaps = []models.NotebookSnapshot{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	snapID := r.PathValue("snapshot_id")
	ctx := r.Context()

	var sourcesJSON []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT ns.cell_sources FROM notebook_snapshots ns
		 JOIN notebooks n ON n.id = ns.notebook_id
		 WHERE ns.id=$1 AND ns.notebook_id=$2 AND n.org_id=$3`,
		snapID, nbID, claims.OrgID,
	).Scan(&sourcesJSON)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var cellSources map[string]string
	json.Unmarshal(sourcesJSON, &cellSources)

	for cellID, src := range cellSources {
		if _, err := s.db.Pool.Exec(ctx, `UPDATE cells SET source=$1, updated_at=NOW() WHERE id=$2 AND notebook_id=$3`, src, cellID, nbID); err != nil {
			writeError(w, http.StatusInternalServerError, "restore failed")
			return
		}
		s.upsertCellVersion(ctx, cellID, src)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}
