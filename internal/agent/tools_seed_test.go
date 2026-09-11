package agent

import (
	"context"
	"testing"
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
