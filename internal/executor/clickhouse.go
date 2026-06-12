package executor

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/shopspring/decimal"
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

// chBaseType strips Nullable(...) and LowCardinality(...) wrappers and
// returns the bare type name without parameters (e.g. "DateTime64(3)" → "DateTime64").
func chBaseType(t string) (base string, nullable bool) {
	if strings.HasPrefix(t, "Nullable(") && strings.HasSuffix(t, ")") {
		t = t[9 : len(t)-1]
		nullable = true
	}
	if strings.HasPrefix(t, "LowCardinality(") && strings.HasSuffix(t, ")") {
		t = t[15 : len(t)-1]
	}
	if idx := strings.IndexByte(t, '('); idx >= 0 {
		t = t[:idx]
	}
	return t, nullable
}

// chAllocDest returns a pointer suitable for scanning a ClickHouse column.
// The driver requires exact-width types for each integer size; wider types
// (Int128/Int256/UInt128/UInt256) use *big.Int.
// Nullable columns require a pointer-to-pointer so the driver can set nil.
func chAllocDest(typeName string) interface{} {
	base, nullable := chBaseType(typeName)
	if nullable {
		switch base {
		case "String", "FixedString", "UUID", "Enum8", "Enum16":
			var v *string
			return &v
		case "Bool":
			var v *bool
			return &v
		case "Float32":
			var v *float32
			return &v
		case "Float64":
			var v *float64
			return &v
		case "Int8":
			var v *int8
			return &v
		case "Int16":
			var v *int16
			return &v
		case "Int32":
			var v *int32
			return &v
		case "Int64":
			var v *int64
			return &v
		case "Int128", "Int256":
			var v *big.Int
			return &v
		case "UInt8":
			var v *uint8
			return &v
		case "UInt16":
			var v *uint16
			return &v
		case "UInt32":
			var v *uint32
			return &v
		case "UInt64":
			var v *uint64
			return &v
		case "UInt128", "UInt256":
			var v *big.Int
			return &v
		case "DateTime", "DateTime64", "Date", "Date32":
			var v *time.Time
			return &v
		case "Decimal":
			var v *decimal.Decimal
			return &v
		default:
			var v *string
			return &v
		}
	}
	switch base {
	case "String", "FixedString", "UUID", "Enum8", "Enum16":
		return new(string)
	case "Bool":
		return new(bool)
	case "Float32":
		return new(float32)
	case "Float64":
		return new(float64)
	case "Int8":
		return new(int8)
	case "Int16":
		return new(int16)
	case "Int32":
		return new(int32)
	case "Int64":
		return new(int64)
	case "Int128", "Int256":
		return new(big.Int)
	case "UInt8":
		return new(uint8)
	case "UInt16":
		return new(uint16)
	case "UInt32":
		return new(uint32)
	case "UInt64":
		return new(uint64)
	case "UInt128", "UInt256":
		return new(big.Int)
	case "DateTime", "DateTime64", "Date", "Date32":
		return new(time.Time)
	case "Decimal":
		return new(decimal.Decimal)
	default:
		return new(string)
	}
}

// chExtractValue dereferences the scan destination into a JSON-friendly value.
// Narrow integers are widened to int64/uint64; big.Int is returned as a string.
func chExtractValue(dest interface{}) interface{} {
	switch v := dest.(type) {
	case *string:
		return *v
	case **string:
		if *v == nil {
			return nil
		}
		return **v
	case *bool:
		return *v
	case **bool:
		if *v == nil {
			return nil
		}
		return **v
	case *float32:
		return float64(*v)
	case **float32:
		if *v == nil {
			return nil
		}
		return float64(**v)
	case *float64:
		return *v
	case **float64:
		if *v == nil {
			return nil
		}
		return **v
	case *int8:
		return int64(*v)
	case **int8:
		if *v == nil {
			return nil
		}
		return int64(**v)
	case *int16:
		return int64(*v)
	case **int16:
		if *v == nil {
			return nil
		}
		return int64(**v)
	case *int32:
		return int64(*v)
	case **int32:
		if *v == nil {
			return nil
		}
		return int64(**v)
	case *int64:
		return *v
	case **int64:
		if *v == nil {
			return nil
		}
		return **v
	case *big.Int:
		return v.String()
	case **big.Int:
		if *v == nil {
			return nil
		}
		return (*v).String()
	case *uint8:
		return uint64(*v)
	case **uint8:
		if *v == nil {
			return nil
		}
		return uint64(**v)
	case *uint16:
		return uint64(*v)
	case **uint16:
		if *v == nil {
			return nil
		}
		return uint64(**v)
	case *uint32:
		return uint64(*v)
	case **uint32:
		if *v == nil {
			return nil
		}
		return uint64(**v)
	case *uint64:
		return *v
	case **uint64:
		if *v == nil {
			return nil
		}
		return **v
	case *decimal.Decimal:
		f, _ := v.Float64()
		return f
	case **decimal.Decimal:
		if *v == nil {
			return nil
		}
		f, _ := (*v).Float64()
		return f
	case *time.Time:
		if v.IsZero() {
			return nil
		}
		return v.Format(time.RFC3339)
	case **time.Time:
		if *v == nil || (*v).IsZero() {
			return nil
		}
		return (*v).Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", dest)
	}
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
		dests := make([]interface{}, len(columns))
		for i, ct := range columnTypes {
			dests[i] = chAllocDest(ct.DatabaseTypeName())
		}
		if err := rows.Scan(dests...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		row := make([]interface{}, len(columns))
		for i, d := range dests {
			row[i] = chExtractValue(d)
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
	// ClickHouse stores column comments in system.columns.comment
	// and table comments in system.tables.metadata_comment
	rows, err := c.conn.Query(ctx,
		`SELECT database, table, name, type, comment
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
		var db, table, col, dtype, comment string
		if err := rows.Scan(&db, &table, &col, &dtype, &comment); err != nil {
			return nil, err
		}
		key := db + "." + table
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &TableInfo{Schema: db, Name: table}
			tableOrder = append(tableOrder, key)
		}
		tableMap[key].Columns = append(tableMap[key].Columns, ColumnInfo{Name: col, Type: dtype, Description: comment})
	}

	// Fetch table-level comments
	for _, key := range tableOrder {
		t := tableMap[key]
		var tableComment string
		if err := c.conn.QueryRow(ctx,
			`SELECT comment FROM system.tables 
			 WHERE database = $1 AND name = $2`,
			t.Schema, t.Name).Scan(&tableComment); err == nil {
			t.Description = tableComment
		}
	}

	tables := make([]TableInfo, 0, len(tableOrder))
	for _, key := range tableOrder {
		tables = append(tables, *tableMap[key])
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (c *ClickHouseExecutor) Databases(ctx context.Context) ([]string, error) {
	rows, err := c.conn.Query(ctx, "SHOW DATABASES")
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

func (c *ClickHouseExecutor) Close() error {
	return c.conn.Close()
}
