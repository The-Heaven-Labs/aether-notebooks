package api

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
)

func TestCompileAndMatchHiddenPatterns(t *testing.T) {
	patterns := compileHiddenPatterns([]string{`^analytics\._tmp`, `_scratch$`, `(`})
	require.Len(t, patterns, 2, "an invalid stored pattern is skipped")

	require.True(t, matchesHiddenPattern(patterns, "analytics", "_tmp_123"))
	require.True(t, matchesHiddenPattern(patterns, "raw", "my_scratch"))
	require.False(t, matchesHiddenPattern(patterns, "analytics", "events"))
	require.False(t, matchesHiddenPattern(patterns, "", "scratchy"))
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
}
