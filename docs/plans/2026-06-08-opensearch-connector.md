# OpenSearch Connector Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an OpenSearch connector to HNB with a Connector Driver Registry that eliminates hardcoded type switches and enables future plugin-style extensibility.

**Architecture:** Introduce a `ConnectorDriver` interface + global registry. Each connector type (Postgres, ClickHouse, OpenSearch) implements the driver interface with its own config struct, executor creation, and connection testing. The API layer delegates to the registry instead of using switch statements. OpenSearch communicates via HTTP REST to the `/_plugins/_sql` endpoint.

**Tech Stack:** Go, `net/http` (OpenSearch REST client), OpenSearch SQL plugin, Docker Compose, React (frontend form)

---

## Task 1: Define the ConnectorDriver Interface and Registry

**Files:**
- Create: `internal/executor/driver.go`
- Create: `internal/executor/driver_test.go`

**Step 1: Write the failing test**

```go
// internal/executor/driver_test.go
package executor

import (
	"encoding/json"
	"testing"

	"github.com/heavenlabs/hnb/internal/models"
)

func TestRegisterAndLookupDriver(t *testing.T) {
	// Clear registry for test isolation
	origDrivers := drivers
	drivers = map[models.ConnectorType]ConnectorDriver{}
	defer func() { drivers = origDrivers }()

	mock := &mockDriver{typ: models.ConnectorType("mock")}
	RegisterDriver(mock)

	got, ok := GetDriver(models.ConnectorType("mock"))
	if !ok {
		t.Fatal("expected driver to be registered")
	}
	if got.Type() != models.ConnectorType("mock") {
		t.Fatalf("expected type 'mock', got %q", got.Type())
	}
}

func TestGetDriver_NotFound(t *testing.T) {
	origDrivers := drivers
	drivers = map[models.ConnectorType]ConnectorDriver{}
	defer func() { drivers = origDrivers }()

	_, ok := GetDriver(models.ConnectorType("nonexistent"))
	if ok {
		t.Fatal("expected driver not to be found")
	}
}

// mockDriver is a minimal implementation for testing the registry
type mockDriver struct {
	typ models.ConnectorType
}

func (m *mockDriver) Type() models.ConnectorType                        { return m.typ }
func (m *mockDriver) ConfigSchema() ConfigSchema                        { return ConfigSchema{} }
func (m *mockDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) { return nil, nil }
func (m *mockDriver) TestConfig(ctx interface{}, rawConfig json.RawMessage) error { return nil }
```

**Step 2: Run test to verify it fails**

Run: `cd /home/jesus/Projects/hnb-claude/.worktrees/opensearch-connector && go test ./internal/executor/ -run TestRegisterAndLookupDriver -v`
Expected: FAIL — `drivers`, `RegisterDriver`, `GetDriver`, `ConnectorDriver`, `ConfigSchema` undefined

**Step 3: Write the implementation**

```go
// internal/executor/driver.go
package executor

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/heavenlabs/hnb/internal/models"
)

// ConnectorDriver defines what each connector type must implement.
type ConnectorDriver interface {
	// Type returns the connector type identifier (e.g. "opensearch").
	Type() models.ConnectorType

	// ConfigSchema returns a descriptor of what config fields this
	// connector needs (for validation + future frontend form rendering).
	ConfigSchema() ConfigSchema

	// NewExecutor parses the raw config JSON and creates an executor.
	NewExecutor(rawConfig json.RawMessage) (Executor, error)

	// TestConfig validates and tests a connection from raw config.
	TestConfig(ctx context.Context, rawConfig json.RawMessage) error
}

// ConfigSchema describes the configuration fields for a connector type.
type ConfigSchema struct {
	Fields []ConfigField
}

// ConfigField describes a single configuration field.
type ConfigField struct {
	Name        string
	Type        string // "string", "int", "bool"
	Required    bool
	Default     interface{}
	Description string
}

var (
	drivers   = map[models.ConnectorType]ConnectorDriver{}
	driversMu sync.RWMutex
)

// RegisterDriver registers a connector driver for its type.
func RegisterDriver(d ConnectorDriver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	drivers[d.Type()] = d
}

// GetDriver returns the driver for the given connector type.
func GetDriver(typ models.ConnectorType) (ConnectorDriver, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	d, ok := drivers[typ]
	return d, ok
}
```

**Step 4: Fix the test mock to match the real interface**

The mock's `TestConfig` signature needs `context.Context`, not `interface{}`:

```go
func (m *mockDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	return nil
}
```

Add `"context"` to the test file imports.

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/executor/ -run "TestRegisterAndLookupDriver|TestGetDriver_NotFound" -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/executor/driver.go internal/executor/driver_test.go
git commit -m "feat(executor): add ConnectorDriver interface and registry"
```

---

## Task 2: Create PostgresDriver Wrapping Existing Executor

**Files:**
- Create: `internal/executor/postgres_driver.go`
- Create: `internal/executor/postgres_driver_test.go`

**Step 1: Write the failing test**

```go
// internal/executor/postgres_driver_test.go
package executor

import (
	"encoding/json"
	"testing"

	"github.com/heavenlabs/hnb/internal/models"
)

func TestPostgresDriver_Type(t *testing.T) {
	d := &PostgresDriver{}
	if d.Type() != models.ConnectorPostgres {
		t.Fatalf("expected %q, got %q", models.ConnectorPostgres, d.Type())
	}
}

func TestPostgresDriver_ConfigSchema(t *testing.T) {
	d := &PostgresDriver{}
	schema := d.ConfigSchema()
	if len(schema.Fields) == 0 {
		t.Fatal("expected config schema to have fields")
	}
	// Verify required fields exist
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	for _, required := range []string{"host", "port", "user", "password", "database"} {
		if !fieldNames[required] {
			t.Fatalf("expected field %q in config schema", required)
		}
	}
}

func TestPostgresDriver_NewExecutor_InvalidConfig(t *testing.T) {
	d := &PostgresDriver{}
	_, err := d.NewExecutor(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run "TestPostgresDriver" -v`
Expected: FAIL — `PostgresDriver` undefined

**Step 3: Write the implementation**

```go
// internal/executor/postgres_driver.go
package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
)

// postgresConfig is the typed config for the Postgres connector.
type postgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}

// PostgresDriver implements ConnectorDriver for PostgreSQL.
type PostgresDriver struct{}

func (d *PostgresDriver) Type() models.ConnectorType {
	return models.ConnectorPostgres
}

func (d *PostgresDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "Database host"},
			{Name: "port", Type: "int", Required: true, Default: 5432, Description: "Database port"},
			{Name: "user", Type: "string", Required: true, Description: "Database user"},
			{Name: "password", Type: "string", Required: true, Description: "Database password"},
			{Name: "database", Type: "string", Required: true, Description: "Database name"},
			{Name: "ssl_mode", Type: "string", Required: false, Default: "disable", Description: "SSL mode (disable, require, verify-full)"},
		},
	}
}

func (d *PostgresDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg postgresConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid postgres config: %w", err)
	}
	if cfg.Host == "" || cfg.Database == "" {
		return nil, fmt.Errorf("postgres config requires host and database")
	}
	// Convert to the existing ConnectorConfig for backward compatibility
	connCfg := models.ConnectorConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
		SSLMode:  cfg.SSLMode,
	}
	return NewPostgresExecutor(connCfg)
}

func (d *PostgresDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/executor/ -run "TestPostgresDriver" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/executor/postgres_driver.go internal/executor/postgres_driver_test.go
git commit -m "feat(executor): add PostgresDriver wrapping existing executor"
```

---

## Task 3: Create ClickHouseDriver Wrapping Existing Executor

**Files:**
- Create: `internal/executor/clickhouse_driver.go`
- Create: `internal/executor/clickhouse_driver_test.go`

**Step 1: Write the failing test**

```go
// internal/executor/clickhouse_driver_test.go
package executor

import (
	"encoding/json"
	"testing"

	"github.com/heavenlabs/hnb/internal/models"
)

func TestClickHouseDriver_Type(t *testing.T) {
	d := &ClickHouseDriver{}
	if d.Type() != models.ConnectorClickHouse {
		t.Fatalf("expected %q, got %q", models.ConnectorClickHouse, d.Type())
	}
}

func TestClickHouseDriver_ConfigSchema(t *testing.T) {
	d := &ClickHouseDriver{}
	schema := d.ConfigSchema()
	if len(schema.Fields) == 0 {
		t.Fatal("expected config schema to have fields")
	}
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	for _, required := range []string{"host", "port", "user", "password", "database"} {
		if !fieldNames[required] {
			t.Fatalf("expected field %q in config schema", required)
		}
	}
}

func TestClickHouseDriver_NewExecutor_InvalidConfig(t *testing.T) {
	d := &ClickHouseDriver{}
	_, err := d.NewExecutor(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run "TestClickHouseDriver" -v`
Expected: FAIL — `ClickHouseDriver` undefined

**Step 3: Write the implementation**

```go
// internal/executor/clickhouse_driver.go
package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
)

// clickhouseConfig is the typed config for the ClickHouse connector.
type clickhouseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// ClickHouseDriver implements ConnectorDriver for ClickHouse.
type ClickHouseDriver struct{}

func (d *ClickHouseDriver) Type() models.ConnectorType {
	return models.ConnectorClickHouse
}

func (d *ClickHouseDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "ClickHouse host"},
			{Name: "port", Type: "int", Required: true, Default: 9000, Description: "Native protocol port"},
			{Name: "user", Type: "string", Required: true, Description: "ClickHouse user"},
			{Name: "password", Type: "string", Required: true, Description: "ClickHouse password"},
			{Name: "database", Type: "string", Required: true, Description: "Database name"},
		},
	}
}

func (d *ClickHouseDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg clickhouseConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid clickhouse config: %w", err)
	}
	if cfg.Host == "" || cfg.Database == "" {
		return nil, fmt.Errorf("clickhouse config requires host and database")
	}
	connCfg := models.ConnectorConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
	}
	return NewClickHouseExecutor(connCfg)
}

func (d *ClickHouseDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/executor/ -run "TestClickHouseDriver" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/executor/clickhouse_driver.go internal/executor/clickhouse_driver_test.go
git commit -m "feat(executor): add ClickHouseDriver wrapping existing executor"
```

---

## Task 4: Implement OpenSearchExecutor

**Files:**
- Create: `internal/executor/opensearch.go`
- Create: `internal/executor/opensearch_test.go`

**Step 1: Write the failing test**

```go
// internal/executor/opensearch_test.go
package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenSearchExecutor_Execute(t *testing.T) {
	// Mock OpenSearch SQL endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_plugins/_sql" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"schema": []map[string]string{
				{"name": "product_name", "type": "text"},
				{"name": "price", "type": "float"},
			},
			"datarows": [][]interface{}{
				{"Widget", 9.99},
				{"Gadget", 24.99},
			},
			"total": 2,
			"size":  2,
			"status": 200,
		})
	}))
	defer server.Close()

	exec := &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	result, err := exec.Execute(context.Background(), "SELECT product_name, price FROM ecommerce", nil, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(result.Columns))
	}
	if result.Columns[0].Name != "product_name" {
		t.Fatalf("expected column 'product_name', got %q", result.Columns[0].Name)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
}

func TestOpenSearchExecutor_Execute_Truncation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"schema": []map[string]string{
				{"name": "id", "type": "integer"},
			},
			"datarows": [][]interface{}{{1}, {2}, {3}},
			"total":    100, // more results exist than returned
			"size":     3,
			"status":   200,
		})
	}))
	defer server.Close()

	exec := &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	result, err := exec.Execute(context.Background(), "SELECT id FROM logs", nil, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Note == "" {
		t.Fatal("expected truncation note, got empty string")
	}
}

func TestOpenSearchExecutor_Execute_MaxRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"schema":   []map[string]string{{"name": "id", "type": "integer"}},
			"datarows": [][]interface{}{{1}, {2}, {3}, {4}, {5}},
			"total":    5,
			"size":     5,
			"status":   200,
		})
	}))
	defer server.Close()

	exec := &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	result, err := exec.Execute(context.Background(), "SELECT id FROM logs", nil, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("expected 3 rows (maxRows), got %d", len(result.Rows))
	}
}

func TestOpenSearchExecutor_TestConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"name":"opensearch","version":{"number":"2.19.0"}}`))
	}))
	defer server.Close()

	exec := &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	err := exec.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenSearchExecutor_Schema(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if r.URL.Path == "/_plugins/_sql" {
			// Return index list for SHOW TABLES, then DESCRIBE results
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			query := body["query"]
			if query == "SHOW TABLES LIKE %" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"schema": []map[string]string{
						{"name": "TABLE_NAME", "type": "keyword"},
					},
					"datarows": [][]interface{}{{"ecommerce"}, {"logs"}},
					"total":    2,
					"size":     2,
					"status":   200,
				})
			} else {
				// DESCRIBE query
				json.NewEncoder(w).Encode(map[string]interface{}{
					"schema": []map[string]string{
						{"name": "COLUMN_NAME", "type": "keyword"},
						{"name": "DATA_TYPE", "type": "keyword"},
					},
					"datarows": [][]interface{}{{"id", "integer"}, {"name", "text"}},
					"total":    2,
					"size":     2,
					"status":   200,
				})
			}
		}
	}))
	defer server.Close()

	exec := &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	schema, err := exec.Schema(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(schema.Tables))
	}
}

func TestOpenSearchExecutor_Databases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"schema":   []map[string]string{{"name": "TABLE_NAME", "type": "keyword"}},
			"datarows": [][]interface{}{{"ecommerce"}, {"logs"}},
			"total":    2,
			"size":     2,
			"status":   200,
		})
	}))
	defer server.Close()

	exec := &OpenSearchExecutor{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	dbs, err := exec.Databases(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(dbs))
	}
}

func TestOpenSearchExecutor_Close(t *testing.T) {
	exec := &OpenSearchExecutor{}
	// Close should be a no-op, not panic
	if err := exec.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
		{"boolean", "boolean"},
		{"text", "text"},
		{"keyword", "text"},
		{"date", "timestamp"},
		{"ip", "text"},
		{"unknown_type", "text"},
	}
	for _, tt := range tests {
		got := mapOpenSearchType(tt.input)
		if got != tt.expected {
			t.Errorf("mapOpenSearchType(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run "TestOpenSearchExecutor|TestMapOpenSearchType" -v`
Expected: FAIL — `OpenSearchExecutor`, `mapOpenSearchType` undefined

**Step 3: Write the implementation**

```go
// internal/executor/opensearch.go
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/heavenlabs/hnb/internal/models"
)

// opensearchConfig is the typed config for the OpenSearch connector.
type opensearchConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	UseTLS   bool   `json:"use_tls"`
}

// OpenSearchExecutor queries OpenSearch via the SQL plugin REST API.
type OpenSearchExecutor struct {
	baseURL    string
	httpClient *http.Client
}

// NewOpenSearchExecutor creates an executor from the typed config.
func NewOpenSearchExecutor(cfg opensearchConfig) (*OpenSearchExecutor, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("opensearch config requires host")
	}
	port := cfg.Port
	if port == 0 {
		port = 9200
	}
	scheme := "http"
	if cfg.UseTLS {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, port)

	client := &http.Client{}
	exec := &OpenSearchExecutor{
		baseURL:    baseURL,
		httpClient: client,
	}

	// If auth is configured, wrap the client with basic auth
	if cfg.User != "" {
		exec.httpClient = &http.Client{
			Transport: &basicAuthTransport{
				user:     cfg.User,
				password: cfg.Password,
				base:     http.DefaultTransport,
			},
		}
	}

	return exec, nil
}

// basicAuthTransport adds Basic Auth to every request.
type basicAuthTransport struct {
	user     string
	password string
	base     http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.user, t.password)
	return t.base.RoundTrip(req)
}

// sqlRequest is the JSON body sent to the OpenSearch SQL plugin.
type sqlRequest struct {
	Query string `json:"query"`
}

// sqlResponse is the JDBC-format response from the OpenSearch SQL plugin.
type sqlResponse struct {
	Schema   []sqlColumn     `json:"schema"`
	DataRows [][]interface{} `json:"datarows"`
	Total    int             `json:"total"`
	Size     int             `json:"size"`
	Status   int             `json:"status"`
	Cursor   string          `json:"cursor,omitempty"`
}

type sqlColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (o *OpenSearchExecutor) Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error) {
	resolved := ResolveParams(query, params)

	reqBody := sqlRequest{Query: resolved}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/_plugins/_sql", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensearch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opensearch error (status %d): %s", resp.StatusCode, string(body))
	}

	var sqlResp sqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&sqlResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if sqlResp.Status != 0 && sqlResp.Status != 200 {
		return nil, fmt.Errorf("opensearch query error (status %d)", sqlResp.Status)
	}

	// Map schema to columns
	columns := make([]Column, len(sqlResp.Schema))
	for i, col := range sqlResp.Schema {
		columns[i] = Column{
			Name: col.Name,
			Type: mapOpenSearchType(col.Type),
		}
	}

	// Apply maxRows cap
	rows := sqlResp.DataRows
	truncated := false
	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
		truncated = true
	}

	// Check if there are more results than returned
	if !truncated && sqlResp.Total > len(rows) {
		truncated = true
	}

	result := &ResultSet{
		Columns: columns,
		Rows:    rows,
	}
	if result.Rows == nil {
		result.Rows = [][]interface{}{}
	}

	if truncated {
		result.Note = fmt.Sprintf("Results truncated: showing %d of %d total. OpenSearch has a 10k row default limit.", len(rows), sqlResp.Total)
	}

	return result, nil
}

func (o *OpenSearchExecutor) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("connection failed: status %d", resp.StatusCode)
	}
	return nil
}

func (o *OpenSearchExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	// Get list of indices
	indices, err := o.runSQL(ctx, "SHOW TABLES LIKE %")
	if err != nil {
		return nil, fmt.Errorf("list indices: %w", err)
	}

	var tables []TableInfo
	for _, row := range indices.Rows {
		if len(row) == 0 {
			continue
		}
		indexName := fmt.Sprintf("%v", row[0])
		// Skip system indices
		if strings.HasPrefix(indexName, ".") {
			continue
		}

		// Get columns for this index
		descResult, err := o.runSQL(ctx, fmt.Sprintf("DESCRIBE %s", indexName))
		if err != nil {
			continue // skip indices we can't describe
		}

		ti := TableInfo{
			Schema: "",
			Name:   indexName,
		}
		for _, colRow := range descResult.Rows {
			if len(colRow) < 2 {
				continue
			}
			colName := fmt.Sprintf("%v", colRow[0])
			colType := fmt.Sprintf("%v", colRow[1])
			ti.Columns = append(ti.Columns, ColumnInfo{
				Name: colName,
				Type: mapOpenSearchType(colType),
			})
		}
		tables = append(tables, ti)
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (o *OpenSearchExecutor) Databases(ctx context.Context) ([]string, error) {
	// OpenSearch has no "databases" — return index names
	result, err := o.runSQL(ctx, "SHOW TABLES LIKE %")
	if err != nil {
		return nil, fmt.Errorf("list indices: %w", err)
	}

	var names []string
	for _, row := range result.Rows {
		if len(row) == 0 {
			continue
		}
		name := fmt.Sprintf("%v", row[0])
		if !strings.HasPrefix(name, ".") {
			names = append(names, name)
		}
	}
	return names, nil
}

func (o *OpenSearchExecutor) Close() error {
	// No-op: HTTP client has no persistent connection to close
	return nil
}

// runSQL executes an SQL query and returns the result set.
func (o *OpenSearchExecutor) runSQL(ctx context.Context, query string) (*ResultSet, error) {
	return o.Execute(ctx, query, nil, 10000)
}

// mapOpenSearchType converts OpenSearch field types to HNB column types.
func mapOpenSearchType(osType string) string {
	switch strings.ToLower(osType) {
	case "long", "integer", "short", "byte":
		return "integer"
	case "float", "double", "half_float", "scaled_float":
		return "float"
	case "boolean":
		return "boolean"
	case "date", "date_nanos":
		return "timestamp"
	case "text", "keyword", "ip", "binary", "geo_point", "geo_shape":
		return "text"
	default:
		return "text"
	}
}
```

**Step 4: Add `Note` field to `ResultSet`**

In `internal/executor/executor.go`, add the `Note` field:

```go
type ResultSet struct {
	Columns []Column        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Note    string          `json:"note,omitempty"`
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/executor/ -run "TestOpenSearchExecutor|TestMapOpenSearchType" -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/executor/opensearch.go internal/executor/opensearch_test.go internal/executor/executor.go
git commit -m "feat(executor): add OpenSearchExecutor with SQL plugin REST API"
```

---

## Task 5: Implement OpenSearchDriver

**Files:**
- Create: `internal/executor/opensearch_driver.go`
- Create: `internal/executor/opensearch_driver_test.go`

**Step 1: Write the failing test**

```go
// internal/executor/opensearch_driver_test.go
package executor

import (
	"encoding/json"
	"testing"

	"github.com/heavenlabs/hnb/internal/models"
)

func TestOpenSearchDriver_Type(t *testing.T) {
	d := &OpenSearchDriver{}
	if d.Type() != models.ConnectorOpenSearch {
		t.Fatalf("expected %q, got %q", models.ConnectorOpenSearch, d.Type())
	}
}

func TestOpenSearchDriver_ConfigSchema(t *testing.T) {
	d := &OpenSearchDriver{}
	schema := d.ConfigSchema()
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	for _, expected := range []string{"host", "port", "user", "password", "use_tls"} {
		if !fieldNames[expected] {
			t.Fatalf("expected field %q in config schema", expected)
		}
	}
	// OpenSearch should NOT have database or ssl_mode
	for _, unexpected := range []string{"database", "ssl_mode"} {
		if fieldNames[unexpected] {
			t.Fatalf("unexpected field %q in OpenSearch config schema", unexpected)
		}
	}
}

func TestOpenSearchDriver_NewExecutor_ValidConfig(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"host":"localhost","port":9200}`)
	exec, err := d.NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec == nil {
		t.Fatal("expected executor, got nil")
	}
}

func TestOpenSearchDriver_NewExecutor_MissingHost(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"port":9200}`)
	_, err := d.NewExecutor(cfg)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestOpenSearchDriver_NewExecutor_DefaultPort(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"host":"localhost"}`)
	exec, err := d.NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	osExec, ok := exec.(*OpenSearchExecutor)
	if !ok {
		t.Fatal("expected *OpenSearchExecutor")
	}
	if osExec.baseURL != "http://localhost:9200" {
		t.Fatalf("expected default port 9200, got %q", osExec.baseURL)
	}
}

func TestOpenSearchDriver_NewExecutor_TLS(t *testing.T) {
	d := &OpenSearchDriver{}
	cfg := json.RawMessage(`{"host":"my-opensearch.example.com","use_tls":true}`)
	exec, err := d.NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	osExec, ok := exec.(*OpenSearchExecutor)
	if !ok {
		t.Fatal("expected *OpenSearchExecutor")
	}
	if osExec.baseURL != "https://my-opensearch.example.com:9200" {
		t.Fatalf("expected https URL, got %q", osExec.baseURL)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run "TestOpenSearchDriver" -v`
Expected: FAIL — `OpenSearchDriver` undefined, `models.ConnectorOpenSearch` undefined

**Step 3: Add `ConnectorOpenSearch` to models**

In `internal/models/connector.go`, add:

```go
const (
	ConnectorPostgres   ConnectorType = "postgres"
	ConnectorClickHouse ConnectorType = "clickhouse"
	ConnectorOpenSearch ConnectorType = "opensearch"
)
```

**Step 4: Write the driver implementation**

```go
// internal/executor/opensearch_driver.go
package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
)

// OpenSearchDriver implements ConnectorDriver for OpenSearch.
type OpenSearchDriver struct{}

func (d *OpenSearchDriver) Type() models.ConnectorType {
	return models.ConnectorOpenSearch
}

func (d *OpenSearchDriver) ConfigSchema() ConfigSchema {
	return ConfigSchema{
		Fields: []ConfigField{
			{Name: "host", Type: "string", Required: true, Description: "OpenSearch host"},
			{Name: "port", Type: "int", Required: false, Default: 9200, Description: "OpenSearch port"},
			{Name: "user", Type: "string", Required: false, Description: "Username (empty for unauthenticated)"},
			{Name: "password", Type: "string", Required: false, Description: "Password (empty for unauthenticated)"},
			{Name: "use_tls", Type: "bool", Required: false, Default: false, Description: "Use HTTPS"},
		},
	}
}

func (d *OpenSearchDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error) {
	var cfg opensearchConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("invalid opensearch config: %w", err)
	}
	return NewOpenSearchExecutor(cfg)
}

func (d *OpenSearchDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error {
	exec, err := d.NewExecutor(rawConfig)
	if err != nil {
		return err
	}
	defer exec.Close()
	return exec.TestConnection(ctx)
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/executor/ -run "TestOpenSearchDriver" -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/executor/opensearch_driver.go internal/executor/opensearch_driver_test.go internal/models/connector.go
git commit -m "feat(executor): add OpenSearchDriver and ConnectorOpenSearch model"
```

---

## Task 6: Register All Drivers and Refactor API Handlers

**Files:**
- Modify: `internal/api/connector_handlers.go`
- Modify: `internal/api/execute_handlers.go`
- Create: `internal/api/driver_init.go`

**Step 1: Create driver registration init file**

```go
// internal/api/driver_init.go
package api

import (
	"github.com/heavenlabs/hnb/internal/executor"
)

func init() {
	executor.RegisterDriver(&executor.PostgresDriver{})
	executor.RegisterDriver(&executor.ClickHouseDriver{})
	executor.RegisterDriver(&executor.OpenSearchDriver{})
}
```

**Step 2: Refactor `handleCreateConnector`**

Replace the hardcoded type check:

```go
// Before:
if req.Type != models.ConnectorPostgres && req.Type != models.ConnectorClickHouse {
    writeError(w, http.StatusBadRequest, "type must be 'postgres' or 'clickhouse'")
    return
}

// After:
if _, ok := executor.GetDriver(req.Type); !ok {
    writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported connector type: %s", req.Type))
    return
}
```

**Step 3: Refactor `buildExecutor`**

```go
// Before: switch statement
// After:
func (s *Server) buildExecutor(connType models.ConnectorType, configEnc []byte) (executor.Executor, error) {
	driver, ok := executor.GetDriver(connType)
	if !ok {
		return nil, fmt.Errorf("unsupported connector type: %s", connType)
	}
	return driver.NewExecutor(configEnc)
}
```

**Step 4: Refactor `handleTestConnectorConfig`**

```go
// Before: switch statement
// After:
driver, ok := executor.GetDriver(req.Type)
if !ok {
    writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unsupported connector type"})
    return
}
configJSON, err := json.Marshal(req.Config)
if err != nil {
    writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid config"})
    return
}
if err := driver.TestConfig(r.Context(), configJSON); err != nil {
    writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
    return
}
writeJSON(w, http.StatusOK, map[string]any{"ok": true})
```

**Step 5: Refactor `handleExecuteCell` executor creation**

In `internal/api/execute_handlers.go`, replace the switch:

```go
// Before:
switch connType {
case models.ConnectorPostgres:
    exec, err = executor.NewPostgresExecutor(cfg)
case models.ConnectorClickHouse:
    exec, err = executor.NewClickHouseExecutor(cfg)
default:
    writeError(w, http.StatusBadRequest, "unsupported connector type")
    return
}

// After:
driver, ok := executor.GetDriver(connType)
if !ok {
    writeError(w, http.StatusBadRequest, "unsupported connector type")
    return
}
configJSON, err := json.Marshal(cfg)
if err != nil {
    writeError(w, http.StatusInternalServerError, "failed to marshal config")
    return
}
exec, err = driver.NewExecutor(configJSON)
```

**Step 6: Run build and tests**

Run: `go build ./...`
Run: `go test ./internal/api/ -v -count=1`
Expected: Build passes, existing tests still pass

**Step 7: Commit**

```bash
git add internal/api/driver_init.go internal/api/connector_handlers.go internal/api/execute_handlers.go
git commit -m "refactor(api): use driver registry instead of hardcoded connector switches"
```

---

## Task 7: Database Migration — Drop CHECK Constraint

**Files:**
- Create: `internal/database/migrations/053_drop_connector_type_check.sql`

**Step 1: Write the migration**

```sql
-- Drop the CHECK constraint on connectors.type to allow dynamic connector types
-- managed by the driver registry instead of database-level enforcement.
ALTER TABLE connectors DROP CONSTRAINT IF EXISTS connectors_type_check;
```

**Step 2: Verify migration runs**

Start infra and run the server briefly to apply migrations:

```bash
cd /home/jesus/Projects/hnb-claude/.worktrees/opensearch-connector
task infra:up
# Start server briefly to apply migration
timeout 30 go run ./cmd/hnb-server 2>&1 | grep -i "migration\|migrate" || true
```

**Step 3: Commit**

```bash
git add internal/database/migrations/053_drop_connector_type_check.sql
git commit -m "migrate: drop CHECK constraint on connectors.type for pluggable types"
```

---

## Task 8: Docker Compose — Add OpenSearch Service

**Files:**
- Modify: `docker-compose.dev.yml`
- Create: `dev/opensearch-seed.sh`

**Step 1: Add OpenSearch service to docker-compose.dev.yml**

Add after the `hnb-clickhouse` service block:

```yaml
  hnb-opensearch:
    image: opensearchproject/opensearch:2.19.0
    environment:
      - discovery.type=single-node
      - DISABLE_SECURITY_PLUGIN=true
      - plugins.sql.enabled=true
      - OPENSEARCH_JAVA_OPTS=-Xms512m -Xmx512m
    ports:
      - "9200:9200"
    volumes:
      - osdata:/usr/share/opensearch/data
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:9200/_cluster/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 30
```

Add the seed loader service:

```yaml
  opensearch-loader:
    image: alpine:3.20
    depends_on:
      hnb-opensearch:
        condition: service_healthy
    volumes:
      - ./dev/opensearch-seed.sh:/seed.sh:ro
    entrypoint: ["sh", "/seed.sh"]
```

Add `osdata` to the volumes section:

```yaml
volumes:
  pgdata:
  chdata:
  osdata:
  go-build-cache:
  ...
```

**Step 2: Create seed script**

```bash
#!/bin/sh
# dev/opensearch-seed.sh
# Seeds OpenSearch with sample data for development and testing.

set -e

OS_HOST="http://hnb-opensearch:9200"

echo "Waiting for OpenSearch..."
until curl -sf "$OS_HOST/_cluster/health?wait_for_status=yellow&timeout=5s" > /dev/null 2>&1; do
  sleep 2
done
echo "OpenSearch is ready."

# Create ecommerce index
echo "Creating ecommerce index..."
curl -sf -X PUT "$OS_HOST/ecommerce" -H 'Content-Type: application/json' -d '{
  "mappings": {
    "properties": {
      "product_name": {"type": "text"},
      "category": {"type": "keyword"},
      "price": {"type": "float"},
      "quantity": {"type": "integer"},
      "order_date": {"type": "date"},
      "customer_name": {"type": "text"},
      "status": {"type": "keyword"}
    }
  }
}'

# Bulk index ecommerce data
echo "Indexing ecommerce documents..."
curl -sf -X POST "$OS_HOST/_bulk" -H 'Content-Type: application/x-ndjson' -d '
{"index":{"_index":"ecommerce"}}
{"product_name":"Laptop Pro 15","category":"Electronics","price":1299.99,"quantity":1,"order_date":"2024-01-15","customer_name":"Alice Johnson","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Wireless Mouse","category":"Electronics","price":29.99,"quantity":3,"order_date":"2024-01-16","customer_name":"Bob Smith","status":"delivered"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Standing Desk","category":"Furniture","price":549.00,"quantity":1,"order_date":"2024-01-17","customer_name":"Carol White","status":"processing"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Mechanical Keyboard","category":"Electronics","price":149.99,"quantity":2,"order_date":"2024-01-18","customer_name":"David Brown","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Monitor 27 inch","category":"Electronics","price":399.99,"quantity":1,"order_date":"2024-01-19","customer_name":"Eve Davis","status":"delivered"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Desk Chair","category":"Furniture","price":299.99,"quantity":1,"order_date":"2024-01-20","customer_name":"Frank Miller","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"USB-C Hub","category":"Electronics","price":49.99,"quantity":5,"order_date":"2024-01-21","customer_name":"Grace Wilson","status":"delivered"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Webcam HD","category":"Electronics","price":79.99,"quantity":2,"order_date":"2024-01-22","customer_name":"Henry Taylor","status":"processing"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Bookshelf","category":"Furniture","price":189.99,"quantity":1,"order_date":"2024-01-23","customer_name":"Ivy Anderson","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Noise Cancelling Headphones","category":"Electronics","price":249.99,"quantity":1,"order_date":"2024-01-24","customer_name":"Jack Thomas","status":"delivered"}
'

# Create logs index
echo "Creating logs index..."
curl -sf -X PUT "$OS_HOST/logs" -H 'Content-Type: application/json' -d '{
  "mappings": {
    "properties": {
      "timestamp": {"type": "date"},
      "level": {"type": "keyword"},
      "message": {"type": "text"},
      "service": {"type": "keyword"},
      "response_time_ms": {"type": "integer"}
    }
  }
}'

# Bulk index log data
echo "Indexing log documents..."
curl -sf -X POST "$OS_HOST/_bulk" -H 'Content-Type: application/x-ndjson' -d '
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:00:00Z","level":"INFO","message":"Request processed successfully","service":"api-gateway","response_time_ms":45}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:01:00Z","level":"ERROR","message":"Database connection timeout","service":"user-service","response_time_ms":5000}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:02:00Z","level":"WARN","message":"High memory usage detected","service":"payment-service","response_time_ms":120}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:03:00Z","level":"INFO","message":"User login successful","service":"auth-service","response_time_ms":89}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:04:00Z","level":"INFO","message":"Order created","service":"order-service","response_time_ms":200}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:05:00Z","level":"ERROR","message":"Failed to send notification","service":"notification-service","response_time_ms":3000}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:06:00Z","level":"INFO","message":"Cache refreshed","service":"api-gateway","response_time_ms":15}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:07:00Z","level":"DEBUG","message":"Query executed","service":"search-service","response_time_ms":30}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:08:00Z","level":"WARN","message":"Rate limit approaching","service":"api-gateway","response_time_ms":50}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:09:00Z","level":"INFO","message":"Batch job completed","service":"scheduler","response_time_ms":15000}
'

echo "Seed complete!"
echo "  ecommerce index: 10 documents"
echo "  logs index: 10 documents"
echo ""
echo "Try: SELECT * FROM ecommerce WHERE price > 100"
echo "Try: SELECT level, count(*) FROM logs GROUP BY level"
```

Make the script executable:

```bash
chmod +x dev/opensearch-seed.sh
```

**Step 3: Commit**

```bash
git add docker-compose.dev.yml dev/opensearch-seed.sh
git commit -m "feat(dev): add OpenSearch service and seed data to docker-compose"
```

---

## Task 9: Frontend — Add OpenSearch to Connectors Page

**Files:**
- Modify: `web/src/types/index.ts`
- Modify: `web/src/pages/ConnectorsPage.tsx`

**Step 1: Update types**

In `web/src/types/index.ts`, update the `Connector` config interface:

```typescript
// Before:
config?: {
  host?: string
  port?: number
  database?: string
  user?: string
  ssl_mode?: string
}

// After:
config?: {
  host?: string
  port?: number
  database?: string
  user?: string
  ssl_mode?: string
  use_tls?: boolean
}
```

**Step 2: Update ConnectorsPage.tsx**

Add `opensearch` to the `ConnectorType` union:

```typescript
type ConnectorType = 'postgres' | 'clickhouse' | 'opensearch'
```

Add `use_tls` to `ConnectorForm`:

```typescript
interface ConnectorForm {
  name: string
  type: ConnectorType
  host: string
  port: string
  database: string
  user: string
  password: string
  ssl_mode: string
  use_tls: boolean
  is_default: boolean
}
```

Update `defaultForm`:

```typescript
const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable',
  use_tls: false, is_default: false,
})
```

Update the type `<select>` onChange to set the correct default port:

```typescript
onChange={(e) => setForm((f) => ({
  ...f, type: e.target.value as ConnectorType,
  port: e.target.value === 'clickhouse' ? '9000' : e.target.value === 'opensearch' ? '9200' : '5432',
}))}
```

Add `<option value="opensearch">OpenSearch</option>` to the type dropdown.

Conditionally render form fields based on type:
- For `postgres` and `clickhouse`: show all existing fields (host, port, database, user, password, ssl_mode)
- For `opensearch`: show host, port, user, password, use_tls (checkbox) — hide database and ssl_mode

Add `use_tls` checkbox:

```tsx
{form.type === 'opensearch' && (
  <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
    <input type="checkbox" checked={form.use_tls}
      onChange={e => setForm(f => ({ ...f, use_tls: e.target.checked }))} />
    Use TLS (HTTPS)
  </label>
)}
```

Update the config payload in `createConnector` and `updateConnector` mutations to include `use_tls` for opensearch type.

**Step 3: Build and verify**

```bash
cd web && npm run build
```

**Step 4: Commit**

```bash
git add web/src/types/index.ts web/src/pages/ConnectorsPage.tsx
git commit -m "feat(web): add OpenSearch connector type to ConnectorsPage"
```

---

## Task 10: Integration Test — Verify End-to-End

**Files:**
- No new files (manual verification)

**Step 1: Start full dev stack**

```bash
cd /home/jesus/Projects/hnb-claude/.worktrees/opensearch-connector
task infra:up
# Wait for opensearch-loader to finish
docker compose -f docker-compose.dev.yml logs opensearch-loader -f
```

**Step 2: Verify OpenSearch is seeded**

```bash
curl -s http://localhost:9200/_cat/indices?v
```

Expected: `ecommerce` and `logs` indices with 10 docs each.

**Step 3: Verify SQL plugin works**

```bash
curl -s -X POST http://localhost:9200/_plugins/_sql \
  -H 'Content-Type: application/json' \
  -d '{"query": "SELECT product_name, price FROM ecommerce WHERE price > 100"}'
```

Expected: JDBC response with matching rows.

**Step 4: Start the Go server and test via API**

```bash
task dev
```

Then in another terminal, create an OpenSearch connector via the API and execute a query.

**Step 5: Run all Go tests**

```bash
go test ./... -count=1
```

Expected: All pass.

---

## Summary

| Task | Description | Est. Time |
|------|-------------|-----------|
| 1 | ConnectorDriver interface + registry | 10 min |
| 2 | PostgresDriver | 5 min |
| 3 | ClickHouseDriver | 5 min |
| 4 | OpenSearchExecutor | 20 min |
| 5 | OpenSearchDriver | 5 min |
| 6 | Refactor API handlers | 15 min |
| 7 | DB migration | 2 min |
| 8 | Docker Compose + seed | 10 min |
| 9 | Frontend form | 15 min |
| 10 | Integration test | 10 min |

**Total: ~1.5 hours**
