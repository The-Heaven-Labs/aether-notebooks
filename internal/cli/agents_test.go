package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testShareUserID  = "11111111-1111-1111-1111-111111111111"
	testShareUserID2 = "33333333-3333-3333-3333-333333333333"
	testShareGroupID = "22222222-2222-2222-2222-222222222222"
)

func TestCreateSessionDecodesResult(t *testing.T) {
	var (
		mu     sync.Mutex
		method string
		path   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, path = r.Method, r.URL.Path
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"session_id":"sess-1","context_window":128000,"auto_approve_tools":true,"auto_answer_questions":false}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	res, err := c.CreateSession("agent-1", CreateSessionOptions{NotebookID: "nb-1"})
	require.NoError(t, err)
	require.Equal(t, "sess-1", res.SessionID)
	require.Equal(t, 128000, res.ContextWindow)
	require.True(t, res.AutoApproveTools)
	require.False(t, res.AutoAnswerQuestions)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/api/v1/agents/agent-1/session", path)
}

func TestCreateSessionSendsResolvedSharesInOneRequest(t *testing.T) {
	var (
		mu       sync.Mutex
		rawBody  []byte
		postHits int
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/members", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrgMember{
			{UserID: testShareUserID, Email: "alice@example.com", Name: "Alice", Role: "editor"},
		})
	})
	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Group{
			{ID: testShareGroupID, Name: "Analysts"},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/agent-1/session", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		rawBody = body
		postHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"session_id":"sess-1","context_window":64000,"auto_approve_tools":false,"auto_answer_questions":false}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	res, err := c.CreateSession("agent-1", CreateSessionOptions{
		NotebookID:           "nb-1",
		ShareUsers:           []string{"Alice@Example.com", testShareUserID2},
		ShareGroups:          []string{"analysts"},
		ShareEveryone:        true,
		ShareNotebookViewers: true,
	})
	require.NoError(t, err)
	require.Equal(t, "sess-1", res.SessionID)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, postHits, "shares must ride on the single create POST")

	var body struct {
		NotebookID               string     `json:"notebook_id"`
		Shares                   []ACLEntry `json:"shares"`
		ShareWithNotebookViewers bool       `json:"share_with_notebook_viewers"`
	}
	require.NoError(t, json.Unmarshal(rawBody, &body))
	require.Equal(t, "nb-1", body.NotebookID)
	require.True(t, body.ShareWithNotebookViewers)
	require.Equal(t, []ACLEntry{
		{SubjectType: "user", SubjectID: testShareUserID, Actions: []string{"view"}},
		{SubjectType: "user", SubjectID: testShareUserID2, Actions: []string{"view"}},
		{SubjectType: "group", SubjectID: testShareGroupID, Actions: []string{"view"}},
		{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view"}},
	}, body.Shares)
}

func TestCreateSessionUUIDShareSkipsMemberLookup(t *testing.T) {
	var (
		mu            sync.Mutex
		membersCalled bool
		groupsCalled  bool
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/members", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		membersCalled = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	})
	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		groupsCalled = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	})
	mux.HandleFunc("POST /api/v1/agents/agent-1/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"session_id":"sess-1"}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := c.CreateSession("agent-1", CreateSessionOptions{
		NotebookID:  "nb-1",
		ShareUsers:  []string{testShareUserID2},
		ShareGroups: []string{testShareGroupID},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.False(t, membersCalled, "UUID share users must not trigger a member lookup")
	require.False(t, groupsCalled, "UUID share groups must not trigger a group lookup")
}

func TestCreateSessionUnknownShareUserFails(t *testing.T) {
	var (
		mu       sync.Mutex
		postHits int
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/members", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrgMember{
			{UserID: testShareUserID, Email: "alice@example.com"},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/agent-1/session", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		postHits++
		mu.Unlock()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := c.CreateSession("agent-1", CreateSessionOptions{
		NotebookID: "nb-1",
		ShareUsers: []string{"bob@example.com"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bob@example.com")
	require.Contains(t, err.Error(), "not found")

	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, postHits, "create must not run when a share user cannot be resolved")
}

func TestCreateSessionAmbiguousShareSubjectFails(t *testing.T) {
	var (
		mu       sync.Mutex
		postHits int
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/members", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrgMember{
			{UserID: testShareUserID, Email: "alice@example.com"},
			{UserID: testShareUserID2, Email: "alice@example.com"},
		})
	})
	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Group{
			{ID: testShareGroupID, Name: "analysts"},
			{ID: testShareUserID2, Name: "Analysts"},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/agent-1/session", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		postHits++
		mu.Unlock()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := c.CreateSession("agent-1", CreateSessionOptions{
		NotebookID: "nb-1",
		ShareUsers: []string{"alice@example.com"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")

	_, err = c.CreateSession("agent-1", CreateSessionOptions{
		NotebookID:  "nb-1",
		ShareGroups: []string{"ANALYSTS"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")

	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, postHits, "create must not run when a share subject cannot be resolved")
}

func TestCreateSessionShareNotebookViewersRequiresNotebook(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Token: "tok"}
	_, err := c.CreateSession("agent-1", CreateSessionOptions{ShareNotebookViewers: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--share-notebook-viewers requires --notebook")
}
