package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Dashboard widget/share tools promised by the agent prompt must be seeded,
// or no agent can ever call them (tool_ids is the sole execution gate).
var dashboardWidgetHandlers = []string{
	"get_dashboard",
	"create_dashboard_widget",
	"update_dashboard_widget",
	"delete_dashboard_widget",
	"update_dashboard",
	"share_dashboard",
}

func TestSeedBuiltinTools_IncludesDashboardWidgetTools(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	var count int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tools WHERE org_id=$1 AND type='builtin'`, orgID).Scan(&count); err != nil {
		t.Fatalf("count tools: %v", err)
	}
	if count != len(BuiltinTools) {
		t.Fatalf("expected %d seeded tools, got %d", len(BuiltinTools), count)
	}

	engine := newTestEngine(db)
	for _, handler := range dashboardWidgetHandlers {
		var name, config string
		err := db.Pool.QueryRow(context.Background(), `
			SELECT name, config->>'handler_name' FROM tools
			WHERE org_id=$1 AND type='builtin' AND config->>'handler_name'=$2
		`, orgID, handler).Scan(&name, &config)
		if err != nil {
			t.Fatalf("seeded row for %s: %v", handler, err)
		}
		if config != handler {
			t.Fatalf("handler_name mismatch for %s: %q", handler, config)
		}
		def, ok := engine.GetRegistry().Get(handler)
		if !ok || def.Handler == nil {
			t.Fatalf("registry has no callable handler for %s", handler)
		}
		// Seeded ACLs: everyone can view, admins can use.
		var adminActions []string
		if err := db.Pool.QueryRow(context.Background(), `
			SELECT actions FROM acl_entries
			WHERE org_id=$1 AND resource_type='tool' AND subject_type='org_role' AND subject_id='admin'
			  AND resource_id=(SELECT id FROM tools WHERE org_id=$1 AND type='builtin' AND config->>'handler_name'=$2)
		`, orgID, handler).Scan(&adminActions); err != nil {
			t.Fatalf("admin ACL for %s: %v", handler, err)
		}
		if len(adminActions) == 0 {
			t.Fatalf("admin ACL for %s is empty", handler)
		}
	}

	// Second run is idempotent.
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tools WHERE org_id=$1 AND type='builtin'`, orgID).Scan(&count); err != nil || count != len(BuiltinTools) {
		t.Fatalf("re-seed must be idempotent, count=%d err=%v", count, err)
	}
}

// Existing orgs self-heal: an org seeded before the widget tools existed
// (fewer rows than the list) gets exactly the missing rows on re-seed.
func TestSeedBuiltinTools_SelfHealsExistingOrgs(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	// Simulate a pre-fix org: remove the six widget rows.
	if _, err := db.Pool.Exec(context.Background(), `
		DELETE FROM tools WHERE org_id=$1 AND type='builtin'
		AND config->>'handler_name' = ANY($2)
	`, orgID, dashboardWidgetHandlers); err != nil {
		t.Fatalf("simulate old org: %v", err)
	}
	var count int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tools WHERE org_id=$1 AND type='builtin'`, orgID).Scan(&count)
	if count != len(BuiltinTools)-len(dashboardWidgetHandlers) {
		t.Fatalf("setup: expected %d rows, got %d", len(BuiltinTools)-len(dashboardWidgetHandlers), count)
	}

	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tools WHERE org_id=$1 AND type='builtin'`, orgID).Scan(&count)
	if count != len(BuiltinTools) {
		t.Fatalf("expected self-heal to %d rows, got %d", len(BuiltinTools), count)
	}
	for _, handler := range dashboardWidgetHandlers {
		var name string
		if err := db.Pool.QueryRow(context.Background(), `
			SELECT name FROM tools WHERE org_id=$1 AND type='builtin' AND config->>'handler_name'=$2
		`, orgID, handler).Scan(&name); err != nil {
			t.Fatalf("missing healed row for %s: %v", handler, err)
		}
	}
}

// Drift guard: every tool registered by RegisterManageTools must be seeded,
// or it is advertised-but-uncallable. Catches the next omission at test time.
func TestSeedBuiltinTools_CoversManageRegistry(t *testing.T) {
	reg := NewToolRegistry()
	RegisterManageTools(reg, nil)
	seeded := map[string]bool{}
	for _, bt := range BuiltinTools {
		seeded[bt.HandlerName] = true
	}
	for _, def := range reg.List() {
		if !seeded[def.Function.Name] {
			t.Errorf("registry tool %q is not seeded in BuiltinTools (uncallable by agents)", def.Function.Name)
		}
	}
}

// App-era dashboard tools: the seed row's text (what the Tools UI shows) and
// the registry def's text (what the model sees) must not drift.
var appEraDashboardTools = []string{
	"create_dashboard",
	"get_dashboard",
	"create_dashboard_widget",
	"update_dashboard_widget",
	"set_widget_config",
	"update_dashboard",
	"update_dashboard_settings",
	"set_dashboard_layout",
	"publish_dashboard",
	"review_dashboard",
	"dashboard_guide",
}

func TestSeedBuiltinTools_AppEraDescriptionsMatchRegistry(t *testing.T) {
	reg := NewToolRegistry()
	RegisterManageTools(reg, nil)
	seeded := map[string]BuiltinToolDef{}
	for _, bt := range BuiltinTools {
		seeded[bt.HandlerName] = bt
	}
	for _, name := range appEraDashboardTools {
		def, ok := reg.Get(name)
		if !ok {
			t.Fatalf("registry is missing %s", name)
		}
		bt, ok := seeded[name]
		if !ok {
			t.Fatalf("BuiltinTools is missing %s", name)
		}
		if bt.Description != def.Function.Description {
			t.Errorf("%s: seed and registry descriptions drifted:\n seed: %q\n  reg: %q",
				name, bt.Description, def.Function.Description)
		}
	}
}

// TestSeedBuiltinTools_RefreshesStaleDescriptions pins the seed revision
// mechanism: a builtin row seeded before the app-era descriptions (stale text,
// seed_rev 0) is refreshed to the catalog's current description and schema,
// while a row already at the current revision keeps its text until the next
// catalog bump. Builtin descriptions are platform-managed.
func TestSeedBuiltinTools_RefreshesStaleDescriptions(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	var want BuiltinToolDef
	for _, bt := range BuiltinTools {
		if bt.HandlerName == "review_dashboard" {
			want = bt
		}
	}
	if want.Name == "" {
		t.Fatal("review_dashboard is not in BuiltinTools")
	}

	// Simulate an org seeded before this catalog revision.
	if _, err := db.Pool.Exec(context.Background(), `
		UPDATE tools SET description = 'stale description', schema = '{}'::jsonb, seed_rev = 0
		WHERE org_id = $1 AND type = 'builtin' AND config->>'handler_name' = 'review_dashboard'`, orgID); err != nil {
		t.Fatalf("simulate stale row: %v", err)
	}

	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	var description, schema string
	var rev int
	if err := db.Pool.QueryRow(context.Background(), `
		SELECT description, schema::text, seed_rev FROM tools
		WHERE org_id = $1 AND type = 'builtin' AND config->>'handler_name' = 'review_dashboard'`, orgID).
		Scan(&description, &schema, &rev); err != nil {
		t.Fatalf("read refreshed row: %v", err)
	}
	if description != want.Description {
		t.Fatalf("stale description not refreshed:\n got: %q\nwant: %q", description, want.Description)
	}
	require.JSONEq(t, `{"type":"object","properties":{}}`, schema, "stale schema not refreshed")
	if rev != want.Rev {
		t.Fatalf("seed_rev not advanced: got %d, want %d", rev, want.Rev)
	}

	// A row at the current revision keeps its text: the mechanism only
	// overwrites when the catalog is newer (so the next revision bump is what
	// replaces a user edit to a builtin).
	if _, err := db.Pool.Exec(context.Background(), `
		UPDATE tools SET description = 'user-edited description'
		WHERE org_id = $1 AND type = 'builtin' AND config->>'handler_name' = 'review_dashboard'`, orgID); err != nil {
		t.Fatalf("edit row: %v", err)
	}
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	if err := db.Pool.QueryRow(context.Background(), `
		SELECT description FROM tools
		WHERE org_id = $1 AND type = 'builtin' AND config->>'handler_name' = 'review_dashboard'`, orgID).
		Scan(&description); err != nil {
		t.Fatalf("re-read row: %v", err)
	}
	if description != "user-edited description" {
		t.Fatalf("a current-revision row must not be overwritten, got %q", description)
	}
}
