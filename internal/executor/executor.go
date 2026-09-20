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
	// Truncated reports whether the result was cut off at a row boundary by
	// the byte budget (OutputLimits.MaxBytes). RowsIncluded is the number of
	// rows kept; RowsTotal is the driver-reported total row count or -1 when
	// unknown. Bytes is the cheap estimated size of the included rows.
	Truncated    bool   `json:"truncated,omitempty"`
	RowsIncluded int    `json:"rows_included,omitempty"`
	RowsTotal    int64  `json:"rows_total,omitempty"`
	Bytes        int64  `json:"bytes,omitempty"`
	Note         string `json:"note,omitempty"`
}

// OutputLimits bounds an execution's result set. MaxBytes is the per-cell
// byte cap (<=0 = unlimited); MaxRows caps the number of rows (<=0 =
// unlimited). Truncation always happens at row boundaries so results stay
// valid tables.
type OutputLimits struct {
	MaxBytes int64
	MaxRows  int
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Executor interface {
	Execute(ctx context.Context, query string, params map[string]string, limits OutputLimits) (*ResultSet, error)
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

// CtxExecutionID is the context key for the Aether execution ID. ClickHouse
// execution tags every query with log_comment "aether:<id>" so rows in
// system.query_log can be joined back to the Aether audit entry.
type CtxExecutionID struct{}

// WithExecutionID returns a context carrying the per-execution ID used for
// query tagging.
func WithExecutionID(ctx context.Context, executionID string) context.Context {
	return context.WithValue(ctx, CtxExecutionID{}, executionID)
}

// ExecutionIDFromContext returns the execution ID carried by ctx, or "" when
// the context has none.
func ExecutionIDFromContext(ctx context.Context) string {
	executionID, _ := ctx.Value(CtxExecutionID{}).(string)
	return executionID
}

// estimateValueSize returns a cheap lower-bound byte estimate for a single
// result value. JSON overhead and numeric encodings are bounded, so summing
// these per row is accurate enough to enforce a byte cap without round-tripping
// through JSON. Precision is not required — the estimate just needs to be
// monotonic with the real marshaled size.
func estimateValueSize(v interface{}) int64 {
	switch t := v.(type) {
	case nil:
		return 4
	case string:
		return int64(len(t))
	case []byte:
		return int64(len(t))
	case bool:
		return 4
	case []interface{}:
		var n int64
		for _, e := range t {
			n += estimateValueSize(e)
		}
		return n
	case map[string]interface{}:
		var n int64
		for k, e := range t {
			n += int64(len(k)) + estimateValueSize(e)
		}
		return n
	default:
		// Numbers, timestamps and driver-native types serialize to a bounded
		// number of bytes; a fixed estimate keeps the per-row cost O(values).
		return 16
	}
}

// estimateRowSize sums the per-value estimates for one result row.
func estimateRowSize(row []interface{}) int64 {
	var n int64
	for _, v := range row {
		n += estimateValueSize(v)
	}
	return n
}

// rowAccumulator appends scanned rows while respecting the byte and row
// budgets. Truncation is always applied at row boundaries; the first row alone
// may exceed the budget, in which case zero rows are included and truncated is
// set (the groupUniqArray-over-huge-strings shape).
type rowAccumulator struct {
	limits       OutputLimits
	rows         [][]interface{}
	bytes        int64
	truncated    bool
	rowsIncluded int
	rowsTotal    int64
}

func newRowAccumulator(limits OutputLimits) *rowAccumulator {
	return &rowAccumulator{limits: limits, rows: [][]interface{}{}, rowsTotal: -1}
}

// full reports whether the row budget is exhausted or the byte budget already
// truncated the scan.
func (a *rowAccumulator) full() bool {
	if a.limits.MaxRows > 0 && a.rowsIncluded >= a.limits.MaxRows {
		return true
	}
	return a.truncated
}

// add appends a scanned row if it fits within the byte budget, returning false
// when the budget is exceeded (the row is dropped and truncated is set so
// results always end on a row boundary).
func (a *rowAccumulator) add(row []interface{}) bool {
	if a.limits.MaxBytes > 0 && a.bytes+estimateRowSize(row) > a.limits.MaxBytes {
		a.truncated = true
		return false
	}
	a.rows = append(a.rows, row)
	a.bytes += estimateRowSize(row)
	a.rowsIncluded = len(a.rows)
	return true
}

// result builds the final ResultSet, exposing truncation metadata only when the
// byte budget actually bit. Non-truncated results keep the pre-cap payload
// shape so existing stored outputs stay byte-compatible.
func (a *rowAccumulator) result(columns []Column) *ResultSet {
	rs := &ResultSet{Columns: columns, Rows: a.rows}
	if a.truncated {
		rs.Truncated = true
		rs.RowsIncluded = a.rowsIncluded
		rs.RowsTotal = a.rowsTotal
		rs.Bytes = a.bytes
	}
	return rs
}
