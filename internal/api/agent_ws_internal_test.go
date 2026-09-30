package api

// White-box test for the connect-time admin-mode gate on the agent WebSocket:
// the transient per-session admin flag is only written by connections that
// hold session edit rights. The external api_test package cannot reach
// s.agentEngine, so the test lives in package api and reuses the session
// permission helpers from permissions_internal_test.go.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestAgentWSViewRevocationClosesConnection pins the post-connect view
// re-validation: a live stream is bound to the caller's *current* session view
// permission. Deleting the share while the socket is open closes it via the
// periodic check; an intact share survives the same wait; and a reconnect frame
// after revocation closes the socket instead of answering with history.
func TestAgentWSViewRevocationClosesConnection(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	orgID := insertSessionPermOrg(t, s, "ws-revoke")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	viewerID := insertSessionPermUser(t, s, "viewer")
	addSessionPermMember(t, s, orgID, viewerID, "editor")

	_, sessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	grantSessionPermACL(t, s, orgID, "agent_session", sessionID, viewerID, []string{"view"})

	viewerToken, err := s.jwt.Issue(viewerID.String(), orgID.String(), "editor")
	require.NoError(t, err)

	ts := httptest.NewServer(s)
	defer ts.Close()

	dial := func(t *testing.T) *websocket.Conn {
		t.Helper()
		url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID.String() + "?token=" + viewerToken
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		return conn
	}

	// readErrorWithin drains messages (control frames are handled by the
	// websocket client and never surface here) until the socket errors.
	readErrorWithin := func(t *testing.T, conn *websocket.Conn, d time.Duration) error {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(d))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return err
			}
		}
	}

	shortenInterval := func(t *testing.T, d time.Duration) {
		t.Helper()
		restore := agentWSViewRevalidateInterval
		agentWSViewRevalidateInterval = d
		t.Cleanup(func() { agentWSViewRevalidateInterval = restore })
	}

	t.Run("viewer stays connected while the share exists", func(t *testing.T) {
		shortenInterval(t, 100*time.Millisecond)
		conn := dial(t)

		// Let several periodic checks run, then prove the socket still serves
		// reconnects (i.e. was not closed by a false revocation).
		time.Sleep(400 * time.Millisecond)
		require.NoError(t, conn.WriteJSON(map[string]string{"type": "reconnect", "last_message_id": ""}))

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, data, err := conn.ReadMessage()
			require.NoError(t, err)
			var m map[string]any
			if json.Unmarshal(data, &m) == nil && m["type"] == "reconnect_sync" {
				return
			}
		}
	})

	t.Run("revoked share closes the socket via the periodic check", func(t *testing.T) {
		shortenInterval(t, 100*time.Millisecond)
		conn := dial(t)
		revokeSessionPermACL(t, s, "agent_session", sessionID, viewerID)

		require.Error(t, readErrorWithin(t, conn, 10*time.Second),
			"the socket must close after the viewer's share is revoked")
	})

	t.Run("reconnect after revocation closes the socket", func(t *testing.T) {
		// Keep the periodic check out of the way: the reconnect path alone must
		// enforce revocation.
		shortenInterval(t, time.Hour)

		grantSessionPermACL(t, s, orgID, "agent_session", sessionID, viewerID, []string{"view"})
		conn := dial(t)
		revokeSessionPermACL(t, s, "agent_session", sessionID, viewerID)

		require.NoError(t, conn.WriteJSON(map[string]string{"type": "reconnect", "last_message_id": ""}))
		require.Error(t, readErrorWithin(t, conn, 10*time.Second),
			"a reconnect from a revoked viewer must close the socket")
	})
}
