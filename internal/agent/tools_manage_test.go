package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
)

// TestUpdatePermissionsRejectsAgentSessions pins the alias-hole guard: the raw
// ACL replace tool must reject agent_session, or an admin-mode actor could
// bypass the read-only share normalization (writing edit grants for a non-owner
// or deleting the owner row) through it.
func TestUpdatePermissionsRejectsAgentSessions(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	sessionID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2::uuid, 'user', $3, ARRAY['view','edit','share','delete','admin'])
	`, orgID, sessionID, userID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	def, ok := reg.Get("update_permissions")
	require.True(t, ok, "update_permissions must be registered")
	require.NotNil(t, def.Handler)

	ctx := setupToolContext(t, db, orgID, userID, "")
	args, err := json.Marshal(map[string]any{
		"resource_type": "agent_session",
		"resource_id":   sessionID,
		"entries": []map[string]any{
			{"subject_type": "user", "subject_id": userID, "actions": []string{"edit"}},
		},
	})
	require.NoError(t, err)

	_, err = def.Handler(args, ctx)
	require.Error(t, err, "update_permissions must reject agent_session")
	require.Contains(t, err.Error(), "ACL API")

	var actions []string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT actions FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1::uuid`,
		sessionID).Scan(&actions))
	require.Equal(t, []string{"view", "edit", "share", "delete", "admin"}, actions,
		"a rejected update must leave existing session ACL rows untouched")
}
