package chaccess

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

// GrantRow is one row of system.grants. is_wildcard does not exist before
// ClickHouse 26.x, so wildcard detection uses the empty-table signal plus
// quote validation (see buildActualFromGrantRows).
type GrantRow struct {
	UserName        string
	RoleName        string
	AccessType      string
	Database        string
	Table           string
	IsPartialRevoke uint8
}

// LoadActual reads users, roles, role grants, and direct grants for one
// warehouse's namespace. The namespace is selected with startsWith on
// IdentifierPrefix(warehouseID) rather than LIKE, so underscores in the
// prefix are never treated as wildcards.
//
// Roles are enumerated from system.roles (not only from grant rows) so a
// zero-grant orphan role is visible and can be dropped.
//
// It does NOT read password hashes: ClickHouse stores salted hashes that
// cannot be compared. Password rotation is driven by
// warehouses.applied_master_fp (see Task 9) via ActualState.ForcePasswordReset.
func LoadActual(ctx context.Context, conn clickhouse.Conn, warehouseID uuid.UUID) (ActualState, error) {
	prefix := IdentifierPrefix(warehouseID)

	var grants []GrantRow
	rows, err := conn.Query(ctx, `
		SELECT coalesce(user_name,''), coalesce(role_name,''), access_type, coalesce(database,''), coalesce(table,''), is_partial_revoke
		FROM system.grants
		WHERE (startsWith(user_name, ?) OR startsWith(role_name, ?))
		  AND access_type = 'SELECT' AND is_partial_revoke = 0`,
		prefix, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r GrantRow
		if err := rows.Scan(&r.UserName, &r.RoleName, &r.AccessType, &r.Database, &r.Table, &r.IsPartialRevoke); err != nil {
			return ActualState{}, fmt.Errorf("scan grant: %w", err)
		}
		grants = append(grants, r)
	}
	if err := rows.Err(); err != nil {
		return ActualState{}, err
	}

	roles := map[string]map[Grant]struct{}{}
	rrows, err := conn.Query(ctx, `SELECT name FROM system.roles WHERE startsWith(name, ?)`, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.roles: %w", err)
	}
	defer rrows.Close()
	for rrows.Next() {
		var name string
		if err := rrows.Scan(&name); err != nil {
			return ActualState{}, err
		}
		roles[name] = map[Grant]struct{}{}
	}
	if err := rrows.Err(); err != nil {
		return ActualState{}, err
	}

	memberships := map[string]map[string]struct{}{}
	mrows, err := conn.Query(ctx, `
		SELECT user_name, granted_role_name FROM system.role_grants
		WHERE startsWith(user_name, ?)`, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.role_grants: %w", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var user, role string
		if err := mrows.Scan(&user, &role); err != nil {
			return ActualState{}, err
		}
		if memberships[user] == nil {
			memberships[user] = map[string]struct{}{}
		}
		memberships[user][role] = struct{}{}
	}
	if err := mrows.Err(); err != nil {
		return ActualState{}, err
	}

	users := map[string]UserActual{}
	urows, err := conn.Query(ctx,
		`SELECT name, default_roles_all FROM system.users WHERE startsWith(name, ?)`, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.users: %w", err)
	}
	defer urows.Close()
	for urows.Next() {
		var name string
		var defaultRolesAll uint8
		if err := urows.Scan(&name, &defaultRolesAll); err != nil {
			return ActualState{}, err
		}
		users[name] = UserActual{Roles: memberships[name], DefaultRolesAll: defaultRolesAll == 1}
	}
	if err := urows.Err(); err != nil {
		return ActualState{}, err
	}

	return buildActualFromGrantRows(grants, roles, users), nil
}

func buildActualFromGrantRows(grants []GrantRow, roles map[string]map[Grant]struct{}, users map[string]UserActual) ActualState {
	a := ActualState{Roles: map[string]map[Grant]struct{}{}, Users: map[string]UserActual{}}
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
		// Version-agnostic wildcard detection (no is_wildcard column before
		// ClickHouse 26.x): an empty table means a database/global wildcard.
		// Table-prefix wildcards (e.g. db.events*) arrive as a non-empty
		// table that cannot be quoted; treat those as wildcards too so
		// reconcile fails closed instead of silently ignoring them.
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
	return a
}
