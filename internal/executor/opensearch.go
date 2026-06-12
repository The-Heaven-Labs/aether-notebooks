package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type opensearchConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	UseTLS   bool   `json:"use_tls"`
}

type OpenSearchExecutor struct {
	baseURL    string
	httpClient *http.Client
}

type sqlRequest struct {
	Query string `json:"query"`
}

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

// basicAuthTransport is an http.RoundTripper that adds basic auth credentials.
type basicAuthTransport struct {
	base     http.RoundTripper
	username string
	password string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.SetBasicAuth(t.username, t.password)
	return t.base.RoundTrip(req)
}

func NewOpenSearchExecutor(cfg opensearchConfig) *OpenSearchExecutor {
	scheme := "http"
	if cfg.UseTLS {
		scheme = "https"
	}
	port := cfg.Port
	if port == 0 {
		port = 9200
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, port)

	var transport http.RoundTripper = http.DefaultTransport
	if cfg.User != "" {
		transport = &basicAuthTransport{
			base:     http.DefaultTransport,
			username: cfg.User,
			password: cfg.Password,
		}
	}

	return &OpenSearchExecutor{
		baseURL:    baseURL,
		httpClient: &http.Client{Transport: transport},
	}
}

func (e *OpenSearchExecutor) Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error) {
	return e.execute(ctx, query, params, maxRows, true)
}

// executeInternal runs a query without column filtering (for Schema/Databases).
func (e *OpenSearchExecutor) executeInternal(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error) {
	return e.execute(ctx, query, params, maxRows, false)
}

func (e *OpenSearchExecutor) execute(ctx context.Context, query string, params map[string]string, maxRows int, applyFilter bool) (*ResultSet, error) {
	resolved := ResolveParams(query, params)

	body, err := json.Marshal(sqlRequest{Query: resolved})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/_plugins/_sql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensearch returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var sqlResp sqlResponse
	if err := json.Unmarshal(respBody, &sqlResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	columns := make([]Column, len(sqlResp.Schema))
	for i, col := range sqlResp.Schema {
		columns[i] = Column{Name: col.Name, Type: mapOpenSearchType(col.Type)}
	}

	rows := sqlResp.DataRows
	if rows == nil {
		rows = [][]interface{}{}
	}

	// Filter SHOW TABLES results to only meaningful columns (for user queries)
	if applyFilter && isShowTables(query) {
		columns, rows = filterShowTables(columns, rows)
	}

	note := ""
	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	if sqlResp.Total > len(rows) {
		note = fmt.Sprintf("Showing %d of %d total results", len(rows), sqlResp.Total)
	}

	return &ResultSet{
		Columns: columns,
		Rows:    rows,
		Note:    note,
	}, nil
}

func (e *OpenSearchExecutor) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opensearch returned status %d", resp.StatusCode)
	}
	return nil
}

func (e *OpenSearchExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	// Get all indices - pattern must be quoted for OpenSearch SQL plugin
	rs, err := e.executeInternal(ctx, "SHOW TABLES LIKE '%'", nil, 10000)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	var tables []TableInfo
	for _, row := range rs.Rows {
		// SHOW TABLES returns: TABLE_CAT, TABLE_SCHEM, TABLE_NAME, TABLE_TYPE, ...
		// We need TABLE_NAME which is at index 2
		if len(row) < 3 {
			continue
		}
		indexName, ok := row[2].(string)
		if !ok {
			continue
		}
		// Skip system indices
		if strings.HasPrefix(indexName, ".") {
			continue
		}

		// Describe each index - use DESCRIBE TABLES LIKE syntax (required by OpenSearch 2.x SQL plugin)
		descRS, err := e.Execute(ctx, fmt.Sprintf("DESCRIBE TABLES LIKE '%s'", indexName), nil, 10000)
		if err != nil {
			continue // skip indices that fail to describe
		}

		// DESCRIBE returns JDBC-style metadata columns. Use the response schema
		// to find the correct indices for COLUMN_NAME and TYPE_NAME rather than
		// hardcoding positions (which vary across OpenSearch versions).
		colNameIdx := -1
		colTypeIdx := -1
		for i, col := range descRS.Columns {
			switch strings.ToUpper(col.Name) {
			case "COLUMN_NAME":
				colNameIdx = i
			case "TYPE_NAME":
				colTypeIdx = i
			}
		}
		// Fallback to common positions if schema lookup fails
		if colNameIdx == -1 {
			colNameIdx = 3 // COLUMN_NAME is typically at index 3
		}
		if colTypeIdx == -1 {
			colTypeIdx = 5 // TYPE_NAME is typically at index 5
		}

		var columns []ColumnInfo
		for _, dRow := range descRS.Rows {
			colName := ""
			colType := ""
			if colNameIdx < len(dRow) {
				if s, ok := dRow[colNameIdx].(string); ok {
					colName = s
				}
			}
			if colTypeIdx < len(dRow) {
				if s, ok := dRow[colTypeIdx].(string); ok {
					colType = s
				}
			}
			if colName == "" {
				continue // skip rows without a column name
			}
			columns = append(columns, ColumnInfo{Name: colName, Type: colType})
		}

		tables = append(tables, TableInfo{
			Name:    indexName,
			Columns: columns,
		})
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (e *OpenSearchExecutor) Databases(ctx context.Context) ([]string, error) {
	rs, err := e.executeInternal(ctx, "SHOW TABLES LIKE '%'", nil, 10000)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	var dbs []string
	for _, row := range rs.Rows {
		// SHOW TABLES returns: TABLE_CAT, TABLE_SCHEM, TABLE_NAME, TABLE_TYPE, ...
		// We need TABLE_NAME which is at index 2
		if len(row) < 3 {
			continue
		}
		name, ok := row[2].(string)
		if !ok {
			continue
		}
		// Skip system indices
		if strings.HasPrefix(name, ".") {
			continue
		}
		dbs = append(dbs, name)
	}
	return dbs, nil
}

func (e *OpenSearchExecutor) Close() error {
	return nil
}

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

// isShowTables returns true if the query is a SHOW TABLES statement.
func isShowTables(query string) bool {
	q := strings.TrimSpace(strings.ToUpper(query))
	return strings.HasPrefix(q, "SHOW TABLES")
}

// showTablesColumns are the meaningful columns to keep from SHOW TABLES results.
var showTablesColumns = []string{"TABLE_CAT", "TABLE_NAME", "TABLE_TYPE"}

// filterShowTables reduces the SHOW TABLES result to only meaningful columns.
func filterShowTables(columns []Column, rows [][]interface{}) ([]Column, [][]interface{}) {
	// Find indices of columns we want to keep
	keepIdx := make([]int, 0, len(showTablesColumns))
	keepCols := make([]Column, 0, len(showTablesColumns))
	for _, wanted := range showTablesColumns {
		for i, col := range columns {
			if col.Name == wanted {
				keepIdx = append(keepIdx, i)
				keepCols = append(keepCols, col)
				break
			}
		}
	}
	if len(keepIdx) == len(columns) {
		return columns, rows // nothing to filter
	}

	filtered := make([][]interface{}, len(rows))
	for r, row := range rows {
		filteredRow := make([]interface{}, len(keepIdx))
		for i, idx := range keepIdx {
			if idx < len(row) {
				filteredRow[i] = row[idx]
			}
		}
		filtered[r] = filteredRow
	}
	return keepCols, filtered
}
