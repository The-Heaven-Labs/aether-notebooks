package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// maxWarehouseValidationWarnings bounds each validation list. A warehouse with
// more misconfigured subjects than this needs a bulk workflow, not a longer
// response.
const maxWarehouseValidationWarnings = 200

// warehouseValidationSubjectJSON is one validation warning subject. Tables is
// populated for "granted but unusable" subjects; Services for subjects that
// can connect but hold no table grants.
type warehouseValidationSubjectJSON struct {
	SubjectType  string   `json:"subject_type"`
	SubjectID    string   `json:"subject_id"`
	SubjectName  string   `json:"subject_name,omitempty"`
	SubjectEmail string   `json:"subject_email,omitempty"`
	Tables       []string `json:"tables,omitempty"`
	Services     []string `json:"services,omitempty"`
}

// warehouseValidationJSON is the GET /warehouses/{id}/validation response.
// Truncated reports that one of the lists stopped at
// maxWarehouseValidationWarnings.
type warehouseValidationJSON struct {
	WarehouseID           string                           `json:"warehouse_id"`
	Truncated             bool                             `json:"truncated"`
	TablesWithoutService  []warehouseValidationSubjectJSON `json:"tables_without_service_access"`
	ServicesWithoutTables []warehouseValidationSubjectJSON `json:"service_access_without_tables"`
}

// warehouseServiceAccessIndex is a preloaded view of which subjects hold a
// `use` ACL on at least one live ClickHouse service of a warehouse. It mirrors
// permissions.go resolution: a user's groups (including the implicit Everyone
// group) and the org_role:everyone entry all count, and folder ancestors of a
// connector carry their ACLs down.
type warehouseServiceAccessIndex struct {
	userServices     map[string]map[string]struct{} // user ID -> connector name set
	groupServices    map[string]map[string]struct{} // group ID -> connector name set
	everyoneServices map[string]struct{}            // ACL on org_role:everyone
	everyoneGroupID  string
	userGroups       map[string][]string // user ID -> group IDs
}

// addService records that subjectID may use a named service.
func addService(m map[string]map[string]struct{}, subjectID, connectorName string) {
	set, ok := m[subjectID]
	if !ok {
		set = map[string]struct{}{}
		m[subjectID] = set
	}
	set[connectorName] = struct{}{}
}

// loadWarehouseServiceAccessIndex preloads service access for one warehouse.
// It replaces the per-subject lookup the grant-create warning used to do; the
// validation endpoint needs the same rules for every subject at once.
func (s *Server) loadWarehouseServiceAccessIndex(ctx context.Context, orgID string, warehouseID uuid.UUID) (*warehouseServiceAccessIndex, error) {
	idx := &warehouseServiceAccessIndex{
		userServices:     map[string]map[string]struct{}{},
		groupServices:    map[string]map[string]struct{}{},
		everyoneServices: map[string]struct{}{},
		userGroups:       map[string][]string{},
	}

	// Mirror permissions.go:52-65: every member implicitly belongs to the
	// org's Everyone group even without a group_members row.
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id::text FROM groups WHERE org_id = $1 AND name = 'Everyone'`, orgID).
		Scan(&idx.everyoneGroupID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load everyone group: %w", err)
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT ae.subject_type, ae.subject_id, c.name
		FROM connectors c
		JOIN acl_entries ae
		  ON ae.org_id = $1
		 AND 'use' = ANY(ae.actions)
		 AND (
		   (ae.resource_type = 'connector' AND ae.resource_id = c.id)
		   OR (ae.resource_type = 'folder' AND ae.resource_id IN (
		        WITH RECURSIVE ancestors AS (
		          SELECT id, parent_id FROM folders WHERE id = c.folder_id
		          UNION ALL
		          SELECT f.id, f.parent_id
		          FROM folders f JOIN ancestors a ON f.id = a.parent_id
		        )
		        SELECT id FROM ancestors))
		 )
		WHERE c.org_id = $1
		  AND c.warehouse_id = $2
		  AND c.type = 'clickhouse'
		  AND c.deleted_at IS NULL`,
		orgID, warehouseID.String())
	if err != nil {
		return nil, fmt.Errorf("load service access: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var subjectType, subjectID, connectorName string
		if err := rows.Scan(&subjectType, &subjectID, &connectorName); err != nil {
			return nil, fmt.Errorf("scan service access: %w", err)
		}
		switch subjectType {
		case "user":
			addService(idx.userServices, subjectID, connectorName)
		case "group":
			addService(idx.groupServices, subjectID, connectorName)
		case "org_role":
			// Only "everyone" matches every member; individual roles are
			// deprecated and never grant access (permissions.go:222-224).
			if subjectID == "everyone" {
				idx.everyoneServices[connectorName] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read service access: %w", err)
	}

	membershipRows, err := s.db.Pool.Query(ctx, `
		SELECT gm.user_id::text, gm.group_id::text
		FROM group_members gm
		JOIN org_members m ON m.user_id = gm.user_id AND m.org_id = $1
		JOIN groups g ON g.id = gm.group_id AND g.org_id = $1`, orgID)
	if err != nil {
		return nil, fmt.Errorf("load group memberships: %w", err)
	}
	defer membershipRows.Close()
	for membershipRows.Next() {
		var userID, groupID string
		if err := membershipRows.Scan(&userID, &groupID); err != nil {
			return nil, fmt.Errorf("scan group membership: %w", err)
		}
		idx.userGroups[userID] = append(idx.userGroups[userID], groupID)
	}
	if err := membershipRows.Err(); err != nil {
		return nil, fmt.Errorf("read group memberships: %w", err)
	}

	return idx, nil
}

// subjectServices returns the sorted service names a subject may use through
// its own entries, its groups, the Everyone group, and org_role:everyone.
func (idx *warehouseServiceAccessIndex) subjectServices(subjectType, subjectID string) []string {
	names := map[string]struct{}{}
	collect := func(set map[string]struct{}) {
		for name := range set {
			names[name] = struct{}{}
		}
	}
	switch subjectType {
	case "user":
		collect(idx.userServices[subjectID])
		for _, groupID := range idx.userGroups[subjectID] {
			collect(idx.groupServices[groupID])
		}
	case "group":
		collect(idx.groupServices[subjectID])
	case "everyone":
		// No concrete subject; only the shared branches below apply.
	}
	if idx.everyoneGroupID != "" {
		collect(idx.groupServices[idx.everyoneGroupID])
	}
	collect(idx.everyoneServices)

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// hasServiceAccess reports whether the subject can use at least one service.
func (idx *warehouseServiceAccessIndex) hasServiceAccess(subjectType, subjectID string) bool {
	return len(idx.subjectServices(subjectType, subjectID)) > 0
}

// warehouseGrantIndex summarizes table grants for validation: which subjects
// hold any grant, plus their table lists for the "tables without service
// access" warning.
type warehouseGrantIndex struct {
	userTables     map[string][]string
	groupTables    map[string][]string
	everyoneTables []string
}

// subjectHasTables reports whether a subject can read at least one table,
// applying the same union semantics execution uses: users combine direct,
// group, and everyone grants; groups combine their own grants with everyone.
func (g *warehouseGrantIndex) subjectHasTables(idx *warehouseServiceAccessIndex, subjectType, subjectID string) bool {
	switch subjectType {
	case "user":
		if len(g.userTables[subjectID]) > 0 {
			return true
		}
		for _, groupID := range idx.userGroups[subjectID] {
			if len(g.groupTables[groupID]) > 0 {
				return true
			}
		}
		return len(g.everyoneTables) > 0
	case "group":
		if len(g.groupTables[subjectID]) > 0 {
			return true
		}
		return len(g.everyoneTables) > 0
	case "everyone":
		return len(g.everyoneTables) > 0
	}
	return false
}

// subjectHasServiceAccess reports whether a warehouse grant subject can use at
// least one live ClickHouse service today. It is the warning check used by
// grant creation.
func (s *Server) subjectHasServiceAccess(ctx context.Context, orgID string, warehouseID uuid.UUID, subjectType, subjectID string) (bool, error) {
	idx, err := s.loadWarehouseServiceAccessIndex(ctx, orgID, warehouseID)
	if err != nil {
		return false, err
	}
	return idx.hasServiceAccess(subjectType, subjectID), nil
}

// @Summary List warehouse validation warnings
// @Description Report subjects that hold table grants but cannot use any warehouse service (granted but unusable) and subjects that can use a service but hold no table grants (can connect, every query fails).
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/validation [get]
func (s *Server) handleWarehouseValidation(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := s.loadWarehouseFromPath(w, r, claims.OrgID)
	if !ok {
		return
	}

	idx, err := s.loadWarehouseServiceAccessIndex(ctx, claims.OrgID, warehouseUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	grants := &warehouseGrantIndex{
		userTables:  map[string][]string{},
		groupTables: map[string][]string{},
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

	grantSubjects := []warehouseValidationSubjectJSON{}
	seenSubjects := map[string]int{}
	for rows.Next() {
		grant, err := scanWarehouseGrant(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		table := grant.Database + "." + grant.Table
		key := grant.SubjectType + ":" + grant.SubjectID
		switch grant.SubjectType {
		case "user":
			grants.userTables[grant.SubjectID] = append(grants.userTables[grant.SubjectID], table)
		case "group":
			grants.groupTables[grant.SubjectID] = append(grants.groupTables[grant.SubjectID], table)
		case "everyone":
			grants.everyoneTables = append(grants.everyoneTables, table)
		}
		if _, ok := seenSubjects[key]; ok {
			continue
		}
		seenSubjects[key] = len(grantSubjects)
		grantSubjects = append(grantSubjects, warehouseValidationSubjectJSON{
			SubjectType:  grant.SubjectType,
			SubjectID:    grant.SubjectID,
			SubjectName:  grant.SubjectName,
			SubjectEmail: grant.SubjectEmail,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	tablesWithoutService := []warehouseValidationSubjectJSON{}
	for _, subject := range grantSubjects {
		if idx.hasServiceAccess(subject.SubjectType, subject.SubjectID) {
			continue
		}
		subject.Tables = tablesForSubject(grants, subject.SubjectType, subject.SubjectID)
		tablesWithoutService = append(tablesWithoutService, subject)
	}

	names, err := s.loadValidationSubjectNames(ctx, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	servicesWithoutTables := []warehouseValidationSubjectJSON{}
	appendServiceWarning := func(subjectType, subjectID string) {
		if grants.subjectHasTables(idx, subjectType, subjectID) {
			return
		}
		warning := warehouseValidationSubjectJSON{
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Services:    idx.subjectServices(subjectType, subjectID),
		}
		if subjectType == "user" {
			warning.SubjectName, warning.SubjectEmail = names.userName(subjectID), names.userEmail(subjectID)
		} else if subjectType == "group" {
			warning.SubjectName = names.groupName(subjectID)
		} else {
			warning.SubjectName = "Everyone"
		}
		servicesWithoutTables = append(servicesWithoutTables, warning)
	}

	userIDs := sortedMapKeys(idx.userServices)
	for _, userID := range userIDs {
		appendServiceWarning("user", userID)
	}
	groupIDs := sortedMapKeys(idx.groupServices)
	for _, groupID := range groupIDs {
		appendServiceWarning("group", groupID)
	}
	if len(idx.everyoneServices) > 0 {
		appendServiceWarning("everyone", "everyone")
	}

	truncated := false
	if len(tablesWithoutService) > maxWarehouseValidationWarnings {
		tablesWithoutService = tablesWithoutService[:maxWarehouseValidationWarnings]
		truncated = true
	}
	if len(servicesWithoutTables) > maxWarehouseValidationWarnings {
		servicesWithoutTables = servicesWithoutTables[:maxWarehouseValidationWarnings]
		truncated = true
	}

	writeJSON(w, http.StatusOK, warehouseValidationJSON{
		WarehouseID:           warehouseUUID.String(),
		Truncated:             truncated,
		TablesWithoutService:  tablesWithoutService,
		ServicesWithoutTables: servicesWithoutTables,
	})
}

// tablesForSubject returns the subject's own table grants, ordered.
func tablesForSubject(grants *warehouseGrantIndex, subjectType, subjectID string) []string {
	switch subjectType {
	case "user":
		return grants.userTables[subjectID]
	case "group":
		return grants.groupTables[subjectID]
	case "everyone":
		return grants.everyoneTables
	}
	return nil
}

// validationSubjectNames labels warning subjects without one query per row.
type validationSubjectNames struct {
	users  map[string]string
	emails map[string]string
	groups map[string]string
}

func (n validationSubjectNames) userName(userID string) string {
	if name := n.users[userID]; name != "" {
		return name
	}
	return userID
}

func (n validationSubjectNames) userEmail(userID string) string { return n.emails[userID] }

func (n validationSubjectNames) groupName(groupID string) string {
	if name := n.groups[groupID]; name != "" {
		return name
	}
	return groupID
}

func (s *Server) loadValidationSubjectNames(ctx context.Context, orgID string) (validationSubjectNames, error) {
	names := validationSubjectNames{
		users:  map[string]string{},
		emails: map[string]string{},
		groups: map[string]string{},
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT u.id::text, coalesce(u.name, ''), coalesce(u.email, '')
		FROM users u
		JOIN org_members m ON m.user_id = u.id AND m.org_id = $1`, orgID)
	if err != nil {
		return names, fmt.Errorf("load member names: %w", err)
	}
	for rows.Next() {
		var id, name, email string
		if err := rows.Scan(&id, &name, &email); err != nil {
			rows.Close()
			return names, fmt.Errorf("scan member name: %w", err)
		}
		names.users[id] = name
		names.emails[id] = email
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return names, fmt.Errorf("read member names: %w", err)
	}

	groupRows, err := s.db.Pool.Query(ctx,
		`SELECT id::text, name FROM groups WHERE org_id = $1`, orgID)
	if err != nil {
		return names, fmt.Errorf("load group names: %w", err)
	}
	defer groupRows.Close()
	for groupRows.Next() {
		var id, name string
		if err := groupRows.Scan(&id, &name); err != nil {
			return names, fmt.Errorf("scan group name: %w", err)
		}
		names.groups[id] = name
	}
	if err := groupRows.Err(); err != nil {
		return names, fmt.Errorf("read group names: %w", err)
	}
	return names, nil
}

// sortedMapKeys returns the keys of a string-keyed map in ascending order.
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
