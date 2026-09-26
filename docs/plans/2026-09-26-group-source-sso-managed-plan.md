# Group Provenance & SSO-Managed Delete Guard — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a durable `groups.source` provenance (`manual`/`sso`/`system`), surface it in the console, and block deleting SSO-managed groups unless an org admin passes `?force=true` (system groups are never deletable).

**Architecture:** A single `source` column with a CHECK constraint is set by every creation path (`manual` default, `sso` in `FindOrCreateGroup`, `system` for Everyone) and flipped one-way to `sso` when SSO sync adopts an existing manual group. The delete handler follows the existing Everyone-guard pattern with an audited force override. The frontend renders a badge and a warning dialog; browser validation with agent-browser and a Playwright spec are mandatory acceptance steps.

**Tech Stack:** Go 1.x (`net/http` ServeMux, pgx), PostgreSQL migrations (auto-applied), React 18 + TypeScript + React Query + Vitest/MSW, Playwright, agent-browser, cobra CLI.

**Spec:** `docs/plans/2026-09-26-group-source-sso-managed-design.md`

---

### Task 1: Migration V119 and `models.Group.Source`

**Files:**
- Create: `internal/database/migrations/V119__group_source.sql`
- Modify: `internal/models/group.go`

**Step 1: Write the migration**

```sql
-- Tag each group with how it came to exist.
ALTER TABLE groups ADD COLUMN source text NOT NULL DEFAULT 'manual';

ALTER TABLE groups ADD CONSTRAINT groups_source_check
  CHECK (source IN ('manual', 'sso', 'system'));

UPDATE groups SET source = 'system' WHERE LOWER(name) = 'everyone';

UPDATE groups SET source = 'sso'
 WHERE source = 'manual'
   AND id IN (SELECT DISTINCT group_id FROM sso_group_memberships);
```

**Step 2: Add the model field**

In `internal/models/group.go`, add `Source string \`json:"source"\`` after `DisplayName`:

```go
type Group struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	DisplayName *string   `json:"display_name"`
	Source      string    `json:"source"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}
```

**Step 3: Verify the migration applies cleanly**

Migrations are embedded (`internal/database/migrate.go:13`) and applied by `database.Migrate`, which every test server boots.

Run:
```bash
go test -timeout 3m ./internal/api/ -run TestEveryoneGroupExists -count=1 -v
```
Expected: PASS (a broken migration would fail server setup). No package errors.

**Step 4: Verify the backfill in the dev DB**

```bash
docker compose -f docker-compose.dev.yml exec -T aether-postgres \
  psql -U aether -d aether -c "SELECT source, count(*) FROM groups GROUP BY source"
```
Expected: rows tagged `sso` for previously tracked groups, `system` for Everyone, `manual` for the rest. Re-running is idempotent (migration tracked by version).

**Step 5: Commit**

```bash
git add internal/database/migrations/V119__group_source.sql internal/models/group.go
git commit -m "feat: add groups.source provenance column (V119)"
```

---

### Task 2: Serialize `source` and tag Everyone as `system`

**Files:**
- Modify: `internal/api/group_handlers.go:50-88` (list), `:122-131` (create), `:195-207` (update)
- Modify: `internal/api/org_handlers.go:105`, `:282`
- Modify: `internal/api/admin_handlers.go:301`
- Modify: `internal/api/auth_handlers.go:123`
- Test: `internal/api/group_handlers_test.go`, `internal/api/everyone_group_test.go`

**Step 1: Write the failing tests**

In `internal/api/group_handlers_test.go`, inside `TestGroupCRUD` after the create response is decoded (around line 46), add:

```go
	if g["source"] != "manual" {
		t.Fatalf("expected create to return source=manual, got %v", g["source"])
	}
```

and after the list group is found (around line 72):

```go
	if listed["source"] != "manual" {
		t.Fatalf("expected list to return source=manual, got %v", listed["source"])
	}
```

and after the rename response is checked (around line 145):

```go
	if labeled["source"] != "manual" {
		t.Fatalf("expected update to return source=manual, got %v", labeled["source"])
	}
```

In `internal/api/everyone_group_test.go`, replace the `found` loop body to also assert source:

```go
	found := false
	for _, g := range groups {
		if g["name"] == "Everyone" {
			found = true
			if g["source"] != "system" {
				t.Errorf("Everyone source = %v, want system", g["source"])
			}
		}
	}
```

**Step 2: Run tests to verify they fail**

```bash
go test -timeout 3m ./internal/api/ -run 'TestGroupCRUD|TestEveryoneGroupExists' -count=1 -v
```
Expected: FAIL — `source` missing from responses (`<nil>`).

**Step 3: Implement**

`internal/api/group_handlers.go` — add `g.source` to both list queries and scans:

```go
		query = `SELECT g.id, g.org_id, g.name, g.display_name, g.source, g.created_at, COUNT(gm2.user_id) AS member_count
                 FROM groups g
                 JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $2
                 LEFT JOIN group_members gm2 ON gm2.group_id = g.id
                 WHERE g.org_id = $1
                 GROUP BY g.id
                 ORDER BY COALESCE(g.display_name, g.name)`
```
```go
		query = `SELECT g.id, g.org_id, g.name, g.display_name, g.source, g.created_at, COUNT(gm.user_id) AS member_count
                 FROM groups g
                 LEFT JOIN group_members gm ON gm.group_id = g.id
                 WHERE g.org_id = $1
                 GROUP BY g.id
                 ORDER BY COALESCE(g.display_name, g.name)`
```
```go
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.DisplayName, &g.Source, &g.CreatedAt, &g.MemberCount); err != nil {
```

Create:

```go
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name, display_name) VALUES ($1, $2, $3)
		 RETURNING id, org_id, name, display_name, source, created_at`,
		claims.OrgID, req.Name, normalizeDisplayName(req.DisplayName),
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.DisplayName, &g.Source, &g.CreatedAt)
```

Update `RETURNING` and scan the same way:

```go
		  RETURNING id, org_id, name, display_name, source, created_at`,
		req.Name, req.DisplayName != nil, display, groupID, claims.OrgID,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.DisplayName, &g.Source, &g.CreatedAt)
```

Everyone bootstraps (all four sites):

```go
		`INSERT INTO groups (org_id, name, source) VALUES ($1, 'Everyone', 'system') ON CONFLICT DO NOTHING`,
```

- `internal/api/org_handlers.go:105` and `:282`
- `internal/api/admin_handlers.go:301`
- `internal/api/auth_handlers.go:123`

**Step 4: Run tests to verify they pass**

```bash
go test -timeout 3m ./internal/api/ -run 'TestGroupCRUD|TestEveryoneGroupExists|TestUpdateEveryoneGroupRejectsNameAndLabel' -count=1 -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/group_handlers.go internal/api/org_handlers.go internal/api/admin_handlers.go internal/api/auth_handlers.go internal/api/group_handlers_test.go internal/api/everyone_group_test.go
git commit -m "feat: expose group source and tag Everyone as system"
```

---

### Task 3: SSO create/adopt sets `source='sso'`

**Files:**
- Modify: `internal/api/sso_group_sync.go:83` (caller), `:167-189` (FindOrCreateGroup)
- Test: `internal/api/sso_group_sync_test.go`

**Step 1: Write the failing tests**

Update the existing call at `sso_group_sync_test.go:31` and `:37` for the new 4-value signature:

```go
	groupID, _, _, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "aether-analysts")
	require.NoError(t, err)
```
```go
	again, created, _, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "AETHER-ANALYSTS")
```

Add a new test at the end of the file:

```go
func TestFindOrCreateGroup_SourceTransitions(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&orgID))

	sourceOf := func(id string) string {
		t.Helper()
		var src string
		require.NoError(t, s.DB().Pool.QueryRow(ctx, `SELECT source FROM groups WHERE id=$1`, id).Scan(&src))
		return src
	}

	// A brand new group is created as sso.
	id, created, adopted, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "engineering")
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, adopted)
	require.Equal(t, "sso", sourceOf(id))

	// A manual group is adopted exactly once.
	var manualID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name, source) VALUES ($1, 'manual-group', 'manual') RETURNING id`,
		orgID).Scan(&manualID))
	id, created, adopted, err = api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "MANUAL-GROUP")
	require.NoError(t, err)
	require.False(t, created)
	require.True(t, adopted)
	require.Equal(t, manualID, id)
	require.Equal(t, "sso", sourceOf(manualID))

	_, _, adopted, err = api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "manual-group")
	require.NoError(t, err)
	require.False(t, adopted, "adoption must only fire on the manual -> sso transition")

	// A system group is never flipped.
	var systemID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name, source) VALUES ($1, 'Platform', 'system') RETURNING id`,
		orgID).Scan(&systemID))
	_, _, adopted, err = api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "Platform")
	require.NoError(t, err)
	require.False(t, adopted)
	require.Equal(t, "system", sourceOf(systemID))
}

func TestSyncSSOGroups_AdoptsManualGroup(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&orgID))

	var userID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("adopt-%d@test.com", time.Now().UnixNano()), "Adopt").Scan(&userID))
	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`, orgID, userID)
	require.NoError(t, err)

	var groupID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name, source) VALUES ($1, 'engineering', 'manual') RETURNING id`,
		orgID).Scan(&groupID))

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope: "org", OrgID: &orgID, Name: "adopt-test", ProviderType: "oidc",
		ClientID: "test-client", ClientSecret: "test-secret",
		DiscoveryURL: "https://example.com/", AllowedDomains: []string{},
		Scopes: []string{}, Enabled: true, AutoSyncGroups: true,
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering"})

	var src string
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `SELECT source FROM groups WHERE id=$1`, groupID).Scan(&src))
	assert.Equal(t, "sso", src)

	var n int
	require.NoError(t, s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id=$1 AND action='group.sso.adopt' AND user_id=$2`,
		orgID, userID).Scan(&n))
	assert.Equal(t, 1, n, "adoption must emit group.sso.adopt")
}
```

**Step 2: Run tests to verify they fail**

```bash
go test -timeout 3m ./internal/api/ -run 'TestFindOrCreateGroup|TestSyncSSOGroups_AdoptsManualGroup' -count=1 -v
```
Expected: compile failure — `FindOrCreateGroup` returns 3 values.

**Step 3: Implement**

In `internal/api/sso_group_sync.go`, replace `FindOrCreateGroup`:

```go
// FindOrCreateGroup returns the ID of the org's group with the given name
// (case-insensitive), inserting it when absent. created reports whether this
// call inserted the row; adopted reports whether an existing manual group was
// flipped to SSO-managed.
func FindOrCreateGroup(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (string, bool, bool, error) {
	var id, source string
	err := pool.QueryRow(ctx,
		`SELECT id, source FROM groups WHERE org_id=$1 AND LOWER(name)=LOWER($2)`,
		orgID, name,
	).Scan(&id, &source)
	if err == nil {
		if source != "manual" {
			return id, false, false, nil
		}
		tag, uerr := pool.Exec(ctx,
			`UPDATE groups SET source='sso' WHERE id=$1 AND source='manual'`,
			id,
		)
		if uerr != nil {
			return "", false, false, fmt.Errorf("adopt group: %w", uerr)
		}
		return id, false, tag.RowsAffected() > 0, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, false, fmt.Errorf("lookup group: %w", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name, source) VALUES ($1, $2, 'sso') RETURNING id`,
		orgID, name,
	).Scan(&id)
	if err != nil {
		return "", false, false, fmt.Errorf("create group: %w", err)
	}

	return id, true, false, nil
}
```

In `SyncSSOGroups`, update the call site (line ~83):

```go
		groupID, created, adopted, err := FindOrCreateGroup(ctx, pool, orgID, groupName)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, "", groupName, err)
			continue
		}
		if created {
			logGroupSyncEvent(ctx, logger, "group.sso.create", orgID, userID, groupID, groupName)
		}
		if adopted {
			logGroupSyncEvent(ctx, logger, "group.sso.adopt", orgID, userID, groupID, groupName)
		}
```

**Step 4: Run tests to verify they pass**

```bash
go test -timeout 3m ./internal/api/ -run 'TestFindOrCreateGroup|TestSyncSSOGroups' -count=1 -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/sso_group_sync.go internal/api/sso_group_sync_test.go
git commit -m "feat: adopt manual groups into SSO source and audit it"
```

---

### Task 4: Delete guard with force override

**Files:**
- Modify: `internal/api/group_handlers.go:236-271`
- Test: `internal/api/group_handlers_test.go`, `internal/api/everyone_group_test.go`

**Step 1: Write the failing tests**

Add to `internal/api/group_handlers_test.go` (imports gain `"context"` and `"github.com/stretchr/testify/require"`):

```go
func TestDeleteGroupSourceGuards(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-source-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Source Org")
	ctx := context.Background()

	create := func(name string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": name})
		req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create group: %d %s", rec.Code, rec.Body.String())
		}
		var g map[string]any
		json.NewDecoder(rec.Body).Decode(&g)
		if g["source"] != "manual" {
			t.Fatalf("created group source = %v, want manual", g["source"])
		}
		return g["id"].(string)
	}

	del := func(id, query string) int {
		t.Helper()
		req := httptest.NewRequest("DELETE", "/api/v1/groups/"+id+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	// SSO group: blocked without force, deletable with force.
	ssoID := create("SSO Managed")
	_, err := srv.DB().Pool.Exec(ctx, `UPDATE groups SET source='sso' WHERE id=$1`, ssoID)
	require.NoError(t, err)
	if code := del(ssoID, ""); code != http.StatusBadRequest {
		t.Fatalf("sso delete without force: got %d, want 400", code)
	}
	if code := del(ssoID, "?force=true"); code != http.StatusNoContent {
		t.Fatalf("sso delete with force: got %d, want 204", code)
	}
	var forcedAudits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.delete.forced' AND resource_id=$1`, ssoID,
	).Scan(&forcedAudits))
	require.Equal(t, 1, forcedAudits)

	// System group: force cannot delete it.
	systemID := create("System Managed")
	_, err = srv.DB().Pool.Exec(ctx, `UPDATE groups SET source='system' WHERE id=$1`, systemID)
	require.NoError(t, err)
	if code := del(systemID, "?force=true"); code != http.StatusBadRequest {
		t.Fatalf("system delete with force: got %d, want 400", code)
	}

	// Manual group: unchanged behavior.
	manualID := create("Manual Group")
	if code := del(manualID, ""); code != http.StatusNoContent {
		t.Fatalf("manual delete: got %d, want 204", code)
	}
}
```

Add to `internal/api/everyone_group_test.go`:

```go
func TestEveryoneGroupIsUndeletableWithForce(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("everyone-force-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Everyone Force Org")

	listReq := httptest.NewRequest("GET", "/api/v1/groups", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	srv.ServeHTTP(listRec, listReq)
	var groups []map[string]any
	json.NewDecoder(listRec.Body).Decode(&groups)

	var everyoneID string
	for _, g := range groups {
		if g["name"] == "Everyone" {
			everyoneID = g["id"].(string)
		}
	}
	if everyoneID == "" {
		t.Fatal("expected the org's Everyone group")
	}

	req := httptest.NewRequest("DELETE", "/api/v1/groups/"+everyoneID+"?force=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete Everyone with force: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test -timeout 3m ./internal/api/ -run 'TestDeleteGroupSourceGuards|TestEveryoneGroupIsUndeletableWithForce' -count=1 -v
```
Expected: FAIL — SSO group delete returns 204 without force.

**Step 3: Implement**

Replace the beginning of `handleDeleteGroup` in `internal/api/group_handlers.go`:

```go
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	var name, source string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT name, source FROM groups WHERE id=$1 AND org_id=$2`,
		groupID, claims.OrgID,
	).Scan(&name, &source)
	if err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	// The name check is defense-in-depth for rows that predate the source
	// backfill; source='system' is the durable marker.
	if strings.EqualFold(name, "everyone") || source == "system" {
		writeError(w, http.StatusBadRequest, "this group cannot be deleted")
		return
	}
	force := r.URL.Query().Get("force") == "true"
	if source == "sso" && !force {
		writeError(w, http.StatusBadRequest, "group is managed by SSO; pass force=true to delete")
		return
	}

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM groups WHERE id=$1 AND org_id=$2`,
		groupID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	// A deleted group loses its roles: reconcile warehouses holding grants for
	// it (the grants outlive the group and are ignored by desired state).
	s.enqueueWarehouseSyncForGroup(ctx, groupID)
	action := "group.delete"
	if source == "sso" {
		action = "group.delete.forced"
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: action, ResourceType: "group", ResourceID: groupID,
	})
	w.WriteHeader(http.StatusNoContent)
}
```

`isEveryoneGroup` stays in the file (still used by `handleUpdateGroup`).

**Step 4: Run tests to verify they pass**

```bash
go test -timeout 3m ./internal/api/ -run 'TestDeleteGroup|TestEveryoneGroup|TestGroupCRUD' -count=1 -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/group_handlers.go internal/api/group_handlers_test.go internal/api/everyone_group_test.go
git commit -m "feat: guard SSO-managed group deletion with audited force override"
```

---

### Task 5: CLI `--force` and `source` field

**Files:**
- Modify: `internal/cli/types.go:120-126`
- Modify: `internal/cli/groups.go:35-37`, `:124-139`

**Step 1: Add the field and flag**

`internal/cli/types.go`:

```go
	Group struct {
		ID          string    `json:"id"`
		OrgID       string    `json:"org_id"`
		Name        string    `json:"name"`
		Source      string    `json:"source"`
		MemberCount int       `json:"member_count"`
		CreatedAt   time.Time `json:"created_at"`
	}
```

`internal/cli/groups.go`:

```go
func (c *Client) DeleteGroup(id string, force bool) error {
	path := "/api/v1/groups/" + id
	if force {
		path += "?force=true"
	}
	return c.DeleteJSON(path)
}
```

Replace the delete command with:

```go
		func() *cobra.Command {
			var force bool
			cmd := &cobra.Command{
				Use:   "delete <id>",
				Short: "Delete a group",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					if err := cl.DeleteGroup(args[0], force); err != nil {
						return err
					}
					fmt.Println("Deleted.")
					return nil
				},
			}
			cmd.Flags().BoolVar(&force, "force", false, "Force-delete an SSO-managed group")
			return cmd
		}(),
```

`groups list` prints JSON via `PrintJSON`, so `source` appears automatically — no display change needed.

**Step 2: Verify build and vet**

```bash
go build ./... && go vet ./internal/cli/
```
Expected: no output (success).

**Step 3: Commit**

```bash
git add internal/cli/types.go internal/cli/groups.go
git commit -m "feat: add --force to groups delete and expose source in CLI"
```

---

### Task 6: Frontend type, badge, menu gating, force-delete dialog

**Files:**
- Modify: `web/src/types/index.ts:290-297`
- Modify: `web/src/pages/GroupsPage.tsx` (mutation `:368-381`, render `:656-784`, `:837`, dialog `:920-928`, styles `:1123-1135`)
- Modify: `web/src/test/handlers.ts:50-59`, `:197-214`
- Test: `web/src/test/GroupsPage.test.tsx`

**Step 1: Write the failing tests**

Add to `web/src/test/GroupsPage.test.tsx`:

```tsx
// ── Group provenance ────────────────────────────────────────────────────────

describe('Group provenance', () => {
  test('shows an SSO badge only for SSO-managed groups', async () => {
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-sso', org_id: 'org-1', name: 'aether-analysts', source: 'sso',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
        { id: 'g-manual', org_id: 'org-1', name: 'Manual Group', source: 'manual',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
      ])),
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('aether-analysts')
    expect(screen.getAllByText('SSO')).toHaveLength(1)
  })

  test('force-deletes an SSO group after the warning dialog', async () => {
    let deletedPath = ''
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-sso', org_id: 'org-1', name: 'aether-analysts', source: 'sso',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
      ])),
      http.delete('/api/v1/groups/:id', ({ request }) => {
        const url = new URL(request.url())
        deletedPath = url.pathname + url.search
        return new HttpResponse(null, { status: 204 })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('aether-analysts')
    fireEvent.click(screen.getByTitle('Group actions'))
    fireEvent.click(screen.getByText('Delete'))
    expect(await screen.findByText(/managed by SSO/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete anyway' }))
    await waitFor(() => expect(deletedPath).toBe('/api/v1/groups/g-sso?force=true'))
  })

  test('manual groups keep the regular delete dialog and request', async () => {
    let deletedPath = ''
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-manual', org_id: 'org-1', name: 'Manual Group', source: 'manual',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
      ])),
      http.delete('/api/v1/groups/:id', ({ request }) => {
        const url = new URL(request.url())
        deletedPath = url.pathname + url.search
        return new HttpResponse(null, { status: 204 })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Manual Group')
    fireEvent.click(screen.getByTitle('Group actions'))
    fireEvent.click(screen.getByText('Delete'))
    expect(await screen.findByText(/This cannot be undone/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(deletedPath).toBe('/api/v1/groups/g-manual'))
  })

  test('system groups expose no actions menu', async () => {
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-system', org_id: 'org-1', name: 'Platform', source: 'system',
          display_name: null, member_count: 1, created_at: '2026-01-01T00:00:00Z' },
      ])),
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Platform')
    expect(screen.queryByTitle('Group actions')).toBeNull()
  })
})
```

**Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/test/GroupsPage.test.tsx
```
Expected: FAIL — no SSO badge, no force query param, system group still renders a menu.

**Step 3: Implement**

`web/src/types/index.ts`:

```ts
export interface Group {
  id: string
  org_id: string
  name: string
  display_name: string | null
  source: 'manual' | 'sso' | 'system'
  member_count: number
  created_at: string
}
```

`web/src/pages/GroupsPage.tsx`:

1. Delete mutation:

```tsx
  const deleteGroup = useMutation({
    mutationFn: ({ id, force }: { id: string; force?: boolean }) =>
      api.delete(`/api/v1/groups/${id}${force ? '?force=true' : ''}`),
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: ['groups'] })
      if (expandedId === id) setExpandedId(null)
      setGroupMembers((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
      setMutateError(null)
    },
    onError: (err: Error) => setMutateError(err.message),
  })
```

2. Inside `groups.map`, after `isEveryone`:

```tsx
            const isEveryone = /^everyone$/i.test(group.name)
            const isSystem = group.source === 'system'
```

3. Badge next to the name (after the `{isEveryone && (...)}` System badge):

```tsx
                    {group.source === 'sso' && (
                      <span style={styles.ssoBadge}>SSO</span>
                    )}
```

4. Generalize admin controls from `!isEveryone` to `!isSystem` in four places: the actions-menu condition (line ~726), member remove (line ~804), pending remove (line ~823), and the add/paste block (line ~837). Keep `isEveryone` for the name display, note, and rename restrictions.

5. Delete dialog:

```tsx
      <ConfirmDialog
        open={!!deleteGroupConfirm}
        title={deleteGroupConfirm?.source === 'sso' ? 'Delete SSO-managed group' : 'Delete group'}
        message={
          deleteGroupConfirm?.source === 'sso'
            ? `"${groupLabel(deleteGroupConfirm)}" is managed by SSO. Deleting it won't stop your identity provider from recreating it if it still sends a group named "${deleteGroupConfirm.name}". Delete anyway?`
            : `Delete group "${deleteGroupConfirm ? groupLabel(deleteGroupConfirm) : ''}"? This cannot be undone.`
        }
        confirmLabel={deleteGroupConfirm?.source === 'sso' ? 'Delete anyway' : 'Delete'}
        destructive
        onConfirm={() => {
          if (deleteGroupConfirm) {
            deleteGroup.mutate({ id: deleteGroupConfirm.id, force: deleteGroupConfirm.source === 'sso' })
          }
          setDeleteGroupConfirm(null)
        }}
        onCancel={() => setDeleteGroupConfirm(null)}
      />
```

6. Style, next to `systemBadge`:

```tsx
  ssoBadge: {
    fontSize: 10,
    fontWeight: 600,
    color: 'var(--accent)',
    background: 'var(--accent-light)',
    border: '1px solid var(--border)',
    borderRadius: 3,
    padding: '1px 6px',
    marginLeft: 6,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.5px',
    lineHeight: '18px',
  },
```

`web/src/test/handlers.ts`: add `source: 'manual'` to both `GROUPS` entries and to the POST/PUT response objects.

**Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/test/GroupsPage.test.tsx
```
Expected: PASS (including all pre-existing GroupsPage tests).

Also run the full web suite and typecheck:
```bash
cd web && npx vitest run && npx tsc --noEmit
```
Expected: PASS, no TS errors.

**Step 5: Commit**

```bash
git add web/src/types/index.ts web/src/pages/GroupsPage.tsx web/src/test/handlers.ts web/src/test/GroupsPage.test.tsx
git commit -m "feat: show SSO badge and guard SSO group deletion in the console"
```

---

### Task 7: Swagger regeneration and docs

**Files:**
- Regenerate: `internal/api/docs/` (docs.go, swagger.json, swagger.yaml)
- Modify: `docs/SSO_GROUP_PROVISIONING.md`
- Modify: `AGENTS.md`

**Step 1: Regenerate Swagger**

```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```
Expected: rewritten `docs.go`, `swagger.json`, `swagger.yaml` (the `models.Group` schema gains `source`).

**Step 2: Update `docs/SSO_GROUP_PROVISIONING.md`**

- Under `### groups — new column` (line ~63), add:

```sql
source text NOT NULL DEFAULT 'manual'  -- 'manual' | 'sso' | 'system'
```

with a short paragraph: set to `sso` on SSO creation, flipped from `manual` to `sso` when sync adopts an existing group by name (one-way), `system` for Everyone, `manual` otherwise. The console renders an SSO badge for `source='sso'`.

- In "Key behaviors" (line ~82), extend the first bullet:

> - Groups are never deleted by sync — only memberships are removed
> - Deleting an `sso` group requires `DELETE /groups/{id}?force=true` (audited as `group.delete.forced`); `system` groups are never deletable

- Add `group.sso.adopt` and `group.delete.forced` rows to the Audit Events table.
- Migration paragraph (line ~191): mention `V119__group_source.sql` and the best-effort backfill (groups with active `sso_group_memberships` rows become `sso`; Everyone becomes `system`).

**Step 3: Update `AGENTS.md`**

- In the groups paragraph, add: groups carry `source` (`manual`/`sso`/`system`); SSO create/adopt sets `sso`; `DELETE /groups/{id}` refuses `sso` unless `?force=true` (audited `group.delete.forced`) and never deletes `system`.
- In the Frontend section, add a mandatory rule:

> **Real-browser validation is mandatory for every UI change.** After the dev stack is up, validate the changed flows with agent-browser (`agent-browser open`, `snapshot -i`, interact, `screenshot`, `errors`), not just component tests.

**Step 4: Commit**

```bash
git add internal/api/docs docs/SSO_GROUP_PROVISIONING.md AGENTS.md
git commit -m "docs: document group provenance, delete guard, and browser validation rule"
```

---

### Task 8: Playwright E2E spec

**Files:**
- Create: `e2e/group-source.spec.ts`

**Step 1: Write the spec**

```ts
import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { loginAsAdmin } from './helpers'

const repoRoot = path.resolve(__dirname, '..')

function setGroupSource(groupId: string, source: 'sso' | 'system'): void {
  execFileSync(
    'docker',
    [
      'compose', '-f', 'docker-compose.dev.yml',
      'exec', '-T', 'aether-postgres',
      'psql', '-U', 'aether', '-d', 'aether', '-c',
      `UPDATE groups SET source='${source}' WHERE id='${groupId}'`,
    ],
    { cwd: repoRoot, stdio: 'pipe' },
  )
}

async function adminHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  expect(token).toBeTruthy()
  return { Authorization: `Bearer ${token}` }
}

test.describe('Group provenance and SSO-managed delete guard', () => {
  test('manual group has no badge and deletes with the regular dialog', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `Manual Group ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok()).toBeTruthy()

    await page.goto('/groups')
    const row = page.getByText(name).locator('..').locator('..')
    await expect(row.getByTitle('Group actions')).toBeVisible()
    await expect(row.getByText('SSO')).toHaveCount(0)

    await row.getByTitle('Group actions').click()
    await page.getByText('Delete', { exact: true }).click()
    await expect(page.getByText(/This cannot be undone/)).toBeVisible()
    await page.getByRole('button', { name: 'Delete', exact: true }).click()
    await expect(page.getByText(name)).toHaveCount(0)
  })

  test('SSO group shows a badge, blocks unforced API delete, and force-deletes from the UI', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `SSO Group ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok()).toBeTruthy()
    const group = await resp.json()
    setGroupSource(group.id, 'sso')

    await page.goto('/groups')
    const row = page.getByText(name).locator('..').locator('..')
    await expect(row.getByText('SSO')).toBeVisible()

    const blocked = await request.delete(`/api/v1/groups/${group.id}`, { headers })
    expect(blocked.status()).toBe(400)

    await row.getByTitle('Group actions').click()
    await page.getByText('Delete', { exact: true }).click()
    await expect(page.getByText(/managed by SSO/i)).toBeVisible()
    await page.getByRole('button', { name: 'Delete anyway' }).click()
    await expect(page.getByText(name)).toHaveCount(0)
  })

  test('Everyone exposes no actions menu', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/groups')
    const row = page.getByText('Everyone', { exact: true }).first().locator('..').locator('..')
    await expect(row.getByText('System')).toBeVisible()
    await expect(row.getByTitle('Group actions')).toHaveCount(0)
  })
})
```

**Step 2: Ensure the dev stack is running, then run the spec**

```bash
docker compose -f docker-compose.dev.yml up -d
cd e2e && npx playwright test group-source.spec.ts
```
Expected: 3 passed.

**Step 3: Commit**

```bash
git add e2e/group-source.spec.ts
git commit -m "test: cover group provenance badge and SSO delete guard end to end"
```

---

### Task 9: Real-browser validation with agent-browser (mandatory)

**Step 1: Start the stack and log in**

```bash
docker compose -f docker-compose.dev.yml up -d
agent-browser --session aether open http://localhost:5173/login
agent-browser --session aether snapshot -i
# fill email admin@heaven-labs.com, submit, fill password admin123, submit
agent-browser --session aether wait --url "http://localhost:5173/"
```

**Step 2: Manual group (no badge)**

```bash
agent-browser --session aether open http://localhost:5173/groups
agent-browser --session aether snapshot -i
# create "Browser Check <ts>" via the create row
agent-browser --session aether screenshot --screenshot-dir /tmp/opencode/groups-validation
```
Expected: row shows no SSO badge. Screenshot saved as evidence.

**Step 3: SSO group (badge + force dialog)**

```bash
docker compose -f docker-compose.dev.yml exec -T aether-postgres \
  psql -U aether -d aether -c "UPDATE groups SET source='sso' WHERE name='Browser Check <ts>'"
# reload /groups, snapshot
```
Expected: SSO badge next to the name; screenshot.

```bash
# open the row menu → Delete
```
Expected: dialog titled "Delete SSO-managed group" with the IdP-recreation warning and a "Delete anyway" button. Screenshot the dialog before confirming.

```bash
# click "Delete anyway"
```
Expected: row disappears; screenshot after.

**Step 4: Everyone gating and API double-check**

- Expand/snapshot the Everyone row: no ⋯ menu.
- `curl -s -o /dev/null -w "%{http_code}" -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8088/api/v1/groups/<sso-group-id>` → `400` (get `$TOKEN` from the browser localStorage or `agent-browser eval 'localStorage.getItem("aether_token")'`).

**Step 5: Console/errors clean**

```bash
agent-browser --session aether errors
agent-browser --session aether console
agent-browser --session aether close
```
Expected: no errors, no warnings from the changed code. If any issue is found, fix it, re-run this task, and commit the fix.

---

### Task 10: Full verification

**Step 1: Run all CI checks locally**

```bash
task check                                  # fmt + vet + tidy + all Go tests
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
task test:e2e
```
Expected: all green.

**Step 2: Review the diff against the spec**

```bash
git log --oneline main..HEAD
git diff main --stat
```
Confirm every design section (schema, API, sync adoption, guard, CLI, UI, tests, docs, browser validation) is implemented and the working tree is clean. Commit any remaining fixups.

---

## Notes for the implementer

- Never commit directly to `main`; this work lives on `feat/group-source-sso-managed`.
- All Go test commands include `-timeout 3m` per AGENTS.md.
- Tests hit the real dev Postgres; bring infra up with `task infra:up` if `go test` cannot connect.
- Frontend colors must use CSS variables; the SSO badge uses `--accent`/`--accent-light`.
- Do not add comments to Go/TS code beyond the few shown here.
- If `e2e` Playwright specs fail on `__dirname`, replace with `path.resolve(process.cwd(), '..')` (specs run from the `e2e/` directory).
