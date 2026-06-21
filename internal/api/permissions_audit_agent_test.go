package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// createSessionInAgent creates a session using the given user's token and returns the session ID.
func createSessionInAgent(t *testing.T, f *AuditFixtures, userKey, agentID, notebookID string) string {
	t.Helper()
	payload := map[string]any{"max_turns": 10}
	if notebookID != "" {
		payload["notebook_id"] = notebookID
	}
	status, resp := f.DoRequest(t, userKey, "POST",
		"/api/v1/agents/"+agentID+"/session", payload)
	require.Equal(t, http.StatusCreated, status, "create session: %s", resp)
	var sm map[string]any
	json.Unmarshal([]byte(resp), &sm)
	sid, _ := sm["session_id"].(string)
	require.NotEmpty(t, sid)
	return sid
}

// ——— LIST ———

func TestAgent_List(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		wantMin int
		wantMax int
	}{
		{
			name:    "adminA sees all Org A agents",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees agents with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL agents",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B agents",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B agents she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/agents", nil)
			require.Equal(t, http.StatusOK, status)

			var agents []map[string]any
			json.Unmarshal([]byte(body), &agents)
			count := len(agents)
			t.Logf("%s: %d agents visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestAgent_Get(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/agents/"+f.OrgA.Agents.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/agents/"+f.OrgA.Agents.NoACL, nil)
		t.Logf("aliceA GET NoACL agent: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL agent without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/agents/"+f.OrgA.Agents.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/agents/"+f.OrgA.Agents.NoACL, nil)
		t.Logf("carolA GET NoACL agent: %d %s", status, body)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/agents/"+f.OrgA.Agents.NoACL, nil)
		t.Logf("adminB GET Org A NoACL agent: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/agents/"+f.OrgA.Agents.NoACL, nil)
		t.Logf("eveB GET Org A NoACL agent: %d %s", status, body)
	})
}

// ——— CREATE ———

func TestAgent_Create(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates agent", "adminA", http.StatusCreated},
		{"aliceA creates agent", "aliceA", http.StatusCreated},
		{"carolA creates agent", "carolA", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/agents",
				map[string]string{"name": "test-" + tt.userKey})
			t.Logf("%s create agent: %d %s", tt.userKey, status, body)
			require.Equal(t, tt.want, status)
		})
	}
}

// ——— UPDATE ———

func TestAgent_Update(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/agents/"+f.OrgA.Agents.NoACL,
			map[string]string{"name": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/agents/"+f.OrgA.Agents.NoACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL agent: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated NoACL agent without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/agents/"+f.OrgA.Agents.UserACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT UserACL agent (view-only): %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated UserACL agent with only view permission")
		}
	})

	t.Run("bobA on GroupACL — 200 (group has edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT", "/api/v1/agents/"+f.OrgA.Agents.GroupACL,
			map[string]string{"name": "updated-by-bob"})
		t.Logf("bobA PUT GroupACL agent: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/agents/"+f.OrgA.Agents.NoACL,
			map[string]string{"name": "updated-by-adminb"})
		t.Logf("adminB PUT Org A NoACL agent: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/agents/"+f.OrgA.Agents.NoACL,
			map[string]string{"name": "updated-by-eveb"})
		t.Logf("eveB PUT Org A NoACL agent: %d %s", status, body)
	})
}

// ——— DELETE ———

func TestAgent_Delete(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/agents/"+f.OrgA.Agents.NoACL, nil)
		t.Logf("adminA DELETE NoACL agent: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (view-only)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/agents/"+f.OrgA.Agents.UserACL, nil)
		t.Logf("aliceA DELETE UserACL agent: %d %s", status, body)
		if status == http.StatusNoContent {
			t.Log("VULNERABILITY: aliceA deleted agent with only view permission")
		}
	})

	t.Run("adminB on EveryoneACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/agents/"+f.OrgA.Agents.EveryoneACL, nil)
		t.Logf("adminB DELETE Org A agent: %d %s", status, body)
	})
}

// ——— SESSION CREATION (F11: no permission check) ———

func TestAgent_SessionLifecycle(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	// Need a notebook_id for sessions to avoid pre-existing scan bug with NULL notebook_id
	nbID := f.OrgA.Notebooks.NoACL

	// F11 vulnerability: aliceA creates session on NoACL agent without any permission check
	t.Run("aliceA creates session on NoACL — no permission check (F11)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/agents/"+f.OrgA.Agents.NoACL+"/session",
			map[string]any{"max_turns": 10, "notebook_id": nbID})
		t.Logf("aliceA create session on NoACL agent: %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY F11: handleCreateSession does NOT check permission on the agent")
			t.Log("Any authenticated user can create a session on any agent in their org")
		}
	})

	// adminA creates session for subsequent tests
	noACLSessionID := createSessionInAgent(t, f, "adminA", f.OrgA.Agents.NoACL, nbID)

	t.Run("adminA lists sessions on NoACL — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET",
			"/api/v1/agents/"+f.OrgA.Agents.NoACL+"/sessions", nil)
		t.Logf("adminA list sessions: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA lists sessions on NoACL — 403 (no view on agent)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/agents/"+f.OrgA.Agents.NoACL+"/sessions", nil)
		t.Logf("aliceA list sessions on NoACL: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA listed sessions of NoACL agent without view")
		}
	})

	t.Run("adminA gets session — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET",
			"/api/v1/sessions/"+noACLSessionID, nil)
		t.Logf("adminA get session: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA gets session (via NoACL agent) — 403", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/sessions/"+noACLSessionID, nil)
		t.Logf("aliceA get session: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA got session of NoACL agent without view")
		}
	})

	t.Run("adminA gets session messages — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET",
			"/api/v1/sessions/"+noACLSessionID+"/messages", nil)
		t.Logf("adminA get messages: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA updates session title on NoACL agent — 403 (no edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PATCH",
			"/api/v1/sessions/"+noACLSessionID+"/title",
			map[string]string{"title": "hacked"})
		t.Logf("aliceA update session title: %d %s", status, body)
	})
}

// ——— SESSION WITH VIEW PERMISSION ———

func TestAgent_SessionWithPermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)
	nbID := f.OrgA.Notebooks.NoACL

	// adminA creates session on UserACL agent
	sessionID := createSessionInAgent(t, f, "adminA", f.OrgA.Agents.UserACL, nbID)

	t.Run("aliceA lists sessions on UserACL — 200 (has view)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/agents/"+f.OrgA.Agents.UserACL+"/sessions", nil)
		t.Logf("aliceA list sessions UserACL: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA gets session (via UserACL agent) — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/sessions/"+sessionID, nil)
		t.Logf("aliceA get session: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA gets session messages — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/sessions/"+sessionID+"/messages", nil)
		t.Logf("aliceA get messages: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA updates session title on UserACL — 403 (view-only, no edit)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PATCH",
			"/api/v1/sessions/"+sessionID+"/title",
			map[string]string{"title": "renamed"})
		t.Logf("aliceA update title: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA updated session title on view-only agent")
		}
	})

	t.Run("bobA updates session title on GroupACL — 200 (group has edit)", func(t *testing.T) {
		gSessionID := createSessionInAgent(t, f, "adminA", f.OrgA.Agents.GroupACL, nbID)
		status, body := f.DoRequest(t, "bobA", "PATCH",
			"/api/v1/sessions/"+gSessionID+"/title",
			map[string]string{"title": "renamed-by-bob"})
		t.Logf("bobA update title: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})
}

// ——— STATS (RequireRole admin) ———

func TestAgent_Stats(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA gets global stats — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET", "/api/v1/agents/stats", nil)
		t.Logf("adminA agents stats: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA gets global stats — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/agents/stats", nil)
		t.Logf("aliceA agents stats: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("adminA gets per-agent stats — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET",
			"/api/v1/agents/"+f.OrgA.Agents.NoACL+"/stats", nil)
		t.Logf("adminA agent stats: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA gets per-agent stats — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET",
			"/api/v1/agents/"+f.OrgA.Agents.NoACL+"/stats", nil)
		t.Logf("aliceA agent stats: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})
}

// ——— GROUP ACL ON AGENT ———

func TestAgent_GroupACL(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("bobA gets GroupACL agent — 200 (group view+edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "GET",
			"/api/v1/agents/"+f.OrgA.Agents.GroupACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("bobA updates GroupACL agent — 200 (group edit)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "bobA", "PUT",
			"/api/v1/agents/"+f.OrgA.Agents.GroupACL,
			map[string]string{"name": "group-renamed-by-bob"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("bobA creates session on GroupACL agent — 201", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "POST",
			"/api/v1/agents/"+f.OrgA.Agents.GroupACL+"/session",
			map[string]any{"max_turns": 10, "notebook_id": f.OrgA.Notebooks.NoACL})
		t.Logf("bobA create session on GroupACL: %d %s", status, body)
		require.Equal(t, http.StatusCreated, status)
	})
}
