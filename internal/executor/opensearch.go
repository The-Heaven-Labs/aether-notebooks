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
	rs, err := e.Execute(ctx, "SHOW TABLES LIKE '%'", nil, 10000)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	var tables []TableInfo
	for _, row := range rs.Rows {
		if len(row) == 0 {
			continue
		}
		indexName, ok := row[0].(string)
		if !ok {
			continue
		}
		// Skip system indices
		if strings.HasPrefix(indexName, ".") {
			continue
		}

		// Describe each index - index name must be quoted
		descRS, err := e.Execute(ctx, fmt.Sprintf("DESCRIBE '%s'", indexName), nil, 10000)
		if err != nil {
			continue // skip indices that fail to describe
		}

		columns := make([]ColumnInfo, len(descRS.Rows))
		for i, dRow := range descRS.Rows {
			colName := ""
			colType := ""
			if len(dRow) > 0 {
				if s, ok := dRow[0].(string); ok {
					colName = s
				}
			}
			if len(dRow) > 1 {
				if s, ok := dRow[1].(string); ok {
					colType = s
				}
			}
			columns[i] = ColumnInfo{Name: colName, Type: colType}
		}

		tables = append(tables, TableInfo{
			Name:    indexName,
			Columns: columns,
		})
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (e *OpenSearchExecutor) Databases(ctx context.Context) ([]string, error) {
	rs, err := e.Execute(ctx, "SHOW TABLES LIKE '%'", nil, 10000)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	var dbs []string
	for _, row := range rs.Rows {
		if len(row) == 0 {
			continue
		}
		name, ok := row[0].(string)
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
