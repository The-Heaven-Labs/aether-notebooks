package api

// White-box test for the connect-time admin-mode gate on the agent WebSocket:
// the transient per-session admin flag is only written by connections that
// hold session edit rights. The external api_test package cannot reach
// s.agentEngine, so the test lives in package api and reuses the session
// permission helpers from permissions_internal_test.go.

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestAgentWSConnectTimeAdminModeRequiresEdit pins the fix for viewers being
// able to flip (or clear) the owner's session admin mode just by dialing with
// or without ?admin_mode=true. Only editors may apply the connect-time flag;
// the owner's own connection still does.
func TestAgentWSConnectTimeAdminModeRequiresEdit(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	orgID := insertSessionPermOrg(t, s, "ws-admin-mode")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	viewerID := insertSessionPermUser(t, s, "viewer")
	addSessionPermMember(t, s, orgID, viewerID, "editor")

	_, sessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	grantSessionPermACL(t, s, orgID, "agent_session", sessionID, viewerID, []string{"view"})

	ownerToken, err := s.jwt.Issue(ownerID.String(), orgID.String(), "editor")
	require.NoError(t, err)
	viewerToken, err := s.jwt.Issue(viewerID.String(), orgID.String(), "editor")
	require.NoError(t, err)

	ts := httptest.NewServer(s)
	defer ts.Close()

	// Dial returns after the 101 response, which the server writes only after
	// the connect-time permission/admin-mode logic has run.
	dial := func(t *testing.T, token string, adminMode bool) *websocket.Conn {
		t.Helper()
		url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID.String() + "?token=" + token
		if adminMode {
			url += "&admin_mode=true"
		}
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		return conn
	}

	store := s.agentEngine.SessionStore()

	// A viewer must not be able to switch admin mode on for the owner's session.
	dial(t, viewerToken, true)
	require.False(t, store.GetAdminMode(sessionID.String()),
		"a viewer dialing admin_mode=true must not enable admin mode on the session")

	// The owner's own connection applies the requested flag.
	dial(t, ownerToken, true)
	require.True(t, store.GetAdminMode(sessionID.String()),
		"an owner dialing admin_mode=true must enable admin mode")

	// A viewer connecting without the param must not clear it either.
	dial(t, viewerToken, false)
	require.True(t, store.GetAdminMode(sessionID.String()),
		"a viewer connecting without admin_mode must not clear the owner's admin mode")
}
