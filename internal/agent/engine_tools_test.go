package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

func setupEngineTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createEngineTestOrgAndUser(t *testing.T, db *database.DB) (orgID, userID string) {
	t.Helper()
	orgID = uuid.New().String()
	userID = uuid.New().String()
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO orgs (id, name, slug, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)
	`, orgID, "Engine Org "+orgID[:8], "slug-"+orgID[:8], now)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, userID, "engine-"+userID[:8]+"@example.com", "Engine User", "hash", now)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO org_members (org_id, user_id, role, created_at) VALUES ($1, $2, 'admin', $3)
	`, orgID, userID, now)
	if err != nil {
		t.Fatalf("create org member: %v", err)
	}
	return orgID, userID
}

func newTestEngine(db *database.DB) *Engine {
	engine := &Engine{
		registry: NewToolRegistry(),
		pool:     db.Pool,
	}
	RegisterNotebookTools(engine.registry, db.Pool)
	RegisterAgentTools(engine.registry, db.Pool, engine)
	RegisterPlatformTools(engine.registry, db.Pool)
	RegisterChartTools(engine.registry, db.Pool)
	RegisterManageTools(engine.registry, db.Pool)
	return engine
}

func seededToolIDs(t *testing.T, db *database.DB, orgID string, names ...string) []string {
	t.Helper()
	var ids []string
	for _, name := range names {
		var id string
		err := db.Pool.QueryRow(context.Background(),
			`SELECT id FROM tools WHERE org_id = $1 AND name = $2`, orgID, name).Scan(&id)
		if err != nil {
			t.Fatalf("lookup seeded tool %s: %v", name, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestEngineLoadAgentToolDefs_AllBuiltin(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	// New semantics: agent whose tool_ids contains every built-in tool gets every built-in tool.
	// Explicitly select all builtins via tool_ids (no AllBuiltinTools flag).
	var allIDs []string
	err := db.Pool.QueryRow(context.Background(), `SELECT array_agg(id) FROM tools WHERE org_id = $1 AND type='builtin'`, orgID).Scan((*string)(nil))
	// fallback: collect via seeded lookup of known names
	_ = err
	rows, _ := db.Pool.Query(context.Background(), `SELECT id FROM tools WHERE org_id=$1 AND type='builtin'`, orgID)
	if rows != nil {
		for rows.Next() {
			var id string
			rows.Scan(&id)
			allIDs = append(allIDs, id)
		}
		rows.Close()
	}

	agent := models.Agent{
		ID:      uuid.New().String(),
		OrgID:   orgID,
		ToolIDs: allIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent, userID, "admin", true)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if !names["list_notebook_parameters"] {
		t.Fatalf("agent missing list_notebook_parameters tool (got %d defs)", len(defs))
	}
	if !names["set_notebook_parameters"] {
		t.Fatalf("agent missing set_notebook_parameters tool (got %d defs)", len(defs))
	}
	if len(defs) < 10 {
		t.Fatalf("expected all built-in tools loaded, got only %d", len(defs))
	}
	t.Logf("explicit-all-builtin agent loaded %d tools", len(defs))
}

func TestEngineLoadAgentToolDefs_AlwaysUsesToolIDs(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	// Legacy AllBuiltinTools=true but tool_ids lists only 2 tools – only those 2 should be returned.
	paramToolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters", "set_notebook_parameters")
	agent := models.Agent{
		ID:              uuid.New().String(),
		OrgID:           orgID,
		AllBuiltinTools: true,
		ToolIDs:         paramToolIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent, userID, "admin", true)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if len(defs) != 2 {
		t.Fatalf("expected exactly 2 tools despite legacy AllBuiltinTools=true, got %d: %v", len(defs), names)
	}
	if !names["list_notebook_parameters"] || !names["set_notebook_parameters"] {
		t.Fatalf("expected only the 2 selected tools, got %v", names)
	}
	t.Logf("legacy-all_builtin flag ignored, only tool_ids honoured")
}

func TestEngineLoadAgentToolDefs_AdminRespectsToolACL(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "execute_sql", "list_notebook_parameters")
	// Revoke use permission for execute_sql by deleting the everyone grant
	var execID string
	db.Pool.QueryRow(context.Background(), `SELECT id FROM tools WHERE org_id=$1 AND name='execute_sql'`, orgID).Scan(&execID)
	_, err := db.Pool.Exec(context.Background(), `DELETE FROM acl_entries WHERE resource_type='tool' AND resource_id=$1::uuid AND org_id=$2 AND subject_type='org_role' AND subject_id='everyone'`, execID, orgID)
	if err != nil {
		t.Fatalf("revoke acl: %v", err)
	}

	agent := models.Agent{
		ID:      uuid.New().String(),
		OrgID:   orgID,
		ToolIDs: toolIDs,
	}

	// adminMode OFF => should respect ACL, execute_sql should be filtered out
	defs := engine.loadAgentToolDefs(context.Background(), agent, userID, "admin", false)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if names["execute_sql"] {
		t.Fatalf("admin with adminMode OFF should not have execute_sql after ACL revocation, got %v", names)
	}
	if !names["list_notebook_parameters"] {
		t.Fatalf("expected list_notebook_parameters still allowed, got %v", names)
	}
	if len(defs) != 1 {
		t.Fatalf("expected exactly 1 tool after ACL filter, got %d: %v", len(defs), names)
	}
}

func TestEngineLoadAgentToolDefs_AdminModeBypassesToolACL(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "execute_sql", "list_notebook_parameters")
	var execID string
	db.Pool.QueryRow(context.Background(), `SELECT id FROM tools WHERE org_id=$1 AND name='execute_sql'`, orgID).Scan(&execID)
	_, err := db.Pool.Exec(context.Background(), `DELETE FROM acl_entries WHERE resource_type='tool' AND resource_id=$1::uuid AND org_id=$2 AND subject_type='org_role' AND subject_id='everyone'`, execID, orgID)
	if err != nil {
		t.Fatalf("revoke acl: %v", err)
	}

	agent := models.Agent{
		ID:      uuid.New().String(),
		OrgID:   orgID,
		ToolIDs: toolIDs,
	}

	// adminMode ON => should bypass ACL and get both tools
	defs := engine.loadAgentToolDefs(context.Background(), agent, userID, "admin", true)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if !names["execute_sql"] {
		t.Fatalf("admin with adminMode ON should bypass ACL and have execute_sql, got %v", names)
	}
	if !names["list_notebook_parameters"] {
		t.Fatalf("expected list_notebook_parameters, got %v", names)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools with adminMode ON, got %d: %v", len(defs), names)
	}
}

func TestEngineLoadAgentToolDefs_ExplicitSelection(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	paramToolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters", "set_notebook_parameters")
	agent := models.Agent{
		ID:              uuid.New().String(),
		OrgID:           orgID,
		AllBuiltinTools: false,
		ToolIDs:         paramToolIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent, userID, "admin", true)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if len(defs) != 2 {
		t.Fatalf("expected exactly 2 explicitly selected tools, got %d: %v", len(defs), names)
	}
	if !names["list_notebook_parameters"] || !names["set_notebook_parameters"] {
		t.Fatalf("explicit selection did not include both parameter tools: %v", names)
	}
	t.Logf("explicit agent loaded exactly its 2 selected tools")
}

func TestEngineLoadAgentToolDefs_ExplicitSkipsUnselected(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	// Explicitly select only list_notebook_parameters — set must NOT be included.
	paramToolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agent := models.Agent{
		ID:              uuid.New().String(),
		OrgID:           orgID,
		AllBuiltinTools: false,
		ToolIDs:         paramToolIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent, userID, "admin", true)

	for _, d := range defs {
		if d.Function.Name == "set_notebook_parameters" {
			t.Fatalf("unselected tool set_notebook_parameters was loaded for explicit agent")
		}
	}
	if len(defs) != 1 {
		t.Fatalf("expected exactly 1 tool, got %d", len(defs))
	}
	t.Logf("explicit agent correctly excluded the unselected tool")
}
