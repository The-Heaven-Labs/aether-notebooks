package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpSection(t *testing.T) {
	t.Run("truncates down section", func(t *testing.T) {
		content := "-- +migrate Up\nCREATE TABLE t (id int);\n\n-- +migrate Down\nDROP TABLE t;\n"
		got := upSection(content)
		require.Contains(t, got, "CREATE TABLE t")
		require.NotContains(t, got, "DROP TABLE t")
	})

	t.Run("keeps file without down section", func(t *testing.T) {
		content := "-- +migrate Up\nCREATE TABLE t (id int);\n"
		require.Equal(t, content, upSection(content))
	})

	t.Run("case insensitive marker with leading whitespace", func(t *testing.T) {
		content := "CREATE TABLE t (id int);\n   -- +MIGRATE down\nDROP TABLE t;\n"
		got := upSection(content)
		require.NotContains(t, got, "DROP TABLE t")
	})

	t.Run("only first down marker matters", func(t *testing.T) {
		content := "SELECT 1;\n-- +migrate Down\nDROP TABLE t;\n-- +migrate Down again\nDROP TABLE u;\n"
		require.Equal(t, "SELECT 1;", upSection(content))
	})

	t.Run("ignores non-comment mention", func(t *testing.T) {
		content := "CREATE TABLE t (note text DEFAULT '+migrate Down');\n"
		require.Equal(t, content, upSection(content))
	})

	t.Run("v056 excludes drop", func(t *testing.T) {
		raw, err := migrationFS.ReadFile("migrations/V056__cell_execution_logs.sql")
		require.NoError(t, err)
		require.Contains(t, string(raw), "DROP TABLE IF EXISTS cell_execution_logs", "fixture must keep its down section")
		got := upSection(string(raw))
		require.Contains(t, got, "CREATE TABLE IF NOT EXISTS cell_execution_logs")
		require.False(t, strings.Contains(got, "DROP TABLE"), "down section must be stripped")
	})
}
