package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// searchResourcesTestDef returns the registered search_resources tool so the
// tests exercise the real registry entry (name, arguments schema, timeout).
func searchResourcesTestDef(t *testing.T) *ToolDef {
	t.Helper()
	reg := NewToolRegistry()
	RegisterPlatformTools(reg, nil)
	def, ok := reg.Get("search_resources")
	require.True(t, ok, "registry must contain search_resources")
	require.Equal(t, 20*time.Second, def.Timeout)
	return def
}

// searchResourcesCall records one delegated search invocation.
type searchResourcesCall struct {
	userID, orgID, orgRole string
	query                  string
	types                  []string
	limit                  int
}

// stubSearchResources returns a SearchResourcesFunc stub that records the call
// and returns a sentinel payload the handler must pass through unchanged.
func stubSearchResources(calls *[]searchResourcesCall) func(context.Context, string, string, string, string, []string, int) (any, error) {
	return func(_ context.Context, userID, orgID, orgRole, query string, types []string, limit int) (any, error) {
		*calls = append(*calls, searchResourcesCall{
			userID: userID, orgID: orgID, orgRole: orgRole,
			query: query, types: types, limit: limit,
		})
		return map[string]any{"delegated": true}, nil
	}
}

func TestSearchResourcesToolDelegatesArgs(t *testing.T) {
	def := searchResourcesTestDef(t)

	t.Run("defaults", func(t *testing.T) {
		var calls []searchResourcesCall
		ctx := &ToolContext{
			Context:             context.Background(),
			UserID:              "user-1",
			OrgID:               "org-1",
			OrgRole:             "editor",
			SearchResourcesFunc: stubSearchResources(&calls),
		}
		result, err := def.Handler(json.RawMessage(`{"query":"  revenue by region  "}`), ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"delegated": true}, result, "the delegated payload must be returned as-is")
		require.Len(t, calls, 1)
		call := calls[0]
		require.Equal(t, "user-1", call.userID)
		require.Equal(t, "org-1", call.orgID)
		require.Equal(t, "editor", call.orgRole)
		require.Equal(t, "revenue by region", call.query, "query must be trimmed")
		require.Nil(t, call.types, "omitted types stay nil so the engine defaults to every type")
		require.Equal(t, searchResourcesDefaultLimit, call.limit)
	})

	t.Run("explicit types and limit", func(t *testing.T) {
		var calls []searchResourcesCall
		ctx := &ToolContext{Context: context.Background(), SearchResourcesFunc: stubSearchResources(&calls)}
		_, err := def.Handler(json.RawMessage(`{"query":"revenue","types":["notebook","dashboard"],"limit":3}`), ctx)
		require.NoError(t, err)
		require.Len(t, calls, 1)
		require.Equal(t, []string{"notebook", "dashboard"}, calls[0].types)
		require.Equal(t, 3, calls[0].limit)
	})

	t.Run("limit passes through unclamped", func(t *testing.T) {
		// The engine's shared normalizer owns the HTTP limit rules (reject
		// < 1, clamp > 20), so the handler must not reshape the value on the
		// way in — clamping 0 up to 1 would silently turn a caller error into
		// a default page.
		for _, tc := range []struct {
			args string
			want int
		}{
			{`{"query":"revenue","limit":500}`, 500},
			{`{"query":"revenue","limit":21}`, 21},
			{`{"query":"revenue","limit":0}`, 0},
			{`{"query":"revenue","limit":-3}`, -3},
		} {
			var calls []searchResourcesCall
			ctx := &ToolContext{Context: context.Background(), SearchResourcesFunc: stubSearchResources(&calls)}
			_, err := def.Handler(json.RawMessage(tc.args), ctx)
			require.NoError(t, err, tc.args)
			require.Len(t, calls, 1, tc.args)
			require.Equal(t, tc.want, calls[0].limit, tc.args)
		}
	})
}

func TestSearchResourcesToolRequiresQuery(t *testing.T) {
	def := searchResourcesTestDef(t)
	var calls []searchResourcesCall
	ctx := &ToolContext{Context: context.Background(), SearchResourcesFunc: stubSearchResources(&calls)}
	for _, args := range []string{`{}`, `{"query":""}`, `{"query":"   "}`} {
		_, err := def.Handler(json.RawMessage(args), ctx)
		require.ErrorContains(t, err, "query is required", args)
	}
	require.Empty(t, calls, "a missing query must never reach the search engine")
}

// A bare ToolContext has no ACL-aware search engine; the tool must fail closed
// instead of searching unfiltered.
func TestSearchResourcesToolFailsClosedWithoutEngine(t *testing.T) {
	def := searchResourcesTestDef(t)
	_, err := def.Handler(json.RawMessage(`{"query":"revenue"}`), &ToolContext{Context: context.Background()})
	require.ErrorContains(t, err, "no search engine is wired")
}

// The tool must be seeded (orgs assign tools by name) with the same text the
// model sees, or the registry and the Tools UI drift.
func TestSearchResourcesToolIsSeeded(t *testing.T) {
	def := searchResourcesTestDef(t)
	var seeded *BuiltinToolDef
	for i := range BuiltinTools {
		if BuiltinTools[i].Name == "search_resources" {
			seeded = &BuiltinTools[i]
		}
	}
	require.NotNil(t, seeded, "search_resources must be in the builtin seed catalog")
	require.Equal(t, "search_resources", seeded.HandlerName)
	require.Equal(t, builtinSeedRev, seeded.Rev)
	require.Equal(t, def.Function.Description, seeded.Description, "seed and registry descriptions must not drift")
}
