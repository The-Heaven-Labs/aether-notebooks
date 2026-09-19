package chaccess

import (
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComputeUnionsGroupsEveryoneAndDirect(t *testing.T) {
	org, wh, u1, g1, g2 := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()

	grants := []SubjectGrant{
		{SubjectType: "user", SubjectID: u1.String(), Database: "db", Table: "u_only"},
		{SubjectType: "group", SubjectID: g1.String(), Database: "db", Table: "g1_a"},
		{SubjectType: "group", SubjectID: g2.String(), Database: "db", Table: "g2_a"},
		{SubjectType: "everyone", SubjectID: "everyone", Database: "db", Table: "public"},
	}
	memberships := map[uuid.UUID][]uuid.UUID{u1: {g1}}
	users := []UserSpec{{ID: u1, Email: "u1@x.com"}}
	master := []byte("0123456789abcdef0123456789abcdef")

	d := Compute(org, wh, master, grants, memberships, users)

	require.Contains(t, d.Roles, RoleIdent(wh, org, g1))
	require.Contains(t, d.Roles, RoleIdent(wh, org, g2))
	require.Contains(t, d.Roles, EveryoneRole(wh))
	require.NotContains(t, d.Roles, RoleIdent(wh, org, uuid.New()))

	require.Contains(t, d.Roles[RoleIdent(wh, org, g1)].Grants, Grant{Database: "db", Table: "g1_a"})
	require.Contains(t, d.Roles[EveryoneRole(wh)].Grants, Grant{Database: "db", Table: "public"})

	ust := d.Users[UserIdent(wh, org, u1)]
	require.Equal(t, []string{EveryoneRole(wh), RoleIdent(wh, org, g1)}, ust.Roles)
	require.Contains(t, ust.DirectGrants, Grant{Database: "db", Table: "u_only"})
	require.Equal(t, DerivePassword(master, wh, u1), ust.Password)
}

func TestComputeSkipsUserWithNoEffectiveGrants(t *testing.T) {
	org, wh, u1 := uuid.New(), uuid.New(), uuid.New()
	d := Compute(org, wh, []byte("k"), nil, map[uuid.UUID][]uuid.UUID{u1: {}}, []UserSpec{{ID: u1}})
	require.NotContains(t, d.Users, UserIdent(wh, org, u1))
}

func TestComputeProvisionsDirectOnlyUser(t *testing.T) {
	org, wh, u1 := uuid.New(), uuid.New(), uuid.New()
	grants := []SubjectGrant{
		{SubjectType: "user", SubjectID: u1.String(), Database: "db", Table: "t"},
	}
	master := []byte("0123456789abcdef0123456789abcdef")

	d := Compute(org, wh, master, grants, nil, []UserSpec{{ID: u1}})

	ust, ok := d.Users[UserIdent(wh, org, u1)]
	require.True(t, ok)
	require.Empty(t, ust.Roles)
	require.Contains(t, ust.DirectGrants, Grant{Database: "db", Table: "t"})
	require.Equal(t, DerivePassword(master, wh, u1), ust.Password)
}

func TestComputeSkipsMembershipInGroupWithoutGrants(t *testing.T) {
	org, wh, u1, g1 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	grants := []SubjectGrant{
		{SubjectType: "group", SubjectID: uuid.New().String(), Database: "db", Table: "other"},
	}
	memberships := map[uuid.UUID][]uuid.UUID{u1: {g1}}

	d := Compute(org, wh, []byte("k"), grants, memberships, []UserSpec{{ID: u1}})

	require.NotContains(t, d.Roles, RoleIdent(wh, org, g1))
	require.NotContains(t, d.Users, UserIdent(wh, org, u1))
}

func TestComputeSortsRolesAcrossGrantingGroups(t *testing.T) {
	org, wh, u1, g1, g2, g3 := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	grants := []SubjectGrant{
		{SubjectType: "group", SubjectID: g3.String(), Database: "db", Table: "t3"},
		{SubjectType: "group", SubjectID: g1.String(), Database: "db", Table: "t1"},
		{SubjectType: "group", SubjectID: g2.String(), Database: "db", Table: "t2"},
		{SubjectType: "everyone", SubjectID: "everyone", Database: "db", Table: "public"},
	}
	memberships := map[uuid.UUID][]uuid.UUID{u1: {g2, g1, g3}}

	d := Compute(org, wh, []byte("k"), grants, memberships, []UserSpec{{ID: u1}})

	want := []string{EveryoneRole(wh), RoleIdent(wh, org, g1), RoleIdent(wh, org, g2), RoleIdent(wh, org, g3)}
	sort.Strings(want)
	require.Equal(t, want, d.Users[UserIdent(wh, org, u1)].Roles)
}

func TestComputeEveryoneRoleWithoutMembers(t *testing.T) {
	org, wh := uuid.New(), uuid.New()
	grants := []SubjectGrant{
		{SubjectType: "everyone", SubjectID: "everyone", Database: "db", Table: "public"},
	}

	d := Compute(org, wh, []byte("k"), grants, nil, nil)

	require.Contains(t, d.Roles, EveryoneRole(wh))
	require.Empty(t, d.Users)
}

func TestComputeIgnoresUnparseableGroupSubject(t *testing.T) {
	org, wh := uuid.New(), uuid.New()
	grants := []SubjectGrant{
		{SubjectType: "group", SubjectID: "not-a-uuid", Database: "db", Table: "t"},
	}

	d := Compute(org, wh, []byte("k"), grants, nil, nil)

	require.Empty(t, d.Roles)
	require.Empty(t, d.Users)
}
