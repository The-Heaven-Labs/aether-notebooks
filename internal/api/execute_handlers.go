package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

type executeRequest struct {
	Parameters map[string]string `json:"parameters,omitempty"`
}

// @Summary Execute a cell
// @Description Execute a cell's SQL query and return results
// @Tags cells
// @Accept json
// @Produce json
// @Param notebook_id path string true "Notebook ID"
// @Param cell_id path string true "Cell ID"
// @Param request body object false "Execution parameters"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{notebook_id}/cells/{cell_id}/execute [post]
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

	// Check "run" permission on the notebook
	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "run"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions to execute cells in this notebook")
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

	// Check "use" permission on the connector
	useOK, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", cell.ConnectorID, "use")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !useOK {
		writeError(w, http.StatusForbidden, "you don't have permission to use this connector")
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
		 FROM connectors WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
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

	// ClickHouse connectors linked to a ready warehouse execute as the
	// requesting user's provisioned ClickHouse identity through the shared
	// connection pool. Unmanaged ClickHouse connectors and every other type
	// keep the legacy stored-credential driver path, so routing is gated on
	// the connector type before warehouse resolution is consulted.
	var (
		exec        executor.Executor
		warehouseID string
		chUser      string
		auditConnID = cell.ConnectorID
	)
	connectStart := time.Now()
	switch {
	case connType == models.ConnectorClickHouse:
		userUUID, userErr := uuid.Parse(claims.UserID)
		if userErr != nil {
			writeError(w, http.StatusInternalServerError, "invalid user id")
			return
		}
		// cells.connector_id is a UUID column, so parsing cannot fail.
		connUUID := uuid.MustParse(cell.ConnectorID)
		target, targetErr := s.resolveExecutionTarget(ctx, userUUID, connUUID, false)
		var choice *executor.ServiceChoiceError
		switch {
		case targetErr == nil:
			conn, release, getErr := s.connPool.Get(target.Endpoint, target.CHUser, target.Config)
			if getErr != nil {
				writeError(w, http.StatusBadGateway, "failed to connect to database")
				return
			}
			// The pooled executor owns the lease: Close releases it, so
			// release must not be deferred separately.
			exec = executor.NewPooledClickHouseExecutor(conn, release)
			warehouseID = target.WarehouseID.String()
			chUser = target.CHUser
			auditConnID = target.ConnectorID.String()
			// Limits belong to the service actually dialed: a preference or
			// sole-service fallback can route to a different connector than
			// the cell's requested one.
			maxRows = target.MaxRows
			timeout = target.TimeoutSeconds
		case errors.Is(targetErr, executor.ErrUnmanagedConnector):
			exec, err = driver.NewExecutor(plain)
			if err != nil {
				writeError(w, http.StatusBadGateway, "failed to connect to database")
				return
			}
		case errors.Is(targetErr, executor.ErrConnectorNotFound):
			writeError(w, http.StatusNotFound, "connector not found")
			return
		case errors.Is(targetErr, executor.ErrProvisioningNotReady):
			writeError(w, http.StatusServiceUnavailable, "warehouse provisioning is not ready")
			return
		case errors.Is(targetErr, executor.ErrServiceAccessDenied):
			writeError(w, http.StatusForbidden, "no permitted service in warehouse")
			return
		case errors.As(targetErr, &choice):
			writeServiceChoiceRequired(w, choice)
			return
		default:
			slog.Error("resolve execution target", "connector_id", cell.ConnectorID, "error", targetErr)
			writeError(w, http.StatusInternalServerError, "failed to resolve execution target")
			return
		}
	default:
		exec, err = driver.NewExecutor(plain)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to connect to database")
			return
		}
	}
	defer exec.Close()
	connectTime := time.Since(connectStart).Milliseconds()

	bgCtx := context.Background()

	// Create cancelable context for query execution
	execCtx, execCancel := context.WithCancel(bgCtx)
	s.hub.SetCancelFunc(cellID, execCancel)

	// Tag the context with the user email for ClickHouse query tracing
	execCtx = context.WithValue(execCtx, executor.CtxUserEmail{}, s.userEmail(bgCtx, claims.UserID))

	// Set query timeout from connector config (0 = unlimited)
	var timeoutCancel context.CancelFunc
	if timeout > 0 {
		execCtx, timeoutCancel = context.WithTimeout(execCtx, time.Duration(timeout)*time.Second)
	}
	defer func() {
		if timeoutCancel != nil {
			timeoutCancel()
		}
	}()

	// Resolve the org's per-cell output byte cap (clamped by the platform
	// ceiling) before broadcasting the running state so a lookup failure does
	// not leave the cell stuck as running.
	cellMaxBytes, capErr := s.orgCellOutputMaxBytes(bgCtx, claims.OrgID)
	if capErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to load output limits")
		return
	}

	// Broadcast executing state and track in Hub (survives page refresh)
	execStart := time.Now()
	s.hub.Broadcast(nbID, map[string]any{"type": "cell_executing", "cell_id": cellID, "started_at": execStart, "user_email": s.userEmail(bgCtx, claims.UserID)})
	s.hub.SetRunning(cellID, nbID, execStart)

	// Execute — respects cancellation, connector timeout, and the org's
	// per-cell output byte cap (clamped by the platform ceiling).
	queryStart := time.Now()
	result, err := exec.Execute(execCtx, resolvedSource, req.Parameters, executor.OutputLimits{MaxBytes: cellMaxBytes, MaxRows: maxRows})
	wasCancelled := execCtx.Err() != nil
	execCancel()
	s.hub.DeleteCancelFunc(cellID)
	s.hub.UnsetRunning(cellID)

	// Some drivers (ClickHouse) may return empty results instead of context.Canceled
	// when the query was cancelled mid-flight. Check if our context was cancelled.
	if err == nil && wasCancelled && (result == nil || len(result.Rows) == 0) {
		err = context.Canceled
	}

	if err != nil {
		// Check if the query was cancelled by the user
		errMsg := err.Error()
		isCancelled := errors.Is(err, context.Canceled) || strings.Contains(errMsg, "context canceled") || strings.Contains(errMsg, "cancelled")
		switch {
		case isCancelled:
			errMsg = "Query cancelled"
		case errors.Is(err, context.DeadlineExceeded) || strings.Contains(errMsg, "context deadline exceeded"):
			errMsg = "Query timed out"
		}
		// Store error output
		errTotalTime := time.Since(startTime).Milliseconds()
		errOutput := models.Output{Type: "error", Data: map[string]string{"message": errMsg}}
		outJSON, _ := json.Marshal([]models.Output{errOutput})
		s.db.Pool.Exec(bgCtx, "UPDATE cells SET outputs = $1, duration_ms = $2, updated_at = NOW() WHERE id = $3", outJSON, errTotalTime, cellID)
		s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": []models.Output{errOutput}, "user_email": s.userEmail(bgCtx, claims.UserID)})
		s.hub.Broadcast(nbID, map[string]any{"type": "cell_cancelled", "cell_id": cellID})
		status := http.StatusUnprocessableEntity
		if !isCancelled && executor.IsClickHouseAccessDenied(err) {
			// A per-user warehouse identity hitting an ungranted table is an
			// authorization failure, not an unprocessable query.
			status = http.StatusForbidden
		}
		writeError(w, status, errMsg)
		return
	}
	queryTime := time.Since(queryStart).Milliseconds()

	// Store table output
	renderStart := time.Now()
	tableOutput := models.Output{Type: "table", Data: result}
	cellOutputs := []models.Output{tableOutput}
	outJSON, _ := json.Marshal(cellOutputs)
	totalTime := time.Since(startTime).Milliseconds()
	s.db.Pool.Exec(bgCtx, "UPDATE cells SET outputs = $1, duration_ms = $2, updated_at = NOW() WHERE id = $3", outJSON, totalTime, cellID)
	s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": cellOutputs, "user_email": s.userEmail(bgCtx, claims.UserID)})
	renderTime := time.Since(renderStart).Milliseconds()

	// Count rows
	rowCount := 0
	if result != nil {
		rowCount = len(result.Rows)
	}

	// Store execution log asynchronously
	go func() {
		logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.Pool.Exec(logCtx,
			`INSERT INTO cell_execution_logs (cell_id, notebook_id, connector_id, connect_time_ms, query_time_ms, render_time_ms, total_time_ms, row_count)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			cellID, nbID, auditConnID, connectTime, queryTime, renderTime, totalTime, rowCount)
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

	s.audit.Log(bgCtx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.execute", ResourceType: "cell", ResourceID: cellID,
		Metadata: auditMetadata(nbID, cellID, auditConnID, cell.Source, rowCount, totalTime, warehouseID, chUser),
	})
}

// auditMetadata builds the cell.execute audit payload. warehouse_id and ch_user
// are present only when the run executed as a warehouse per-user identity;
// connector_id is the service actually dialed, which can differ from the
// cell's connector when warehouse routing picks a preferred service.
func auditMetadata(notebookID, cellID, connectorID, query string, rowCount int, durationMS int64, warehouseID, chUser string) map[string]any {
	metadata := map[string]any{
		"notebook_id":  notebookID,
		"cell_id":      cellID,
		"connector_id": connectorID,
		"query":        query,
		"row_count":    rowCount,
		"duration_ms":  durationMS,
	}
	if warehouseID != "" {
		metadata["warehouse_id"] = warehouseID
		metadata["ch_user"] = chUser
	}
	return metadata
}

// writeServiceChoiceRequired renders the 409 payload used to prompt for a
// warehouse service. The caller has already matched the concrete error, so the
// allowed list is always present.
func writeServiceChoiceRequired(w http.ResponseWriter, choice *executor.ServiceChoiceError) {
	services := make([]map[string]string, 0, len(choice.Allowed))
	for _, svc := range choice.Allowed {
		services = append(services, map[string]string{
			"connector_id": svc.ConnectorID.String(),
			"name":         svc.Name,
		})
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":    "service_choice_required",
		"services": services,
	})
}

// @Summary Cancel a running cell execution
// @Description Cancel the currently running query for a cell
// @Tags cells
// @Accept json
// @Produce json
// @Param notebook_id path string true "Notebook ID"
// @Param cell_id path string true "Cell ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{notebook_id}/cells/{cell_id}/cancel [post]
func (s *Server) handleCancelCell(w http.ResponseWriter, r *http.Request) {
	cellID := r.PathValue("cell_id")

	cancel, ok := s.hub.GetCancelFunc(cellID)
	if !ok {
		writeError(w, http.StatusNotFound, "no running query found for this cell")
		return
	}

	cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
