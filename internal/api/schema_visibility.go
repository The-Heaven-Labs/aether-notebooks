package api

import (
	"context"
	"log/slog"
	"regexp"
	"sort"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// tableKey identifies one table independent of the type carrying it.
type tableKey struct {
	Database string
	Table    string
}

// compileHiddenPatterns compiles stored patterns. Invalid patterns are
// impossible after write validation; a stored one that no longer compiles is
// logged and skipped because patterns are curation, not security.
func compileHiddenPatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			slog.Warn("skipping invalid hidden-table pattern", "pattern", pattern, "error", err)
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}

// matchesHiddenPattern reports whether database.table matches any pattern,
// with the same unanchored "database.table" semantics as connector filters.
func matchesHiddenPattern(patterns []*regexp.Regexp, database, table string) bool {
	name := table
	if database != "" {
		name = database + "." + table
	}
	for _, re := range patterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// loadWarehouseHiddenPatterns loads and compiles one warehouse's patterns.
// Failures fail open (log and return none): patterns are curation.
func (s *Server) loadWarehouseHiddenPatterns(ctx context.Context, warehouseID uuid.UUID, orgID string) []*regexp.Regexp {
	var patterns []string
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT hidden_table_patterns FROM warehouses WHERE id = $1 AND org_id = $2`,
		warehouseID.String(), orgID).Scan(&patterns); err != nil {
		slog.Warn("failed to load warehouse hidden-table patterns",
			"warehouse_id", warehouseID, "error", err)
		return nil
	}
	return compileHiddenPatterns(patterns)
}

// loadWarehouseGrantKeys returns every granted (database, table) of a warehouse.
func (s *Server) loadWarehouseGrantKeys(ctx context.Context, warehouseID uuid.UUID, orgID string) (map[tableKey]struct{}, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT database_name, table_name FROM warehouse_table_grants
		WHERE warehouse_id = $1 AND org_id = $2`,
		warehouseID.String(), orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := map[tableKey]struct{}{}
	for rows.Next() {
		var key tableKey
		if err := rows.Scan(&key.Database, &key.Table); err != nil {
			return nil, err
		}
		keys[key] = struct{}{}
	}
	return keys, rows.Err()
}

// loadEffectiveWarehouseGrants resolves one user's union of everyone + direct
// + group grants, the same resolution execution relies on. Like
// loadWarehouseDesiredState, all three branches require current org
// membership: the caller must be in org_members for any grant to count, a user
// grant counts only while its subject is an org member, and a group grant
// reaches only group members who are org members. It also returns the sorted
// ClickHouse role idents implied by group/everyone grants so the
// effective-access endpoint keeps reporting them. Group membership is joined
// through org groups so a cross-org membership row can never import another
// org's grant.
func (s *Server) loadEffectiveWarehouseGrants(ctx context.Context, warehouseID uuid.UUID, orgID, userID string) ([]tableKey, []string, error) {
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT wtg.subject_type, wtg.subject_id, wtg.database_name, wtg.table_name
		FROM warehouse_table_grants wtg
		WHERE wtg.warehouse_id = $1 AND wtg.org_id = $2
		  AND (
		    (wtg.subject_type = 'everyone' AND EXISTS (
		       SELECT 1 FROM org_members m WHERE m.org_id = $2 AND m.user_id::text = $3))
		    OR (wtg.subject_type = 'user' AND wtg.subject_id = $3
		        AND EXISTS (SELECT 1 FROM org_members m
		                    WHERE m.org_id = $2 AND m.user_id = wtg.subject_id::uuid))
		    OR (wtg.subject_type = 'group' AND EXISTS (
		          SELECT 1 FROM group_members gm
		          JOIN groups g ON g.id = gm.group_id AND g.org_id = $2
		          WHERE gm.user_id = $4 AND gm.group_id::text = wtg.subject_id
		            AND EXISTS (SELECT 1 FROM org_members m
		                        WHERE m.org_id = $2 AND m.user_id = gm.user_id)))
		  )
		ORDER BY wtg.database_name ASC, wtg.table_name ASC`,
		warehouseID.String(), orgID, userID, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var keys []tableKey
	seen := map[tableKey]struct{}{}
	roleSet := map[string]struct{}{}
	for rows.Next() {
		var subjectType, subjectID, database, table string
		if err := rows.Scan(&subjectType, &subjectID, &database, &table); err != nil {
			return nil, nil, err
		}
		key := tableKey{Database: database, Table: table}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		switch subjectType {
		case "group":
			if groupUUID, err := uuid.Parse(subjectID); err == nil {
				roleSet[chaccess.RoleIdent(warehouseID, orgUUID, groupUUID)] = struct{}{}
			}
		case "everyone":
			roleSet[chaccess.EveryoneRole(warehouseID)] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return keys, roles, nil
}

// keysToSet converts a sorted key list into a lookup set.
func keysToSet(keys []tableKey) map[tableKey]struct{} {
	set := make(map[tableKey]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

// filterVisibleSchemaTables applies warehouse visibility. A nil allowed set
// means "no per-user restriction"; a non-nil one keeps only its keys. Patterns
// drop matched tables unless the key is in patternProtected.
func filterVisibleSchemaTables(
	tables []executor.TableInfo,
	patterns []*regexp.Regexp,
	allowed, patternProtected map[tableKey]struct{},
) []executor.TableInfo {
	filtered := make([]executor.TableInfo, 0, len(tables))
	for _, table := range tables {
		key := tableKey{Database: table.Schema, Table: table.Name}
		if allowed != nil {
			if _, ok := allowed[key]; !ok {
				continue
			}
		}
		if matchesHiddenPattern(patterns, table.Schema, table.Name) {
			if patternProtected == nil {
				continue
			}
			if _, ok := patternProtected[key]; !ok {
				continue
			}
		}
		filtered = append(filtered, table)
	}
	return filtered
}

// filterHiddenCatalogTables drops pattern-matched tables from a raw catalog
// snapshot (reconcile path), where there is no granted-table protection: the
// inbox already excludes granted tables.
func filterHiddenCatalogTables(patterns []*regexp.Regexp, tables []chaccess.CatalogTable) []chaccess.CatalogTable {
	if len(patterns) == 0 {
		return tables
	}
	filtered := make([]chaccess.CatalogTable, 0, len(tables))
	for _, table := range tables {
		if matchesHiddenPattern(patterns, table.Database, table.Table) {
			continue
		}
		filtered = append(filtered, table)
	}
	return filtered
}
