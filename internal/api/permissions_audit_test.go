package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/stretchr/testify/require"
)

type ResourceSet struct {
	NoACL       string
	UserACL     string
	GroupACL    string
	EveryoneACL string
}

type OrgFixtures struct {
	OrgID        string
	GroupIDs     map[string]string
	Notebooks    ResourceSet
	Folders      ResourceSet
	Connectors   ResourceSet
	Dashboards   ResourceSet
	Agents       ResourceSet
	ModelConfigs ResourceSet
	Skills       ResourceSet
	MCPServers   ResourceSet
}

type AuditFixtures struct {
	srv     *api.Server
	Tokens  map[string]string
	UserIDs map[string]string
	OrgA    OrgFixtures
	OrgB    OrgFixtures
}

func (f *AuditFixtures) Request(t *testing.T, userKey, method, path string, body any) *http.Request {
	t.Helper()
	token, ok := f.Tokens[userKey]
	if !ok {
		t.Fatalf("unknown user key: %s", userKey)
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func (f *AuditFixtures) DoRequest(t *testing.T, userKey, method, path string, body any) (int, string) {
	t.Helper()
	req := f.Request(t, userKey, method, path, body)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func userIDFromToken(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	id, _ := resp["id"].(string)
	require.NotEmpty(t, id)
	return id
}

func orgIDFromUser(t *testing.T, srv *api.Server, userID string) string {
	t.Helper()
	var orgID string
	err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT org_id FROM org_members WHERE user_id = $1`, userID,
	).Scan(&orgID)
	require.NoError(t, err)
	return orgID
}

func doRequest(t *testing.T, srv *api.Server, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	}
	return rec.Code, resp
}

func resourceID(t *testing.T, code int, resp map[string]any) string {
	t.Helper()
	require.Equal(t, http.StatusCreated, code, "resource creation failed, got body: %v", resp)
	id, _ := resp["id"].(string)
	require.NotEmpty(t, id)
	return id
}

func SetupAuditTest(t *testing.T) *AuditFixtures {
	t.Helper()

	srv := setupTestServer(t)
	f := &AuditFixtures{
		srv:     srv,
		Tokens:  make(map[string]string),
		UserIDs: make(map[string]string),
	}
	f.OrgA.GroupIDs = make(map[string]string)

	now := time.Now().UnixNano()

	// ===== ORG A =====

	// adminA — register org, becomes admin
	adminAEmail := fmt.Sprintf("admina-%d@test.com", now)
	f.Tokens["adminA"] = registerAndGetToken(t, srv, adminAEmail, "Org A")
	f.UserIDs["adminA"] = userIDFromToken(t, srv, f.Tokens["adminA"])
	orgAID := orgIDFromUser(t, srv, f.UserIDs["adminA"])
	f.OrgA.OrgID = orgAID

	// aliceA — direct SQL insert, org member
	aliceAEmail := fmt.Sprintf("alicea-%d@test.com", now)
	f.UserIDs["aliceA"] = insertUser(t, srv, aliceAEmail, "Alice A")
	addOrgMember(t, srv, orgAID, f.UserIDs["aliceA"], "editor")
	f.Tokens["aliceA"] = issueToken(t, f.UserIDs["aliceA"], orgAID, "editor")

	// bobA — direct SQL insert, org member, will be added to engineers group
	bobAEmail := fmt.Sprintf("boba-%d@test.com", now)
	f.UserIDs["bobA"] = insertUser(t, srv, bobAEmail, "Bob A")
	addOrgMember(t, srv, orgAID, f.UserIDs["bobA"], "editor")
	f.Tokens["bobA"] = issueToken(t, f.UserIDs["bobA"], orgAID, "editor")

	// carolA — direct SQL insert, org member, no ACLs ever
	carolAEmail := fmt.Sprintf("carola-%d@test.com", now)
	f.UserIDs["carolA"] = insertUser(t, srv, carolAEmail, "Carol A")
	addOrgMember(t, srv, orgAID, f.UserIDs["carolA"], "editor")
	f.Tokens["carolA"] = issueToken(t, f.UserIDs["carolA"], orgAID, "editor")

	// Create group "engineers" in Org A via adminA
	groupID := createGroup(t, srv, f.Tokens["adminA"], "engineers")
	f.OrgA.GroupIDs["engineers"] = groupID
	// Add bobA to engineers group
	addGroupMember(t, srv, f.Tokens["adminA"], groupID, f.UserIDs["bobA"])

	// ===== ORG A RESOURCES =====

	f.OrgA.Notebooks = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createNotebookResource)
	f.OrgA.Folders = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createFolderResource)
	f.OrgA.Connectors = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createConnectorResource)
	f.OrgA.Dashboards = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createDashboardResource)
	f.OrgA.Agents = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createAgentResource)
	f.OrgA.ModelConfigs = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createModelConfigResource)
	f.OrgA.Skills = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createSkillResource)
	f.OrgA.MCPServers = createResourceSet(t, srv, f.Tokens["adminA"], orgAID, createMCPServerResource)

	// Seed ACLs for Org A resources
	seedResourceACLs(t, srv, orgAID, "notebook", f.OrgA.Notebooks, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "folder", f.OrgA.Folders, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "connector", f.OrgA.Connectors, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "dashboard", f.OrgA.Dashboards, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "agent", f.OrgA.Agents, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "model_config", f.OrgA.ModelConfigs, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "skill", f.OrgA.Skills, f.UserIDs["aliceA"], groupID)
	seedResourceACLs(t, srv, orgAID, "mcp_server", f.OrgA.MCPServers, f.UserIDs["aliceA"], groupID)

	f.OrgB.GroupIDs = make(map[string]string)

	// ===== ORG B =====

	adminBEmail := fmt.Sprintf("adminb-%d@test.com", now)
	f.Tokens["adminB"] = registerAndGetToken(t, srv, adminBEmail, "Org B")
	f.UserIDs["adminB"] = userIDFromToken(t, srv, f.Tokens["adminB"])
	orgBID := orgIDFromUser(t, srv, f.UserIDs["adminB"])
	f.OrgB.OrgID = orgBID

	eveBEmail := fmt.Sprintf("eveb-%d@test.com", now)
	f.UserIDs["eveB"] = insertUser(t, srv, eveBEmail, "Eve B")
	addOrgMember(t, srv, orgBID, f.UserIDs["eveB"], "editor")
	f.Tokens["eveB"] = issueToken(t, f.UserIDs["eveB"], orgBID, "editor")

	// Cross-org resources (1 each, no ACL)
	f.OrgB.Notebooks = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createNotebookResource)}
	f.OrgB.Folders = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createFolderResource)}
	f.OrgB.Connectors = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createConnectorResource)}
	f.OrgB.Dashboards = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createDashboardResource)}
	f.OrgB.Agents = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createAgentResource)}
	f.OrgB.ModelConfigs = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createModelConfigResource)}
	f.OrgB.Skills = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createSkillResource)}
	f.OrgB.MCPServers = ResourceSet{NoACL: createSingleResource(t, srv, f.Tokens["adminB"], orgBID, createMCPServerResource)}

	// ===== PLATFORM ADMIN =====

	platEmail := fmt.Sprintf("platadmin-%d@test.com", now)
	f.UserIDs["platAdmin"] = insertUser(t, srv, platEmail, "Plat Admin")
	addOrgMember(t, srv, orgAID, f.UserIDs["platAdmin"], "admin")
	tok, err := testJWT.IssuePlatformAdmin(f.UserIDs["platAdmin"], orgAID, "admin")
	require.NoError(t, err)
	f.Tokens["platAdmin"] = tok

	return f
}

// insertUser creates a user via direct SQL and returns the user ID.
func insertUser(t *testing.T, srv *api.Server, email, name string) string {
	t.Helper()
	var id string
	err := srv.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', $2, false) RETURNING id`,
		email, name,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// addOrgMember adds a user to an org with the given role.
func addOrgMember(t *testing.T, srv *api.Server, orgID, userID, role string) {
	t.Helper()
	_, err := srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID, userID, role,
	)
	require.NoError(t, err)
}

// issueToken issues a JWT for the given user/org/role using the test JWT issuer.
func issueToken(t *testing.T, userID, orgID, role string) string {
	t.Helper()
	tok, err := testJWT.Issue(userID, orgID, role)
	require.NoError(t, err)
	return tok
}

// createGroup creates a group and returns its ID.
func createGroup(t *testing.T, srv *api.Server, token, name string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/groups", map[string]string{"name": name})
	return resourceID(t, code, resp)
}

// addGroupMember adds a user to a group.
func addGroupMember(t *testing.T, srv *api.Server, token, groupID, userID string) {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/groups/"+groupID+"/members", map[string]string{"user_id": userID})
	require.Equal(t, http.StatusCreated, code, "add group member failed: %v", resp)
}

type resourceCreator func(t *testing.T, srv *api.Server, token string, label string) string

// createSingleResource creates one resource and returns its ID.
func createSingleResource(t *testing.T, srv *api.Server, token string, orgID string, creator resourceCreator) string {
	t.Helper()
	return creator(t, srv, token, "res-"+orgID[:8])
}

// createResourceSet creates 4 variants of a resource and returns the set of IDs.
func createResourceSet(t *testing.T, srv *api.Server, token, orgID string, creator resourceCreator) ResourceSet {
	t.Helper()
	return ResourceSet{
		NoACL:       creator(t, srv, token, "noacl-"+orgID[:8]),
		UserACL:     creator(t, srv, token, "user-"+orgID[:8]),
		GroupACL:    creator(t, srv, token, "group-"+orgID[:8]),
		EveryoneACL: creator(t, srv, token, "everyone-"+orgID[:8]),
	}
}

func createNotebookResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/notebooks", map[string]string{"title": label})
	return resourceID(t, code, resp)
}

func createFolderResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/folders", map[string]string{"name": label})
	return resourceID(t, code, resp)
}

func createConnectorResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	body := map[string]any{
		"name": label,
		"type": "postgres",
		"config": map[string]any{
			"host": "localhost", "port": 5432,
			"user": "hnb", "password": "hnb_dev", "database": "hnb",
		},
	}
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/connectors", body)
	return resourceID(t, code, resp)
}

func createDashboardResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/dashboards", map[string]string{"title": label})
	return resourceID(t, code, resp)
}

func createAgentResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/agents", map[string]any{
		"name": label,
	})
	return resourceID(t, code, resp)
}

func createModelConfigResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/model-configs", map[string]any{
		"name":     label,
		"provider": "openai",
		"base_url": "https://api.openai.com/v1",
		"model":    "gpt-4",
		"api_key":  "test-key",
	})
	return resourceID(t, code, resp)
}

func createSkillResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/skills", map[string]any{
		"name": label,
	})
	return resourceID(t, code, resp)
}

func createMCPServerResource(t *testing.T, srv *api.Server, token, label string) string {
	t.Helper()
	code, resp := doRequest(t, srv, token, "POST", "/api/v1/mcp-servers", map[string]any{
		"name":    label,
		"type":    "stdio",
		"command": "echo",
	})
	return resourceID(t, code, resp)
}

// seedResourceACLs creates ACL entries for the four resource variants.
// - NoACL: no entries (tests deny-by-default)
// - UserACL: user:aliceA:["view"]
// - GroupACL: group:engineers:["view","edit"]
// - EveryoneACL: org_role:everyone:["view"]
func seedResourceACLs(t *testing.T, srv *api.Server, orgID, resourceType string, rs ResourceSet, aliceUserID, groupID string) {
	t.Helper()
	ctx := context.Background()
	db := srv.DB().Pool

	if rs.UserACL != "" {
		_, err := db.Exec(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			 VALUES ($1, $2, $3::uuid, 'user', $4, ARRAY['view'])`,
			orgID, resourceType, rs.UserACL, aliceUserID,
		)
		require.NoError(t, err)
	}

	if rs.GroupACL != "" {
		_, err := db.Exec(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			 VALUES ($1, $2, $3::uuid, 'group', $4, ARRAY['view','edit'])`,
			orgID, resourceType, rs.GroupACL, groupID,
		)
		require.NoError(t, err)
	}

	if rs.EveryoneACL != "" {
		_, err := db.Exec(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			 VALUES ($1, $2, $3::uuid, 'org_role', 'everyone', ARRAY['view'])`,
			orgID, resourceType, rs.EveryoneACL,
		)
		require.NoError(t, err)
	}
}
