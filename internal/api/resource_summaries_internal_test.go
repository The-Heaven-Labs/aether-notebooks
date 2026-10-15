package api

// White-box guards for the shared summary builder (used by favorites now and
// search next). These tests live in package api because the whitelist, the
// metadata fetcher, and the builder's option semantics are unexported.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestFavoritesResourceTypesHaveMetadataQueries pins the favorites whitelist
// to the metadata fetcher: favoriteResourceTypes derives from resourceTable
// plus folder, and every type a caller can favorite must have a metadata
// query — a missing one would 500 the whole /favorites listing instead of
// omitting the resource. The final assertion keeps the guard honest: unknown
// types must still error, otherwise this test would pass vacuously.
func TestFavoritesResourceTypesHaveMetadataQueries(t *testing.T) {
	srv := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, srv, "favorites-parity").String()

	// The whitelist must cover everything resolveActions supports.
	require.Contains(t, favoriteResourceTypes, "folder")
	for resourceType := range resourceTable {
		require.Contains(t, favoriteResourceTypes, resourceType)
	}

	for _, resourceType := range favoriteResourceTypes {
		meta, err := srv.fetchResourceSummaryMeta(ctx, orgID, resourceType, []string{uuid.NewString()})
		require.NoError(t, err, "resource type %q has no metadata query", resourceType)
		require.Empty(t, meta)
	}

	_, err := srv.fetchResourceSummaryMeta(ctx, orgID, "not_a_type", []string{uuid.NewString()})
	require.Error(t, err)
}

// TestFavoritesBuilderLoadsFavoritesWhenNil covers the builder's fallback:
// with no preloaded Favorites it loads the caller's favorites itself, so the
// is_favorite flag is correct without the caller scanning the table.
func TestFavoritesBuilderLoadsFavoritesWhenNil(t *testing.T) {
	srv := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, srv, "fav-options")
	userID := insertSessionPermUser(t, srv, "fav-options")
	notebookID := uuid.New()

	_, err := srv.db.Pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
		notebookID, orgID, "Options Notebook", userID)
	require.NoError(t, err)
	_, err = srv.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2, 'user', $3, ARRAY['view'])`,
		orgID, notebookID, userID)
	require.NoError(t, err)
	_, err = srv.db.Pool.Exec(ctx,
		`INSERT INTO favorites (user_id, org_id, resource_type, resource_id) VALUES ($1, $2, 'notebook', $3)`,
		userID, orgID, notebookID)
	require.NoError(t, err)

	ident := permIdentity{UserID: userID.String(), OrgID: orgID.String(), OrgRole: "editor"}
	groups, err := srv.buildResourceSummaries(ctx, ident, []summaryRequest{
		{ResourceType: "notebook", IDs: []string{notebookID.String()}},
	}, summaryBuildOptions{})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Results, 1)
	require.True(t, groups[0].Results[0].IsFavorite)
	require.False(t, groups[0].Results[0].CanEdit)
}
