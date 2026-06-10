package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type executeRequest struct {
	Parameters map[string]string `json:"parameters,omitempty"`
}

func (s *Server) handleExecuteCell(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")

	var req executeRequest
	// Ignore body decode errors — params are optional
	decodeJSON(r, &req)
	if req.Parameters == nil {
		req.Parameters = map[string]string{}
	}

	ctx := r.Context()

	// Load cell
	var cell models.Cell
	var lang, connID *string
	var outputs, cellParamsJSON []byte
	var cellLimit *int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT c.id, c.notebook_id, c.position, c.type, c.language, c.connector_id, c.source, c.outputs, c.parameters, c."limit", c.created_at, c.updated_at
		 FROM cells c
		 JOIN notebooks n ON n.id = c.notebook_id
		 WHERE c.id = $1 AND c.notebook_id = $2 AND n.org_id = $3`,
		cellID, nbID, claims.OrgID,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cellParamsJSON, &cellLimit, &cell.CreatedAt, &cell.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if lang != nil {
		cell.Language = *lang
	}
	if connID != nil {
		cell.ConnectorID = *connID
	}
	if cellLimit != nil {
		cell.Limit = cellLimit
	}

	if cell.Type != models.CellTypeCode || cell.Language != "sql" {
		writeError(w, http.StatusBadRequest, "only SQL code cells can be executed")
		return
	}

	// Notebook connector fallback: if cell has no connector, try the notebook's connector
	if cell.ConnectorID == "" {
		var nbConnID *string
		if err := s.db.Pool.QueryRow(ctx, "SELECT connector_id FROM notebooks WHERE id = $1", nbID).Scan(&nbConnID); err != nil && err != pgx.ErrNoRows {
			writeError(w, http.StatusInternalServerError, "failed to load notebook connector")
			return
		}
		if nbConnID != nil {
			cell.ConnectorID = *nbConnID
		}
	}
	if cell.ConnectorID == "" {
		writeError(w, http.StatusBadRequest, "cell has no connector assigned")
		return
	}

	// Build slug map from all sibling cells in the notebook that have a slug
	slugMap := map[string]string{}
	slugRows, slugErr := s.db.Pool.Query(ctx,
		`SELECT slug, source FROM cells WHERE notebook_id = $1 AND slug IS NOT NULL AND slug != ''`,
		nbID,
	)
	if slugErr == nil {
		defer slugRows.Close()
		for slugRows.Next() {
			var slug, source string
			if scanErr := slugRows.Scan(&slug, &source); scanErr == nil {
				slugMap[slug] = source
			}
		}
	}

	// Load notebook parameters as defaults (before slug resolution so params are known)
	var paramsJSON2 []byte
	s.db.Pool.QueryRow(ctx, "SELECT parameters FROM notebooks WHERE id = $1", nbID).Scan(&paramsJSON2)
	var notebookParams []models.Parameter
	json.Unmarshal(paramsJSON2, &notebookParams)
	for _, p := range notebookParams {
		if _, ok := req.Parameters[p.Name]; !ok {
			req.Parameters[p.Name] = p.Default
		}
	}

	// Build set of known parameter names so slug resolver leaves them untouched
	knownParams := make(map[string]bool, len(req.Parameters))
	for k := range req.Parameters {
		knownParams[k] = true
	}

	// Resolve slug references in cell source (parameter refs pass through unchanged)
	resolvedSource, err := resolveSlugRefs(cell.Source, slugMap, knownParams)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Apply cell-level LIMIT
	if cell.Limit != nil {
		resolvedSource = executor.ApplyLimit(resolvedSource, *cell.Limit)
	}

	// Load connector
	var connType models.ConnectorType
	var encryptedConfig []byte
	var maxRows, timeout int
	err = s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted, max_rows, timeout_seconds
		 FROM connectors WHERE id = $1 AND org_id = $2`,
		cell.ConnectorID, claims.OrgID,
	).Scan(&connType, &encryptedConfig, &maxRows, &timeout)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load connector failed")
		return
	}

	// Decrypt connector config
	plain, err := crypto.Decrypt(encryptedConfig, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt connector config")
		return
	}

	// Apply cell parameter defaults for any keys not already set at runtime
	var cellParams []models.Parameter
	json.Unmarshal(cellParamsJSON, &cellParams)
	for _, p := range cellParams {
		if _, ok := req.Parameters[p.Name]; !ok {
			req.Parameters[p.Name] = p.Default
		}
	}

	// Build executor
	driver, ok := executor.GetDriver(connType)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector type")
		return
	}

	connectStart := time.Now()
	exec, err := driver.NewExecutor(plain)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect to database")
		return
	}
	defer exec.Close()
	connectTime := time.Since(connectStart).Milliseconds()

	// Execute
	queryStart := time.Now()
	result, err := exec.Execute(ctx, resolvedSource, req.Parameters, maxRows)
	if err != nil {
		// Store error output
		errOutput := models.Output{Type: "error", Data: map[string]string{"message": err.Error()}}
		outJSON, _ := json.Marshal([]models.Output{errOutput})
		s.db.Pool.Exec(ctx, "UPDATE cells SET outputs = $1, updated_at = NOW() WHERE id = $2", outJSON, cellID)
		s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": []models.Output{errOutput}})
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	queryTime := time.Since(queryStart).Milliseconds()

	// Store table output
	renderStart := time.Now()
	tableOutput := models.Output{Type: "table", Data: result}
	cellOutputs := []models.Output{tableOutput}
	outJSON, _ := json.Marshal(cellOutputs)
	s.db.Pool.Exec(ctx, "UPDATE cells SET outputs = $1, updated_at = NOW() WHERE id = $2", outJSON, cellID)
	s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": cellOutputs})
	renderTime := time.Since(renderStart).Milliseconds()

	totalTime := time.Since(startTime).Milliseconds()

	// Count rows
	rowCount := 0
	if result != nil {
		rowCount = len(result.Rows)
	}

	// Store execution log asynchronously
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.Pool.Exec(bgCtx,
			`INSERT INTO cell_execution_logs (cell_id, notebook_id, connector_id, connect_time_ms, query_time_ms, render_time_ms, total_time_ms, row_count)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			cellID, nbID, cell.ConnectorID, connectTime, queryTime, renderTime, totalTime, rowCount)
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"outputs": cellOutputs,
		"metrics": map[string]interface{}{
			"connect_time_ms": connectTime,
			"query_time_ms":   queryTime,
			"render_time_ms":  renderTime,
			"total_time_ms":   totalTime,
		},
	})

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.execute", ResourceType: "cell", ResourceID: cellID,
	})
}
