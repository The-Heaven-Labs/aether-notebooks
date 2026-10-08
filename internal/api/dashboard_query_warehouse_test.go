package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// resolveWidgetConnector decides which connector serves a dashboard widget:
// the viewer's dashboard-selector choice wins only inside the widget's own
// warehouse; otherwise the widget's saved connector serves.
func TestResolveWidgetConnector(t *testing.T) {
	ctx := context.Background()
	fx := setupExecutionTargetFixture(t)

	// A second, ready warehouse with one service: selections pointing at it
	// must never redirect a widget hosted in fx.warehouseID.
	otherWH := uuid.New()
	_, err := fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouses (id, org_id, name, sync_status)
		VALUES ($1, $2, $3, 'ready')`,
		otherWH.String(), fx.orgID.String(), "Other Warehouse")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM warehouses WHERE id = $1`, otherWH.String()); err != nil {
			t.Logf("cleanup other warehouse: %v", err)
		}
	})
	otherConn := insertClickHouseService(t, fx.s, fx.orgID, fx.provisionerID, "Other Warehouse Service", &otherWH)

	cases := []struct {
		name          string
		widgetConn    string
		viewerConn    string
		wantConnector string
		wantErr       bool
	}{
		{"same warehouse serves the viewer selection", fx.connA.String(), fx.connB.String(), fx.connB.String(), false},
		{"cross warehouse keeps the widget connector", fx.connA.String(), otherConn.String(), fx.connA.String(), false},
		{"unknown viewer connector keeps the widget connector", fx.connA.String(), uuid.NewString(), fx.connA.String(), false},
		{"empty viewer keeps the widget connector", fx.connA.String(), "", fx.connA.String(), false},
		{"viewer equal to widget keeps the widget connector", fx.connA.String(), fx.connA.String(), fx.connA.String(), false},
		{"missing widget connector errors", uuid.NewString(), fx.connB.String(), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fx.s.resolveWidgetConnector(ctx, fx.orgID.String(), tc.widgetConn, tc.viewerConn)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantConnector, got)
		})
	}
}

// seedDashboardQueryWidget inserts a dashboard plus one SQL query widget on
// connectorID and grants the fixture user view_with_data, since the warehouse
// fixtures live in package api and cannot use the api_test HTTP helpers.
func seedDashboardQueryWidget(t *testing.T, s *Server, orgID, userID, connectorID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	dashID := uuid.New()
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO dashboards (id, org_id, title, settings, created_by)
		VALUES ($1, $2, $3, '{}'::jsonb, $4)`,
		dashID.String(), orgID.String(), "Viewer Connector Dashboard", userID.String())
	require.NoError(t, err)

	widgetID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO widgets (id, dashboard_id, connector_id, query, language, type, layout, config)
		VALUES ($1, $2, $3, $4, 'sql', 'table', $5::jsonb, '{}'::jsonb)`,
		widgetID.String(), dashID.String(), connectorID.String(), "SELECT 1 AS x",
		`{"row":0,"col":0,"width":6,"height":6}`)
	require.NoError(t, err)

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'dashboard', $2::uuid, 'user', $3, ARRAY['view','view_with_data'])`,
		orgID.String(), dashID.String(), userID.String())
	require.NoError(t, err)

	// Run before the fixture cleanup (LIFO): dashboards.created_by references
	// the fixture user, so the dashboard must go before the user does.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM dashboards WHERE id = $1`, dashID.String()); err != nil {
			t.Logf("cleanup dashboard: %v", err)
		}
	})

	return dashID, widgetID
}

// executeDashboardQueryWidget drives the full HTTP handler, including auth
// middleware, with a token for the fixture user.
func executeDashboardQueryWidget(t *testing.T, s *Server, userID, orgID, dashID uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	token, err := s.jwt.Issue(userID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/dashboards/"+dashID.String()+"/execute", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// dashboardQueryAuditConnectorID reads the served connector recorded on the
// latest dashboard.query audit entry. The audit write is synchronous, so the
// row is readable right after the response.
func dashboardQueryAuditConnectorID(t *testing.T, s *Server, orgID, dashID uuid.UUID) string {
	t.Helper()
	var metaJSON []byte
	require.NoError(t, s.db.Pool.QueryRow(context.Background(), `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'dashboard.query' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`,
		orgID.String(), dashID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	connectorID, _ := meta["connector_id"].(string)
	return connectorID
}

// A viewer selection living in the widget connector's warehouse serves the
// widget: the per-viewer dashboard selector overrides the saved connector.
func TestDashboardQueryViewerConnectorSameWarehouseWins(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	fx.grantConnectorUse(t, fx.connB)

	dashID, widgetID := seedDashboardQueryWidget(t, fx.s, fx.orgID, fx.userID, fx.connA)

	rec := executeDashboardQueryWidget(t, fx.s, fx.userID, fx.orgID, dashID, map[string]any{
		"widget_id":    widgetID.String(),
		"connector_id": fx.connB.String(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, fx.connB.String(), dashboardQueryAuditConnectorID(t, fx.s, fx.orgID, dashID),
		"the same-warehouse viewer selection must serve the widget")
}

// A viewer selection from another warehouse is ignored: the widget's saved
// connector serves, since the selection cannot reach the widget's tables.
func TestDashboardQueryViewerConnectorCrossWarehouseIgnored(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)

	otherWH := uuid.New()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouses (id, org_id, name, sync_status)
		VALUES ($1, $2, $3, 'ready')`,
		otherWH.String(), fx.orgID.String(), "Viewer Selection Other Warehouse")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM warehouses WHERE id = $1`, otherWH.String()); err != nil {
			t.Logf("cleanup other warehouse: %v", err)
		}
	})
	otherConn := insertClickHouseService(t, fx.s, fx.orgID, fx.connA, "Other Warehouse Service", &otherWH)

	dashID, widgetID := seedDashboardQueryWidget(t, fx.s, fx.orgID, fx.userID, fx.connA)

	rec := executeDashboardQueryWidget(t, fx.s, fx.userID, fx.orgID, dashID, map[string]any{
		"widget_id":    widgetID.String(),
		"connector_id": otherConn.String(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, fx.connA.String(), dashboardQueryAuditConnectorID(t, fx.s, fx.orgID, dashID),
		"a cross-warehouse viewer selection must be ignored")
}
