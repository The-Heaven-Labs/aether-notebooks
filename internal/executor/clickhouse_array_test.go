package executor

import (
	"context"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func ptrTo[T any](v T) *T { return &v }

func TestChTupleIsNamed(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"named", "Tuple(a Int64, b String)", true},
		{"unnamed", "Tuple(UInt8, String)", false},
		{"named single", "Tuple(a Int64)", true},
		{"unnamed single", "Tuple(Int64)", false},
		{"named with type params", "Tuple(a Decimal(10, 2), b DateTime64(3, 'UTC'))", true},
		{"unnamed with type params", "Tuple(Decimal(10, 2), DateTime64(3, 'UTC'))", false},
		{"named nested tuple", "Tuple(a Tuple(b Int64, c String))", true},
		{"unnamed nested tuple", "Tuple(Tuple(Int64, String))", false},
		{"named nested element", "Nested(a String, b UInt8)", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, chTupleIsNamed(tc.in))
		})
	}
}

func TestChAllocDest(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want interface{}
	}{
		{"string", "String", new(string)},
		{"nullable string", "Nullable(String)", ptrTo(new(string))},
		{"uint8", "UInt8", new(uint8)},
		{"int128", "Int128", new(big.Int)},
		{"datetime64", "DateTime64(3)", new(time.Time)},
		{"decimal", "Decimal(10, 2)", new(decimal.Decimal)},
		{"json", "JSON", new(any)},
		{"map", "Map(String, String)", new(map[string]any)},
		{"tuple", "Tuple(a Int64)", new(map[string]any)},
		{"unknown falls back to string", "AggregateFunction(sum, UInt64)", new(string)},

		// Arrays whose elements are not objects keep the generic []any
		// destination, including nested and parameterised element types.
		{"array string", "Array(String)", new([]any)},
		{"array array string", "Array(Array(String))", new([]any)},
		{"array map", "Array(Map(String, String))", new([]any)},

		// Object arrays: named tuples scan as objects, unnamed as positional
		// arrays; each Array dimension adds a slice level.
		{"array unnamed tuple", "Array(Tuple(UInt8, String))", new([][]any)},
		{"array named tuple", "Array(Tuple(a Int64, b String))", new([]map[string]any)},
		{"array nested", "Array(Nested(a String, b UInt8))", new([]map[string]any)},
		{"array array unnamed tuple", "Array(Array(Tuple(UInt8, String)))", new([][][]any)},
		{"array array named tuple", "Array(Array(Tuple(a Int64, b String)))", new([][]map[string]any)},
		{"array array array named tuple", "Array(Array(Array(Tuple(a Int64))))", new([][][]map[string]any)},
		{"nullable array named tuple", "Nullable(Array(Tuple(a Int64)))", ptrTo(new([]map[string]any))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chAllocDest(tc.in)
			require.Equal(t, reflect.TypeOf(tc.want), reflect.TypeOf(got))
		})
	}
}

func TestChExtractValueNestedObjectArrays(t *testing.T) {
	unnamed := [][]any{{uint8(1), "x"}}
	require.Equal(t, unnamed, chExtractValue(&unnamed))

	named := []map[string]any{{"a": int64(1), "b": "x"}}
	require.Equal(t, named, chExtractValue(&named))

	nested := [][]map[string]any{{{"a": int64(1)}}}
	require.Equal(t, nested, chExtractValue(&nested))

	var nilNamed *[]map[string]any
	require.Nil(t, chExtractValue(&nilNamed))
}

// fakeColumnType is a driver.ColumnType for tests that need column metadata
// without a live ClickHouse server.
type fakeColumnType struct {
	name   string
	dbType string
}

func (c fakeColumnType) Name() string             { return c.name }
func (c fakeColumnType) Nullable() bool           { return false }
func (c fakeColumnType) ScanType() reflect.Type   { return reflect.TypeOf("") }
func (c fakeColumnType) DatabaseTypeName() string { return c.dbType }

// panicScanRows panics from Scan the way clickhouse-go does when a
// destination cannot be reconciled with the column type; the production panic
// message is reproduced verbatim.
type panicScanRows struct{ fakeRows }

func (panicScanRows) Next() bool { return true }

func (panicScanRows) ColumnTypes() []driver.ColumnType {
	return []driver.ColumnType{fakeColumnType{name: "attachments", dbType: "Array(Tuple(filename String))"}}
}

func (panicScanRows) Scan(...any) error {
	panic("reflect.Set: value of type []map[string]interface {} is not assignable to type []interface {}")
}

type panicScanConn struct{ *fakeConn }

func (c panicScanConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	return panicScanRows{}, nil
}

// A driver panic during Scan must surface as a normal error naming the query
// columns, never escape Execute (which would reach net/http as a 500 panic).
func TestExecuteScanPanicBecomesError(t *testing.T) {
	e := NewPooledClickHouseExecutor(&panicScanConn{fakeConn: &fakeConn{}}, nil)

	_, err := e.Execute(context.Background(), "SELECT attachments FROM t", nil, OutputLimits{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scan:")
	require.Contains(t, err.Error(), "driver panic")
	require.Contains(t, err.Error(), "attachments Array(Tuple(filename String))")
	require.Contains(t, err.Error(), "reflect.Set")
}
