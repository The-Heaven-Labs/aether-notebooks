package chaccess

import "github.com/google/uuid"

// SubjectGrant is one row of warehouse_table_grants.
type SubjectGrant struct {
	SubjectType string // user | group | everyone
	SubjectID   string
	Database    string
	Table       string
}

// UserSpec is a user who may be provisioned in a warehouse.
type UserSpec struct {
	ID    uuid.UUID
	Email string
}

// Compute builds the desired ClickHouse state for a warehouse.
//
//   - One role per group with at least one grant.
//   - Everyone grants go to the warehouse's EveryoneRole(warehouseID) role.
//   - A user is provisioned only when their union (direct + groups + everyone)
//     is non-empty.
//   - A user's default roles are the roles of their granting groups plus
//     EveryoneRole(warehouseID) when applicable.
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
			if userDirect[gr.SubjectID] == nil {
				userDirect[gr.SubjectID] = map[Grant]struct{}{}
			}
			userDirect[gr.SubjectID][key] = struct{}{}
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
		var userRoles []string
		if len(everyone) > 0 {
			userRoles = append(userRoles, EveryoneRole(warehouseID))
		}
		for _, gid := range memberships[u.ID] {
			ident := RoleIdent(warehouseID, orgID, gid)
			if _, ok := roles[ident]; ok {
				userRoles = append(userRoles, ident)
			}
		}
		direct := userDirect[u.ID.String()]
		if len(userRoles) == 0 && len(direct) == 0 {
			continue // no effective grants: do not provision
		}
		pw := DerivePassword(masterKey, warehouseID, u.ID)
		usersOut[UserIdent(warehouseID, orgID, u.ID)] = UserState{
			Password:     pw,
			Roles:        sortedStrings(userRoles),
			DirectGrants: direct,
		}
	}

	return DesiredState{Roles: roles, Users: usersOut}
}
