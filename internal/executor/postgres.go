package executor

import (
	"context"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresExecutor struct {
	pool *pgxpool.Pool
}

func NewPostgresExecutor(cfg models.ConnectorConfig) (*PostgresExecutor, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	if cfg.SSLMode != "" {
		dsn += "?sslmode=" + cfg.SSLMode
	} else {
		dsn += "?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &PostgresExecutor{pool: pool}, nil
}

func (p *PostgresExecutor) Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error) {
	resolved := ResolveParams(query, params)

	rows, err := p.pool.Query(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columns := make([]Column, len(fields))
	for i, f := range fields {
		columns[i] = Column{Name: string(f.Name), Type: pgTypeToString(f.DataTypeOID)}
	}

	var resultRows [][]interface{}
	count := 0
	for rows.Next() && count < maxRows {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		resultRows = append(resultRows, values)
		count++
	}

	if resultRows == nil {
		resultRows = [][]interface{}{}
	}

	return &ResultSet{Columns: columns, Rows: resultRows}, nil
}

func (p *PostgresExecutor) TestConnection(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT table_schema, table_name, column_name, data_type
		 FROM information_schema.columns
		 WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		 ORDER BY table_schema, table_name, ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tableMap := map[string]*TableInfo{}
	var tableOrder []string

	for rows.Next() {
		var schema, table, col, dtype string
		if err := rows.Scan(&schema, &table, &col, &dtype); err != nil {
			return nil, err
		}
		key := schema + "." + table
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &TableInfo{Schema: schema, Name: table}
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

func (p *PostgresExecutor) Close() error {
	p.pool.Close()
	return nil
}

func pgTypeToString(oid uint32) string {
	switch oid {
	case 16:
		return "boolean"
	case 20, 21, 23:
		return "integer"
	case 25, 1043:
		return "text"
	case 700, 701:
		return "float"
	case 1082:
		return "date"
	case 1114, 1184:
		return "timestamp"
	case 3802:
		return "jsonb"
	default:
		return "unknown"
	}
}
