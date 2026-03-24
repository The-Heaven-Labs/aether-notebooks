package executor

import "context"

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
