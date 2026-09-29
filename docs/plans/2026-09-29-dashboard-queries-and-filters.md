# Dashboard Queries and Filters Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Grafana-style live dashboards to Aether: widgets own SQL + connector, dashboard-level variables/filters re-run them, and existing cell-linked widgets keep working.

**Architecture:** Approach 1 from the design (`docs/plans/2026-09-29-dashboard-queries-and-filters-design.md`): extend `widgets` with `connector_id/query/language`, store variables in `dashboards.settings.variables` JSONB, extract a shared `openQuery` execution helper from `handleExecuteCell`, and add per-widget execute + variable-options endpoints with type-aware server-side interpolation and a short-TTL Redis cache.

**Tech Stack:** Go (`net/http` ServeMux, pgx, Redis), React 19 + TypeScript + React Query + CodeMirror 6 + ECharts + react-grid-layout, Playwright, Vitest.

**Preconditions:**
- Work on branch `feat/dashboard-queries-and-filters` (already created).
- Start infra before Go tests: `task infra:up`
- Start the full dev stack before E2E/browser validation: `docker compose -f docker-compose.dev.yml up -d`
- All Go test commands include `-timeout 3m` (repo rule).

---

## Phase 1 — Data model & variable engine

### Task 1: Migration V121, models, migration test

**Files:**
- Create: `internal/database/migrations/V121__dashboard_query_widgets.sql`
- Modify: `internal/models/dashboard.go`
- Modify: `internal/database/database_test.go` (append test)
- Modify: `internal/api/dashboard_handlers.go` (only if compile breaks from model changes)

**Step 1: Write the migration**

```sql
-- V121: dashboard query widgets + dashboard variables.

-- Query-backed widgets: a widget may reference a connector + SQL instead of
-- a notebook cell. Cell-linked widgets keep notebook_id/cell_id.
ALTER TABLE widgets
    ADD COLUMN IF NOT EXISTS connector_id UUID NULL REFERENCES connectors(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS query TEXT NULL,
    ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'sql';

ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_source_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_source_check CHECK (
    connector_id IS NULL OR (notebook_id IS NULL AND cell_id IS NULL AND query IS NOT NULL)
);

-- Convert legacy input widgets into dashboard variables. Markers let the
-- migration test execute exactly this block.
-- +conversion:start
WITH converted AS (
    SELECT DISTINCT ON (w.dashboard_id, COALESCE(NULLIF(w.config->>'paramName', ''), w.id))
        w.dashboard_id,
        COALESCE(NULLIF(w.config->>'paramName', ''), w.id) AS var_name,
        COALESCE(NULLIF(w.config->>'label', ''), NULLIF(w.config->>'paramName', ''), w.id) AS var_label,
        CASE w.type
            WHEN 'date_picker' THEN 'date'
            WHEN 'date_range' THEN 'date_range'
            WHEN 'number' THEN 'number'
            WHEN 'multi_select' THEN 'multi_select'
            ELSE 'text'
        END AS var_type,
        CASE
            WHEN w.type = 'multi_select' AND jsonb_typeof(w.config->'options') = 'array' THEN
                jsonb_build_object(
                    'mode', 'static',
                    'values', (
                        SELECT COALESCE(
                            jsonb_agg(jsonb_build_object('label', v, 'value', v)) FILTER (WHERE v <> ''),
                            '[]'::jsonb)
                        FROM jsonb_array_elements_text(w.config->'options') AS v
                    )
                )
        END AS options_json
    FROM widgets w
    WHERE w.type IN ('date_picker', 'date_range', 'freetext', 'number', 'multi_select')
    ORDER BY w.dashboard_id, COALESCE(NULLIF(w.config->>'paramName', ''), w.id), w.created_at
),
aggregated AS (
    SELECT dashboard_id,
           jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
               'name', var_name,
               'label', var_label,
               'type', var_type,
               'options', options_json
           ))) AS variables
    FROM converted
    GROUP BY dashboard_id
)
UPDATE dashboards d
SET settings = jsonb_set(COALESCE(d.settings, '{}'::jsonb), '{variables}', a.variables, true)
FROM aggregated a
WHERE d.id = a.dashboard_id;

DELETE FROM widgets
WHERE type IN ('date_picker', 'date_range', 'freetext', 'number', 'multi_select');
-- +conversion:end

-- Input widget types are no longer valid; they are variables now.
ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('chart', 'table', 'text', 'metric'));

-- parameter_overrides was never implemented; variables replace it.
UPDATE dashboards SET settings = settings - 'parameter_overrides'
WHERE settings ? 'parameter_overrides';
```

**Step 2: Update the models**

Replace `DashboardSettings` and add variable types in `internal/models/dashboard.go`:

```go
type DashboardSettings struct {
	AutoRefreshSeconds int                 `json:"auto_refresh_seconds,omitempty"`
	GridCols           int                 `json:"grid_cols,omitempty"`
	QueryCacheSeconds  *int                `json:"query_cache_seconds,omitempty"`
	PublicLive         bool                `json:"public_live,omitempty"`
	Variables          []DashboardVariable `json:"variables,omitempty"`
}

// DashboardVariable is a dashboard-level filter. Values are interpolated
// server-side into query widgets as {{name}} tokens.
type DashboardVariable struct {
	Name      string           `json:"name"`
	Label     string           `json:"label"`
	Type      string           `json:"type"` // text|number|boolean|date|date_range|single_select|multi_select
	Default   interface{}      `json:"default,omitempty"`
	Required  bool             `json:"required,omitempty"`
	Options   *VariableOptions `json:"options,omitempty"`
	DependsOn []string         `json:"depends_on,omitempty"`
}

type VariableOptions struct {
	Mode        string        `json:"mode"` // "static" | "query"
	Values      []OptionValue `json:"values,omitempty"`
	Query       *VariableQuery `json:"query,omitempty"`
	LabelColumn string        `json:"label_column,omitempty"`
	ValueColumn string        `json:"value_column,omitempty"`
}

type OptionValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type VariableQuery struct {
	ConnectorID string `json:"connector_id"`
	SQL         string `json:"sql"`
}
```

Add fields to `Widget`:

```go
type Widget struct {
	ID          string                 `json:"id"`
	DashboardID string                 `json:"dashboard_id"`
	NotebookID  *string                `json:"notebook_id,omitempty"`
	CellID      *string                `json:"cell_id,omitempty"`
	ConnectorID *string                `json:"connector_id,omitempty"`
	Query       *string                `json:"query,omitempty"`
	Language    string                 `json:"language"`
	Type        WidgetType             `json:"type"`
	Layout      WidgetLayout           `json:"layout"`
	Config      map[string]interface{} `json:"config"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}
```

Remove the five input `WidgetType` constants (`WidgetDatePicker`, `WidgetDateRange`, `WidgetFreetext`, `WidgetNumber`, `WidgetMultiSelect`). Also remove `ParameterOverrides` (done above). Fix every compile error this creates (expected: only `dashboard_handlers.go` scan/insert lists and `loadWidgets`).

**Step 3: Update widget SELECT/INSERT lists in `dashboard_handlers.go`**

In `handleAddWidget` (currently line ~532) include the new columns:

```go
err = s.db.Pool.QueryRow(ctx,
    `INSERT INTO widgets (dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config)
     VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
     RETURNING id, dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config, created_at, updated_at`,
    dashID, req.NotebookID, req.CellID, req.ConnectorID, req.Query, lang, req.Type, layoutJSON, configJSON,
).Scan(&widget.ID, &widget.DashboardID, &widget.NotebookID, &widget.CellID,
    &widget.ConnectorID, &widget.Query, &widget.Language, &widget.Type, &widget.Layout, &configOut, &widget.CreatedAt, &widget.UpdatedAt)
```

In `loadWidgets` (currently line ~902):

```go
`SELECT id, dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config, created_at, updated_at
 FROM widgets WHERE dashboard_id = $1 ORDER BY created_at ASC`
```

and scan the new columns into `wgt.ConnectorID`, `wgt.Query`, `wgt.Language`.

Also extend `addWidgetRequest`:

```go
type addWidgetRequest struct {
	NotebookID  *string                `json:"notebook_id"`
	CellID      *string                `json:"cell_id"`
	ConnectorID *string                `json:"connector_id"`
	Query       *string                `json:"query"`
	Language    *string                `json:"language"`
	Type        models.WidgetType      `json:"type"`
	Layout      models.WidgetLayout    `json:"layout"`
	Config      map[string]interface{} `json:"config,omitempty"`
}
```

with defaults `language = "sql"` when empty, and source validation: reject when both `notebook_id` and `connector_id` are set; reject query widgets without `query`; reject cell widgets without `notebook_id` + `cell_id`.

**Step 4: Write the migration test**

Append to `internal/database/database_test.go` (package `database_test`):

```go
func TestMigration121DashboardVariables(t *testing.T) {
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()

	// Schema: new columns exist.
	for _, col := range []string{"connector_id", "query", "language"} {
		var exists bool
		if err := db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='widgets' AND column_name=$1)`, col).Scan(&exists); err != nil {
			t.Fatalf("column %s query: %v", col, err)
		}
		if !exists {
			t.Fatalf("widgets.%s missing after V121", col)
		}
	}

	// Conversion: run the marked block against a simulated legacy dashboard.
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check`); err != nil {
		t.Fatalf("drop constraint: %v", err)
	}
	var orgID, userID, dashID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('V121 Org', 'v121-' || md5(random()::text)) RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ('v121-' || md5(random()::text) || '@example.com', 'V121') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO dashboards (org_id, title, settings, created_by)
		 VALUES ($1, 'V121', '{"parameter_overrides":{"x":"y"}}', $2) RETURNING id`, orgID, userID).Scan(&dashID); err != nil {
		t.Fatalf("insert dashboard: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO widgets (dashboard_id, type, layout, config) VALUES
		 ($1, 'date_picker', '{}', '{"paramName":"start_date","label":"Start date"}'),
		 ($1, 'multi_select', '{}', '{"paramName":"region","label":"Region","options":["EMEA","AMER"]}')`, dashID); err != nil {
		t.Fatalf("insert widgets: %v", err)
	}

	content, err := os.ReadFile("migrations/V121__dashboard_query_widgets.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	startMarker, endMarker := "-- +conversion:start", "-- +conversion:end"
	start := strings.Index(string(content), startMarker)
	end := strings.Index(string(content), endMarker)
	if start < 0 || end < 0 {
		t.Fatal("conversion markers missing from V121")
	}
	if _, err := tx.Exec(ctx, string(content)[start+len(startMarker):end]); err != nil {
		t.Fatalf("run conversion: %v", err)
	}

	var settings []byte
	if err := tx.QueryRow(ctx, `SELECT settings FROM dashboards WHERE id=$1`, dashID).Scan(&settings); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	var got struct {
		Variables []models.DashboardVariable `json:"variables"`
	}
	if err := json.Unmarshal(settings, &got); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if len(got.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %+v", got.Variables)
	}
	var region *models.DashboardVariable
	for i := range got.Variables {
		if got.Variables[i].Name == "region" {
			region = &got.Variables[i]
		}
	}
	if region == nil || region.Type != "multi_select" || region.Options == nil || len(region.Options.Values) != 2 {
		t.Fatalf("region variable wrong: %+v", region)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM widgets WHERE dashboard_id=$1`, dashID).Scan(&remaining); err != nil {
		t.Fatalf("count widgets: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected input widgets deleted, %d remain", remaining)
	}
}
```

Add imports if missing: `encoding/json`, `strings`, `context`, `os`, `testing`, `database`, `models`.

**Step 5: Run tests**

```bash
task infra:up
go test ./internal/database/ -run TestMigration121DashboardVariables -timeout 3m -v -count=1
go build ./...
```

Expected: test PASS, build clean.

**Step 6: Commit**

```bash
git add internal/database/migrations/V121__dashboard_query_widgets.sql internal/models/dashboard.go internal/api/dashboard_handlers.go internal/database/database_test.go
git commit -m "feat(dashboards): add query widget columns and variable migration"
```

---

### Task 2: Type-aware variable interpolation package

**Files:**
- Create: `internal/dashboard/variables.go`
- Create: `internal/dashboard/variables_test.go`

**Step 1: Write the failing tests**

`internal/dashboard/variables_test.go`:

```go
package dashboard

import (
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func varOf(name, typ string, def interface{}) models.DashboardVariable {
	return models.DashboardVariable{Name: name, Type: typ, Default: def}
}

func TestInterpolateEscapesText(t *testing.T) {
	sql, err := Interpolate(
		`SELECT * FROM t WHERE name = {{who}}`,
		[]models.DashboardVariable{varOf("who", "text", "")},
		map[string]any{"who": "O'Brien"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `'O''Brien'`) {
		t.Fatalf("not escaped: %s", sql)
	}
}

func TestInterpolateAllowsSpacesInsideToken(t *testing.T) {
	sql, err := Interpolate(`SELECT {{ x }}`, []models.DashboardVariable{varOf("x", "number", 1)}, map[string]any{"x": 42.5})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT 42.5" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateRejectsUnknownVariable(t *testing.T) {
	_, err := Interpolate(`SELECT {{nope}}`, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("expected unknown variable error, got %v", err)
	}
}

func TestInterpolateMultiSelect(t *testing.T) {
	sql, err := Interpolate(`WHERE region IN {{region}}`,
		[]models.DashboardVariable{varOf("region", "multi_select", nil)},
		map[string]any{"region": []any{"EMEA", "AMER"}})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "WHERE region IN ('EMEA','AMER')" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateMultiSelectEmptyOptionalIsNull(t *testing.T) {
	sql, err := Interpolate(`WHERE region IN {{region}}`,
		[]models.DashboardVariable{varOf("region", "multi_select", nil)},
		map[string]any{"region": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "WHERE region IN (NULL)" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateRequiredEmptyErrors(t *testing.T) {
	v := models.DashboardVariable{Name: "region", Type: "multi_select", Required: true}
	if _, err := Interpolate(`WHERE region IN {{region}}`, []models.DashboardVariable{v}, map[string]any{"region": []any{}}); err == nil {
		t.Fatal("expected required error")
	}
}

func TestInterpolateNumberRejectsNonNumeric(t *testing.T) {
	if _, err := Interpolate(`SELECT {{n}}`, []models.DashboardVariable{varOf("n", "number", 0)}, map[string]any{"n": "abc"}); err == nil {
		t.Fatal("expected number error")
	}
}

func TestInterpolateBoolean(t *testing.T) {
	sql, err := Interpolate(`SELECT * FROM t WHERE active = {{a}}`,
		[]models.DashboardVariable{varOf("a", "boolean", false)}, map[string]any{"a": true})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT * FROM t WHERE active = true" {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateDateRange(t *testing.T) {
	v := models.DashboardVariable{Name: "range", Type: "date_range"}
	sql, err := Interpolate(`WHERE ts >= {{range_start}} AND ts < {{range_end}}`,
		[]models.DashboardVariable{v}, map[string]any{"range": []any{"2026-01-01", "2026-02-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if sql != `WHERE ts >= '2026-01-01' AND ts < '2026-02-01'` {
		t.Fatalf("got %q", sql)
	}
}

func TestInterpolateDateRejectsGarbage(t *testing.T) {
	if _, err := Interpolate(`SELECT {{d}}`, []models.DashboardVariable{varOf("d", "date", "")}, map[string]any{"d": "not-a-date"}); err == nil {
		t.Fatal("expected date error")
	}
}

func TestInterpolateDefaultUsedWhenValueMissing(t *testing.T) {
	sql, err := Interpolate(`SELECT {{n}}`, []models.DashboardVariable{varOf("n", "number", 7)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT 7" {
		t.Fatalf("got %q", sql)
	}
}

func TestValidateVariablesRejectsBadInput(t *testing.T) {
	if err := ValidateVariables([]models.DashboardVariable{{Name: "a b", Type: "text"}}); err == nil {
		t.Fatal("expected invalid name error")
	}
	if err := ValidateVariables([]models.DashboardVariable{{Name: "a", Type: "wat"}}); err == nil {
		t.Fatal("expected invalid type error")
	}
	if err := ValidateVariables([]models.DashboardVariable{
		{Name: "a", Type: "multi_select", Options: &models.VariableOptions{Mode: "static"}},
		{Name: "b", Type: "text", DependsOn: []string{"missing"}},
	}); err == nil {
		t.Fatal("expected unknown dependency error")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/ -timeout 3m
```

Expected: FAIL — package/function does not exist.

**Step 3: Implement `internal/dashboard/variables.go`**

```go
// Package dashboard implements dashboard variable validation and
// type-aware interpolation into query widget SQL. Interpolation is
// escaping-only: values can never become identifiers or SQL fragments.
package dashboard

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/the-heaven-labs/aether/internal/models"
)

var (
	refRe   = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}`)
	nameRe  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	types   = map[string]bool{
		"text": true, "number": true, "boolean": true, "date": true,
		"date_range": true, "single_select": true, "multi_select": true,
	}
)

func ValidateVariables(vars []models.DashboardVariable) error {
	seen := map[string]bool{}
	for _, v := range vars {
		if !nameRe.MatchString(v.Name) {
			return fmt.Errorf("invalid variable name %q", v.Name)
		}
		if seen[v.Name] {
			return fmt.Errorf("duplicate variable name %q", v.Name)
		}
		if !types[v.Type] {
			return fmt.Errorf("invalid type %q for variable %q", v.Type, v.Name)
		}
		if v.Options != nil {
			switch v.Options.Mode {
			case "static":
			case "query":
				if v.Options.Query == nil || v.Options.Query.ConnectorID == "" || v.Options.Query.SQL == "" {
					return fmt.Errorf("variable %q has an incomplete options query", v.Name)
				}
			default:
				return fmt.Errorf("variable %q has invalid options mode %q", v.Name, v.Options.Mode)
			}
		}
		seen[v.Name] = true
	}
	for _, v := range vars {
		for _, dep := range v.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("variable %q depends on unknown variable %q", v.Name, dep)
			}
		}
	}
	return nil
}

// Interpolate substitutes every {{token}} in sql with a SQL literal derived
// from the matching variable's declared type. provided holds raw JSON values
// from the client; missing entries fall back to the variable default.
func Interpolate(sql string, vars []models.DashboardVariable, provided map[string]any) (string, error) {
	literals, err := resolveLiterals(vars, provided)
	if err != nil {
		return "", err
	}
	var firstErr error
	out := refRe.ReplaceAllStringFunc(sql, func(token string) string {
		name := refRe.FindStringSubmatch(token)[1]
		lit, ok := literals[name]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown variable %q referenced in query", name)
			}
			return token
		}
		return lit
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func resolveLiterals(vars []models.DashboardVariable, provided map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range vars {
		raw, ok := provided[v.Name]
		if !ok || raw == nil {
			raw = v.Default
		}
		if v.Type == "date_range" {
			start, end, err := asStringPair(raw, v)
			if err != nil {
				return nil, err
			}
			out[v.Name+"_start"] = quote(start)
			out[v.Name+"_end"] = quote(end)
			continue
		}
		lit, err := formatValue(v, raw)
		if err != nil {
			return nil, err
		}
		out[v.Name] = lit
	}
	return out, nil
}

func formatValue(v models.DashboardVariable, raw any) (string, error) {
	empty := raw == nil || raw == ""
	switch v.Type {
	case "text", "single_select", "":
		s, err := asString(raw, v)
		if err != nil {
			return "", err
		}
		if v.Required && s == "" {
			return "", fmt.Errorf("variable %q is required", v.Name)
		}
		return quote(s), nil
	case "number":
		if empty {
			if v.Required {
				return "", fmt.Errorf("variable %q is required", v.Name)
			}
			return "NULL", nil
		}
		num, err := asNumber(raw, v)
		if err != nil {
			return "", err
		}
		return strconv.FormatFloat(num, 'f', -1, 64), nil
	case "boolean":
		if empty {
			if v.Required {
				return "", fmt.Errorf("variable %q is required", v.Name)
			}
			return "NULL", nil
		}
		b, ok := raw.(bool)
		if !ok {
			return "", fmt.Errorf("invalid value for variable %q: expected a boolean", v.Name)
		}
		return strconv.FormatBool(b), nil
	case "date":
		s, err := asString(raw, v)
		if err != nil {
			return "", err
		}
		if v.Required && s == "" {
			return "", fmt.Errorf("variable %q is required", v.Name)
		}
		if s != "" {
			if _, err := parseDate(s); err != nil {
				return "", fmt.Errorf("invalid value for variable %q: %v", v.Name, err)
			}
		}
		return quote(s), nil
	case "multi_select":
		list, err := asStringList(raw, v)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			if v.Required {
				return "", fmt.Errorf("variable %q is required", v.Name)
			}
			return "(NULL)", nil
		}
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, quote(item))
		}
		return "(" + strings.Join(parts, ",") + ")", nil
	default:
		return "", fmt.Errorf("unsupported variable type %q", v.Type)
	}
}

func asString(raw any, v models.DashboardVariable) (string, error) {
	if raw == nil {
		return "", nil
	}
	if s, ok := raw.(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("invalid value for variable %q: expected a string", v.Name)
}

func asNumber(raw any, v models.DashboardVariable) (float64, error) {
	switch n := raw.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("invalid value for variable %q", v.Name)
		}
		return n, nil
	case int:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("invalid value for variable %q: not a number", v.Name)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("invalid value for variable %q: not a number", v.Name)
	}
}

func asStringList(raw any, v models.DashboardVariable) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		if ss, ok := raw.([]string); ok {
			return ss, nil
		}
		return nil, fmt.Errorf("invalid value for variable %q: expected a list", v.Name)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value for variable %q: list items must be strings", v.Name)
		}
		out = append(out, s)
	}
	return out, nil
}

func asStringPair(raw any, v models.DashboardVariable) (string, string, error) {
	empty := raw == nil || raw == ""
	if empty {
		if v.Required {
			return "", "", fmt.Errorf("variable %q is required", v.Name)
		}
		return "", "", nil
	}
	list, ok := raw.([]any)
	if !ok || len(list) != 2 {
		if ss, ok := raw.([]string); ok && len(ss) == 2 {
			return validatePair(ss[0], ss[1], v)
		}
		return "", "", fmt.Errorf("invalid value for variable %q: expected a [start, end] pair", v.Name)
	}
	start, ok1 := list[0].(string)
	end, ok2 := list[1].(string)
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("invalid value for variable %q: expected a [start, end] pair", v.Name)
	}
	return validatePair(start, end, v)
}

func validatePair(start, end string, v models.DashboardVariable) (string, string, error) {
	for _, s := range []string{start, end} {
		if s == "" {
			continue
		}
		if _, err := parseDate(s); err != nil {
			return "", "", fmt.Errorf("invalid value for variable %q: %v", v.Name, err)
		}
	}
	return start, end, nil
}

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -timeout 3m -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dashboard/
git commit -m "feat(dashboards): add type-aware variable interpolation"
```

---

## Phase 2 — Shared execution and dashboard endpoints

### Task 3: Extract `openQuery` from `handleExecuteCell`

This is a behavior-preserving refactor; all existing execute tests must stay green.

**Files:**
- Create: `internal/api/query_runner.go`
- Modify: `internal/api/execute_handlers.go` (replace connector load/check/switch block, lines ~115-310)

**Step 1: Create `internal/api/query_runner.go`**

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

var (
	errQueryConnectorNotFound = errors.New("connector not found")
	errQueryConnectorDenied   = errors.New("connector access denied")
	errQueryProvisionerDenied = errors.New("provisioner not executable")
	errQueryServiceDenied     = errors.New("service access denied")
	errQueryNotReady          = errors.New("warehouse not ready")
	errQueryConnectFailed     = errors.New("connect failed")
	errQueryUnsupported       = errors.New("unsupported connector type")
	errQueryInternal          = errors.New("internal error")
)

type queryServiceChoiceError struct{ Choice *executor.ServiceChoiceError }

func (e *queryServiceChoiceError) Error() string { return "service choice required" }

// openedQuery is a connector resolved into a ready executor for one identity.
// The caller owns Exec and must Close it.
type openedQuery struct {
	Exec              executor.Executor
	DialedConnectorID string
	MaxRows           int
	TimeoutSecs       int
	WarehouseID       string
	WarehouseName     string
	ServiceName       string
	CHUser            string
}

// openQuery loads the connector, applies the `use` gate for unmanaged
// connectors, resolves the execution target for managed ClickHouse, and
// returns a ready executor. Shared by cell runs (handleExecuteCell) and
// dashboard query widgets.
func (s *Server) openQuery(ctx context.Context, orgID, userID, orgRole, connectorID string, pinned bool) (*openedQuery, error) {
	var connType models.ConnectorType
	var encryptedConfig []byte
	var maxRows, timeout int
	var connectorWarehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted, max_rows, timeout_seconds, warehouse_id
		 FROM connectors WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connectorID, orgID,
	).Scan(&connType, &encryptedConfig, &maxRows, &timeout, &connectorWarehouseID)
	if err == pgx.ErrNoRows {
		return nil, errQueryConnectorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load connector: %w", err)
	}

	// Managed ClickHouse service access is enforced against the service that
	// actually serves the run (routing preference / sole-allowed-service).
	managedClickHouse := connType == models.ConnectorClickHouse && connectorWarehouseID != nil
	if !managedClickHouse || !s.warehouseManagementEnabled() {
		useOK, err := s.checkPermission(ctx, userID, orgID, orgRole, "connector", connectorID, "use")
		if err != nil {
			return nil, fmt.Errorf("permission check: %w", err)
		}
		if !useOK {
			return nil, errQueryConnectorDenied
		}
	}

	plain, err := crypto.Decrypt(encryptedConfig, s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt connector config: %w", err)
	}
	driver, ok := executor.GetDriver(connType)
	if !ok {
		return nil, errQueryUnsupported
	}

	out := &openedQuery{DialedConnectorID: connectorID, MaxRows: maxRows, TimeoutSecs: timeout}
	switch {
	case connType == models.ConnectorClickHouse:
		userUUID, err := uuid.Parse(userID)
		if err != nil {
			return nil, fmt.Errorf("invalid user id: %w", err)
		}
		connUUID, err := uuid.Parse(connectorID)
		if err != nil {
			return nil, errQueryConnectorNotFound
		}
		target, targetErr := s.resolveExecutionTarget(ctx, userUUID, connUUID, pinned)
		var choice *executor.ServiceChoiceError
		switch {
		case targetErr == nil:
			conn, release, getErr := s.connPool.Get(target.Endpoint, target.CHUser, target.Config)
			if getErr != nil {
				return nil, errQueryConnectFailed
			}
			out.Exec = executor.NewPooledClickHouseExecutor(conn, release)
			out.WarehouseID = target.WarehouseID.String()
			out.WarehouseName = target.WarehouseName
			out.ServiceName = target.ConnectorName
			out.CHUser = target.CHUser
			out.DialedConnectorID = target.ConnectorID.String()
			out.MaxRows = target.MaxRows
			out.TimeoutSecs = target.TimeoutSeconds
		case errors.Is(targetErr, executor.ErrProvisionerNotExecutable):
			return nil, errQueryProvisionerDenied
		case errors.Is(targetErr, executor.ErrUnmanagedConnector):
			out.Exec, err = driver.NewExecutor(plain)
			if err != nil {
				return nil, errQueryConnectFailed
			}
		case errors.Is(targetErr, executor.ErrConnectorNotFound):
			return nil, errQueryConnectorNotFound
		case errors.Is(targetErr, executor.ErrProvisioningNotReady):
			return nil, errQueryNotReady
		case errors.Is(targetErr, executor.ErrServiceAccessDenied):
			return nil, errQueryServiceDenied
		case errors.As(targetErr, &choice):
			return nil, &queryServiceChoiceError{Choice: choice}
		default:
			slog.Error("resolve execution target", "connector_id", connectorID, "error", targetErr)
			return nil, errQueryInternal
		}
	default:
		out.Exec, err = driver.NewExecutor(plain)
		if err != nil {
			return nil, errQueryConnectFailed
		}
	}
	return out, nil
}

// writeOpenQueryError maps openQuery failures onto the responses shared by
// cell execution and dashboard query execution.
func writeOpenQueryError(w http.ResponseWriter, err error, pinned bool) {
	var choice *queryServiceChoiceError
	switch {
	case errors.Is(err, errQueryConnectorNotFound):
		writeError(w, http.StatusNotFound, "connector not found")
	case errors.Is(err, errQueryConnectorDenied):
		writeError(w, http.StatusForbidden, "you don't have permission to use this connector")
	case errors.Is(err, errQueryProvisionerDenied):
		writeError(w, http.StatusForbidden,
			"this connector is the warehouse provisioner and cannot run queries; choose another service or ask an admin to enable queries through the provisioner")
	case errors.Is(err, errQueryServiceDenied):
		if pinned {
			writeError(w, http.StatusForbidden, "you don't have access to the pinned service")
			return
		}
		writeError(w, http.StatusForbidden, "no permitted service in warehouse")
	case errors.Is(err, errQueryNotReady):
		writeError(w, http.StatusServiceUnavailable, "warehouse provisioning is not ready")
	case errors.Is(err, errQueryUnsupported):
		writeError(w, http.StatusBadRequest, "unsupported connector type")
	case errors.Is(err, errQueryConnectFailed):
		writeError(w, http.StatusBadGateway, "failed to connect to database")
	case errors.As(err, &choice):
		writeServiceChoiceRequired(w, choice.Choice)
	default:
		slog.Error("open query", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve execution target")
	}
}
```

**Step 2: Replace the block in `handleExecuteCell`**

Remove the connector load (lines ~115-133), the `use` check (135-156), and the whole `switch` that builds `exec` (219-310). Replace with:

```go
opened, err := s.openQuery(ctx, claims.OrgID, claims.UserID, claims.Role, cell.ConnectorID, req.Pinned)
if err != nil {
	writeOpenQueryError(w, err, req.Pinned)
	return
}
exec := opened.Exec
defer exec.Close()
connectTime := time.Since(connectStart).Milliseconds()

auditConnID := opened.DialedConnectorID
warehouseID := opened.WarehouseID
warehouseName := opened.WarehouseName
serviceName := opened.ServiceName
chUser := opened.CHUser
maxRows = opened.MaxRows
timeout = opened.TimeoutSecs
```

Keep `connectStart := time.Now()` immediately before the call and the `var (...)` declarations for the fields used later. `driver` and `plain` are no longer needed by the handler.

**Step 3: Run the full cell execution test suite**

```bash
go test ./internal/api/ -run 'TestExecuteCell' -timeout 3m -count=1
```

Expected: PASS (all pre-existing tests).

**Step 4: Commit**

```bash
git add internal/api/query_runner.go internal/api/execute_handlers.go
git commit -m "refactor(api): extract shared connector/executor open helper"
```

---

### Task 4: Dashboard widget execute endpoint

**Files:**
- Create: `internal/api/dashboard_query_handlers.go`
- Create: `internal/api/dashboard_query_test.go`
- Modify: `internal/api/router.go` (routes)

**Step 1: Write the failing test**

`internal/api/dashboard_query_test.go` (package `api_test`). Use the real dev Postgres connector pattern from `testhelpers_test.go`.

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDashboardQueryExecuteEndToEnd(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	email := fmt.Sprintf("dash-query-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Query Org")
	connID := createConnector(t, srv, token)

	// Create a dashboard with a variable and a query widget.
	settings := map[string]any{
		"variables": []map[string]any{
			{"name": "who", "label": "Who", "type": "text", "default": "world"},
		},
	}
	body, _ := json.Marshal(map[string]any{"title": "Query Dash", "settings": settings})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var dash map[string]any
	json.NewDecoder(rec.Body).Decode(&dash)
	dashID := dash["id"].(string)

	// Grant view_with_data explicitly (creator seed includes it after this change).
	widgetBody, _ := json.Marshal(map[string]any{
		"connector_id": connID,
		"query":        "SELECT '{{who}}' AS greeting",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})
	req = httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets", bytes.NewReader(widgetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var widget map[string]any
	json.NewDecoder(rec.Body).Decode(&widget)
	widgetID := widget["id"].(string)

	// Execute with a provided variable value.
	execBody, _ := json.Marshal(map[string]any{
		"widget_id": widgetID,
		"variables": map[string]any{"who": "Aether"},
	})
	req = httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/execute", bytes.NewReader(execBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	require.NotEmpty(t, resp["outputs"])
}

func TestDashboardQueryExecuteRequiresViewWithData(t *testing.T) {
	// Second user with only dashboard view permission must get 403.
}

func TestDashboardQueryExecuteUnknownVariableReturns400(t *testing.T) {
	// query "SELECT {{nope}}" with no matching variable -> 400 mentioning "nope".
}

func TestDashboardQueryExecuteNonQueryWidgetReturns400(t *testing.T) {
	// Legacy cell widget -> 400 "not a query widget".
}
```

> Note: implement the three extra tests (`RequiresViewWithData`, `UnknownVariableReturns400`, `NonQueryWidgetReturns400`) by copying the setup (create connector → dashboard → widget → execute) with the specific assertion. For the 403 case, register a second user in the same org and grant only `view` on the dashboard via SQL (`INSERT INTO acl_entries ... ARRAY['view']`) then call execute with their token. Extract a shared `createDashWithSettings(t, srv, token, settings) (dashID string)` helper in this test file and reuse it across tests.

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/api/ -run 'TestDashboardQueryExecute' -timeout 3m -count=1
```

Expected: FAIL — 404 route not found.

**Step 3: Implement `internal/api/dashboard_query_handlers.go`**

```go
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/dashboard"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

const (
	defaultDashboardQueryCacheSeconds = 30
	dashboardOptionMaxRows            = 1000
	dashboardOptionTimeout            = 30 * time.Second
	dashboardCachePrefix              = "dashq:"
)

type dashboardExecuteRequest struct {
	WidgetID    string         `json:"widget_id"`
	Variables   map[string]any `json:"variables,omitempty"`
	BypassCache bool           `json:"bypass_cache,omitempty"`
}

type dashboardVariableOptionsRequest struct {
	Variables map[string]any `json:"variables,omitempty"`
}

type dashboardQueryResponse struct {
	Outputs        []models.Output        `json:"outputs"`
	Metrics        map[string]interface{} `json:"metrics"`
	Routing        map[string]interface{} `json:"routing,omitempty"`
	Cached         bool                   `json:"cached"`
	CacheExpiresAt *time.Time             `json:"cache_expires_at,omitempty"`
}

// httpQueryError carries an HTTP status out of runDashboardQuery.
type httpQueryError struct {
	status  int
	message string
}

func (e *httpQueryError) Error() string { return e.message }

type dashboardIdentity struct {
	UserID string
	Role   string
}

type dashboardQueryParams struct {
	OrgID           string
	Identity        dashboardIdentity
	ConnectorID     string
	SQL             string
	BypassCache     bool
	CacheSeconds    *int
	MaxRowsOverride int
	Timeout         time.Duration
	CacheScope      string // public token; empty for authenticated runs
}

func (s *Server) handleExecuteDashboardWidget(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	var req dashboardExecuteRequest
	if err := decodeJSON(r, &req); err != nil || req.WidgetID == "" {
		writeError(w, http.StatusBadRequest, "widget_id is required")
		return
	}

	dash, err := s.loadDashboardSettings(ctx, claims.OrgID, dashID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you need view_with_data permission to run dashboard queries")
		return
	}

	widget, err := s.loadQueryWidget(ctx, dashID, req.WidgetID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if widget.ConnectorID == nil || widget.Query == nil {
		writeError(w, http.StatusBadRequest, "widget is not a query widget")
		return
	}
	if widget.Language != "" && widget.Language != "sql" {
		writeError(w, http.StatusBadRequest, "only SQL query widgets can be executed")
		return
	}

	if err := dashboard.ValidateVariables(dash.Settings.Variables); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sqlText, err := dashboard.Interpolate(*widget.Query, dash.Settings.Variables, req.Variables)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := s.runDashboardQuery(ctx, dashboardQueryParams{
		OrgID:        claims.OrgID,
		Identity:     dashboardIdentity{UserID: claims.UserID, Role: claims.Role},
		ConnectorID:  *widget.ConnectorID,
		SQL:          sqlText,
		BypassCache:  req.BypassCache,
		CacheSeconds: dash.Settings.QueryCacheSeconds,
	})
	if err != nil {
		writeDashboardQueryError(w, err)
		return
	}

	rowCount := 0
	if len(resp.Outputs) > 0 {
		if rs, ok := resp.Outputs[0].Data.(*executor.ResultSet); ok {
			rowCount = len(rs.Rows)
		}
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.query", ResourceType: "dashboard", ResourceID: dashID,
		Metadata: map[string]any{
			"widget_id":    req.WidgetID,
			"connector_id": *widget.ConnectorID,
			"query":        sqlText,
			"row_count":    rowCount,
			"duration_ms":  time.Since(startTime).Milliseconds(),
			"cache_hit":    resp.Cached,
		},
	})
	writeJSON(w, http.StatusOK, resp)
}
```

Also implement in the same file:

```go
func (s *Server) loadDashboardSettings(ctx context.Context, orgID, dashID string) (*models.Dashboard, error) {
	var d models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashID, orgID,
	).Scan(&d.ID, &d.OrgID, &d.Title, &settingsOut, &d.FolderID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(settingsOut, &d.Settings)
	return &d, nil
}

func (s *Server) loadQueryWidget(ctx context.Context, dashID, widgetID string) (*models.Widget, error) {
	var w models.Widget
	var layoutOut, configOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config, created_at, updated_at
		 FROM widgets WHERE id = $1 AND dashboard_id = $2`,
		widgetID, dashID,
	).Scan(&w.ID, &w.DashboardID, &w.NotebookID, &w.CellID, &w.ConnectorID, &w.Query,
		&w.Language, &w.Type, &layoutOut, &configOut, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(layoutOut, &w.Layout)
	json.Unmarshal(configOut, &w.Config)
	return &w, nil
}

func (s *Server) runDashboardQuery(ctx context.Context, p dashboardQueryParams) (*dashboardQueryResponse, error) {
	ttl := defaultDashboardQueryCacheSeconds
	if p.CacheSeconds != nil {
		ttl = *p.CacheSeconds
	}
	cacheKey := dashboardQueryCacheKey(p)
	if s.Cache != nil && ttl > 0 && !p.BypassCache {
		if rs, expires, ok := s.dashboardQueryCacheGet(ctx, cacheKey); ok {
			return dashboardQueryResult(rs, 0, true, &expires), nil
		}
	}

	opened, err := s.openQuery(ctx, p.OrgID, p.Identity.UserID, p.Identity.Role, p.ConnectorID, false)
	if err != nil {
		return nil, err
	}
	defer opened.Exec.Close()

	maxRows := opened.MaxRows
	if p.MaxRowsOverride > 0 && (maxRows == 0 || p.MaxRowsOverride < maxRows) {
		maxRows = p.MaxRowsOverride
	}
	timeout := time.Duration(opened.TimeoutSecs) * time.Second
	if p.Timeout > 0 && (timeout <= 0 || p.Timeout < timeout) {
		timeout = p.Timeout
	}
	maxBytes, err := s.orgCellOutputMaxBytes(ctx, p.OrgID)
	if err != nil {
		return nil, err
	}

	execCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(execCtx, timeout)
		defer cancel()
	}
	execCtx = context.WithValue(execCtx, executor.CtxUserEmail{}, s.userEmail(ctx, p.Identity.UserID))

	queryStart := time.Now()
	result, err := opened.Exec.Execute(execCtx, p.SQL, nil, executor.OutputLimits{MaxBytes: maxBytes, MaxRows: maxRows})
	queryMS := time.Since(queryStart).Milliseconds()
	if err != nil {
		return nil, mapDashboardExecError(err)
	}
	if cacheKey != "" {
		// Detach from the request context so an aborted response can still warm
		// the cache; the write is best-effort.
		s.dashboardQueryCacheSet(context.WithoutCancel(ctx), cacheKey, result, ttl)
	}
	return dashboardQueryResult(result, queryMS, false, nil), nil
}

func mapDashboardExecError(err error) error {
	msg := err.Error()
	isCancelled := errors.Is(err, context.Canceled) || strings.Contains(msg, "context canceled") || strings.Contains(msg, "cancelled")
	switch {
	case isCancelled:
		return &httpQueryError{status: http.StatusUnprocessableEntity, message: "Query cancelled"}
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "context deadline exceeded"):
		return &httpQueryError{status: http.StatusUnprocessableEntity, message: "Query timed out"}
	case executor.IsClickHouseAccessDenied(err):
		return &httpQueryError{status: http.StatusForbidden, message: msg}
	default:
		return &httpQueryError{status: http.StatusUnprocessableEntity, message: msg}
	}
}

func dashboardQueryResult(rs *executor.ResultSet, queryMS int64, cached bool, expires *time.Time) *dashboardQueryResponse {
	return &dashboardQueryResponse{
		Outputs: []models.Output{{Type: "table", Data: rs}},
		Metrics: map[string]interface{}{
			"query_time_ms": queryMS,
			"total_time_ms": queryMS,
		},
		Cached:         cached,
		CacheExpiresAt: expires,
	}
}

func writeDashboardQueryError(w http.ResponseWriter, err error) {
	var httpErr *httpQueryError
	var choice *queryServiceChoiceError
	switch {
	case errors.As(err, &httpErr):
		writeError(w, httpErr.status, httpErr.message)
	case errors.As(err, &choice):
		writeServiceChoiceRequired(w, choice.Choice)
	default:
		writeOpenQueryError(w, err, false)
	}
}

func dashboardQueryCacheKey(p dashboardQueryParams) string {
	if p.SQL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		p.OrgID, p.Identity.UserID, p.CacheScope, p.ConnectorID, p.SQL,
		fmt.Sprintf("%d", p.MaxRowsOverride),
	}, "\x00")))
	return dashboardCachePrefix + hex.EncodeToString(sum[:])
}

func (s *Server) dashboardQueryCacheGet(ctx context.Context, key string) (*executor.ResultSet, time.Time, bool) {
	if s.Cache == nil || key == "" {
		return nil, time.Time{}, false
	}
	getCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	raw, err := s.Cache.Client().Get(getCtx, key).Bytes()
	if err != nil {
		return nil, time.Time{}, false
	}
	var rs executor.ResultSet
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, time.Time{}, false
	}
	expires := time.Now().Add(defaultDashboardQueryCacheSeconds * time.Second)
	if ttl, err := s.Cache.Client().TTL(getCtx, key).Result(); err == nil && ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	return &rs, expires, true
}

func (s *Server) dashboardQueryCacheSet(ctx context.Context, key string, rs *executor.ResultSet, ttlSeconds int) {
	if s.Cache == nil || key == "" || ttlSeconds <= 0 || rs == nil {
		return
	}
	raw, err := json.Marshal(rs)
	if err != nil {
		return
	}
	setCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s.Cache.Client().Set(setCtx, key, raw, time.Duration(ttlSeconds)*time.Second)
}
```

> Implementation notes for the executing agent:
> - `ResultSet` JSON tags are snake_case (`json:"columns"` etc., see `internal/executor/executor.go:11-23`); verify `json.Unmarshal` round-trips (fields may be unexported — if they are, add a `MarshalJSON`/`UnmarshalJSON` or cache a small wrapper struct with the same fields; check `executor.go` first and adapt).
> - `dashboardQueryResult`'s Data field must hold `*executor.ResultSet` so `models.Output` serializes identically to cell outputs.
> - Remove the unused `time.Since(time.Now())`; pass real `queryMS=0` on cache hits.

**Step 4: Add routes**

In `internal/api/router.go` after the existing dashboard routes:

```go
s.mux.Handle("POST /api/v1/dashboards/{id}/execute", authMW(http.HandlerFunc(s.handleExecuteDashboardWidget)))
s.mux.Handle("POST /api/v1/dashboards/{id}/variables/{name}/options", authMW(http.HandlerFunc(s.handleDashboardVariableOptions)))
```

**Step 5: Seed `view_with_data` on dashboard creation**

In `handleCreateDashboard`, change the ACL seed actions to `ARRAY['view','view_with_data','edit','delete','share']`.

**Step 6: Run tests**

```bash
go test ./internal/api/ -run 'TestDashboardQueryExecute' -timeout 3m -count=1 -v
```

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/api/dashboard_query_handlers.go internal/api/dashboard_query_test.go internal/api/router.go internal/api/dashboard_handlers.go
git commit -m "feat(dashboards): add live widget query execution endpoint"
```

---

### Task 5: Variable options endpoint

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go`
- Modify: `internal/api/dashboard_query_test.go`

**Step 1: Write the failing test**

Add to `dashboard_query_test.go`:

```go
func TestDashboardVariableOptions(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-opts-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Opts Org")
	connID := createConnector(t, srv, token)

	settings := map[string]any{
		"variables": []map[string]any{
			{"name": "country", "type": "text", "default": "US"},
			{
				"name": "city", "label": "City", "type": "single_select", "depends_on": []string{"country"},
				"options": map[string]any{
					"mode": "query",
					"query": map[string]any{
						"connector_id": connID,
						"sql":         "SELECT 'Paris' AS label, 'paris' AS value",
					},
					"label_column": "label", "value_column": "value",
				},
			},
		},
	}
	// create dashboard as in Task 4 ...
	// POST /api/v1/dashboards/{id}/variables/city/options {"variables":{"country":"FR"}}
	// expect 200 {options:[{label:"Paris",value:"paris"}]}
}

func TestDashboardVariableOptionsRejectsStaticVariable(t *testing.T) {
	// variable without query options -> 400
}
```

Implement the setup by extracting a helper in the test file (`createDashWithSettings(t, srv, token, settings) string`) and reusing it in Task 4's tests if convenient.

**Step 2: Run to verify failure**

```bash
go test ./internal/api/ -run 'TestDashboardVariableOptions' -timeout 3m -count=1
```

Expected: FAIL (404).

**Step 3: Implement handler**

Append to `dashboard_query_handlers.go`:

```go
// @Summary Run a dashboard variable's options query
// @Description Executes the query-backed options for a dashboard variable and returns label/value pairs
// @Tags dashboards
// @Accept json
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param name path string true "Variable name"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/variables/{name}/options [post]
func (s *Server) handleDashboardVariableOptions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	name := r.PathValue("name")
	ctx := r.Context()

	var req dashboardVariableOptionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	dash, err := s.loadDashboardSettings(ctx, claims.OrgID, dashID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you need view_with_data permission to run dashboard queries")
		return
	}

	v := findDashboardVariable(dash.Settings.Variables, name)
	if v == nil {
		writeError(w, http.StatusNotFound, "variable not found")
		return
	}
	if v.Options == nil || v.Options.Mode != "query" || v.Options.Query == nil {
		writeError(w, http.StatusBadRequest, "variable has no query-backed options")
		return
	}
	if err := dashboard.ValidateVariables(dash.Settings.Variables); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sqlText, err := dashboard.Interpolate(v.Options.Query.SQL, dash.Settings.Variables, req.Variables)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := s.runDashboardQuery(ctx, dashboardQueryParams{
		OrgID:           claims.OrgID,
		Identity:        dashboardIdentity{UserID: claims.UserID, Role: claims.Role},
		ConnectorID:     v.Options.Query.ConnectorID,
		SQL:             sqlText,
		CacheSeconds:    dash.Settings.QueryCacheSeconds,
		MaxRowsOverride: dashboardOptionMaxRows,
		Timeout:         dashboardOptionTimeout,
	})
	if err != nil {
		writeDashboardQueryError(w, err)
		return
	}

	options, err := optionsFromResult(resp.Outputs, v.Options.LabelColumn, v.Options.ValueColumn)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.variable_options", ResourceType: "dashboard", ResourceID: dashID,
		Metadata: map[string]any{"variable": name, "option_count": len(options)},
	})
	writeJSON(w, http.StatusOK, map[string]any{"options": options})
}

func findDashboardVariable(vars []models.DashboardVariable, name string) *models.DashboardVariable {
	for i := range vars {
		if vars[i].Name == name {
			return &vars[i]
		}
	}
	return nil
}

func optionsFromResult(outputs []models.Output, labelCol, valueCol string) ([]models.OptionValue, error) {
	if len(outputs) == 0 {
		return []models.OptionValue{}, nil
	}
	rs, ok := outputs[0].Data.(*executor.ResultSet)
	if !ok || rs == nil {
		return []models.OptionValue{}, nil
	}
	labelIdx, valueIdx := 0, 1
	for i, c := range rs.Columns {
		if c.Name == labelCol && labelCol != "" {
			labelIdx = i
		}
		if c.Name == valueCol && valueCol != "" {
			valueIdx = i
		}
	}
	if len(rs.Columns) == 1 {
		valueIdx = 0
	}
	options := make([]models.OptionValue, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		get := func(i int) string {
			if i < 0 || i >= len(row) || row[i] == nil {
				return ""
			}
			return fmt.Sprintf("%v", row[i])
		}
		options = append(options, models.OptionValue{Label: get(labelIdx), Value: get(valueIdx)})
	}
	return options, nil
}
```

Check the actual `ResultSet`/`Column` field names (exported? `Columns []Column`, `Rows [][]interface{}` — see `executor.go`; the JSON tags are `columns`, `rows`).

**Step 4: Run tests**

```bash
go test ./internal/api/ -run 'TestDashboardVariableOptions' -timeout 3m -count=1 -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/dashboard_query_handlers.go internal/api/dashboard_query_test.go
git commit -m "feat(dashboards): add variable options endpoint"
```

---

### Task 6: Public live-mode endpoints

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/dashboard_query_test.go`

**Step 1: Write failing tests**

```go
func TestPublicDashboardExecuteRequiresOptIn(t *testing.T) {
	// Dashboard shared publicly, public_live unset -> POST /api/v1/public/{token}/execute -> 403
}

func TestPublicDashboardExecuteRunsAsCreator(t *testing.T) {
	// settings.public_live = true, query widget "SELECT 1 AS x"
	// POST /api/v1/public/{token}/execute {"widget_id":...} -> 200 with rows
}

func TestPublicDashboardExecuteRateLimited(t *testing.T) {
	// Loop until 429 (or assert X-RateLimit headers present on first call).
}
```

Set `settings.public_live: true` in the dashboard creation body and obtain the public token via `POST /api/v1/dashboards/{id}/share`. For the rate-limit test set `t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")` and use a fresh `X-Forwarded-For` value per test to avoid cross-test counters.

**Step 2: Implement**

Add to `dashboard_query_handlers.go`:

```go
func (s *Server) resolvePublicDashboard(ctx context.Context, token string) (*models.Dashboard, error) {
	var d models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT d.id, d.org_id, d.title, d.settings, d.created_by
		 FROM public_tokens pt
		 JOIN orgs o ON o.id = pt.org_id AND o.public_sharing_enabled = true
		 JOIN dashboards d ON d.id = pt.resource_id AND d.deleted_at IS NULL
		 WHERE pt.token = $1 AND pt.resource_type = 'dashboard'`,
		token,
	).Scan(&d.ID, &d.OrgID, &d.Title, &settingsOut, &d.CreatedBy)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(settingsOut, &d.Settings)
	return &d, nil
}

func (s *Server) handlePublicDashboardExecute(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	ctx := r.Context()
	var req dashboardExecuteRequest
	if err := decodeJSON(r, &req); err != nil || req.WidgetID == "" {
		writeError(w, http.StatusBadRequest, "widget_id is required")
		return
	}
	dash, err := s.resolvePublicDashboard(ctx, token)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource not found or sharing disabled")
		return
	}
	if !dash.Settings.PublicLive {
		writeError(w, http.StatusForbidden, "live queries are not enabled for this dashboard")
		return
	}
	widget, err := s.loadQueryWidget(ctx, dash.ID, req.WidgetID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	if err != nil || widget.ConnectorID == nil || widget.Query == nil {
		writeError(w, http.StatusBadRequest, "widget is not a query widget")
		return
	}
	// ... ValidateVariables + Interpolate (same as authenticated handler)
	resp, err := s.runDashboardQuery(ctx, dashboardQueryParams{
		OrgID:        dash.OrgID,
		Identity:     dashboardIdentity{UserID: dash.CreatedBy, Role: "viewer"},
		ConnectorID:  *widget.ConnectorID,
		SQL:          sqlText,
		BypassCache:  req.BypassCache,
		CacheSeconds: dash.Settings.QueryCacheSeconds,
		CacheScope:   "token:" + token,
	})
	// ... writeDashboardQueryError / writeJSON
}
```

Implement `handlePublicDashboardVariableOptions` analogously (find variable, require query options, run, respond `{options}`).

Add routes in `router.go` near the existing public route:

```go
s.mux.Handle("POST /api/v1/public/{token}/execute",
	s.rateLimit(rateLimitConfig{
		keyFunc: func(r *http.Request) string { return "public-dash-exec:" + r.PathValue("token") + ":" + clientIP(r) },
		limit:   60,
		window:  time.Minute,
	})(http.HandlerFunc(s.handlePublicDashboardExecute)))
s.mux.Handle("POST /api/v1/public/{token}/variables/{name}/options",
	s.rateLimit(rateLimitConfig{
		keyFunc: func(r *http.Request) string { return "public-dash-opts:" + r.PathValue("token") + ":" + clientIP(r) },
		limit:   120,
		window:  time.Minute,
	})(http.HandlerFunc(s.handlePublicDashboardVariableOptions)))
```

(`time` is already imported in router.go; verify.)

**Step 3: Run tests**

```bash
go test ./internal/api/ -run 'TestPublicDashboard' -timeout 3m -count=1 -v
```

Expected: PASS. Note pre-existing public dashboard tests (`TestDashboardCRUD`) must also pass.

**Step 4: Commit**

```bash
git add internal/api/dashboard_query_handlers.go internal/api/router.go internal/api/dashboard_query_test.go
git commit -m "feat(dashboards): add opt-in public live queries with rate limits"
```

---

### Task 7: Convert-to-query-widget endpoint

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go`
- Modify: `internal/api/dashboard_query_test.go`

**Step 1: Write the failing test**

```go
func TestConvertCellWidgetToQueryWidget(t *testing.T) {
	// notebook + sql cell "SELECT {{who}}" with notebook parameter who (default 'world'),
	// cell connector; dashboard + cell widget.
	// POST /api/v1/dashboards/{id}/widgets/{widget_id}/convert-to-query -> 200
	// widget now has connector_id/query, notebook_id/cell_id null,
	// dashboard settings.variables contains "who".
}
```

Also cover a cell using `{{other_cell_slug}}` and assert the resolved SQL is inlined.

**Step 2: Implement**

```go
func (s *Server) handleConvertWidgetToQuery(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID, widgetID := r.PathValue("id"), r.PathValue("widget_id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "you don't have permission to edit this dashboard")
		return
	}
	widget, err := s.loadQueryWidget(ctx, dashID, widgetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	if widget.CellID == nil || widget.NotebookID == nil {
		writeError(w, http.StatusBadRequest, "widget is not linked to a notebook cell")
		return
	}
	// Load cell + notebook and permission-check notebook view.
	var source, language string
	var cellConnID *string
	var cellParamsJSON []byte
	err = s.db.Pool.QueryRow(ctx,
		`SELECT c.source, c.language, c.connector_id, c.parameters
		 FROM cells c JOIN notebooks n ON n.id = c.notebook_id
		 WHERE c.id = $1 AND c.notebook_id = $2 AND n.org_id = $3`,
		*widget.CellID, *widget.NotebookID, claims.OrgID,
	).Scan(&source, &language, &cellConnID, &cellParamsJSON)
	if err != nil {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}
	// notebook connector fallback
	if cellConnID == nil || *cellConnID == "" {
		var nbConn *string
		s.db.Pool.QueryRow(ctx, `SELECT connector_id FROM notebooks WHERE id=$1`, *widget.NotebookID).Scan(&nbConn)
		cellConnID = nbConn
	}
	if cellConnID == nil || *cellConnID == "" {
		writeError(w, http.StatusBadRequest, "cell has no connector assigned")
		return
	}

	// Slug map + notebook params, then resolve. knownParams must include the
	// names that will become variables so slug resolution leaves them alone.
	dash, err := s.loadDashboardSettings(ctx, claims.OrgID, dashID)
	if err != nil { ... }
	var slugMap = map[string]string{}
	// (copy the slug-map query + notebook/cell parameter loading from handleExecuteCell)
	// Build variables for every parameter referenced in source that is not already defined,
	// preserving notebook/cell types/defaults; reject unknown tokens via resolveSlugRefs.
	resolved, err := resolveSlugRefs(source, slugMap, knownParams)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Persist: widget query + connector, clear cell link; dashboard variables (dedup by name).
	tx, err := s.db.Pool.Begin(ctx)
	// UPDATE widgets SET notebook_id=NULL, cell_id=NULL, connector_id=$1, query=$2, language=$3, updated_at=NOW() WHERE id=...
	// UPDATE dashboards SET settings = jsonb_set(...) with merged variables
	tx.Commit(ctx)

	s.audit.Log(ctx, audit.Entry{ Action: "widget.convert_to_query", ... })
	writeJSON(w, http.StatusOK, map[string]any{"widget": updatedWidget, "variables": mergedVars})
}
```

Implementation detail: build variables server-side in Go (`[]models.DashboardVariable`), merge into `dash.Settings.Variables` deduped by name, marshal settings, update. Reuse `paramTypesToVariableType` mapping (`string→text`, `number→number`, `boolean→boolean`, `date→date`, `daterange→date_range`).

Add route:

```go
s.mux.Handle("POST /api/v1/dashboards/{id}/widgets/{widget_id}/convert-to-query", authMW(http.HandlerFunc(s.handleConvertWidgetToQuery)))
```

**Step 3: Run tests + commit**

```bash
go test ./internal/api/ -run 'TestConvertCellWidgetToQuery' -timeout 3m -count=1 -v
git add internal/api/dashboard_query_handlers.go internal/api/dashboard_query_test.go internal/api/router.go
git commit -m "feat(dashboards): add cell-widget to query-widget conversion"
```

---

## Phase 3 — Frontend

### Task 8: API client signal support + shared types

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/types/index.ts`

**Step 1: Extend the API client**

```ts
async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options?: { binary?: boolean; signal?: AbortSignal },
): Promise<T> {
  // ...
  const res = await fetch(BASE_URL + path, {
    method,
    headers,
    body: body ? (options?.binary ? (body as BodyInit) : JSON.stringify(body)) : undefined,
    signal: options?.signal,
  })
  // ...
}

export const api = {
  get: <T>(path: string, options?: { signal?: AbortSignal }) => request<T>('GET', path, undefined, options),
  post: <T>(path: string, body?: unknown, options?: { signal?: AbortSignal }) => request<T>('POST', path, body, options),
  put: <T>(path: string, body?: unknown, options?: { signal?: AbortSignal }) => request<T>('PUT', path, body, options),
  patch: <T>(path: string, body?: unknown, options?: { signal?: AbortSignal }) => request<T>('PATCH', path, body, options),
  delete: <T>(path: string, options?: { signal?: AbortSignal }) => request<T>('DELETE', path, undefined, options),
}
```

Run `cd web && npx tsc --noEmit` to confirm no call-site breakage.

**Step 2: Types**

In `web/src/types/index.ts`:

```ts
export type DashboardVariableType =
  | 'text' | 'number' | 'boolean' | 'date' | 'date_range' | 'single_select' | 'multi_select'

export interface DashboardVariableOption { label: string; value: string }

export interface DashboardVariableOptions {
  mode: 'static' | 'query'
  values?: DashboardVariableOption[]
  query?: { connector_id: string; sql: string }
  label_column?: string
  value_column?: string
}

export interface DashboardVariable {
  name: string
  label?: string
  type: DashboardVariableType
  default?: unknown
  required?: boolean
  options?: DashboardVariableOptions
  depends_on?: string[]
}

export interface WidgetQueryResult {
  outputs: Output[]
  metrics: { query_time_ms?: number; total_time_ms?: number }
  routing?: { warehouse_id?: string; connector_name?: string; ch_user?: string }
  cached: boolean
  cache_expires_at?: string
}
```

Update `Dashboard.settings` to `{ auto_refresh_seconds?: number; grid_cols?: number; query_cache_seconds?: number; public_live?: boolean; variables?: DashboardVariable[] }`.

Update `Widget`:

```ts
export interface Widget {
  id: string
  dashboard_id: string
  notebook_id?: string | null
  cell_id?: string | null
  connector_id?: string | null
  query?: string | null
  language?: string
  type: 'chart' | 'table' | 'text' | 'metric'
  layout: { row: number; col: number; width: number; height: number }
  config: Record<string, unknown>
  created_at: string
}
```

**Step 3: Verify + commit**

```bash
cd web && npx tsc --noEmit
git add web/src/api/client.ts web/src/types/index.ts
git commit -m "feat(web): add dashboard variable and query widget types"
```

---

### Task 9: Variables context with option loading, URL and localStorage sync

**Files:**
- Create: `web/src/contexts/DashboardVariablesContext.tsx`
- Delete: `web/src/contexts/DashboardParamsContext.tsx` (after Task 11 removes its usages)
- Create: `web/src/contexts/DashboardVariablesContext.test.tsx`

**Step 1: Write failing tests**

Cover: defaults applied; URL `?region=EMEA` wins over localStorage; `setValue` persists to localStorage; query-backed options load via the endpoint with parent values and expose loading/error state. Use Vitest + Testing Library with a `renderHook` inside a `MemoryRouter` (needed for `useSearchParams`) and MSW (`web/src/test/` already has MSW patterns — mirror `ConnectorSelector.test.tsx`).

**Step 2: Implement the context**

```tsx
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api/client'
import type { DashboardVariable, DashboardVariableOption } from '../types'

export type VariableValue = string | number | boolean | string[]

interface VariableOptionsState {
  options: DashboardVariableOption[]
  loading: boolean
  error: string | null
  reload: () => void
}

interface DashboardVariablesContextValue {
  variables: DashboardVariable[]
  values: Record<string, unknown>
  setValue: (name: string, value: unknown) => void
  setValues: (next: Record<string, unknown>) => void
  optionState: Record<string, VariableOptionsState>
}

const Ctx = createContext<DashboardVariablesContextValue>({
  variables: [], values: {}, setValue: () => {}, setValues: () => {}, optionState: {},
})

function storageKey(dashboardId: string) { return `aether_dashvars_${dashboardId}` }

function parseSearchValue(v: DashboardVariable, search: URLSearchParams): unknown {
  const all = search.getAll(v.name)
  if (!all.length) return undefined
  switch (v.type) {
    case 'multi_select': return all
    case 'number': { const n = Number(all[0]); return Number.isFinite(n) ? n : undefined }
    case 'boolean': return all[0] === 'true' ? true : all[0] === 'false' ? false : undefined
    case 'date_range': return all[0].includes(',') ? all[0].split(',', 2) : undefined
    default: return all[0]
  }
}

function loadInitial(dashboardId: string, variables: DashboardVariable[], search: URLSearchParams): Record<string, unknown> {
  let stored: Record<string, unknown> = {}
  try { stored = JSON.parse(localStorage.getItem(storageKey(dashboardId)) ?? '{}') } catch { stored = {} }
  const out: Record<string, unknown> = {}
  for (const v of variables) {
    const fromUrl = parseSearchValue(v, search)
    if (fromUrl !== undefined) { out[v.name] = fromUrl; continue }
    if (stored[v.name] !== undefined) { out[v.name] = stored[v.name]; continue }
    if (v.default !== undefined) out[v.name] = v.default
  }
  return out
}

function serializeValue(v: DashboardVariable, value: unknown): string[] {
  if (value === undefined || value === null) return []
  switch (v.type) {
    case 'multi_select': return Array.isArray(value) ? value.map(String) : []
    case 'date_range': return Array.isArray(value) && value.length === 2 ? [`${value[0]},${value[1]}`] : []
    case 'boolean': return [String(value)]
    case 'number': return [String(value)]
    default: return [String(value)]
  }
}

export function DashboardVariablesProvider({ dashboardId, variables, children }: {
  dashboardId: string
  variables: DashboardVariable[]
  children: React.ReactNode
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [values, setValuesState] = useState<Record<string, unknown>>(() => loadInitial(dashboardId, variables, searchParams))
  const [optionState, setOptionState] = useState<Record<string, VariableOptionsState>>({})
  const valuesRef = useRef(values)
  valuesRef.current = values

  const persist = useCallback((next: Record<string, unknown>) => {
    try { localStorage.setItem(storageKey(dashboardId), JSON.stringify(next)) } catch { /* ignore quota */ }
    const params = new URLSearchParams()
    for (const v of variables) {
      const serialized = serializeValue(v, next[v.name])
      const isDefault = JSON.stringify(next[v.name]) === JSON.stringify(v.default)
      if (!isDefault) serialized.forEach(s => params.append(v.name, s))
    }
    setSearchParams(params, { replace: true })
  }, [dashboardId, variables, setSearchParams])

  const setValue = useCallback((name: string, value: unknown) => {
    setValuesState(prev => {
      const next = { ...prev, [name]: value }
      persist(next)
      return next
    })
  }, [persist])

  const setValues = useCallback((next: Record<string, unknown>) => {
    setValuesState(next); persist(next)
  }, [persist])

  const loadOptions = useCallback(async (name: string) => {
    setOptionState(prev => ({ ...prev, [name]: { ...(prev[name] ?? { options: [] }), loading: true, error: null } }))
    try {
      const resp = await api.post<{ options: DashboardVariableOption[] }>(
        `/api/v1/dashboards/${dashboardId}/variables/${name}/options`,
        { variables: valuesRef.current },
      )
      setOptionState(prev => ({ ...prev, [name]: { options: resp.options, loading: false, error: null, reload: () => { void loadOptions(name) } } }))
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to load options'
      setOptionState(prev => ({ ...prev, [name]: { options: [], loading: false, error: message, reload: () => { void loadOptions(name) } } }))
    }
  }, [dashboardId])

  // Reload query-backed options on mount and whenever a dependency changes.
  const depSignature = JSON.stringify(variables.map(v => ({
    name: v.name,
    deps: (v.depends_on ?? []).map(d => [d, values[d]]),
  })))
  useEffect(() => {
    const timer = setTimeout(() => {
      for (const v of variables) {
        if (v.options?.mode === 'query') void loadOptions(v.name)
      }
    }, 300)
    return () => clearTimeout(timer)
  }, [depSignature]) // eslint-disable-line react-hooks/exhaustive-deps

  // Seed static options so controls render immediately.
  const optionStateWithStatic = useMemo(() => {
    const out = { ...optionState }
    for (const v of variables) {
      if (v.options?.mode === 'static' && !out[v.name]) {
        out[v.name] = { options: v.options.values ?? [], loading: false, error: null, reload: () => {} }
      }
    }
    return out
  }, [optionState, variables])

  return (
    <Ctx.Provider value={{ variables, values, setValue, setValues, optionState: optionStateWithStatic }}>
      {children}
    </Ctx.Provider>
  )
}

export function useDashboardVariables() { return useContext(Ctx) }
```

**Step 3: Run tests + commit**

```bash
cd web && npx vitest run --project=default src/contexts/DashboardVariablesContext.test.tsx
git add web/src/contexts/
git commit -m "feat(web): add dashboard variables context"
```

---

### Task 10: Variable bar component

**Files:**
- Create: `web/src/components/DashboardVariableBar.tsx`
- Create: `web/src/components/DashboardVariableBar.test.tsx`

**Step 1: Write failing tests** — renders one control per variable type (text input, number input, checkbox/select, date input, two date inputs for date_range, single select populated from `optionState`, multi select), shows loading/error with retry for query options, calls `setValue` on change.

**Step 2: Implement**

Render a wrapping flex row (same card styling as the current `inputRow` in `DashboardPage.tsx:770-781`) with one labeled control per variable from `useDashboardVariables()`. Control behavior:

- `text` / `number` → `<input>` with local draft state committed on Enter/blur for text (debounced 300ms is fine) and immediately for number.
- `boolean` → `<input type="checkbox">`.
- `date` → `<input type="date">`.
- `date_range` → two date inputs bound to `[start, end]` array.
- `single_select` → `<select>` from `optionState[name].options`; include an empty option when `!required`.
- `multi_select` → multi-select `<select multiple>` (reuse the old InputWidget implementation from `DashboardPage.tsx:110-130`) + hint text.

Each control shows `optionState[name]?.error` inline with a Retry button calling `reload`.

**Step 3: Run tests + commit**

```bash
cd web && npx vitest run --project=default src/components/DashboardVariableBar.test.tsx
git add web/src/components/DashboardVariableBar.tsx web/src/components/DashboardVariableBar.test.tsx
git commit -m "feat(web): add dashboard variable bar"
```

---

### Task 11: `useWidgetQuery` hook + query widget rendering in the viewer

**Files:**
- Create: `web/src/hooks/useWidgetQuery.ts`
- Create: `web/src/components/QueryDataWidget.tsx`
- Modify: `web/src/pages/DashboardPage.tsx`
- Delete: `web/src/contexts/DashboardParamsContext.tsx`
- Create: `web/src/hooks/useWidgetQuery.test.tsx`

**Step 1: Implement the hook**

```ts
import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Widget, WidgetQueryResult } from '../types'

function canonicalValues(values: Record<string, unknown>): string {
  return JSON.stringify(Object.keys(values).sort().map(k => [k, values[k]]))
}

export function useWidgetQuery(opts: {
  dashboardId: string
  widget: Widget
  values: Record<string, unknown>
  enabled: boolean
}) {
  const bypassRef = useRef(false)
  const query = useQuery({
    queryKey: ['widget-query', opts.dashboardId, opts.widget.id, canonicalValues(opts.values)],
    queryFn: ({ signal }) => {
      const bypass = bypassRef.current
      bypassRef.current = false
      return api.post<WidgetQueryResult>(
        `/api/v1/dashboards/${opts.dashboardId}/execute`,
        { widget_id: opts.widget.id, variables: opts.values, bypass_cache: bypass },
        { signal },
      )
    },
    enabled: opts.enabled && !!opts.widget.connector_id && !!opts.widget.query,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
  const refresh = () => { bypassRef.current = true; void query.refetch() }
  return { ...query, refresh }
}
```

**Step 2: Implement `QueryDataWidget`**

Props: `{ dashboardId, widget, canViewWithData }`. Behavior:

- `!canViewWithData` → muted card "You need view_with_data access to see data".
- Uses `useDashboardVariables()` for `values`; hook enabled for `widget.connector_id && widget.query`.
- Loading first time → empty card (or reuse `Skeleton`).
- Error:
  - `ApiError.status === 409` → render `ServiceChoiceDialog` using `serviceChoicesFromError(err)` and `setServicePreference` from `web/src/api/warehouses.ts` (mirror the NotebookPage usage; find it with `rg -n "ServiceChoiceDialog" web/src/pages/NotebookPage.tsx`), then `refresh()` on success.
  - `ApiError.status === 403` → permission message.
  - otherwise → error message + Retry button calling `refresh()`.
- Success → `<OutputRenderer outputs={result.outputs} fixedView={widget.type === 'chart' ? 'chart' : 'table'} chartConfig={normalizeChartConfig(widget.config)} ... />` plus a footer showing `Executed {age}` and a refresh button (`widget.type === 'chart'` renders chart, everything else table).

**Step 3: Rewire `DashboardPage.tsx`**

- Wrap with `DashboardVariablesProvider dashboardId={dashboard.id} variables={dashboard.settings?.variables ?? []}` (replace `DashboardParamsProvider`).
- Delete `INPUT_WIDGET_TYPES`, `InputWidget`, and the input row; render `<DashboardVariableBar />` in its place when `variables.length > 0`.
- Split widgets: query widgets (`w.connector_id && w.query`) use `QueryDataWidget`; cell widgets keep the existing `QueryWidget` (rename to `CellDataWidget` for clarity).
- `executeAllWidgets`: for cell widgets run the existing cell execute calls; for query widgets call each widget's `refresh()` — simplest wiring is a `DashboardContent`-level `refreshAll` counter passed down to `QueryDataWidget` via prop `refreshNonce`, with an effect in the widget calling `refresh()` when the nonce changes. Auto-refresh reuses the same path.
- Remove the dead `{{` debounce stub (lines 207-225).

**Step 4: Run checks + commit**

```bash
cd web && npx tsc --noEmit && npx vitest run --project=default src/hooks/useWidgetQuery.test.tsx
git add web/src/hooks/useWidgetQuery.ts web/src/hooks/useWidgetQuery.test.tsx web/src/components/QueryDataWidget.tsx web/src/pages/DashboardPage.tsx
git rm web/src/contexts/DashboardParamsContext.tsx
git commit -m "feat(web): render live query widgets on dashboard view"
```

---

### Task 12: Editable SQL editor component + widget config drawer

**Files:**
- Create: `web/src/components/SqlEditor.tsx`
- Create: `web/src/components/WidgetConfigDrawer.tsx`
- Create: `web/src/components/WidgetConfigDrawer.test.tsx`
- Modify: `web/src/pages/DashboardEditorPage.tsx`

**Step 1: `SqlEditor`**

A minimal controlled CodeMirror editor mirroring `ReadOnlyCode.tsx` but editable:

```tsx
export function SqlEditor({ value, onChange, minHeight = 160, connectorType }: {
  value: string
  onChange: (value: string) => void
  minHeight?: number
  connectorType?: string
}) {
  // useEffect creates EditorView with:
  //   languageExtension = sql({ dialect: connectorType === 'postgres' ? PostgreSQL : MySQL })
  //   syntaxHighlighting(sqlHighlight)
  //   EditorView.updateListener.of(u => { if (u.docChanged) onChangeRef.current(u.state.doc.toString()) })
  //   the same font/theme extensions as ReadOnlyCode
  // useEffect on external `value` change replaces the doc only when different
  //   (view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } }))
  // cleanup destroys the view
}
```

Avoid recreating the view on each keystroke (keep `onChange` in a ref).

**Step 2: `WidgetConfigDrawer`**

Props: `{ dashboardId, dashboard, widget, onClose, onSaved }`. A fixed right-side panel (width 420, full height, `var(--bg-card)`, `borderLeft: 1px solid var(--border)`), with:

- **Source section**: read-only label "Query widget" or "Notebook cell"; for query widgets:
  - `ConnectorSelector` (import from `web/src/components/ConnectorSelector.tsx`) bound to a local `connectorId` state.
  - `<SqlEditor>` bound to `query` state; save on blur/debounce (600ms) via `PUT /api/v1/dashboards/{id}/widgets/{widgetId}` with `{ connector_id, query }`.
  - **Detected variables**: `Array.from(query.matchAll(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g))` deduped; each token missing from `dashboard.settings.variables` gets a "Define variable" button that calls `onDefineVariable(name)` (opens the variables panel from Task 13 prefilled).
  - **Run** button → `POST /api/v1/dashboards/{id}/execute` with no variables (defaults) → render `OutputRenderer` preview below; show ApiError messages (409 → ServiceChoiceDialog as in Task 11).
- **Visualization section**: type `<select>` (`chart|table|text|metric`) and the existing chart config controls via `OutputRenderer`'s chart config props (reuse `saveWidgetConfig` mutation already present in the editor page).

**Step 3: Wire into `DashboardEditorPage.tsx`**

- Add `const [editingWidget, setEditingWidget] = useState<Widget | null>(null)`.
- Pencil button opens the drawer instead of the picker (keep the existing picker for notebook-cell widgets; extend the Add Widget picker with a **Source** choice: "Notebook cell" (existing flow) or "Query" (connector + SQL + type → `POST /widgets` with `connector_id`, `query`, `language: 'sql'`, `config: {}`)).
- Render `<WidgetConfigDrawer>` when `editingWidget` is set; on save invalidate `['dashboard', id]`.
- Render query widgets in the grid with a read-only preview (`OutputRenderer` from widget config, no execution) so the editor is useful without running.

**Step 4: Verify + commit**

```bash
cd web && npx tsc --noEmit && npx vitest run --project=default src/components/WidgetConfigDrawer.test.tsx
git add web/src/components/SqlEditor.tsx web/src/components/WidgetConfigDrawer.tsx web/src/components/WidgetConfigDrawer.test.tsx web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): add dashboard widget config drawer with SQL editor"
```

---

### Task 13: Variables panel + convert action

**Files:**
- Create: `web/src/components/DashboardVariablesPanel.tsx`
- Create: `web/src/components/DashboardVariablesPanel.test.tsx`
- Modify: `web/src/pages/DashboardEditorPage.tsx`
- Modify: `web/src/types/index.ts` (only if helpers are needed)

**Step 1: Variables panel**

A modal/panel opened from the editor header ("Variables" button) managing `dashboard.settings.variables`:

- List rows with inline editing: name, label, type (`select`), default (control depends on type), required checkbox, remove.
- Per select-type variable: options mode (`static` / `query`); static → textarea of `label=value` lines (or plain values); query → `ConnectorSelector` + `SqlEditor` + label/value column inputs.
- `depends_on` multi-select populated from other variable names.
- Validation before save: unique names, valid identifier regex `/^[a-zA-Z0-9_-]+$/`, query mode requires connector + SQL.
- Save → `PUT /api/v1/dashboards/{id}` with `{ settings: { ...dashboard.settings, variables } }`, then invalidate `['dashboard', id]`.

**Step 2: Header button + convert action**

- Add a `Variables` button in the editor header next to Cols.
- For cell-linked widgets (pencil button or drawer), add **Convert to query widget** → `POST /api/v1/dashboards/{id}/widgets/{widget_id}/convert-to-query`; on success invalidate `['dashboard', id]` and show a toast/banner on failure (e.g. unknown slug).

**Step 3: Verify + commit**

```bash
cd web && npx tsc --noEmit && npx vitest run --project=default src/components/DashboardVariablesPanel.test.tsx
git add web/src/components/DashboardVariablesPanel.tsx web/src/components/DashboardVariablesPanel.test.tsx web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): add dashboard variables editor and convert action"
```

---

### Task 14: Public dashboard live mode

**Files:**
- Modify: `web/src/pages/PublicDashboardPage.tsx`
- Modify: `web/src/pages/DashboardEditorPage.tsx` (sharing settings)
- Create: `web/src/components/PublicVariableBar.tsx` (or reuse provider with token-scoped API)

**Step 1: Public page**

- If `dashboard.settings.public_live`, wrap content in a variables provider that calls the public endpoints (`/api/v1/public/{token}/variables/{name}/options`, `/api/v1/public/{token}/execute`). Simplest: extend `DashboardVariablesProvider` and `useWidgetQuery` with an optional `endpointBase` prop/option (`/api/v1/dashboards/{id}` vs `/api/v1/public/{token}`) and a `publicToken` prop; render the variable bar and `QueryDataWidget` with it.
- If not `public_live`, hide query widgets and keep rendering cached cell widget outputs exactly as today.

**Step 2: Editor toggle**

Add a "Public live queries" checkbox to the editor header area or the Variables panel: writes `settings.public_live` through the normal dashboard PUT.

**Step 3: Verify + commit**

```bash
cd web && npx tsc --noEmit
git add web/src/pages/PublicDashboardPage.tsx web/src/pages/DashboardEditorPage.tsx web/src/contexts/DashboardVariablesContext.tsx web/src/hooks/useWidgetQuery.ts
git commit -m "feat(web): support public live dashboards"
```

---

## Phase 4 — E2E, docs, verification

### Task 15: E2E and real-browser validation

**Files:**
- Create: `e2e/dashboard-filters.spec.ts`

**Step 1: Write the E2E spec**

Model the flow on `e2e/dashboard.spec.ts` and `e2e/helpers.ts` (`registerAndOnboard`). Cover:

1. Create dashboard via API with a variable (`region` text, default `EMEA`) and a query widget against a new connector that runs `SELECT '{{region}}' AS region` (use `POST /api/v1/connectors` with the dev Postgres credentials used by `createConnector`).
2. Navigate to `/dashboards/{id}/view`; expect the variable bar visible and the widget to render `EMEA`.
3. Change the filter to `AMER`; expect the widget to update to `AMER`.
4. Reload the page with `?region=APAC`; expect the widget to render `APAC`.
5. Public: enable `public_live`, share, open `/public/dashboards/{token}` in a clean context; expect the variable bar to work (or be hidden when `public_live` is off).

**Step 2: Run E2E**

```bash
docker compose -f docker-compose.dev.yml up -d
cd web && npx playwright test --config=e2e/playwright.config.ts e2e/dashboard-filters.spec.ts
```

Expected: PASS. If a screenshot snapshot is needed, follow the existing `--update-snapshots` pattern.

**Step 3: Agent-browser validation (mandatory for UI changes)**

```bash
agent-browser open http://localhost:5173
agent-browser snapshot -i
# log in as nova@heaven-labs.com / nova123, open a dashboard, edit a query widget,
# define a variable, change a filter, then:
agent-browser errors
agent-browser screenshot
```

Verify no console errors and that loading/error/empty states render correctly.

**Step 4: Commit**

```bash
git add e2e/dashboard-filters.spec.ts
git commit -m "test(e2e): cover dashboard variables and live queries"
```

---

### Task 16: Swagger, docs, full verification

**Files:**
- Modify: `internal/api/docs/` (generated)
- Modify: `AGENTS.md` (architecture note)

**Step 1: Regenerate Swagger**

```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```

**Step 2: Update `AGENTS.md`**

Add a short paragraph after the "Key Patterns" execution-routing section:

```markdown
**Dashboard query widgets**: widgets may own SQL + a connector (`widgets.connector_id/query/language`) instead of
referencing a notebook cell. Dashboard variables live in `dashboards.settings.variables` and are interpolated
server-side with type-aware escaping (`internal/dashboard`), never client-side. `POST /dashboards/{id}/execute`
runs one widget as the viewer via the shared `openQuery` helper and caches successful results in Redis for
`settings.query_cache_seconds` (default 30, 0 disables). Public dashboards run embedded queries only when
`settings.public_live` is true, as the dashboard creator, rate-limited per token+IP.
```

**Step 3: Full verification**

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd ../relay && npm run build
task test:e2e
```

Expected: all green. Fix any lint/type errors before proceeding.

**Step 4: Commit**

```bash
git add internal/api/docs AGENTS.md
git commit -m "docs: document dashboard query widgets and filter variables"
```

---

## Execution notes

- Commit after every task; squash-merge the PR when approved (repo workflow).
- The riskiest tasks are 3 (refactor) and 4 (execution); run the full `go test ./internal/api/ -timeout 3m` after each to catch regressions in warehouse routing and audit behavior.
- Keep `internal/executor.ResolveParams` untouched: notebooks keep raw interpolation; dashboards use `internal/dashboard.Interpolate`.
- Do not cache failed executions or option-query errors.
- If `executor.ResultSet` cannot round-trip through JSON (unexported fields), cache a small local struct mirroring its JSON shape instead of changing the executor package.
