package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// warehouseGrantJSON is the API representation of a warehouse_table_grants
// row. SubjectName/SubjectEmail are denormalized so the admin matrix can label
// subjects without a second lookup; the stored grant carries only the
// canonical subject UUID (or "everyone").
type warehouseGrantJSON struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	WarehouseID  string    `json:"warehouse_id"`
	SubjectType  string    `json:"subject_type"`
	SubjectID    string    `json:"subject_id"`
	SubjectName  string    `json:"subject_name,omitempty"`
	SubjectEmail string    `json:"subject_email,omitempty"`
	Database     string    `json:"database"`
	Table        string    `json:"table"`
	CreatedBy    *string   `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// warehouseGrantCreateJSON is the POST response: the created (or pre-existing,
// for an idempotent replay) grant plus an advisory warning when the subject
// cannot use any of the warehouse's services yet. The grant is still saved;
// warning is a state report, not an error.
type warehouseGrantCreateJSON struct {
	warehouseGrantJSON
	Warning string `json:"warning,omitempty"`
}

// warehouseEffectiveTableJSON is one table in a subject's effective union.
type warehouseEffectiveTableJSON struct {
	Database string `json:"database"`
	Table    string `json:"table"`
}

// warehouseEffectiveServiceJSON is one service the subject may execute on.
// Preferred mirrors the stored routing preference; it is false for every
// service when no preference is set.
type warehouseEffectiveServiceJSON struct {
	ConnectorID string `json:"connector_id"`
	Name        string `json:"name"`
	Preferred   bool   `json:"preferred"`
}

// warehouseEffectiveAccessJSON answers "what can this subject touch in this
// warehouse": the union of direct, group, and everyone table grants, the
// ClickHouse identity names the sync worker uses for this subject, and the
// services the subject may route to. CHUser/CHRoles are emitted only when the
// grant union is non-empty, matching chaccess.Compute's provisioning
// condition; a direct-grants-only subject has no CHRoles. PreferredConnectorID
// is null unless the stored preference still names an allowed service.
type warehouseEffectiveAccessJSON struct {
	UserID               string                          `json:"user_id"`
	WarehouseID          string                          `json:"warehouse_id"`
	CHUser               string                          `json:"ch_user,omitempty"`
	CHRoles              []string                        `json:"ch_roles,omitempty"`
	Tables               []warehouseEffectiveTableJSON   `json:"tables"`
	Services             []warehouseEffectiveServiceJSON `json:"services"`
	PreferredConnectorID *string                         `json:"preferred_connector_id"`
}

const warehouseGrantSelect = `
	SELECT wtg.id, wtg.org_id, wtg.warehouse_id, wtg.subject_type, wtg.subject_id,
	       COALESCE(u.name, g.name, '') AS subject_name,
	       COALESCE(u.email, '') AS subject_email,
	       wtg.database_name, wtg.table_name, wtg.created_by, wtg.created_at
	FROM warehouse_table_grants wtg
	LEFT JOIN users u ON wtg.subject_type = 'user' AND u.id::text = wtg.subject_id
	LEFT JOIN groups g ON wtg.subject_type = 'group' AND g.id::text = wtg.subject_id`

// maxWarehouseGrantRows bounds the grant list response. A warehouse with more
// explicit grants than this is far outside expected use, and the UI cannot
// render more anyway.
const maxWarehouseGrantRows = 1000

// grantValidationError marks a request that must be rejected with 400. Any
// other error from the same helpers is a database failure and maps to 500.
type grantValidationError struct{ msg string }

func (e *grantValidationError) Error() string { return e.msg }

func invalidGrant(format string, args ...any) error {
	return &grantValidationError{msg: fmt.Sprintf(format, args...)}
}

// writeGrantValidationError maps a subject/name validation failure to a 400
// and anything else to a 500.
func writeGrantValidationError(w http.ResponseWriter, err error) {
	var invalid *grantValidationError
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, invalid.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to validate grant")
}

// validateGrantSubject canonicalizes a grant subject and verifies it belongs
// to the warehouse's org: "user" must be an org member, "group" must be an org
// group, and "everyone" carries the fixed canonical ID. subject_id is stored
// as text with no foreign key, so this validation is the only thing keeping a
// grant from naming a subject in another org.
func (s *Server) validateGrantSubject(ctx context.Context, orgID, subjectType, subjectID string) (string, error) {
	switch subjectType {
	case "everyone":
		if subjectID != "" && subjectID != "everyone" {
			return "", invalidGrant(`subject_id must be "everyone" for subject_type everyone`)
		}
		return "everyone", nil
	case "user":
		parsed, err := uuid.Parse(subjectID)
		if err != nil {
			return "", invalidGrant("subject_id must be a UUID for subject_type user")
		}
		var member bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
			orgID, parsed.String()).Scan(&member); err != nil {
			return "", fmt.Errorf("check org membership: %w", err)
		}
		if !member {
			return "", invalidGrant("subject user is not a member of this organization")
		}
		return parsed.String(), nil
	case "group":
		parsed, err := uuid.Parse(subjectID)
		if err != nil {
			return "", invalidGrant("subject_id must be a UUID for subject_type group")
		}
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM groups WHERE id = $1 AND org_id = $2)`,
			parsed.String(), orgID).Scan(&exists); err != nil {
			return "", fmt.Errorf("check group: %w", err)
		}
		if !exists {
			return "", invalidGrant("subject group does not belong to this organization")
		}
		return parsed.String(), nil
	default:
		return "", invalidGrant("subject_type must be one of user, group, everyone")
	}
}

// validateGrantObjectNames rejects database/table names outside the ClickHouse
// catalog charset, which also rejects every wildcard spelling (`*`, `db.*`,
// `events*`). Validation goes through chaccess.QuoteObjectIdent so the
// handler and the sync worker agree on exactly which names are representable.
func validateGrantObjectNames(database, table string) error {
	if database == "" {
		return invalidGrant("database is required")
	}
	if table == "" {
		return invalidGrant("table is required")
	}
	if _, err := chaccess.QuoteObjectIdent(database); err != nil {
		return invalidGrant("invalid database name %q: wildcards and unsupported characters are not allowed", database)
	}
	if _, err := chaccess.QuoteObjectIdent(table); err != nil {
		return invalidGrant("invalid table name %q: wildcards and unsupported characters are not allowed", table)
	}
	return nil
}

// isForeignKeyViolation reports a SQLSTATE 23503 error. A grant write whose
// warehouse vanished between the handler's existence check and the INSERT
// fails its composite FK instead of inserting; callers map it to 404.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func scanWarehouseGrant(row pgx.Row) (warehouseGrantJSON, error) {
	var g warehouseGrantJSON
	err := row.Scan(&g.ID, &g.OrgID, &g.WarehouseID, &g.SubjectType, &g.SubjectID,
		&g.SubjectName, &g.SubjectEmail, &g.Database, &g.Table, &g.CreatedBy, &g.CreatedAt)
	if err != nil {
		return g, err
	}
	if g.SubjectType == "everyone" {
		g.SubjectName = "Everyone"
	} else if g.SubjectName == "" {
		// The subject row may have been deleted; the canonical UUID is still
		// the only accurate label.
		g.SubjectName = g.SubjectID
	}
	return g, nil
}

// loadWarehouseGrant returns one grant scoped to its org. pgx.ErrNoRows is
// returned unwrapped so callers can map it to 404.
func (s *Server) loadWarehouseGrant(ctx context.Context, orgID, grantID string) (warehouseGrantJSON, error) {
	return scanWarehouseGrant(s.db.Pool.QueryRow(ctx,
		warehouseGrantSelect+` WHERE wtg.id = $1 AND wtg.org_id = $2`, grantID, orgID))
}

// loadWarehouseFromPath parses the warehouse path value, loads it scoped
// to the caller's org, and writes the matching error response. It reports
// false when the handler must stop.
func (s *Server) loadWarehouseFromPath(w http.ResponseWriter, r *http.Request, orgID string) (uuid.UUID, bool) {
	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return uuid.UUID{}, false
	}
	_, err := s.loadWarehouseForOrg(r.Context(), orgID, warehouseUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return uuid.UUID{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return uuid.UUID{}, false
	}
	return warehouseUUID, true
}

// @Summary List warehouse table grants
// @Description List the table grants configured for a warehouse
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/grants [get]
func (s *Server) handleListWarehouseGrants(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := s.loadWarehouseFromPath(w, r, claims.OrgID)
	if !ok {
		return
	}

	rows, err := s.db.Pool.Query(ctx, warehouseGrantSelect+`
		WHERE wtg.warehouse_id = $1 AND wtg.org_id = $2
		ORDER BY wtg.subject_type ASC, wtg.subject_id ASC,
		         wtg.database_name ASC, wtg.table_name ASC
		LIMIT $3`,
		warehouseUUID.String(), claims.OrgID, maxWarehouseGrantRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	grants := []warehouseGrantJSON{}
	for rows.Next() {
		g, err := scanWarehouseGrant(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, grants)
}

// createWarehouseGrantRequest: database/table are ClickHouse catalog object
// names; subject_id is required for user/group and ignored for everyone.
type createWarehouseGrantRequest struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Database    string `json:"database"`
	Table       string `json:"table"`
}

// @Summary Grant tables to a warehouse subject
// @Description Create a table grant for a user, group, or everyone in a warehouse. Replayed duplicates are idempotent and return the existing grant.
// @Tags warehouses
// @Accept json
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param request body object true "Grant subject and table"
// @Success 201 {object} object
// @Success 200 {object} object "Grant already existed"
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/grants [post]
func (s *Server) handleCreateWarehouseGrant(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := s.loadWarehouseFromPath(w, r, claims.OrgID)
	if !ok {
		return
	}

	var req createWarehouseGrantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateGrantObjectNames(req.Database, req.Table); err != nil {
		writeGrantValidationError(w, err)
		return
	}
	subjectID, err := s.validateGrantSubject(ctx, claims.OrgID, req.SubjectType, req.SubjectID)
	if err != nil {
		writeGrantValidationError(w, err)
		return
	}

	inserted := false
	var grantID string
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (warehouse_id, subject_type, subject_id, database_name, table_name) DO NOTHING
		RETURNING id`,
		claims.OrgID, warehouseUUID.String(), req.SubjectType, subjectID,
		req.Database, req.Table, claims.UserID).Scan(&grantID)
	switch {
	case err == nil:
		inserted = true
	case errors.Is(err, pgx.ErrNoRows):
		// Idempotent replay: return the grant that already exists. A missing
		// row here means a concurrent delete won the race.
		err = s.db.Pool.QueryRow(ctx, `
			SELECT id FROM warehouse_table_grants
			WHERE warehouse_id = $1 AND org_id = $2 AND subject_type = $3
			  AND subject_id = $4 AND database_name = $5 AND table_name = $6`,
			warehouseUUID.String(), claims.OrgID, req.SubjectType, subjectID,
			req.Database, req.Table).Scan(&grantID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "warehouse not found")
			return
		}
	case isForeignKeyViolation(err):
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create grant")
		return
	}

	grant, err := s.loadWarehouseGrant(ctx, claims.OrgID, grantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load grant")
		return
	}

	// The warning describes current state, so it is computed on replays too:
	// a subject whose service access was granted after the table grant no
	// longer needs the warning. It is advisory only; a failed lookup never
	// fails a committed grant.
	var warning string
	if hasAccess, accessErr := s.subjectHasServiceAccess(ctx, claims.OrgID, warehouseUUID, req.SubjectType, subjectID); accessErr != nil {
		slog.Warn("warehouse grant service-access warning check failed",
			"warehouse_id", warehouseUUID.String(), "error", accessErr)
	} else if !hasAccess {
		warning = "no_service_access"
	}

	status := http.StatusOK
	if inserted {
		status = http.StatusCreated
		s.enqueueWarehouseSync(warehouseUUID)
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "warehouse.grant.create", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
			Metadata: map[string]any{
				"grant_id":     grantID,
				"subject_type": req.SubjectType,
				"subject_id":   subjectID,
				"database":     req.Database,
				"table":        req.Table,
			},
		})
	}

	writeJSON(w, status, warehouseGrantCreateJSON{warehouseGrantJSON: grant, Warning: warning})
}

// @Summary Delete a warehouse table grant
// @Description Delete one table grant from a warehouse
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param grant_id path string true "Grant ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/grants/{grant_id} [delete]
func (s *Server) handleDeleteWarehouseGrant(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := s.loadWarehouseFromPath(w, r, claims.OrgID)
	if !ok {
		return
	}
	grantUUID, ok := parsePathUUID(r.PathValue("grant_id"))
	if !ok {
		writeError(w, http.StatusNotFound, "grant not found")
		return
	}

	var subjectType, subjectID, database, table string
	err := s.db.Pool.QueryRow(ctx, `
		DELETE FROM warehouse_table_grants
		WHERE id = $1 AND warehouse_id = $2 AND org_id = $3
		RETURNING subject_type, subject_id, database_name, table_name`,
		grantUUID.String(), warehouseUUID.String(), claims.OrgID).
		Scan(&subjectType, &subjectID, &database, &table)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "grant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete grant")
		return
	}

	s.enqueueWarehouseSync(warehouseUUID)
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.grant.delete", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
		Metadata: map[string]any{
			"grant_id":     grantUUID.String(),
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"database":     database,
			"table":        table,
		},
	})

	w.WriteHeader(http.StatusNoContent)
}

// @Summary Get a subject's effective warehouse access
// @Description Return the union of table grants for a user in a warehouse, the ClickHouse identity names, and the services the user may route to. Identity fields appear only when the user has at least one effective grant, and the preference only while it names an allowed service. Org admins may inspect any member; other members may only inspect themselves.
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param user_id query string false "User ID (org admins only; defaults to the caller)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/effective-access [get]
func (s *Server) handleWarehouseEffectiveAccess(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := s.loadWarehouseFromPath(w, r, claims.OrgID)
	if !ok {
		return
	}

	targetUserID := claims.UserID
	if raw := r.URL.Query().Get("user_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user_id")
			return
		}
		targetUserID = parsed.String()
		if claims.Role != "admin" && targetUserID != claims.UserID {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	// The target's role is needed for the same ACL resolution execution uses;
	// a non-member has no effective access to report.
	var targetRole string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
		claims.OrgID, targetUserID).Scan(&targetRole)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found in this organization")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	targetUUID, err := uuid.Parse(targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid user id")
		return
	}
	orgUUID, err := uuid.Parse(claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid org id")
		return
	}

	// Union of everyone + direct + groups the user belongs to. Group
	// membership is joined through org groups so a cross-org membership row
	// can never import another org's grant.
	rows, err := s.db.Pool.Query(ctx, `
		SELECT wtg.subject_type, wtg.subject_id, wtg.database_name, wtg.table_name
		FROM warehouse_table_grants wtg
		WHERE wtg.warehouse_id = $1 AND wtg.org_id = $2
		  AND (
		    wtg.subject_type = 'everyone'
		    OR (wtg.subject_type = 'user' AND wtg.subject_id = $3)
		    OR (wtg.subject_type = 'group' AND EXISTS (
		          SELECT 1 FROM group_members gm
		          JOIN groups g ON g.id = gm.group_id AND g.org_id = $2
		          WHERE gm.user_id = $4 AND gm.group_id::text = wtg.subject_id))
		  )
		ORDER BY wtg.database_name ASC, wtg.table_name ASC`,
		warehouseUUID.String(), claims.OrgID, targetUserID, targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	tables := []warehouseEffectiveTableJSON{}
	seenTables := map[string]struct{}{}
	roleSet := map[string]struct{}{}
	for rows.Next() {
		var subjectType, subjectID, database, table string
		if err := rows.Scan(&subjectType, &subjectID, &database, &table); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		key := database + "\x00" + table
		if _, seen := seenTables[key]; !seen {
			seenTables[key] = struct{}{}
			tables = append(tables, warehouseEffectiveTableJSON{Database: database, Table: table})
		}
		switch subjectType {
		case "group":
			if groupUUID, err := uuid.Parse(subjectID); err == nil {
				roleSet[chaccess.RoleIdent(warehouseUUID, orgUUID, groupUUID)] = struct{}{}
			}
		case "everyone":
			roleSet[chaccess.EveryoneRole(warehouseUUID)] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	allowedServices, preferredID, err := s.allowedWarehouseServices(ctx, targetUUID, orgUUID, targetRole, warehouseUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	services := make([]warehouseEffectiveServiceJSON, 0, len(allowedServices))
	for _, svc := range allowedServices {
		services = append(services, warehouseEffectiveServiceJSON{
			ConnectorID: svc.id.String(),
			Name:        svc.name,
			Preferred:   preferredID != nil && *preferredID == svc.id,
		})
	}
	var preferredValue *string
	if preferredID != nil {
		preferred := preferredID.String()
		preferredValue = &preferred
	}

	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	resp := warehouseEffectiveAccessJSON{
		UserID:               targetUserID,
		WarehouseID:          warehouseUUID.String(),
		Tables:               tables,
		Services:             services,
		PreferredConnectorID: preferredValue,
	}
	// chaccess.Compute provisions a ClickHouse user only with at least one
	// effective grant; a subject with none must not look provisioned.
	if len(tables) > 0 {
		resp.CHUser = chaccess.UserIdent(warehouseUUID, orgUUID, targetUUID)
		resp.CHRoles = roles
	}

	writeJSON(w, http.StatusOK, resp)
}

// setWarehousePreferenceRequest: connector_id is required; an explicit null
// clears the stored preference and returns routing to the automatic fallback.
type setWarehousePreferenceRequest struct {
	ConnectorID json.RawMessage `json:"connector_id"`
}

// @Summary Set a user's warehouse service preference
// @Description Choose which of the caller's permitted services their warehouse queries run on. The connector must belong to the warehouse and the caller must have `use` on it; an explicit null clears the preference.
// @Tags warehouses
// @Accept json
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param request body object true "Preferred connector (null clears)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/preference [put]
func (s *Server) handleSetWarehousePreference(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := s.loadWarehouseFromPath(w, r, claims.OrgID)
	if !ok {
		return
	}

	var req setWarehousePreferenceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ConnectorID == nil {
		writeError(w, http.StatusBadRequest, "connector_id is required (use null to clear)")
		return
	}
	connectorID, err := parseOptionalUUID(req.ConnectorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid connector_id")
		return
	}

	userUUID, err := uuid.Parse(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid user id")
		return
	}
	orgUUID, err := uuid.Parse(claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid org id")
		return
	}

	if connectorID != nil {
		// The preference table has no composite FK to warehouse membership on
		// purpose (connectors.warehouse_id is ON DELETE SET NULL); membership
		// and liveness are validated here so a preference can never point at
		// a cross-warehouse, soft-deleted, or non-ClickHouse service.
		var valid bool
		if err := s.db.Pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM connectors
				WHERE id = $1 AND warehouse_id = $2 AND org_id = $3
				  AND type = 'clickhouse' AND deleted_at IS NULL)`,
			connectorID.String(), warehouseUUID.String(), claims.OrgID).Scan(&valid); err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "connector does not belong to this warehouse")
			return
		}
		allowed, err := s.connectorUseAllowed(ctx, userUUID, orgUUID, claims.Role, *connectorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "no access to the selected service")
			return
		}
	}

	action := "warehouse.preference.clear"
	value := any(nil)
	if connectorID != nil {
		action = "warehouse.preference.set"
		value = connectorID.String()
		_, err = s.db.Pool.Exec(ctx, `
			INSERT INTO warehouse_service_preferences (user_id, warehouse_id, connector_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, warehouse_id) DO UPDATE
			SET connector_id = EXCLUDED.connector_id, updated_at = now()`,
			claims.UserID, warehouseUUID.String(), connectorID.String())
	} else {
		_, err = s.db.Pool.Exec(ctx, `
			DELETE FROM warehouse_service_preferences
			WHERE user_id = $1 AND warehouse_id = $2`,
			claims.UserID, warehouseUUID.String())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save preference")
		return
	}

	// Preferences do not shape desired ClickHouse state, but every warehouse
	// mutation schedules a reconcile per the API contract; enqueues coalesce
	// and a reconcile with no changes emits no DDL.
	s.enqueueWarehouseSync(warehouseUUID)
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: action, ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
		Metadata: map[string]any{"connector_id": value},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":      claims.UserID,
		"warehouse_id": warehouseUUID.String(),
		"connector_id": value,
	})
}
