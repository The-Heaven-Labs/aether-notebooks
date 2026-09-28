package api

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
)

func TestCompileAndMatchHiddenPatterns(t *testing.T) {
	patterns := compileHiddenPatterns([]string{`^analytics\._tmp`, `_scratch$`, `^scratchy$`, `(`})
	require.Len(t, patterns, 3, "an invalid stored pattern is skipped")

	require.True(t, matchesHiddenPattern(patterns, "analytics", "_tmp_123"))
	require.True(t, matchesHiddenPattern(patterns, "raw", "my_scratch"))
	require.False(t, matchesHiddenPattern(patterns, "analytics", "events"))
	require.True(t, matchesHiddenPattern(patterns, "", "scratchy"),
		"an empty database matches the table name alone")
}

func TestFilterVisibleSchemaTables(t *testing.T) {
	tables := []executor.TableInfo{
		{Schema: "analytics", Name: "events"},
		{Schema: "analytics", Name: "_tmp_scratch"},
		{Schema: "raw", Name: "clicks"},
	}
	patterns := compileHiddenPatterns([]string{`_tmp`})

	// Admin view: patterns drop matched ungranted tables.
	got := filterVisibleSchemaTables(tables, patterns, nil, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "events"}, {Schema: "raw", Name: "clicks"}}, got)

	// Granted tables survive a pattern match.
	protected := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, nil, protected)
	require.Len(t, got, 3)

	// An effective-grant allowlist wins over everything else.
	allowed := map[tableKey]struct{}{{Database: "raw", Table: "clicks"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, allowed, allowed)
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)

	// A pattern-matched granted table stays visible while it is protected:
	// patterns can never hide granted access.
	grantedMatched := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, grantedMatched, grantedMatched)
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "_tmp_scratch"}}, got)

	// allowed and patternProtected are distinct parameters: an allowed
	// pattern-matched table is dropped when patternProtected is empty...
	got = filterVisibleSchemaTables(tables, patterns, grantedMatched, map[tableKey]struct{}{})
	require.Empty(t, got)

	// ...while an allowed table that matches no pattern survives an empty
	// patternProtected.
	got = filterVisibleSchemaTables(tables, patterns, allowed, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)
}
