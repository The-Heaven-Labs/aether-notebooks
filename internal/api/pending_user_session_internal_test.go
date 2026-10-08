package api

import (
	"context"
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
