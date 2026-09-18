package executor

import "testing"

func TestEstimateValueSize(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want int64
	}{
		{"nil", nil, 4},
		{"bool", true, 4},
		{"short string", "abc", 3},
		{"long string", string(make([]byte, 1000)), 1000},
		{"bytes", []byte("hello"), 5},
		{"number", int64(42), 16},
		{"float", 1.5, 16},
		{"empty array", []interface{}{}, 0},
		{"array of strings", []interface{}{"a", "bb"}, 3},
		{"map", map[string]interface{}{"k": "value"}, 1 + 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateValueSize(tt.val); got != tt.want {
				t.Errorf("estimateValueSize(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestEstimateRowSize(t *testing.T) {
	row := []interface{}{"name", int64(1), nil, []interface{}{"nested", "array"}}
	got := estimateRowSize(row)
	want := int64(4 + 16 + 4 + (6 + 5))
	if got != want {
		t.Errorf("estimateRowSize = %d, want %d", got, want)
	}
}

func TestRowAccumulator_ByteCapRowBoundary(t *testing.T) {
	// Each row is 4 bytes ("a" is 1 byte × 4 columns of 1 char).
	acc := newRowAccumulator(OutputLimits{MaxBytes: 11})
	row := []interface{}{"a", "a", "a", "a"}
	for i := 0; i < 10; i++ {
		acc.add(row)
	}
	if len(acc.rows) != 2 {
		t.Fatalf("expected 2 rows within an 11-byte budget, got %d", len(acc.rows))
	}
	if !acc.truncated {
		t.Fatal("expected truncated=true after budget exhausted")
	}
	if acc.rowsIncluded != 2 {
		t.Fatalf("expected rowsIncluded=2, got %d", acc.rowsIncluded)
	}
	if acc.bytes != 8 {
		t.Fatalf("expected 8 bytes included, got %d", acc.bytes)
	}
}

func TestRowAccumulator_FirstRowExceedsCap(t *testing.T) {
	acc := newRowAccumulator(OutputLimits{MaxBytes: 4})
	acc.add([]interface{}{"a-very-long-value"}) // 19 bytes > 4
	if len(acc.rows) != 0 {
		t.Fatalf("expected zero rows, got %d", len(acc.rows))
	}
	if !acc.truncated {
		t.Fatal("expected truncated=true when first row exceeds cap")
	}
	if acc.rowsIncluded != 0 {
		t.Fatalf("expected rowsIncluded=0, got %d", acc.rowsIncluded)
	}
}

func TestRowAccumulator_Unlimited(t *testing.T) {
	acc := newRowAccumulator(OutputLimits{}) // MaxBytes 0 = unlimited
	for i := 0; i < 100; i++ {
		acc.add([]interface{}{"x"})
	}
	if len(acc.rows) != 100 || acc.truncated {
		t.Fatalf("unlimited accumulator should keep all rows (got %d, truncated=%v)", len(acc.rows), acc.truncated)
	}
}

func TestRowAccumulator_RowCap(t *testing.T) {
	acc := newRowAccumulator(OutputLimits{MaxRows: 3})
	for i := 0; i < 10; i++ {
		if acc.full() {
			break
		}
		acc.add([]interface{}{"x"})
	}
	if len(acc.rows) != 3 {
		t.Fatalf("expected 3 rows capped by MaxRows, got %d", len(acc.rows))
	}
	if !acc.full() {
		t.Fatal("expected accumulator to report full after row cap")
	}
}

func TestRowAccumulator_ZeroRowsUnlimited(t *testing.T) {
	// MaxRows <= 0 means unlimited (matches ApplyLimit and OpenSearch semantics).
	acc := newRowAccumulator(OutputLimits{MaxRows: 0})
	if acc.full() {
		t.Fatal("MaxRows=0 should not be considered full")
	}
}
func TestApplyLimit(t *testing.T) {
	tests := []struct {
		name  string
		query string
		limit int
		want  string
	}{
		{
			name:  "simple query without semicolon",
			query: "SELECT 1",
			limit: 1000,
			want:  "SELECT 1 LIMIT 1000",
		},
		{
			name:  "query with trailing semicolon",
			query: "SELECT 1;",
			limit: 1000,
			want:  "SELECT 1 LIMIT 1000",
		},
		{
			name:  "query with trailing semicolon and newline",
			query: "SELECT 1;\n",
			limit: 1000,
			want:  "SELECT 1 LIMIT 1000",
		},
		{
			name:  "query with trailing whitespace and semicolon",
			query: "SELECT 1;  \n\t",
			limit: 1000,
			want:  "SELECT 1 LIMIT 1000",
		},
		{
			name:  "query that already has LIMIT",
			query: "SELECT 1 LIMIT 10",
			limit: 1000,
			want:  "SELECT 1 LIMIT 10",
		},
		{
			name:  "query with lowercase limit",
			query: "select 1 limit 10",
			limit: 1000,
			want:  "select 1 limit 10",
		},
		{
			name:  "limit is zero no append",
			query: "SELECT 1",
			limit: 0,
			want:  "SELECT 1",
		},
		{
			name:  "negative limit no append",
			query: "SELECT 1",
			limit: -1,
			want:  "SELECT 1",
		},
		{
			name:  "multiline query with semicolon",
			query: "SELECT *\nFROM users\nWHERE active = true;",
			limit: 500,
			want:  "SELECT *\nFROM users\nWHERE active = true LIMIT 500",
		},
		{
			name:  "multiline query with trailing newline after semicolon",
			query: "SELECT country, SUM(revenue)\nFROM analytics.events\nGROUP BY country\nORDER BY total_revenue DESC;\n",
			limit: 5,
			want:  "SELECT country, SUM(revenue)\nFROM analytics.events\nGROUP BY country\nORDER BY total_revenue DESC LIMIT 5",
		},
		{
			name:  "SHOW TABLES not modified",
			query: "SHOW TABLES LIKE %",
			limit: 1000,
			want:  "SHOW TABLES LIKE %",
		},
		{
			name:  "SHOW TABLES lowercase not modified",
			query: "show tables like %",
			limit: 1000,
			want:  "show tables like %",
		},
		{
			name:  "DESCRIBE not modified",
			query: "DESCRIBE ecommerce",
			limit: 1000,
			want:  "DESCRIBE ecommerce",
		},
		{
			name:  "describe lowercase not modified",
			query: "describe logs",
			limit: 500,
			want:  "describe logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyLimit(tt.query, tt.limit)
			if got != tt.want {
				t.Errorf("ApplyLimit(%q, %d) = %q, want %q", tt.query, tt.limit, got, tt.want)
			}
		})
	}
}
