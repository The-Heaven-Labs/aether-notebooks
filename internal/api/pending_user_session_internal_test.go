package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSessionShareEntriesAcceptsPendingViewOnly(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-share")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")

	got, err := s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "Future@Example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "future@example.com", Actions: []string{"view"}},
	}, got, "pending emails are lowercased and default to view")

	_, err = s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "future@example.com", Actions: []string{"view", "edit"}},
	})
	require.ErrorIs(t, err, errInvalidSessionShare, "pending shares stay read-only")

	_, err = s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "not-an-email", Actions: []string{"view"}},
	})
	require.ErrorIs(t, err, errInvalidSessionShare, "pending shares require a valid email")
}

func TestNormalizeSessionShareEntriesConvertsMemberEmail(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-convert")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	memberID := insertSessionPermUser(t, s, "member")
	addSessionPermMember(t, s, orgID, memberID, "editor")

	var memberEmail, ownerEmail string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, memberID.String()).Scan(&memberEmail))
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, ownerID.String()).Scan(&ownerEmail))

	got, err := s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: strings.ToUpper(memberEmail), Actions: []string{"view"}},
		{SubjectType: "pending_user", SubjectID: "stranger@example.com", Actions: []string{"view"}},
		{SubjectType: "user", SubjectID: memberID.String(), Actions: []string{"view"}},
	})
	require.NoError(t, err)
	require.Equal(t, []aclEntryInput{
		{SubjectType: "user", SubjectID: memberID.String(), Actions: []string{"view"}},
		{SubjectType: "pending_user", SubjectID: "stranger@example.com", Actions: []string{"view"}},
	}, got, "a member's email converts to a real user share, collapses with a direct entry, and a stranger stays staged")

	got, err = s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: ownerEmail, Actions: []string{"view"}},
	})
	require.NoError(t, err)
	require.Empty(t, got, "the owner's email is dropped like any owner entry")

	_, err = s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: memberEmail, Actions: []string{"view", "edit"}},
	})
	require.ErrorIs(t, err, errInvalidSessionShare, "converted member shares stay read-only")
}
