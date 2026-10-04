package executor

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	dbsql "github.com/databricks/databricks-sql-go"
)

const (
	// databricksConnectTimeout bounds connector creation and the initial ping.
	// Databricks SQL warehouses auto-stop and a cold start can take tens of
	// seconds, so this is intentionally longer than the Postgres/ClickHouse
	// 10s budgets (which have no comparable cold start).
	databricksConnectTimeout = 60 * time.Second
	defaultDatabricksPort    = 443
)

// databricksCommandPrefixes are statements that return no result set and run
// through ExecContext. Mirrors the ClickHouse executor's classification.
var databricksCommandPrefixes = []string{
	"USE ", "SET ", "CREATE ", "DROP ", "ALTER ", "INSERT ", "UPDATE ",
	"DELETE ", "TRUNCATE ", "MERGE ", "GRANT ", "REVOKE ", "OPTIMIZE ",
	"VACUUM ", "REFRESH ", "MSCK ", "COPY ", "CACHE ", "UNCACHE ",
	"COMMENT ", "ANALYZE ",
}

// databricksCatalogNameRE guards catalog-name interpolation into SQL
// identifiers (catalog names cannot be bound as query parameters).
var databricksCatalogNameRE = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

var _ Executor = (*DatabricksExecutor)(nil)

// DatabricksExecutor executes SQL against a Databricks SQL warehouse or cluster.
type DatabricksExecutor struct {
	db        *sql.DB
	closeOnce sync.Once
	closeErr  error
}

// normalizeDatabricksHost strips an optional scheme and trailing slash so users
// can paste the workspace URL exactly as shown in the browser.
func normalizeDatabricksHost(host string) string {
	h := strings.TrimSpace(host)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	return strings.TrimRight(h, "/")
}

// validateDatabricksConfig checks conditional auth requirements and applies
// defaults. It returns a normalized copy of the config.
func validateDatabricksConfig(cfg databricksConfig) (databricksConfig, error) {
	cfg.Host = normalizeDatabricksHost(cfg.Host)
	if cfg.Host == "" {
		return cfg, fmt.Errorf("databricks host is required")
	}
	cfg.HTTPPath = strings.TrimSpace(cfg.HTTPPath)
	if cfg.HTTPPath == "" {
		return cfg, fmt.Errorf("databricks http_path is required")
	}
	cfg.AuthType = strings.TrimSpace(cfg.AuthType)
	if cfg.AuthType == "" {
		cfg.AuthType = "pat"
	}
	switch cfg.AuthType {
	case "pat":
		if cfg.Token == "" {
			return cfg, fmt.Errorf("databricks token is required for pat auth_type")
		}
	case "oauth_m2m":
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			return cfg, fmt.Errorf("databricks client_id and client_secret are required for oauth_m2m auth_type")
		}
	default:
		return cfg, fmt.Errorf("unsupported databricks auth_type %q (want pat or oauth_m2m)", cfg.AuthType)
	}
	if cfg.Port == 0 {
		cfg.Port = defaultDatabricksPort
	}
	return cfg, nil
}

// NewDatabricksExecutor opens a pooled connection and verifies it with a
// bounded ping. OAuth M2M credentials are exchanged for short-lived tokens by
// the driver; Aether stores only the client ID/secret.
func NewDatabricksExecutor(cfg databricksConfig) (*DatabricksExecutor, error) {
	opts := []dbsql.ConnOption{
		dbsql.WithServerHostname(cfg.Host),
		dbsql.WithPort(cfg.Port),
		dbsql.WithHTTPPath(cfg.HTTPPath),
		// Pin the session timezone so DATE/TIMESTAMP values parse
		// deterministically regardless of the warehouse's default.
		dbsql.WithSessionParams(map[string]string{"timezone": "UTC"}),
	}
	if cfg.AuthType == "oauth_m2m" {
		opts = append(opts, dbsql.WithClientCredentials(cfg.ClientID, cfg.ClientSecret))
	} else {
		opts = append(opts, dbsql.WithAccessToken(cfg.Token))
	}
	if cfg.Catalog != "" || cfg.Schema != "" {
		opts = append(opts, dbsql.WithInitialNamespace(cfg.Catalog, cfg.Schema))
	}

	connector, err := dbsql.NewConnector(opts...)
	if err != nil {
		return nil, fmt.Errorf("databricks connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), databricksConnectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DatabricksExecutor{db: db}, nil
}

// databricksIsCommand reports whether the statement returns no result set.
// Classification happens before the tracing comment is prepended so every
// statement does not look like a read.
func databricksIsCommand(query string) bool {
	return hasPrefixAny(strings.TrimSpace(strings.ToUpper(query)), databricksCommandPrefixes)
}

// normalizeDatabricksValue converts driver-native values into JSON-friendly
// representations, matching the other executors.
func normalizeDatabricksValue(v interface{}) interface{} {
	if t, ok := v.(time.Time); ok {
		if t.IsZero() {
			return nil
		}
		return t.Format(time.RFC3339Nano)
	}
	return v
}

func (d *DatabricksExecutor) Execute(ctx context.Context, query string, params map[string]string, limits OutputLimits) (*ResultSet, error) {
	resolved := ResolveParams(query, params)
	isCommand := databricksIsCommand(resolved)

	// Tag queries with the Aether user email for tracing in Databricks query history.
	if userEmail, ok := ctx.Value(CtxUserEmail{}).(string); ok && userEmail != "" {
		resolved = fmt.Sprintf("/* aether_user:%s */ %s", userEmail, resolved)
	}

	if isCommand {
		if _, err := d.db.ExecContext(ctx, resolved); err != nil {
			return nil, fmt.Errorf("exec: %w", err)
		}
		return &ResultSet{Columns: []Column{}, Rows: [][]interface{}{}}, nil
	}

	rows, err := d.db.QueryContext(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("column types: %w", err)
	}
	columns := make([]Column, len(colNames))
	for i, name := range colNames {
		typ := "unknown"
		if i < len(colTypes) {
			if t := colTypes[i].DatabaseTypeName(); t != "" {
				typ = t
			}
		}
		columns[i] = Column{Name: name, Type: typ}
	}

	acc := newRowAccumulator(limits)
	for rows.Next() && !acc.full() {
		values := make([]interface{}, len(colNames))
		ptrs := make([]interface{}, len(colNames))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		for i, v := range values {
			values[i] = normalizeDatabricksValue(v)
		}
		if !acc.add(values) {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if acc.rows == nil {
		acc.rows = [][]interface{}{}
	}
	return acc.result(columns), nil
}

func (d *DatabricksExecutor) TestConnection(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// validDatabricksCatalogName reports whether a catalog name is safe to
// interpolate as a SQL identifier.
func validDatabricksCatalogName(name string) bool {
	return databricksCatalogNameRE.MatchString(name)
}

// schemaForCatalog reads information_schema for one catalog and returns tables
// with three-level names flattened to "catalog.schema".
func (d *DatabricksExecutor) schemaForCatalog(ctx context.Context, catalog string) ([]TableInfo, error) {
	query := fmt.Sprintf(
		"SELECT c.table_schema, c.table_name, c.column_name, c.data_type, c.comment, t.comment "+
			"FROM `%s`.information_schema.columns c "+
			"LEFT JOIN `%s`.information_schema.tables t "+
			"ON t.table_catalog = c.table_catalog AND t.table_schema = c.table_schema AND t.table_name = c.table_name "+
			"WHERE c.table_schema <> 'information_schema' "+
			"ORDER BY c.table_schema, c.table_name, c.ordinal_position",
		catalog, catalog)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query information_schema: %w", err)
	}
	defer rows.Close()

	tableMap := map[string]*TableInfo{}
	var order []string
	for rows.Next() {
		var schema, table, column, dtype string
		var colComment, tableComment sql.NullString
		if err := rows.Scan(&schema, &table, &column, &dtype, &colComment, &tableComment); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		key := schema + "." + table
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &TableInfo{
				Schema: catalog + "." + schema, Name: table, Description: tableComment.String,
			}
			order = append(order, key)
		}
		tableMap[key].Columns = append(tableMap[key].Columns, ColumnInfo{
			Name: column, Type: dtype, Description: colComment.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema rows: %w", err)
	}
	tables := make([]TableInfo, 0, len(order))
	for _, key := range order {
		tables = append(tables, *tableMap[key])
	}
	return tables, nil
}

func (d *DatabricksExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	catalogs, err := d.Databases(ctx)
	if err != nil {
		return nil, err
	}
	tables := []TableInfo{}
	succeeded := 0
	var firstErr error
	for _, catalog := range catalogs {
		if !validDatabricksCatalogName(catalog) {
			slog.Warn("skipping catalog with unsupported identifier characters", "catalog", catalog)
			continue
		}
		catTables, err := d.schemaForCatalog(ctx, catalog)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("catalog %s: %w", catalog, err)
			}
			continue
		}
		tables = append(tables, catTables...)
		succeeded++
	}
	if succeeded == 0 && firstErr != nil {
		return nil, firstErr
	}
	return &SchemaInfo{Tables: tables}, nil
}

// scanFirstColumnStrings reads every row of a SHOW-style statement and returns
// the first column as strings, regardless of the statement's column layout.
func scanFirstColumnStrings(rows *sql.Rows) ([]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if len(values) == 0 {
			continue
		}
		switch v := values[0].(type) {
		case nil:
			continue
		case string:
			out = append(out, v)
		case []byte:
			out = append(out, string(v))
		default:
			out = append(out, fmt.Sprint(v))
		}
	}
	return out, rows.Err()
}

// Databases returns the accessible Unity Catalog catalogs (the UI's database
// picker); the hidden system catalog is excluded.
func (d *DatabricksExecutor) Databases(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW CATALOGS")
	if err != nil {
		return nil, fmt.Errorf("list catalogs: %w", err)
	}
	defer rows.Close()
	names, err := scanFirstColumnStrings(rows)
	if err != nil {
		return nil, err
	}
	var dbs []string
	for _, name := range names {
		if name == "" || name == "system" {
			continue
		}
		dbs = append(dbs, name)
	}
	sort.Strings(dbs)
	return dbs, nil
}

func (d *DatabricksExecutor) Close() error {
	d.closeOnce.Do(func() {
		d.closeErr = d.db.Close()
	})
	return d.closeErr
}
