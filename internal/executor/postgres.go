package executor

import (
	"context"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// normalizeValue converts pgx native types that JSON-marshal poorly into
// friendlier representations (e.g. [16]byte UUID → "xxxxxxxx-xxxx-…").
func normalizeValue(v interface{}) interface{} {
	switch t := v.(type) {
	case [16]byte:
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			t[0:4], t[4:6], t[6:8], t[8:10], t[10:16])
	default:
		return v
	}
}

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
		for i, v := range values {
			values[i] = normalizeValue(v)
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
	// Query to get table and column info with comments
	// PostgreSQL stores comments in pg_catalog.pg_description:
	//   - objsubid = 0 → table/view comment
	//   - objsubid > 0 → column comment (objsubid = column ordinal position)
	rows, err := p.pool.Query(ctx,
		`SELECT 
		    c.table_schema,
		    c.table_name,
		    c.column_name,
		    c.data_type,
		    COALESCE(col_description(
		        (c.table_schema || '.' || c.table_name)::regclass,
		        c.ordinal_position), ''
		    ) AS column_comment
		FROM information_schema.columns c
		WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY c.table_schema, c.table_name, c.ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tableMap := map[string]*TableInfo{}
	var tableOrder []string

	for rows.Next() {
		var schema, table, col, dtype, comment string
		if err := rows.Scan(&schema, &table, &col, &dtype, &comment); err != nil {
			return nil, err
		}
		key := schema + "." + table
		if _, ok := tableMap[key]; !ok {
			// Get table-level comment
			var tableComment string
			_ = p.pool.QueryRow(ctx,
				`SELECT COALESCE(obj_description(
				    ($1 || '.' || $2)::regclass
				), '')`, schema, table).Scan(&tableComment)
			tableMap[key] = &TableInfo{Schema: schema, Name: table, Description: tableComment}
			tableOrder = append(tableOrder, key)
		}
		tableMap[key].Columns = append(tableMap[key].Columns, ColumnInfo{Name: col, Type: dtype, Description: comment})
	}

	tables := make([]TableInfo, 0, len(tableOrder))
	for _, key := range tableOrder {
		tables = append(tables, *tableMap[key])
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (p *PostgresExecutor) Databases(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
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
	case 25, 19, 1042, 1043:
		return "text"
	case 700, 701:
		return "float"
	case 1700:
		return "numeric"
	case 1082:
		return "date"
	case 1083, 1266:
		return "time"
	case 1114, 1184:
		return "timestamp"
	case 1186:
		return "interval"
	case 114:
		return "json"
	case 3802:
		return "jsonb"
	case 2950:
		return "uuid"
	case 17:
		return "bytea"
	default:
		return "unknown"
	}
}
