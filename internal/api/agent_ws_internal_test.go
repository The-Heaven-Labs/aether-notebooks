package api

// White-box test for the connect-time admin-mode gate on the agent WebSocket:
// the transient per-session admin flag is only written by connections that
// hold session edit rights. The external api_test package cannot reach
// s.agentEngine, so the test lives in package api and reuses the session
// permission helpers from permissions_internal_test.go.

import (
	"encoding/json"
	"errors"
	"net"
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
// after revocation closes the socket instead of answering with history. An org
// admin in admin mode with no session ACL also survives the wait, pinning the
// permissionCtx replay of the connect-time admin flag.
func TestAgentWSViewRevocationClosesConnection(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	orgID := insertSessionPermOrg(t, s, "ws-revoke")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	viewerID := insertSessionPermUser(t, s, "viewer")
	addSessionPermMember(t, s, orgID, viewerID, "editor")
	adminID := insertSessionPermUser(t, s, "admin")
	addSessionPermMember(t, s, orgID, adminID, "admin")

	_, sessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	grantSessionPermACL(t, s, orgID, "agent_session", sessionID, viewerID, []string{"view"})

	viewerToken, err := s.jwt.Issue(viewerID.String(), orgID.String(), "editor")
	require.NoError(t, err)
	adminToken, err := s.jwt.Issue(adminID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	ts := httptest.NewServer(s)
	defer ts.Close()

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

	// requireServerCloseWithin drains messages (control frames are handled by
	// the websocket client and never surface here) until the socket errors and
	// asserts the error is a real server-side close — a *websocket.CloseError
	// (an abrupt close is code 1006), never the client's own read deadline.
	// Shape matters: require.Error alone would also pass when shutdown() is
	// neutered and the test client just times out after the full deadline.
	// gorilla hides deadline errors behind its own net.Error (it does not
	// support errors.Is(os.ErrDeadlineExceeded)), so the explicit Timeout()
	// check below is the deadline test.
	requireServerCloseWithin := func(t *testing.T, conn *websocket.Conn, d time.Duration, msg string) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(d))
		start := time.Now()
		var err error
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				break
			}
		}
		elapsed := time.Since(start)

		require.Error(t, err, msg)
		require.Less(t, elapsed, d/2,
			"%s: the socket closed after %s, at the client read deadline rather than on re-validation", msg, elapsed)
		var closeErr *websocket.CloseError
		require.True(t, errors.As(err, &closeErr),
			"%s: the server must close the socket, but the read failed with %v", msg, err)
		var netErr net.Error
		require.False(t, errors.As(err, &netErr) && netErr.Timeout(),
			"%s: the server must close the socket, not let the client read deadline expire: %v", msg, err)
	}

	// readUntilReconnectSync proves the socket is still alive by exchanging a
	// reconnect frame and reading the authoritative sync response.
	readUntilReconnectSync := func(t *testing.T, conn *websocket.Conn) {
		t.Helper()
		require.NoError(t, conn.WriteJSON(map[string]string{"type": "reconnect", "last_message_id": ""}))
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, data, err := conn.ReadMessage()
			require.NoError(t, err, "the socket must stay connected while access holds")
			var m map[string]any
			if json.Unmarshal(data, &m) == nil && m["type"] == "reconnect_sync" {
				return
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
		conn := dial(t, viewerToken, false)

		// Let several periodic checks run, then prove the socket still serves
		// reconnects (i.e. was not closed by a false revocation).
		time.Sleep(400 * time.Millisecond)
		readUntilReconnectSync(t, conn)
	})

	t.Run("admin mode survives re-validation without a session ACL", func(t *testing.T) {
		shortenInterval(t, 100*time.Millisecond)
		conn := dial(t, adminToken, true)

		// The admin holds no agent_session ACL: only the replayed connect-time
		// admin mode lets every periodic check pass. Without the replay the
		// first tick (~100ms) would close the socket.
		time.Sleep(400 * time.Millisecond)
		readUntilReconnectSync(t, conn)
	})

	t.Run("revoked share closes the socket via the periodic check", func(t *testing.T) {
		shortenInterval(t, 100*time.Millisecond)
		conn := dial(t, viewerToken, false)
		revokeSessionPermACL(t, s, "agent_session", sessionID, viewerID)

		requireServerCloseWithin(t, conn, 10*time.Second,
			"the socket must close after the viewer's share is revoked")
	})

	t.Run("reconnect after revocation closes the socket", func(t *testing.T) {
		// Keep the periodic check out of the way: the reconnect path alone must
		// enforce revocation.
		shortenInterval(t, time.Hour)

		grantSessionPermACL(t, s, orgID, "agent_session", sessionID, viewerID, []string{"view"})
		conn := dial(t, viewerToken, false)
		revokeSessionPermACL(t, s, "agent_session", sessionID, viewerID)

		require.NoError(t, conn.WriteJSON(map[string]string{"type": "reconnect", "last_message_id": ""}))
		requireServerCloseWithin(t, conn, 10*time.Second,
			"a reconnect from a revoked viewer must close the socket")
	})
}
