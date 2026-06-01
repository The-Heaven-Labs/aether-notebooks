package executor

import (
	"context"
	"strconv"
	"strings"
)

type ResultSet struct {
	Columns []Column        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
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
func ApplyLimit(query string, limit int) string {
	if limit <= 0 {
		return query
	}
	if strings.Contains(strings.ToUpper(query), "LIMIT") {
		return query
	}
	return strings.TrimRight(strings.TrimSpace(query), ";") + " LIMIT " + strconv.Itoa(limit)
}