package chaccess

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

// GrantRow is one relevant row of system.grants: SELECT grants without a
// column list (the query filters access_type and column-level rows).
type GrantRow struct {
	UserName        string
	RoleName        string
	Database        string
	Table           string
	IsPartialRevoke uint8
	GrantOption     uint8
	IsWildcard      uint8
}

// LoadActual reads users, roles, role grants, and direct grants for one
// warehouse's namespace. The namespace is selected with startsWith on
// IdentifierPrefix(warehouseID) rather than LIKE, so underscores in the
// prefix are never treated as wildcards.
//
// Roles are enumerated from system.roles (not only from grant rows) so a
// zero-grant orphan role is visible and can be dropped.
//
// Wildcard detection is version-agnostic. Empty-table rows (db.* / *.*) are
// wildcards everywhere. Prefix wildcards (db.events*) are flagged by
// system.grants.is_wildcard on servers that have that column (ClickHouse
// >= 26.x); older servers strip the "*" from the table name, so LoadActual
// falls back to scanning SHOW GRANTS FOR for every namespace identity and
// treats any SELECT line containing "*" as a wildcard. False positives fail
// closed (the reconcile treats wildcards as drift).
//
// Non-SELECT grants (INSERT, ALTER, DROP, ...) in the namespace are recorded
// in Unexpected: Aether only ever grants SELECT, so they are drift.
//
// It does NOT read password hashes: ClickHouse stores salted hashes that
// cannot be compared. Password rotation is driven by
// warehouses.applied_master_fp (see Task 9) via ActualState.ForcePasswordReset.
func LoadActual(ctx context.Context, conn clickhouse.Conn, warehouseID uuid.UUID) (ActualState, error) {
	prefix := IdentifierPrefix(warehouseID)

	hasIsWildcard, err := hasIsWildcardColumn(ctx, conn)
	if err != nil {
		return ActualState{}, err
	}

	grants, err := loadGrants(ctx, conn, prefix, hasIsWildcard)
	if err != nil {
		return ActualState{}, err
	}

	roles, err := loadRoles(ctx, conn, prefix)
	if err != nil {
		return ActualState{}, err
	}

	memberships, unexpected, err := loadRoleGrants(ctx, conn, prefix)
	if err != nil {
		return ActualState{}, err
	}

	nonSelect, err := loadNonSelectGrants(ctx, conn, prefix)
	if err != nil {
		return ActualState{}, err
	}
	unexpected = append(unexpected, nonSelectDrift(nonSelect)...)

	users, err := loadUsers(ctx, conn, prefix, memberships)
	if err != nil {
		return ActualState{}, err
	}

	a := buildActualFromGrantRows(grants, roles, users, unexpected)
	if hasIsWildcard {
		return a, nil
	}
	wildcards, err := scanWildcardGrants(ctx, conn, prefix, roles, users)
	if err != nil {
		return ActualState{}, err
	}
	a.Wildcards = normalizeWildcards(append(a.Wildcards, wildcards...))
	return a, nil
}

// hasIsWildcardColumn reports whether system.grants exposes is_wildcard
// (added in ClickHouse 26.x; absent on 24.x).
func hasIsWildcardColumn(ctx context.Context, conn clickhouse.Conn) (bool, error) {
	var n uint64
	err := conn.QueryRow(ctx, `
		SELECT count() FROM system.columns
		WHERE database = 'system' AND table = 'grants' AND name = 'is_wildcard'`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("probe system.grants.is_wildcard: %w", err)
	}
	return n > 0, nil
}

func loadGrants(ctx context.Context, conn clickhouse.Conn, prefix string, hasIsWildcard bool) ([]GrantRow, error) {
	cols := "coalesce(user_name,''), coalesce(role_name,''), coalesce(database,''), coalesce(table,''), is_partial_revoke, grant_option"
	if hasIsWildcard {
		cols += ", is_wildcard"
	}
	query := "SELECT " + cols + " FROM system.grants" +
		" WHERE (startsWith(user_name, ?) OR startsWith(role_name, ?))" +
		" AND access_type = 'SELECT' AND is_partial_revoke = 0" +
		" AND coalesce(`column`, '') = ''"

	rows, err := conn.Query(ctx, query, prefix, prefix)
	if err != nil {
		return nil, fmt.Errorf("system.grants: %w", err)
	}
	var grants []GrantRow
	for rows.Next() {
		var r GrantRow
		dest := []any{&r.UserName, &r.RoleName, &r.Database, &r.Table, &r.IsPartialRevoke, &r.GrantOption}
		if hasIsWildcard {
			dest = append(dest, &r.IsWildcard)
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan system.grants: %w", err)
		}
		grants = append(grants, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read system.grants: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close system.grants: %w", err)
	}
	return grants, nil
}

func loadRoles(ctx context.Context, conn clickhouse.Conn, prefix string) (map[string]map[Grant]struct{}, error) {
	roles := map[string]map[Grant]struct{}{}
	rows, err := conn.Query(ctx, "SELECT name FROM system.roles WHERE startsWith(name, ?)", prefix)
	if err != nil {
		return nil, fmt.Errorf("system.roles: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan system.roles: %w", err)
		}
		roles[name] = map[Grant]struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read system.roles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close system.roles: %w", err)
	}
	return roles, nil
}

// loadRoleGrants reads user memberships in the namespace and records drift
// rows (nested role-to-role grants, namespace roles granted to users outside
// the namespace) in the returned slice.
func loadRoleGrants(ctx context.Context, conn clickhouse.Conn, prefix string) (map[string]map[string]struct{}, []string, error) {
	memberships := map[string]map[string]struct{}{}
	var unexpected []string
	rows, err := conn.Query(ctx, `
		SELECT coalesce(user_name,''), coalesce(role_name,''), granted_role_name FROM system.role_grants
		WHERE startsWith(user_name, ?) OR startsWith(role_name, ?) OR startsWith(granted_role_name, ?)`,
		prefix, prefix, prefix)
	if err != nil {
		return nil, nil, fmt.Errorf("system.role_grants: %w", err)
	}
	for rows.Next() {
		var user, role, granted string
		if err := rows.Scan(&user, &role, &granted); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("scan system.role_grants: %w", err)
		}
		memberUser, drift := classifyRoleGrantRow(user, role, granted, prefix)
		if memberUser != "" {
			if memberships[memberUser] == nil {
				memberships[memberUser] = map[string]struct{}{}
			}
			memberships[memberUser][granted] = struct{}{}
		}
		if drift != "" {
			unexpected = append(unexpected, drift)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("read system.role_grants: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("close system.role_grants: %w", err)
	}
	return memberships, unexpected, nil
}

// NonSelectGrant is one non-SELECT grant in the warehouse namespace.
// Aether only ever issues SELECT grants, so any of these is drift.
type NonSelectGrant struct {
	AccessType string
	UserName   string
	RoleName   string
	Database   string
	Table      string
}

// loadNonSelectGrants reads namespace grants whose access type is not SELECT
// (INSERT, ALTER, DROP, ...). Partial revokes are not grants and are
// excluded, matching loadGrants.
func loadNonSelectGrants(ctx context.Context, conn clickhouse.Conn, prefix string) ([]NonSelectGrant, error) {
	rows, err := conn.Query(ctx, `
		SELECT access_type, coalesce(user_name,''), coalesce(role_name,''), coalesce(database,''), coalesce(table,'')
		FROM system.grants
		WHERE (startsWith(user_name, ?) OR startsWith(role_name, ?))
		  AND access_type != 'SELECT' AND is_partial_revoke = 0`,
		prefix, prefix)
	if err != nil {
		return nil, fmt.Errorf("system.grants (non-SELECT): %w", err)
	}
	var grants []NonSelectGrant
	for rows.Next() {
		var g NonSelectGrant
		if err := rows.Scan(&g.AccessType, &g.UserName, &g.RoleName, &g.Database, &g.Table); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan system.grants (non-SELECT): %w", err)
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read system.grants (non-SELECT): %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close system.grants (non-SELECT): %w", err)
	}
	return grants, nil
}

// nonSelectDrift renders non-SELECT grants as unexpected-drift entries
// ("INSERT on db.table for <ident>"), sorted and deduplicated like the other
// Unexpected entries.
func nonSelectDrift(grants []NonSelectGrant) []string {
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		subject := g.RoleName
		if subject == "" {
			subject = g.UserName
		}
		if subject == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s on %s for %s", g.AccessType, grantScopeLabel(g.Database, g.Table), subject))
	}
	return dedupeSorted(out)
}

// grantScopeLabel renders a grant target for drift messages: table-level,
// db.*, or *.*.
func grantScopeLabel(database, table string) string {
	switch {
	case database == "" && table == "":
		return "*.*"
	case table == "":
		return database + ".*"
	case database == "":
		return table
	default:
		return database + "." + table
	}
}

// classifyRoleGrantRow interprets one system.role_grants row. A namespace
// user's membership returns memberUser set; every other shape is drift:
// role_name is the grantee role for role-to-role grants (GRANT role1 TO
// role2), granted_role_name is always the role being granted.
func classifyRoleGrantRow(user, role, granted, prefix string) (memberUser, drift string) {
	switch {
	case user != "" && strings.HasPrefix(user, prefix):
		return user, ""
	case user != "":
		return "", fmt.Sprintf("role %s granted to %s", granted, user)
	case role != "":
		return "", fmt.Sprintf("role %s granted role %s", granted, role)
	}
	return "", ""
}

func loadUsers(ctx context.Context, conn clickhouse.Conn, prefix string, memberships map[string]map[string]struct{}) (map[string]UserActual, error) {
	users := map[string]UserActual{}
	rows, err := conn.Query(ctx,
		"SELECT name, default_roles_all FROM system.users WHERE startsWith(name, ?)", prefix)
	if err != nil {
		return nil, fmt.Errorf("system.users: %w", err)
	}
	for rows.Next() {
		var name string
		var defaultRolesAll uint8
		if err := rows.Scan(&name, &defaultRolesAll); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan system.users: %w", err)
		}
		users[name] = UserActual{Roles: memberships[name], DefaultRolesAll: defaultRolesAll == 1}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read system.users: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close system.users: %w", err)
	}
	return users, nil
}

// scanWildcardGrants recovers prefix wildcards on ClickHouse servers without
// system.grants.is_wildcard. system.grants reports `GRANT SELECT ON db.events*`
// with the "*" stripped, making it indistinguishable from an exact grant
// there, while SHOW GRANTS FOR still renders the wildcard. Only identities in
// the warehouse namespace (prefix-scoped users and roles) are scanned. A
// failed scan is returned as an error: a wildcard that cannot be checked must
// not let the sync report ready.
func scanWildcardGrants(ctx context.Context, conn clickhouse.Conn, prefix string, roles map[string]map[Grant]struct{}, users map[string]UserActual) ([]WildcardGrant, error) {
	idents := make([]string, 0, len(roles)+len(users))
	for name := range roles {
		idents = append(idents, name)
	}
	for name := range users {
		idents = append(idents, name)
	}
	sort.Strings(idents)

	var wildcards []WildcardGrant
	for _, ident := range idents {
		quoted, ok := tryQuoteIdent(ident)
		if !ok {
			continue // catalog names outside Aether's charset are skipped by reconcile too
		}
		rows, err := conn.Query(ctx, "SHOW GRANTS FOR "+quoted)
		if err != nil {
			return nil, fmt.Errorf("show grants for %s: %w", ident, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan show grants for %s: %w", ident, err)
			}
			if scope, ok := wildcardScopeFromGrantLine(line); ok {
				wildcards = append(wildcards, WildcardGrant{Subject: ident, Scope: scope})
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read show grants for %s: %w", ident, err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close show grants for %s: %w", ident, err)
		}
	}
	return wildcards, nil
}

// wildcardScopeFromGrantLine parses one line of SHOW GRANTS output. It is
// deliberately conservative: any line that mentions SELECT and contains "*"
// is reported as a wildcard (false positives fail closed). The scope is the
// target between " ON " and " TO "/" FROM " with backticks stripped; a line
// that cannot be split still counts as a wildcard, using the whole line.
func wildcardScopeFromGrantLine(line string) (string, bool) {
	upper := strings.ToUpper(line)
	if !strings.Contains(upper, "SELECT") || !strings.Contains(line, "*") {
		return "", false
	}
	onIdx := strings.Index(upper, " ON ")
	if onIdx < 0 {
		return cleanGrantTarget(line), true
	}
	rest := line[onIdx+len(" ON "):]
	end := len(rest)
	for _, sep := range []string{" TO ", " FROM "} {
		if i := strings.Index(strings.ToUpper(rest), sep); i >= 0 && i < end {
			end = i
		}
	}
	return cleanGrantTarget(rest[:end]), true
}

func cleanGrantTarget(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "`", ""))
}

func buildActualFromGrantRows(grants []GrantRow, roles map[string]map[Grant]struct{}, users map[string]UserActual, unexpected []string) ActualState {
	a := ActualState{Roles: map[string]map[Grant]struct{}{}, Users: map[string]UserActual{}, Unexpected: append([]string{}, unexpected...)}
	for name, gs := range roles {
		if gs == nil {
			gs = map[Grant]struct{}{}
		}
		a.Roles[name] = gs
	}
	for k, v := range users {
		if v.Roles == nil {
			v.Roles = map[string]struct{}{}
		}
		if v.DirectGrants == nil {
			v.DirectGrants = map[Grant]struct{}{}
		}
		a.Users[k] = v
	}
	for _, r := range grants {
		if r.IsPartialRevoke == 1 {
			continue
		}
		subject := r.RoleName
		if subject == "" {
			subject = r.UserName
		}
		if r.GrantOption == 1 {
			a.Unexpected = append(a.Unexpected, fmt.Sprintf("grant option on %s.%s for %s", r.Database, r.Table, subject))
		}
		// Version-agnostic wildcard detection. IsWildcard is set by newer
		// servers; on older ones system.grants strips the "*" from prefix
		// grants (recovered via SHOW GRANTS in LoadActual), and an empty
		// table means a database/global wildcard. Names that cannot be quoted
		// are treated as wildcards too so reconcile fails closed instead of
		// silently ignoring them.
		if r.IsWildcard == 1 {
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: wildcardScope(r.Database, r.Table)})
			continue
		}
		if r.Table == "" {
			scope := "*.*"
			if r.Database != "" {
				scope = r.Database + ".*"
			}
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: scope})
			continue
		}
		if _, err := QuoteObjectIdent(r.Database); err != nil {
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: r.Database + "." + r.Table})
			continue
		}
		if _, err := QuoteObjectIdent(r.Table); err != nil {
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: r.Database + "." + r.Table})
			continue
		}
		gr := Grant{Database: r.Database, Table: r.Table}
		if r.RoleName != "" {
			if a.Roles[r.RoleName] == nil {
				a.Roles[r.RoleName] = map[Grant]struct{}{}
			}
			a.Roles[r.RoleName][gr] = struct{}{}
		} else if r.UserName != "" {
			u := a.Users[r.UserName]
			if u.DirectGrants == nil {
				u.DirectGrants = map[Grant]struct{}{}
			}
			u.DirectGrants[gr] = struct{}{}
			a.Users[r.UserName] = u
		}
	}
	a.Wildcards = normalizeWildcards(a.Wildcards)
	a.Unexpected = dedupeSorted(a.Unexpected)
	return a
}

// wildcardScope renders the scope of a row flagged is_wildcard. ClickHouse
// versions that expose the star in the table name keep it; a bare prefix
// (e.g. table "events" for db.events*) gets the star appended.
func wildcardScope(database, table string) string {
	switch {
	case database == "" && table == "":
		return "*.*"
	case table == "":
		return database + ".*"
	case database == "":
		return table
	case strings.Contains(table, "*"):
		return database + "." + table
	default:
		return database + "." + table + "*"
	}
}

func normalizeWildcards(in []WildcardGrant) []WildcardGrant {
	if len(in) == 0 {
		return nil
	}
	out := append([]WildcardGrant(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Scope < out[j].Scope
	})
	deduped := out[:1]
	for _, w := range out[1:] {
		if w != deduped[len(deduped)-1] {
			deduped = append(deduped, w)
		}
	}
	return deduped
}
