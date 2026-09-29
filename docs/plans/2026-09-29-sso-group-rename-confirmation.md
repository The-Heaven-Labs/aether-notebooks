# Guarded Rename for SSO-Managed Groups — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
> Use @test-driven-development for every task that changes behavior: write the failing test, watch it fail, implement, watch it pass, commit.

**Goal:** Renaming an SSO-synced group requires an explicit type-the-current-name confirmation in the UI and a matching `confirm_name` acknowledgement in the API; display-label-only edits stay frictionless.

**Architecture:** `groups.name` is the SSO sync key, so the API becomes source-aware: `handleUpdateGroup` loads `name, source`, returns 409 unless `confirm_name` equals the current name when an `sso` group's name changes, and audits acknowledged renames as `group.sso.rename`. The frontend intercepts such renames in the inline editor and opens a `ConfirmDialog` with a typed confirmation before calling the API. The CLI gains `--confirm-name`.

**Tech Stack:** Go (net/http ServeMux, pgx), React + TypeScript + React Query + Vitest/MSW, Cobra CLI, Playwright e2e, agent-browser for manual validation.

**Design doc:** `docs/plans/2026-09-29-sso-group-rename-confirmation-design.md`

**Prerequisite:** `task infra:up` (tests hit a real Postgres at `localhost:5432`). Go test commands must keep `-timeout 3m`. Shell env for direct `go test` runs (Taskfile sets these for `task`, not for bare commands):

```bash
export AETHER_DATABASE_URL='postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable'
export AETHER_MASTER_KEY='dev-master-key-change-in-production'
export AETHER_JWT_SECRET='dev-jwt-secret-change-in-production'
```

---

### Task 1: Backend — require `confirm_name` for SSO group renames

**Files:**
- Modify: `internal/api/group_handlers.go:17-20` (request struct), `:151-214` (`handleUpdateGroup`)
- Test: `internal/api/group_handlers_test.go` (append new test)

**Step 1: Write the failing test**

Append to `internal/api/group_handlers_test.go`:

```go
func TestUpdateSSOGroupRenameRequiresConfirmation(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-sso-rename-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group SSO Rename Org")
	ctx := context.Background()

	createBody, _ := json.Marshal(map[string]any{"name": "aether-analysts"})
	createReq := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created map[string]any
	require.NoError(t, json.NewDecoder(createRec.Body).Decode(&created))
	groupID := created["id"].(string)

	_, err := srv.DB().Pool.Exec(ctx, `UPDATE groups SET source='sso' WHERE id=$1`, groupID)
	require.NoError(t, err)

	put := func(body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var decoded map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&decoded)
		return rec, decoded
	}

	// Rename without confirm_name: 409 and the name stays put.
	rec, _ := put(map[string]any{"name": "renamed"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var dbName string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, groupID).Scan(&dbName))
	require.Equal(t, "aether-analysts", dbName)

	// Wrong confirm_name: still 409.
	rec, _ = put(map[string]any{"name": "renamed", "confirm_name": "not-the-name"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// A case-only change is still an identity change and needs confirmation.
	rec, _ = put(map[string]any{"name": "AETHER-ANALYSTS"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// Correct confirm_name: acknowledged rename.
	rec, decoded := put(map[string]any{"name": "renamed", "confirm_name": "aether-analysts"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "renamed", decoded["name"])
	var renameAudits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.sso.rename' AND resource_id=$1`, groupID,
	).Scan(&renameAudits))
	require.Equal(t, 1, renameAudits)
	var oldName, newName string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT metadata->>'old_name', metadata->>'new_name' FROM audit_logs WHERE action='group.sso.rename' AND resource_id=$1`,
		groupID,
	).Scan(&oldName, &newName))
	require.Equal(t, "aether-analysts", oldName)
	require.Equal(t, "renamed", newName)

	// Display-only update needs no confirmation and stays a plain group.update.
	rec, decoded = put(map[string]any{"display_name": "Data Analysts"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "renamed", decoded["name"])
	require.Equal(t, "Data Analysts", decoded["display_name"])
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.sso.rename' AND resource_id=$1`, groupID,
	).Scan(&renameAudits))
	require.Equal(t, 1, renameAudits)
	var updateAudits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action='group.update' AND resource_id=$1`, groupID,
	).Scan(&updateAudits))
	require.Equal(t, 1, updateAudits)
}
```

**Step 2: Run test to verify it fails**

```bash
go test -timeout 3m ./internal/api/ -run TestUpdateSSOGroupRenameRequiresConfirmation -v -count=1
```

Expected: FAIL — first assertion gets 200 instead of 409 (`StatusConflict`).

**Step 3: Implement**

In `internal/api/group_handlers.go`, add `fmt` to the import block, then:

Add `ConfirmName` to the request struct:

```go
type updateGroupRequest struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	ConfirmName *string `json:"confirm_name"`
}
```

Replace the opening lookup of `handleUpdateGroup` (currently `isEveryone, err := s.isEveryoneGroup(...)` at `:156-160`) with:

```go
	var currentName, currentSource string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT name, source FROM groups WHERE id=$1 AND org_id=$2`,
		groupID, claims.OrgID,
	).Scan(&currentName, &currentSource)
	if err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	isEveryone := strings.EqualFold(currentName, "everyone")
```

After the name/display validation block (right before `display := ""` at `:191`), insert the guard:

```go
	nameChanged := req.Name != nil && *req.Name != currentName
	if nameChanged && currentSource == "sso" {
		if req.ConfirmName == nil || *req.ConfirmName != currentName {
			writeError(w, http.StatusConflict,
				fmt.Sprintf("group is managed by SSO; renaming it disconnects sync. Send confirm_name with the current name (%q) to proceed", currentName))
			return
		}
	}
```

Replace the audit block (`:208-212`) with:

```go
	action := "group.update"
	metadata := map[string]any{"display_name": g.DisplayName}
	if nameChanged && currentSource == "sso" {
		action = "group.sso.rename"
		metadata = map[string]any{"old_name": currentName, "new_name": g.Name, "display_name": g.DisplayName}
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: action, ResourceType: "group", ResourceID: groupID, ResourceName: g.Name,
		Metadata: metadata,
	})
```

Also update the swagger block above the handler: extend the `@Description` with `Renaming an SSO-managed group requires confirm_name equal to the current name.` and add `@Failure 409 {object} map[string]string`.

**Step 4: Run test to verify it passes**

```bash
go test -timeout 3m ./internal/api/ -run 'TestUpdateSSOGroupRenameRequiresConfirmation' -v -count=1
```

Expected: PASS.

**Step 5: Run the neighboring group tests for regressions**

```bash
go test -timeout 3m ./internal/api/ -run 'TestGroupCRUD|TestDeleteGroupSourceGuards|TestUpdateEveryoneGroupRejectsNameAndLabel|TestUpdateSSOGroupRenameRequiresConfirmation' -v -count=1
```

Expected: all PASS.

**Step 6: Commit**

```bash
git add internal/api/group_handlers.go internal/api/group_handlers_test.go
git commit -m "feat(api): require confirm_name to rename SSO-managed groups"
```

---

### Task 2: Backend — duplicate group name returns 409, not 404

**Files:**
- Test: `internal/api/group_handlers_test.go` (append)
- Modify: `internal/api/group_handlers.go:204-207` (UPDATE error branch)

**Step 1: Write the failing test**

```go
func TestUpdateGroupDuplicateNameConflict(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-duplicate-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Duplicate Org")
	ctx := context.Background()

	create := func(name string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": name})
		req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var g map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&g))
		return g["id"].(string)
	}

	create("Alpha")
	secondID := create("Beta")

	payload, _ := json.Marshal(map[string]any{"name": "Alpha"})
	req := httptest.NewRequest("PUT", "/api/v1/groups/"+secondID, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	var dbName string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, secondID).Scan(&dbName))
	require.Equal(t, "Beta", dbName)
}
```

**Step 2: Run test to verify it fails**

```bash
go test -timeout 3m ./internal/api/ -run TestUpdateGroupDuplicateNameConflict -v -count=1
```

Expected: FAIL — gets 404 instead of 409.

**Step 3: Implement**

`isUniqueViolation` already exists in the same package (`internal/api/mcp_server_handlers.go:303`). Replace the UPDATE error branch (`:204-207`):

```go
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a group with this name already exists")
			return
		}
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
```

**Step 4: Run the test**

```bash
go test -timeout 3m ./internal/api/ -run TestUpdateGroupDuplicateNameConflict -v -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/group_handlers.go internal/api/group_handlers_test.go
git commit -m "fix(api): return 409 when renaming a group to an existing name"
```

---

### Task 3: Frontend — typed confirmation before renaming an SSO group

**Files:**
- Test: `web/src/test/GroupsPage.test.tsx` (append new describe block)
- Modify: `web/src/pages/GroupsPage.tsx:357-366` (mutation), `:493-507` (handlers), new state near `:327-330`, dialog JSX before the closing `</AppShell>` at `:967`

**Step 1: Write the failing tests**

Append to `web/src/test/GroupsPage.test.tsx` (it already has `server`, `fireEvent`, `waitFor`, `http`, `HttpResponse` imported):

```tsx
// ── SSO group rename guard ──────────────────────────────────────────────────

describe('SSO group rename guard', () => {
  const ssoGroup = {
    id: 'g-sso', org_id: 'org-1', name: 'aether-analysts', source: 'sso',
    display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z',
  }

  test('renaming an SSO group requires typing the synced name', async () => {
    let putBody: Record<string, unknown> = {}
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([ssoGroup])),
      http.put('/api/v1/groups/g-sso', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ ...ssoGroup, name: putBody.name as string })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('aether-analysts')

    fireEvent.click(screen.getByTitle('Group actions'))
    fireEvent.click(screen.getByText('Rename'))
    const nameInput = screen.getByLabelText('Group name')
    fireEvent.change(nameInput, { target: { value: 'Analysts' } })
    fireEvent.keyDown(nameInput, { key: 'Enter' })

    expect(await screen.findByText('Rename SSO-managed group?')).toBeInTheDocument()
    const confirmBtn = screen.getByRole('button', { name: 'Rename group' })
    expect(confirmBtn).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Confirm group name'), { target: { value: 'wrong' } })
    expect(confirmBtn).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Confirm group name'), { target: { value: 'aether-analysts' } })
    expect(confirmBtn).toBeEnabled()
    fireEvent.click(confirmBtn)
    await waitFor(() => expect(putBody).toEqual({
      name: 'Analysts', display_name: '', confirm_name: 'aether-analysts',
    }))
  })

  test('a display-label-only edit on an SSO group saves without the dialog', async () => {
    let putBody: Record<string, unknown> = {}
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([ssoGroup])),
      http.put('/api/v1/groups/g-sso', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ ...ssoGroup, display_name: putBody.display_name })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('aether-analysts')

    fireEvent.click(screen.getByTitle('Group actions'))
    fireEvent.click(screen.getByText('Rename'))
    const labelInput = screen.getByLabelText('Group display name')
    fireEvent.change(labelInput, { target: { value: 'Data Analysts' } })
    fireEvent.keyDown(labelInput, { key: 'Enter' })

    await waitFor(() => expect(putBody).toEqual({ name: 'aether-analysts', display_name: 'Data Analysts' }))
    expect(screen.queryByText('Rename SSO-managed group?')).toBeNull()
  })

  test('cancelling the confirmation leaves the group unchanged', async () => {
    let putCalled = false
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([ssoGroup])),
      http.put('/api/v1/groups/g-sso', () => {
        putCalled = true
        return HttpResponse.json(ssoGroup)
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('aether-analysts')

    fireEvent.click(screen.getByTitle('Group actions'))
    fireEvent.click(screen.getByText('Rename'))
    const nameInput = screen.getByLabelText('Group name')
    fireEvent.change(nameInput, { target: { value: 'Analysts' } })
    fireEvent.keyDown(nameInput, { key: 'Enter' })
    await screen.findByText('Rename SSO-managed group?')

    // The inline editor also renders a Cancel button; the dialog's is last in the DOM.
    const cancelButtons = screen.getAllByRole('button', { name: 'Cancel' })
    fireEvent.click(cancelButtons[cancelButtons.length - 1])
    await waitFor(() => expect(screen.queryByText('Rename SSO-managed group?')).toBeNull())
    expect(putCalled).toBe(false)
  })

  test('manual group rename does not prompt', async () => {
    let putCalled = false
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-manual', org_id: 'org-1', name: 'Manual Group', source: 'manual',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
      ])),
      http.put('/api/v1/groups/g-manual', () => {
        putCalled = true
        return HttpResponse.json({ id: 'g-manual', org_id: 'org-1', name: 'Renamed',
          source: 'manual', member_count: 0, created_at: '2026-01-01T00:00:00Z' })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Manual Group')

    fireEvent.click(screen.getByTitle('Group actions'))
    fireEvent.click(screen.getByText('Rename'))
    const nameInput = screen.getByLabelText('Group name')
    fireEvent.change(nameInput, { target: { value: 'Renamed' } })
    fireEvent.keyDown(nameInput, { key: 'Enter' })

    await waitFor(() => expect(putCalled).toBe(true))
    expect(screen.queryByText('Rename SSO-managed group?')).toBeNull()
  })
})
```

**Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/test/GroupsPage.test.tsx
```

Expected: FAIL — "Rename SSO-managed group?" never appears (rename goes straight to PUT).

**Step 3: Implement**

In `web/src/pages/GroupsPage.tsx`:

1. Add state next to the rename state (`:328-330`):

```tsx
  // SSO rename confirmation: pending rename awaiting typed confirmation
  const [ssoRenameConfirm, setSsoRenameConfirm] = useState<{ group: Group; name: string; displayName: string } | null>(null)
  const [ssoConfirmNameInput, setSsoConfirmNameInput] = useState('')
  const ssoConfirmInputRef = useRef<HTMLInputElement>(null)
```

2. Update the mutation (`:357-366`) to pass `confirm_name` through:

```tsx
  const updateGroup = useMutation({
    mutationFn: ({ id, name, display_name, confirm_name }: { id: string; name: string; display_name: string; confirm_name?: string }) =>
      api.put<Group>(`/api/v1/groups/${id}`, { name, display_name, confirm_name }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['groups'] })
      setRenamingId(null)
      setSsoRenameConfirm(null)
      setMutateError(null)
    },
    onError: (err: Error) => setMutateError(err.message),
  })
```

3. Replace `handleRenameSubmit` (`:499-507`) and add the confirm handler:

```tsx
  const handleRenameSubmit = (id: string) => {
    const trimmed = renameValue.trim()
    if (!trimmed) return
    if (/^everyone$/i.test(trimmed)) {
      setMutateError('"everyone" is a reserved group name')
      return
    }
    const group = groups.find((g) => g.id === id)
    const displayName = renameDisplayValue.trim()
    if (group && group.source === 'sso' && trimmed !== group.name) {
      setSsoConfirmNameInput('')
      setSsoRenameConfirm({ group, name: trimmed, displayName })
      return
    }
    updateGroup.mutate({ id, name: trimmed, display_name: displayName })
  }

  const handleSsoRenameConfirm = () => {
    if (!ssoRenameConfirm) return
    updateGroup.mutate({
      id: ssoRenameConfirm.group.id,
      name: ssoRenameConfirm.name,
      display_name: ssoRenameConfirm.displayName,
      confirm_name: ssoRenameConfirm.group.name,
    })
  }
```

4. Add the dialog after the pending-member `ConfirmDialog` (`:966`), before `</AppShell>`:

```tsx
      <ConfirmDialog
        open={!!ssoRenameConfirm}
        title="Rename SSO-managed group?"
        message={
          ssoRenameConfirm
            ? `"${groupLabel(ssoRenameConfirm.group)}" is synced from your identity provider. Renaming it disconnects the sync: the next SSO login will create a new group for "${ssoRenameConfirm.group.name}" and remove members from this one, so its permissions and warehouse grants stop applying.`
            : ''
        }
        confirmLabel="Rename group"
        destructive
        confirmDisabled={ssoConfirmNameInput !== ssoRenameConfirm?.group.name || updateGroup.isPending}
        defaultFocusRef={ssoConfirmInputRef}
        onConfirm={handleSsoRenameConfirm}
        onCancel={() => setSsoRenameConfirm(null)}
      >
        <div style={{ marginBottom: 4 }}>
          <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>
            Type <strong>{ssoRenameConfirm?.group.name}</strong> to confirm
          </label>
          <input
            ref={ssoConfirmInputRef}
            style={{ ...styles.input, width: '100%', boxSizing: 'border-box' }}
            value={ssoConfirmNameInput}
            onChange={(e) => setSsoConfirmNameInput(e.target.value)}
            aria-label="Confirm group name"
            placeholder={ssoRenameConfirm?.group.name}
          />
        </div>
      </ConfirmDialog>
```

Note: `api.put` JSON-serializes the body, so `confirm_name: undefined` is omitted for manual groups and display-only edits — the existing 409-free paths keep exactly the same payload shape.

**Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/test/GroupsPage.test.tsx
```

Expected: all PASS (including the pre-existing Rename/Display names/Group provenance suites).

**Step 5: Typecheck**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

**Step 6: Commit**

```bash
git add web/src/pages/GroupsPage.tsx web/src/test/GroupsPage.test.tsx
git commit -m "feat(web): require typed confirmation to rename SSO-managed groups"
```

---

### Task 4: CLI — `--confirm-name` flag

**Files:**
- Modify: `internal/cli/groups.go:26-33` (`UpdateGroup`), `:105-127` (update command)

**Step 1: Implement**

Replace `UpdateGroup`:

```go
func (c *Client) UpdateGroup(id, name, confirmName string) (*Group, error) {
	body := map[string]interface{}{"name": name}
	if confirmName != "" {
		body["confirm_name"] = confirmName
	}
	var g Group
	if err := c.PutJSON("/api/v1/groups/"+id, body, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
```

Replace the update command closure:

```go
		func() *cobra.Command {
			var name, confirmName string
			c := &cobra.Command{
				Use:   "update <id>",
				Short: "Rename a group",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cl, err := LoadClient()
					if err != nil {
						return err
					}
					g, err := cl.UpdateGroup(args[0], name, confirmName)
					if err != nil {
						return err
					}
					PrintJSON(g)
					return nil
				},
			}
			c.Flags().StringVarP(&name, "name", "n", "", "New group name (required)")
			c.Flags().StringVar(&confirmName, "confirm-name", "", "Current group name; required to rename an SSO-managed group")
			c.MarkFlagRequired("name")
			return c
		}(),
```

**Step 2: Verify it builds and the flag is registered**

```bash
go build ./... && go run ./cmd/aether groups update --help
```

Expected: build succeeds; help lists `--confirm-name string` with the description above.

**Step 3: Commit**

```bash
git add internal/cli/groups.go
git commit -m "feat(cli): add --confirm-name to groups update"
```

---

### Task 5: E2E — typed confirmation flow

**Files:**
- Modify: `e2e/group-source.spec.ts` (append a test to the existing describe)

**Step 1: Write the test**

```ts
  test('renaming an SSO group requires typing the synced name', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `SSO Rename ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok(), await resp.text()).toBeTruthy()
    const group = await resp.json()

    try {
      setGroupSource(group.id, 'sso')

      // API fails closed without confirm_name.
      const blocked = await request.put(`/api/v1/groups/${group.id}`, {
        headers, data: { name: `${name} API` },
      })
      expect(blocked.status()).toBe(409)

      await page.goto('/groups')
      const row = page.getByText(name).locator('..').locator('..')
      await row.getByTitle('Group actions').click()
      await page.getByText('Rename', { exact: true }).click()
      const nameInput = page.getByLabel('Group name')
      await nameInput.fill(`${name} v2`)
      await nameInput.press('Enter')

      await expect(page.getByText('Rename SSO-managed group?')).toBeVisible()
      const confirm = page.getByRole('button', { name: 'Rename group' })
      await expect(confirm).toBeDisabled()
      await page.getByLabel('Confirm group name').fill('wrong-name')
      await expect(confirm).toBeDisabled()
      await page.getByLabel('Confirm group name').fill(name)
      await expect(confirm).toBeEnabled()
      await confirm.click()

      await expect(page.getByText(`${name} v2`)).toBeVisible()
    } finally {
      await request.delete(`/api/v1/groups/${group.id}?force=true`, { headers }).catch(() => {})
    }
  })
```

**Step 2: Run the spec against the dev stack**

```bash
docker compose -f docker-compose.dev.yml up -d
cd e2e && npx playwright test group-source.spec.ts
```

Expected: PASS (4 tests).

**Step 3: Commit**

```bash
git add e2e/group-source.spec.ts
git commit -m "test(e2e): cover guarded SSO group rename"
```

---

### Task 6: Docs + Swagger

**Files:**
- Modify: `docs/SSO_GROUP_PROVISIONING.md` (Group Renames section `:110-112`, Display Names `:122`, audit table `:182-193`)
- Regenerate: `internal/api/docs/` (swagger.json, swagger.yaml, docs.go)

**Step 1: Update the docs**

Replace the Group Renames section body (`:112`):

```markdown
Renaming an SSO-managed group is guarded because `name` is the sync identity. `PUT /groups/{id}` returns `409 Conflict` when the name changes and `confirm_name` is missing or not equal to the group's current name; on success the rename is audited as `group.sso.rename` (with `old_name` and `new_name`). The console mirrors this: changing the name of an SSO group opens a type-the-synced-name confirmation, while editing only `display_name` saves directly.

If the IDP renames a group, the old Aether group persists with stale memberships and a new group is created. There is no rename tracking — the old group must be cleaned up manually. This is a known limitation. Renaming from the Aether side deliberately does the same thing, which is what the confirmation warns about.
```

Append to the `PUT /groups/{id}` paragraph (`:122`):

```markdown
Renaming an SSO-managed group additionally requires `confirm_name` equal to the current `name` — a deliberate acknowledgement that the rename disconnects the group from SSO sync (subsequent logins create a replacement group and move members out of this one).
```

Add a row to the audit events table after `group.sso.error`:

```markdown
| `group.sso.rename` | An SSO-managed group was renamed with `confirm_name` acknowledgement |
```

**Step 2: Regenerate Swagger**

```bash
~/go/bin/swag init -g cmd/aether-server/main.go -o internal/api/docs
```

Expected: swagger files regenerate with the new 409 response and `confirm_name` description.

**Step 3: Commit**

```bash
git add docs/SSO_GROUP_PROVISIONING.md internal/api/docs
git commit -m "docs: document guarded SSO group rename"
```

---

### Task 7: Full verification

**Step 1: Go checks**

```bash
task check
```

Expected: fmt, vet, tidy, and all Go tests pass (`-timeout 3m` is baked into the task).

**Step 2: Frontend build**

```bash
cd web && npx tsc --noEmit && npm run build
```

Expected: no type errors; Vite build succeeds (`tsc -b` is stricter than `--noEmit`).

**Step 3: Browser validation (mandatory per AGENTS.md)**

With the dev stack up (`docker compose -f docker-compose.dev.yml up -d`), log in as an org admin and exercise the flow with agent-browser:

```bash
agent-browser open http://localhost:5173/groups
agent-browser snapshot -i
# 1. Create a group, then flip it to SSO via:
#    docker compose -f docker-compose.dev.yml exec -T aether-postgres \
#      psql -U aether -d aether -c "UPDATE groups SET source='sso' WHERE id='<id>'"
# 2. Reload, open ⋯ → Rename, change the name, press Enter.
# 3. Verify the dialog appears, confirm stays disabled for wrong input,
#    enable by typing the exact synced name.
agent-browser screenshot /tmp/opencode/sso-rename-confirm.png
agent-browser errors
agent-browser console
```

Expected: dialog renders correctly (screenshot reviewed), rename succeeds, no console/page errors. Confirm a display-label-only edit still saves without a dialog, and that `curl`/e2e already showed the API 409 for unacknowledged renames.

**Step 4: Verify the working tree is clean and hand off**

```bash
git status --short && git log --oneline -8
```

Expected: clean tree, one commit per task on `feat/guarded-sso-group-rename`.
