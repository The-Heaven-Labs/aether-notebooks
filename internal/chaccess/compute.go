package chaccess

import "github.com/google/uuid"

// SubjectGrant is one row of warehouse_table_grants. SubjectID is a UUID
// string for "user" and "group" subjects; it is ignored for "everyone".
type SubjectGrant struct {
	SubjectType string // user | group | everyone
	SubjectID   string
	Database    string
	Table       string
}

// UserSpec is a user who may be provisioned in a warehouse. Identities are
// UUID-derived, so no email (or other mutable attribute) is carried here.
type UserSpec struct {
	ID uuid.UUID
}

// Compute builds the desired ClickHouse state for a warehouse.
//
// Preconditions: grants, memberships, and users must already be scoped to the
// warehouse's org. Callers validate subjects against org membership before
// calling (the sync worker joins org_members); Compute makes no DB calls.
//
//   - One role per group with at least one grant; a group with no grants gets
//     no role. SubjectID is ignored for "everyone".
//   - Everyone grants go to the warehouse's EveryoneRole(warehouseID) role.
//   - A user is provisioned only when their union (direct + groups + everyone)
//     is non-empty.
//   - A user's default roles are the roles of their granting groups plus
//     EveryoneRole(warehouseID) when applicable, deduplicated and sorted.
//   - Malformed subject IDs and unknown subject types are deliberately ignored
//     (fail closed): they grant nothing and provision nobody.
func Compute(
	orgID, warehouseID uuid.UUID,
	masterKey []byte,
	grants []SubjectGrant,
	memberships map[uuid.UUID][]uuid.UUID,
	users []UserSpec,
) DesiredState {
	roleGrants := map[string]map[Grant]struct{}{}
	userDirect := map[string]map[Grant]struct{}{}
	everyone := map[Grant]struct{}{}

	for _, gr := range grants {
		key := Grant{Database: gr.Database, Table: gr.Table}
		switch gr.SubjectType {
		case "group":
			gid, err := uuid.Parse(gr.SubjectID)
			if err != nil {
				continue
			}
			ident := RoleIdent(warehouseID, orgID, gid)
			if roleGrants[ident] == nil {
				roleGrants[ident] = map[Grant]struct{}{}
			}
			roleGrants[ident][key] = struct{}{}
		case "user":
			uid, err := uuid.Parse(gr.SubjectID)
			if err != nil {
				continue
			}
			sid := uid.String() // canonical key: non-canonical spellings must match
			if userDirect[sid] == nil {
				userDirect[sid] = map[Grant]struct{}{}
			}
			userDirect[sid][key] = struct{}{}
		case "everyone":
			everyone[key] = struct{}{}
		}
	}

	roles := map[string]RoleState{}
	for ident, gs := range roleGrants {
		roles[ident] = RoleState{Grants: gs}
	}
	if len(everyone) > 0 {
		roles[EveryoneRole(warehouseID)] = RoleState{Grants: everyone}
	}

	usersOut := map[string]UserState{}
	for _, u := range users {
		roleSet := map[string]struct{}{}
		if len(everyone) > 0 {
			roleSet[EveryoneRole(warehouseID)] = struct{}{}
		}
		for _, gid := range memberships[u.ID] {
			ident := RoleIdent(warehouseID, orgID, gid)
			if _, ok := roles[ident]; ok {
				roleSet[ident] = struct{}{}
			}
		}
		direct := userDirect[u.ID.String()]
		if len(roleSet) == 0 && len(direct) == 0 {
			continue // no effective grants: do not provision
		}
		pw := DerivePassword(masterKey, warehouseID, u.ID)
		usersOut[UserIdent(warehouseID, orgID, u.ID)] = UserState{
			Password:     pw,
			Roles:        sortedSet(roleSet),
			DirectGrants: direct,
		}
	}

	return DesiredState{Roles: roles, Users: usersOut}
}
