package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// createNotebookForFixtureUser drives the real POST /api/v1/notebooks handler
// as the fixture user and returns the created notebook's connector_id. The
// token carries the user's current org role, and no admin-mode header is sent,
// so ACLs stay load-bearing exactly as they are for a browser session.
//
// The notebook cleanup is registered after the fixture's own cleanup, so it
// runs first (LIFO): notebooks.created_by references the fixture user.
func createNotebookForFixtureUser(t *testing.T, fx *executionTargetFixture, body string) string {
	t.Helper()

	var role string
	require.NoError(t, fx.s.db.Pool.QueryRow(context.Background(),
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
		fx.orgID.String(), fx.userID.String()).Scan(&role))

	token, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), role)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notebooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create notebook response: %s", rec.Body.String())

	var created struct {
		ID          string `json:"id"`
		ConnectorID string `json:"connector_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotEmpty(t, created.ID)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM notebooks WHERE id = $1`, created.ID); err != nil {
			t.Logf("cleanup notebook: %v", err)
		}
	})

	return created.ConnectorID
}

// addSecondWarehouse inserts a second ready warehouse in the fixture org with
// one service connector, for multi-warehouse preference cases. The fixture's
// org cleanup cascades both rows, so no extra cleanup is registered.
func addSecondWarehouse(t *testing.T, fx *executionTargetFixture) (uuid.UUID, uuid.UUID) {
	t.Helper()
	secondWH := uuid.New()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouses (id, org_id, name, sync_status)
		VALUES ($1, $2, $3, 'ready')`,
		secondWH.String(), fx.orgID.String(), "Second Warehouse")
	require.NoError(t, err)
	serviceID := insertClickHouseService(t, fx.s, fx.orgID, fx.provisionerID, "Second Warehouse Service", &secondWH)
	return secondWH, serviceID
}

// TestCreateNotebookSeedsConnectorFromSolePreference pins the seeding rule: a
// creator with exactly one live preference naming a service they may still use
// gets that service on a new notebook.
func TestCreateNotebookSeedsConnectorFromSolePreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	require.Equal(t, fx.connB.String(), createNotebookForFixtureUser(t, fx, `{"title":"Sole preference"}`))
}

// TestCreateNotebookNoPreferenceLeavesConnectorEmpty pins that no preference
// leaves the notebook connector unset (the fixture org has no org-default
// connector).
func TestCreateNotebookNoPreferenceLeavesConnectorEmpty(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	// two usable services, no preference recorded

	require.Empty(t, createNotebookForFixtureUser(t, fx, `{"title":"No preference"}`))
}

// TestCreateNotebookSeveralPreferencesLeaveConnectorEmpty pins the structural
// "exactly one" rule: live preferences in two warehouses yield two candidates
// and no seed, even though both name services the creator may use.
func TestCreateNotebookSeveralPreferencesLeaveConnectorEmpty(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	secondWH, secondConn := addSecondWarehouse(t, fx)
	fx.grantUse(t, fx.connB)
	fx.grantUse(t, secondConn)
	fx.prefer(t, fx.connB)
	preferWarehouseService(t, fx.s, fx.userID, secondWH, secondConn)

	require.Empty(t, createNotebookForFixtureUser(t, fx, `{"title":"Several preferences"}`))
}

// TestCreateNotebookProvisionerPreferenceDoesNotSeed pins that a stale
// preference naming the warehouse provisioner is excluded while the admin
// override is off, exactly as execution fails closed; with the override on,
// the provisioner is a selectable service and seeds like any other.
func TestCreateNotebookProvisionerPreferenceDoesNotSeed(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.provisionerID)
	fx.prefer(t, fx.provisionerID)

	require.Empty(t, createNotebookForFixtureUser(t, fx, `{"title":"Provisioner preference"}`))

	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)
	require.Equal(t, fx.provisionerID.String(),
		createNotebookForFixtureUser(t, fx, `{"title":"Provisioner override"}`))
}

// TestCreateNotebookPreferenceBeatsOrgDefault pins that a resolvable
// preference outranks the org-default connector.
func TestCreateNotebookPreferenceBeatsOrgDefault(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET is_default = true WHERE id = $1`, fx.connA.String())
	require.NoError(t, err)

	require.Equal(t, fx.connB.String(), createNotebookForFixtureUser(t, fx, `{"title":"Preference beats org default"}`))
}

// TestCreateNotebookMovedConnectorDoesNotSeed pins that a preference whose
// connector was moved to another warehouse no longer seeds.
func TestCreateNotebookMovedConnectorDoesNotSeed(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	secondWH, _ := addSecondWarehouse(t, fx)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET warehouse_id = $1 WHERE id = $2`,
		secondWH.String(), fx.connB.String())
	require.NoError(t, err)

	require.Empty(t, createNotebookForFixtureUser(t, fx, `{"title":"Moved connector"}`))
}

// TestCreateNotebookExplicitConnectorBeatsPreference pins that a connector_id
// in the create request always wins over preference seeding.
func TestCreateNotebookExplicitConnectorBeatsPreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	body := fmt.Sprintf(`{"title":"Explicit connector","connector_id":%q}`, fx.connA.String())
	require.Equal(t, fx.connA.String(), createNotebookForFixtureUser(t, fx, body))
}

// TestCreateNotebookRevokedPreferenceDoesNotSeed pins that a stale preference
// (access revoked after it was recorded) never seeds a new notebook.
func TestCreateNotebookRevokedPreferenceDoesNotSeed(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)
	fx.revokeUse(t, fx.connB)

	require.Empty(t, createNotebookForFixtureUser(t, fx, `{"title":"Revoked preference"}`))
}

// TestCreateNotebookNotReadyWarehouseDoesNotSeed pins that a preference in a
// warehouse that is not ready never seeds a new notebook.
func TestCreateNotebookNotReadyWarehouseDoesNotSeed(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)
	fx.setSyncStatus(t, "pending")

	require.Empty(t, createNotebookForFixtureUser(t, fx, `{"title":"Not ready warehouse"}`))
}

// TestCreateNotebookGroupGrantAuthorizesSeeding pins that seeding uses the
// full ACL resolution (a group grant counts), not just direct user entries.
func TestCreateNotebookGroupGrantAuthorizesSeeding(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE org_members SET role = 'non-admin' WHERE org_id = $1 AND user_id = $2`,
		fx.orgID.String(), fx.userID.String())
	require.NoError(t, err)
	fx.grantGroupUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	require.Equal(t, fx.connB.String(), createNotebookForFixtureUser(t, fx, `{"title":"Group grant"}`))
}

// TestCreateNotebookFallsBackToOrgDefaultWithoutPreference pins that the
// existing org-default fallback still applies when no preference exists.
func TestCreateNotebookFallsBackToOrgDefaultWithoutPreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)

	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET is_default = true WHERE id = $1`, fx.connA.String())
	require.NoError(t, err)

	require.Equal(t, fx.connA.String(), createNotebookForFixtureUser(t, fx, `{"title":"Org default"}`))
}
