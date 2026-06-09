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
	Schema  string       `json:"schema"`
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns"`
}

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ApplyLimit appends a LIMIT clause to the query if limit > 0 and the query
// does not already contain LIMIT. It trims trailing whitespace and semicolons
// before appending to avoid producing invalid SQL like "SELECT 1;\n LIMIT 1000".
// Metadata commands (SHOW, DESCRIBE) are not modified as they don't support LIMIT.
func ApplyLimit(query string, limit int) string {
	if limit <= 0 {
		return query
	}
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)
	// Skip metadata commands that don't support LIMIT
	if strings.HasPrefix(upper, "SHOW ") || strings.HasPrefix(upper, "DESCRIBE ") {
		return query
	}
	if strings.Contains(upper, "LIMIT") {
		return query
	}
	return strings.TrimRight(trimmed, ";") + " LIMIT " + strconv.Itoa(limit)
}