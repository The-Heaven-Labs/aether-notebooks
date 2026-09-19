package chaccess

// Grant is a single table-level SELECT grant.
type Grant struct {
	Database string
	Table    string
}

// RoleState is the desired grant set for one group role.
type RoleState struct {
	Ident  string
	Grants map[Grant]struct{}
}

// UserState is the desired state for one Aether user in a warehouse.
type UserState struct {
	Ident           string
	Password        string
	Roles           []string
	DirectGrants    map[Grant]struct{}
	SettingsProfile string // empty = none
}

// DesiredState is the full desired ClickHouse access state for a warehouse.
type DesiredState struct {
	Roles map[string]RoleState
	Users map[string]UserState
}

// UserActual is the observed state of one ClickHouse user.
type UserActual struct {
	Roles           map[string]struct{}
	DirectGrants    map[Grant]struct{}
	DefaultRolesAll bool
}

// ActualState is the observed ClickHouse access state. ForcePasswordReset is
// set by the caller when warehouses.applied_master_fp differs from the current
// master-key fingerprint (ClickHouse password hashes are salted and cannot be
// compared).
type ActualState struct {
	Roles              map[string]map[Grant]struct{}
	Users              map[string]UserActual
	HasWildcard        bool
	ForcePasswordReset bool
}
