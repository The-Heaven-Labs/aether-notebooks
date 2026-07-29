// Package executor defines the Executor interface for executing queries against various database backends.
// Implementations exist for PostgreSQL, ClickHouse, and JavaScript (inline execution).
package executor

import (
	"context"
	"strconv"
	"strings"
)

type ResultSet struct {
	Columns []Column        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Note    string          `json:"note,omitempty"`
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Executor interface {
	Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error)
	TestConnection(ctx context.Context) error
	Schema(ctx context.Context) (*SchemaInfo, error)
	Databases(ctx context.Context) ([]string, error)
	Close() error
}

type SchemaInfo struct {
	Tables []TableInfo `json:"tables"`
}

type TableInfo struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Columns     []ColumnInfo `json:"columns"`
}

type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// ApplyLimit appends a LIMIT clause to the query if limit > 0 and the query
// does not already contain LIMIT. It trims trailing whitespace and semicolons
// before appending to avoid producing invalid SQL like "SELECT 1;\n LIMIT 1000".
// Metadata and DDL commands that don't support LIMIT are skipped.
func ApplyLimit(query string, limit int) string {
	if limit <= 0 {
		return query
	}
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)
	// Skip commands that don't support LIMIT
	if hasPrefixAny(upper, []string{
		"SHOW ", "DESCRIBE ", "USE ", "CREATE ", "DROP ", "ALTER ",
		"INSERT ", "UPDATE ", "DELETE ", "TRUNCATE ", "OPTIMIZE ",
		"ATTACH ", "DETACH ", "CHECK ", "RENAME ", "KILL ",
		"SET ", "EXPLAIN ", "EXISTS ",
	}) {
		return query
	}
	if strings.Contains(upper, "LIMIT") {
		return query
	}
	return strings.TrimRight(trimmed, ";") + " LIMIT " + strconv.Itoa(limit)
}

func hasPrefixAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// CtxUserEmail is the context key for the Aether user email, used for query tracing.
type CtxUserEmail struct{}
