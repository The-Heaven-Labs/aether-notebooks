package api_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentSendMessage(t *testing.T) {
	srv := setupTestServer(t)

	email := fmt.Sprintf("send-msg-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Send Msg Org")
	nbID := createNotebook(t, srv, token, "Send Msg NB")
	mcID := createModelConfig(t, srv, token)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	post := func(path, tok, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	// happy path → 202
	require.Equal(t, 202, post("/api/v1/sessions/"+sessionID+"/messages", token, `{"message":"hello"}`).Code)
	// empty message → 400
	require.Equal(t, 400, post("/api/v1/sessions/"+sessionID+"/messages", token, `{"message":""}`).Code)
	// whitespace-only message → 400
	require.Equal(t, 400, post("/api/v1/sessions/"+sessionID+"/messages", token, `{"message":"   "}`).Code)
	// malformed body → 400
	require.Equal(t, 400, post("/api/v1/sessions/"+sessionID+"/messages", token, `{`).Code)
	// missing session → 404
	require.Equal(t, 404, post("/api/v1/sessions/00000000-0000-0000-0000-000000000099/messages", token, `{"message":"hi"}`).Code)
	// no auth → 401
	require.Equal(t, 401, post("/api/v1/sessions/"+sessionID+"/messages", "", `{"message":"hi"}`).Code)
	// second user (different org), no edit permission → 403
	token2 := registerAndGetToken(t, srv, fmt.Sprintf("send-msg-other-%d@example.com", time.Now().UnixNano()), "Other Org")
	require.Equal(t, 403, post("/api/v1/sessions/"+sessionID+"/messages", token2, `{"message":"hi"}`).Code)
}
