package chaccess

import (
	"context"
	"fmt"
	"sort"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// CatalogTable is one database/table pair observed in a ClickHouse catalog.
type CatalogTable struct {
	Database string
	Table    string
}

// LoadCatalogTables enumerates grantable catalog objects from system.tables.
// It is the raw snapshot source for the new-tables inbox: no per-subject
// filtering happens here, and names that cannot be represented as grant
// object identifiers are dropped for the same reason grants rejects them.
//
// system databases are excluded because Aether never grants them, and
// temporary tables are excluded because they vanish with their session.
func LoadCatalogTables(ctx context.Context, conn clickhouse.Conn) ([]CatalogTable, error) {
	rows, err := conn.Query(ctx, `
		SELECT database, name
		FROM system.tables
		WHERE database NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		  AND is_temporary = 0
		ORDER BY database ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("query system.tables: %w", err)
	}
	defer rows.Close()

	var raw []CatalogTable
	for rows.Next() {
		var t CatalogTable
		if err := rows.Scan(&t.Database, &t.Table); err != nil {
			return nil, fmt.Errorf("scan system.tables: %w", err)
		}
		raw = append(raw, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read system.tables: %w", err)
	}
	return SanitizeCatalogTables(raw), nil
}

// SanitizeCatalogTables drops names that QuoteObjectIdent rejects (they could
// never be grant rows), deduplicates pairs, and returns a sorted copy. It is
// exported so snapshot writers can reuse the same validation: duplicate pairs
// in one INSERT..ON CONFLICT batch are a Postgres error ("cannot affect row a
// second time"), not a benign replay.
func SanitizeCatalogTables(in []CatalogTable) []CatalogTable {
	seen := make(map[CatalogTable]struct{}, len(in))
	out := make([]CatalogTable, 0, len(in))
	for _, t := range in {
		if _, err := QuoteObjectIdent(t.Database); err != nil {
			continue
		}
		if _, err := QuoteObjectIdent(t.Table); err != nil {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Database != out[j].Database {
			return out[i].Database < out[j].Database
		}
		return out[i].Table < out[j].Table
	})
	return out
}
