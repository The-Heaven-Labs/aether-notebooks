package executor

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/heavenlabs/hnb/internal/models"
)

type ClickHouseExecutor struct {
	conn clickhouse.Conn
}

func NewClickHouseExecutor(cfg models.ConnectorConfig) (*ClickHouseExecutor, error) {
	port := cfg.Port
	if port == 0 {
		port = 9000
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, port)},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		Protocol: clickhouse.Native,
	})
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &ClickHouseExecutor{conn: conn}, nil
}

func (c *ClickHouseExecutor) Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error) {
	resolved := ResolveParams(query, params)

	rows, err := c.conn.Query(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	columnTypes := rows.ColumnTypes()
	columns := make([]Column, len(columnTypes))
	for i, ct := range columnTypes {
		columns[i] = Column{Name: ct.Name(), Type: ct.DatabaseTypeName()}
	}

	var resultRows [][]interface{}
	count := 0
	for rows.Next() && count < maxRows {
		row := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		resultRows = append(resultRows, row)
		count++
	}

	if resultRows == nil {
		resultRows = [][]interface{}{}
	}

	return &ResultSet{Columns: columns, Rows: resultRows}, nil
}

func (c *ClickHouseExecutor) TestConnection(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

func (c *ClickHouseExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	rows, err := c.conn.Query(ctx,
		`SELECT database, table, name, type
		 FROM system.columns
		 WHERE database NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		 ORDER BY database, table, position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tableMap := map[string]*TableInfo{}
	var tableOrder []string

	for rows.Next() {
		var db, table, col, dtype string
		if err := rows.Scan(&db, &table, &col, &dtype); err != nil {
			return nil, err
		}
		key := db + "." + table
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &TableInfo{Schema: db, Name: table}
			tableOrder = append(tableOrder, key)
		}
		tableMap[key].Columns = append(tableMap[key].Columns, ColumnInfo{Name: col, Type: dtype})
	}

	tables := make([]TableInfo, 0, len(tableOrder))
	for _, key := range tableOrder {
		tables = append(tables, *tableMap[key])
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (c *ClickHouseExecutor) Close() error {
	return c.conn.Close()
}
