package api

// White-box coverage for the folder listing section helpers (Task B1 review):
// a non-zero offset must return the correctly sliced page hydrated with full
// rows and resolved action flags, and an offset at or past the total must
// return an empty page with has_more false. The helpers are unexported, so the
// test lives in package api; the setup mirrors permissions_internal_test.go.

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFolderSectionOffsetHydratesPage(t *testing.T) {
	srv := newSessionPermissionTestServer(t)
	ctx := context.Background()

	orgID := uuid.New()
	_, err := srv.db.Pool.Exec(ctx, `INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Offset Org", "offset-org-"+orgID.String())
	require.NoError(t, err)

	userID := uuid.New()
	_, err = srv.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), userID.String()+"@example.com", "Offset User")
	require.NoError(t, err)

	folderID := uuid.New()
	_, err = srv.db.Pool.Exec(ctx,
		`INSERT INTO folders (id, org_id, name, owner_id, created_by) VALUES ($1, $2, $3, $4, $4)`,
		folderID.String(), orgID.String(), "Offset Folder", userID.String())
	require.NoError(t, err)

	// Five notebooks, each one minute older than the previous: display order
	// (updated_at DESC) is insertion order.
	var want []string
	for i := 0; i < 5; i++ {
		nbID := uuid.New()
		_, err := srv.db.Pool.Exec(ctx,
			`INSERT INTO notebooks (id, org_id, title, created_by, folder_id, updated_at)
			 VALUES ($1, $2, $3, $4, $5, now() - make_interval(mins => $6))`,
			nbID.String(), orgID.String(), fmt.Sprintf("offset notebook %d", i), userID.String(), folderID.String(), i)
		require.NoError(t, err)
		want = append(want, nbID.String())

		// The second-newest row is editable, the rest view-only, so the test
		// can prove flags ride along with the offset page.
		actions := []string{"view"}
		if i == 1 {
			actions = []string{"view", "edit"}
		}
		_, err = srv.db.Pool.Exec(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			 VALUES ($1, 'notebook', $2::uuid, 'user', $3, $4)`,
			orgID.String(), nbID.String(), userID.String(), actions)
		require.NoError(t, err)
	}

	ident := permIdentity{UserID: userID.String(), OrgID: orgID.String(), OrgRole: "editor"}
	folder := folderID.String()

	// Offset 1, limit 2: rows 2 and 3 of the display order, fully hydrated.
	page, total, hasMore, err := srv.folderNotebookSection(ctx, ident, &folder, 1, 2)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.True(t, hasMore)
	require.Len(t, page, 2)
	require.Equal(t, want[1], page[0].ID)
	require.Equal(t, want[2], page[1].ID)
	require.True(t, page[0].CanEdit, "the offset page must carry the resolved action flags")
	require.False(t, page[1].CanEdit)
	require.Equal(t, "offset notebook 1", page[0].Title, "the offset page must be hydrated from full rows")

	// Offset at the end and past it: empty page, no next page, non-nil slice.
	for _, offset := range []int{5, 9} {
		page, total, hasMore, err = srv.folderNotebookSection(ctx, ident, &folder, offset, 2)
		require.NoError(t, err)
		require.Equal(t, 5, total)
		require.False(t, hasMore)
		require.Empty(t, page)
		require.NotNil(t, page, "an empty page must still be a non-nil empty slice")
	}
}
