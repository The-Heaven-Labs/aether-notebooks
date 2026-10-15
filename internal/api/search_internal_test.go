package api

// White-box guards for the shared search validation and the searchable-type
// derivation. These live in package api because the normalizer, the whitelist,
// and the title-SQL maps are unexported.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSearchSupportedTypesAreImplemented pins the whitelist/default
// derivation: every accepted type must have a title query and a group-order
// slot (dashboard carries its dedicated query), and it must stay on the
// favorites whitelist the normalizer intersects it with. A future favoritable
// type without search SQL must be excluded here rather than reaching
// searchTypePage, where it would fail every default search with an internal
// error.
func TestSearchSupportedTypesAreImplemented(t *testing.T) {
	require.NotEmpty(t, searchSupportedTypes)
	for _, resourceType := range searchSupportedTypes {
		require.True(t, searchImplementedTypeSet[resourceType], "%s must be implemented", resourceType)
		require.Contains(t, searchGroupOrder, resourceType, "%s has no group-order slot", resourceType)
		require.True(t, favoriteResourceTypeSet[resourceType], "%s must be favoritable", resourceType)
		if resourceType != "dashboard" {
			require.Contains(t, searchTitleSQL, resourceType, "%s has no title query", resourceType)
		}
	}
}

// TestNormalizeSearchParams pins the shared normalizer rules both entry
// points consume: the q/query label, query trimming and bounds, the types
// whitelist with dedupe, and the HTTP limit semantics (empty defaults,
// non-numeric or < 1 rejected, > 20 clamped).
func TestNormalizeSearchParams(t *testing.T) {
	t.Run("query label, trimming, and bounds", func(t *testing.T) {
		_, errMsg := normalizeSearchParams(searchParamsInput{QueryLabel: "q", Query: "   "})
		require.Equal(t, "q is required", errMsg)
		_, errMsg = normalizeSearchParams(searchParamsInput{QueryLabel: "query", Query: ""})
		require.Equal(t, "query is required", errMsg)
		_, errMsg = normalizeSearchParams(searchParamsInput{
			QueryLabel: "query",
			Query:      strings.Repeat("a", searchMaxQueryRunes+1),
		})
		require.Equal(t, "query must be at most 200 characters", errMsg)

		req, errMsg := normalizeSearchParams(searchParamsInput{QueryLabel: "q", Query: "  revenue  "})
		require.Empty(t, errMsg)
		require.Equal(t, "revenue", req.Query)
		require.Equal(t, searchDefaultLimit, req.Limit)
		require.Equal(t, searchSupportedTypes, req.Types, "an empty type list defaults to every searchable type")
		require.False(t, req.FavoritesOnly, "the favorites flag is endpoint-only and never set by the normalizer")
	})

	t.Run("types whitelist and dedupe", func(t *testing.T) {
		req, errMsg := normalizeSearchParams(searchParamsInput{
			Query: "x", Types: []string{" notebook ", "notebook", "folder"},
		})
		require.Empty(t, errMsg)
		require.Equal(t, []string{"notebook", "folder"}, req.Types)

		_, errMsg = normalizeSearchParams(searchParamsInput{Query: "x", Types: []string{"bogus"}})
		require.Equal(t, `unknown resource type "bogus"`, errMsg)
		_, errMsg = normalizeSearchParams(searchParamsInput{Query: "x", Types: []string{"notebook", "bogus"}})
		require.Equal(t, `unknown resource type "bogus"`, errMsg)
	})

	t.Run("limit rules", func(t *testing.T) {
		req, errMsg := normalizeSearchParams(searchParamsInput{Query: "x", Limit: "3"})
		require.Empty(t, errMsg)
		require.Equal(t, 3, req.Limit)

		req, errMsg = normalizeSearchParams(searchParamsInput{Query: "x", Limit: "999"})
		require.Empty(t, errMsg)
		require.Equal(t, searchMaxLimit, req.Limit, "over-ceiling limits clamp")

		for _, raw := range []string{"abc", "0", "-2", "1.5"} {
			_, errMsg = normalizeSearchParams(searchParamsInput{Query: "x", Limit: raw})
			require.Equal(t, "limit must be a positive integer", errMsg, "raw limit %q", raw)
		}
	})
}
