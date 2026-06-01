package executor

import "testing"

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