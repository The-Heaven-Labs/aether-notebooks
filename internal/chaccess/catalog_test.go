package chaccess

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeCatalogTablesDropsUnrepresentableNames(t *testing.T) {
	in := []CatalogTable{
		{Database: "analytics", Table: "events"},
		{Database: "analytics", Table: "events"},     // duplicate
		{Database: "analytics", Table: "weird name"}, // whitespace is not grantable
		{Database: "bad`db", Table: "events"},
		{Database: "raw", Table: "$clicks-v2"},
		{Database: "analytics", Table: "events_daily"},
	}
	out := sanitizeCatalogTables(in)
	require.Equal(t, []CatalogTable{
		{Database: "analytics", Table: "events"},
		{Database: "analytics", Table: "events_daily"},
		{Database: "raw", Table: "$clicks-v2"},
	}, out)
}

func TestSanitizeCatalogTablesEmpty(t *testing.T) {
	require.Empty(t, sanitizeCatalogTables(nil))
}
