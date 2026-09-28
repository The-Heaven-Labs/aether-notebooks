package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/executor"
)

func TestCompileAndMatchHiddenPatterns(t *testing.T) {
	patterns := compileHiddenPatterns([]string{`^analytics\._tmp`, `_scratch$`, `^scratchy$`, `(`})
	require.Len(t, patterns, 3, "an invalid stored pattern is skipped")

	require.True(t, matchesHiddenPattern(patterns, "analytics", "_tmp_123"))
	require.True(t, matchesHiddenPattern(patterns, "raw", "my_scratch"))
	require.False(t, matchesHiddenPattern(patterns, "analytics", "events"))
	require.True(t, matchesHiddenPattern(patterns, "", "scratchy"),
		"an empty database matches the table name alone")
}

func TestFilterVisibleSchemaTables(t *testing.T) {
	tables := []executor.TableInfo{
		{Schema: "analytics", Name: "events"},
		{Schema: "analytics", Name: "_tmp_scratch"},
		{Schema: "raw", Name: "clicks"},
	}
	patterns := compileHiddenPatterns([]string{`_tmp`})

	// Admin view: patterns drop matched ungranted tables.
	got := filterVisibleSchemaTables(tables, patterns, nil, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "events"}, {Schema: "raw", Name: "clicks"}}, got)

	// Granted tables survive a pattern match.
	protected := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, nil, protected)
	require.Len(t, got, 3)

	// An effective-grant allowlist wins over everything else.
	allowed := map[tableKey]struct{}{{Database: "raw", Table: "clicks"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, allowed, allowed)
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)

	// A pattern-matched granted table stays visible while it is protected:
	// patterns can never hide granted access.
	grantedMatched := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, grantedMatched, grantedMatched)
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "_tmp_scratch"}}, got)

	// allowed and patternProtected are distinct parameters: an allowed
	// pattern-matched table is dropped when patternProtected is empty...
	got = filterVisibleSchemaTables(tables, patterns, grantedMatched, map[tableKey]struct{}{})
	require.Empty(t, got)

	// ...while an allowed table that matches no pattern survives an empty
	// patternProtected.
	got = filterVisibleSchemaTables(tables, patterns, allowed, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)
}

func TestConnectorSchemaHiddenPatterns(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()

	database := "aether_vis_" + uuid.NewString()[:8]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	for _, table := range []string{"events", "_tmp_scratch", "daily_revenue"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}

	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)

	token, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+fx.connectorID.String()+"/schema", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var schema executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got := map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	require.True(t, got[database+".events"], "unmatched tables survive")
	require.True(t, got[database+".daily_revenue"])
	require.False(t, got[database+"._tmp_scratch"], "pattern-matched ungranted tables are hidden")

	// The schema-read snapshot is pattern-filtered too.
	var hiddenRows int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = '_tmp_scratch'`,
		fx.connectorID.String(), database).Scan(&hiddenRows))
	require.Zero(t, hiddenRows)

	// ...and the surviving table was actually written: the filtered catalog is
	// a real snapshot, not an empty no-op.
	var visibleRows int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = 'events'`,
		fx.connectorID.String(), database).Scan(&visibleRows))
	require.Equal(t, 1, visibleRows)

	// A granted table that also matches stays visible.
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', $3, '_tmp_scratch')`,
		fx.orgID.String(), fx.warehouseID.String(), database)
	require.NoError(t, err)

	rec = warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+fx.connectorID.String()+"/schema", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got = map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	require.True(t, got[database+"._tmp_scratch"], "granted tables stay visible even when matched")
}

// With the kill switch off, a non-admin viewing a warehouse-linked service
// connector must still see a granted table that matches a hidden pattern:
// patterns hide only ungranted tables, never existing access. setupWarehouseFixture
// builds a dedicated server, so flipping the kill switch is safe here.
func TestConnectorSchemaHiddenPatternsKillSwitchOff(t *testing.T) {
	fx := setupWarehouseFixture(t)
	defer fx.s.SetCHTablePermissions(true)
	fx.s.SetCHTablePermissions(false)
	ctx := context.Background()

	database := "aether_visoff_" + uuid.NewString()[:8]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	quotedTable, err := chaccess.QuoteObjectIdent("_tmp_scratch")
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quotedTable+
		" (id UInt64) ENGINE = MergeTree ORDER BY id"))

	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, $4, '_tmp_scratch')`,
		fx.orgID.String(), fx.warehouseID.String(), fx.userID.String(), database)
	require.NoError(t, err)

	serviceID := insertClickHouseService(t, fx.s, fx.orgID, fx.connectorID, "Visibility Off Service", &fx.warehouseID)
	grantConnectorUse(t, fx.s, fx.orgID, fx.userID, serviceID)

	token, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "non-admin")
	require.NoError(t, err)
	rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceID.String()+"/schema", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var schema executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got := map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	require.True(t, got[database+"._tmp_scratch"],
		"a granted pattern-matched table must stay visible with the kill switch off")
}
