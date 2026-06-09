package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestOpenSearchExecutor(handler http.Handler) *OpenSearchExecutor {
	server := httptest.NewServer(handler)
	return &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}
}

func TestOpenSearchExecutor_Execute(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_plugins/_sql" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		resp := sqlResponse{
			Schema: []sqlColumn{
				{Name: "id", Type: "long"},
				{Name: "name", Type: "text"},
			},
			DataRows: [][]interface{}{
				{float64(1), "alice"},
				{float64(2), "bob"},
			},
			Total:  2,
			Size:   2,
			Status: 200,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	exec := newTestOpenSearchExecutor(handler)
	rs, err := exec.Execute(context.Background(), "SELECT * FROM users", nil, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rs.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(rs.Columns))
	}
	if rs.Columns[0].Name != "id" {
		t.Errorf("expected column name 'id', got %q", rs.Columns[0].Name)
	}
	if rs.Columns[0].Type != "integer" {
		t.Errorf("expected mapped type 'integer', got %q", rs.Columns[0].Type)
	}
	if rs.Columns[1].Name != "name" {
		t.Errorf("expected column name 'name', got %q", rs.Columns[1].Name)
	}
	if rs.Columns[1].Type != "text" {
		t.Errorf("expected mapped type 'text', got %q", rs.Columns[1].Type)
	}
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rs.Rows))
	}
	if rs.Note != "" {
		t.Errorf("expected no note, got %q", rs.Note)
	}
}

func TestOpenSearchExecutor_Execute_Truncation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := sqlResponse{
			Schema: []sqlColumn{
				{Name: "id", Type: "long"},
			},
			DataRows: [][]interface{}{
				{float64(1)},
				{float64(2)},
				{float64(3)},
			},
			Total:  100,
			Size:   3,
			Status: 200,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	exec := newTestOpenSearchExecutor(handler)
	rs, err := exec.Execute(context.Background(), "SELECT id FROM users", nil, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rs.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rs.Rows))
	}
	expected := "Showing 3 of 100 total results"
	if rs.Note != expected {
		t.Errorf("expected note %q, got %q", expected, rs.Note)
	}
}

func TestOpenSearchExecutor_Execute_MaxRows(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := sqlResponse{
			Schema: []sqlColumn{
				{Name: "id", Type: "long"},
			},
			DataRows: [][]interface{}{
				{float64(1)},
				{float64(2)},
				{float64(3)},
				{float64(4)},
				{float64(5)},
			},
			Total:  5,
			Size:   5,
			Status: 200,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	exec := newTestOpenSearchExecutor(handler)
	rs, err := exec.Execute(context.Background(), "SELECT id FROM users", nil, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rs.Rows) != 3 {
		t.Fatalf("expected 3 rows (capped by maxRows), got %d", len(rs.Rows))
	}
	// Total (5) > len(rows) after cap (3), so note should be set
	if rs.Note == "" {
		t.Error("expected truncation note when maxRows caps results")
	}
}

func TestOpenSearchExecutor_TestConnection(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"name":"opensearch","version":{"number":"2.11.0"}}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	exec := newTestOpenSearchExecutor(handler)
	err := exec.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenSearchExecutor_Schema(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req sqlRequest
		json.NewDecoder(r.Body).Decode(&req)

		var resp sqlResponse
		if req.Query == "SHOW TABLES LIKE '%'" {
			// SHOW TABLES returns: TABLE_CAT, TABLE_SCHEM, TABLE_NAME, TABLE_TYPE, ...
			resp = sqlResponse{
				Schema: []sqlColumn{
					{Name: "TABLE_CAT", Type: "keyword"},
					{Name: "TABLE_SCHEM", Type: "keyword"},
					{Name: "TABLE_NAME", Type: "keyword"},
					{Name: "TABLE_TYPE", Type: "keyword"},
				},
				DataRows: [][]interface{}{
					{"docker-cluster", nil, "logs", "BASE TABLE"},
					{"docker-cluster", nil, "metrics", "BASE TABLE"},
					{"docker-cluster", nil, ".kibana", "BASE TABLE"},
				},
				Total:  3,
				Size:   3,
				Status: 200,
			}
		} else if req.Query == "DESCRIBE 'logs'" {
			resp = sqlResponse{
				Schema: []sqlColumn{
					{Name: "col_name", Type: "keyword"},
					{Name: "col_type", Type: "keyword"},
				},
				DataRows: [][]interface{}{
					{"timestamp", "date"},
					{"message", "text"},
				},
				Total:  2,
				Size:   2,
				Status: 200,
			}
		} else if req.Query == "DESCRIBE 'metrics'" {
			resp = sqlResponse{
				Schema: []sqlColumn{
					{Name: "col_name", Type: "keyword"},
					{Name: "col_type", Type: "keyword"},
				},
				DataRows: [][]interface{}{
					{"cpu", "float"},
					{"memory", "long"},
				},
				Total:  2,
				Size:   2,
				Status: 200,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	exec := newTestOpenSearchExecutor(handler)
	schema, err := exec.Schema(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// .kibana should be filtered out
	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(schema.Tables))
	}
	if schema.Tables[0].Name != "logs" {
		t.Errorf("expected first table 'logs', got %q", schema.Tables[0].Name)
	}
	if len(schema.Tables[0].Columns) != 2 {
		t.Errorf("expected 2 columns for logs, got %d", len(schema.Tables[0].Columns))
	}
	if schema.Tables[1].Name != "metrics" {
		t.Errorf("expected second table 'metrics', got %q", schema.Tables[1].Name)
	}
}

func TestOpenSearchExecutor_Databases(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SHOW TABLES returns: TABLE_CAT, TABLE_SCHEM, TABLE_NAME, TABLE_TYPE, ...
		resp := sqlResponse{
			Schema: []sqlColumn{
				{Name: "TABLE_CAT", Type: "keyword"},
				{Name: "TABLE_SCHEM", Type: "keyword"},
				{Name: "TABLE_NAME", Type: "keyword"},
				{Name: "TABLE_TYPE", Type: "keyword"},
			},
			DataRows: [][]interface{}{
				{"docker-cluster", nil, "logs", "BASE TABLE"},
				{"docker-cluster", nil, "metrics", "BASE TABLE"},
				{"docker-cluster", nil, ".opendistro", "BASE TABLE"},
			},
			Total:  3,
			Size:   3,
			Status: 200,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	exec := newTestOpenSearchExecutor(handler)
	dbs, err := exec.Databases(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// .opendistro should be filtered out
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(dbs))
	}
	if dbs[0] != "logs" {
		t.Errorf("expected first db 'logs', got %q", dbs[0])
	}
	if dbs[1] != "metrics" {
		t.Errorf("expected second db 'metrics', got %q", dbs[1])
	}
}

func TestOpenSearchExecutor_Close(t *testing.T) {
	exec := &OpenSearchExecutor{}
	if err := exec.Close(); err != nil {
		t.Fatalf("Close should be no-op, got error: %v", err)
	}
}

func TestMapOpenSearchType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"long", "integer"},
		{"integer", "integer"},
		{"short", "integer"},
		{"byte", "integer"},
		{"float", "float"},
		{"double", "float"},
		{"half_float", "float"},
		{"scaled_float", "float"},
		{"boolean", "boolean"},
		{"date", "timestamp"},
		{"date_nanos", "timestamp"},
		{"text", "text"},
		{"keyword", "text"},
		{"ip", "text"},
		{"binary", "text"},
		{"geo_point", "text"},
		{"geo_shape", "text"},
		{"unknown_type", "text"},
		{"", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapOpenSearchType(tt.input)
			if result != tt.expected {
				t.Errorf("mapOpenSearchType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
