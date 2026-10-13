# Data Apps — Wave 2 Implementation Plan (Composition & Publish)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make a dashboard behave like a shipped product: **tabs** as chapters, a **draft → publish** split so viewers never see work-in-progress, and **org + dashboard theming** for app surfaces — plus the wave-2 agent tooling (`update_dashboard_settings`, `set_dashboard_layout`, `publish_dashboard`, query-backed widget creation).

**Architecture:** Approach A from `docs/plans/2026-10-11-data-apps-design.md`, wave 2 only. Settings stay additive JSONB (`settings.tabs`, `settings.app.theme`); publish adds one table (`dashboard_versions`, V130) whose snapshots are produced from the **dashboard document** (the source of truth since #245) and materialized back into the draft on restore. Viewers read the published snapshot through the existing REST shapes (no new renderer); execute/options resolve definitions **server-side** from the snapshot when `published: true`. Theming is scoped CSS-variable overrides on an app-surface container (editor unaffected) plus a chart-palette override for canvas charts.

**Tech Stack:** Go (`net/http` ServeMux, pgx), React 19 + TypeScript + React Query + ECharts + react-grid-layout, Vitest, Playwright.

**Preconditions:**
- Branch `feat/data-apps-wave-2` is checked out from `main` (wave 1 merged as `d7bcdfa8`, release `v0.72.0`).
- Start infra before Go tests: `task infra:up` (the disposable per-worktree DB is handled by `scripts/go-test.sh`).
- Full dev stack before E2E/browser validation: `docker compose -f docker-compose.dev.yml up -d` (relay on :3001 must be reachable — the app surface's live doc requires it).
- All Go test commands include `-timeout 3m` (repo rule).
- Waves 3–4 (HTML panel, custom components) are separate plans; do not implement them here.

## Decisions locked for this wave

| Question | Decision |
|---|---|
| Struct-first requirement | `Tabs` + `App.Theme` are added to the Go structs (Task 1) **before** any client writes them — `handleGetDashboard`'s struct round-trip would otherwise drop them. |
| Snapshot contents | `title` + `settings` (with `variables` folded in) + every widget definition (query/connector/config/layout incl. `tab`). Design's sketch gains a `title` column — the masthead title is draft state and must freeze. |
| Snapshot source | The dashboard document projection (`loadOrSeedDashboardDoc` → `dashboarddoc.Project`), not the materialized rows. |
| Publish refusal | A projection with `WidgetWarnings` refuses publish (422) — publishing would silently drop widgets. |
| Content hash | `sha256` of canonical JSON (`{title, settings, widgets sorted by id}`) computed by one Go helper; stored on publish, recomputed for the draft in `GET /versions`. |
| Viewer default | `GET /dashboards/{id}` returns the published snapshot to callers **without** `edit` when a version exists; `?published=1` forces it for anyone. Editors get the draft. |
| Draft preview | Editors preview the draft at `/dashboards/:id/view?preview=draft` (live doc + LIVE chip); the plain `/view` shows the published app. |
| Viewer live doc | Published mode does **not** connect to the relay (no draft leak, no LIVE chip). Draft preview keeps #245 behavior. |
| Draft-doc ACL | Once a version exists, the internal dashboard-yjs routes + `/internal/collab/authorize` require `edit` — viewers can no longer subscribe to the draft. |
| Restore | Copies the snapshot into the draft (doc writes through `storeAndMaterializeDashboardDoc`); never publishes. Audit `dashboard.version_restore`. |
| Tabs | `settings.tabs` (≤10, id `^[a-z0-9][a-z0-9-]*$`, title ≤60 runes, unique ids); `widgets.layout.tab` (empty/unknown → first tab). Deleting a tab clears its widgets' `tab` client-side; the server stays permissive. |
| Tab bar | Shown when `tabs.length > 0`; URL-synced `?tab=`; editor strip has an `All` chip and is a drag target. |
| Theme scope | Scoped overrides of the **existing** token vars on an `.app-theme` container + `data-app-scheme` block in `theme.css`; charts read a module-level palette override in `charts/common.tsx`. |
| Theme precedence | dashboard `settings.app.theme` → org `branding` → built-in defaults. |
| Contrast guardrails | text/background ≥ 4.5, accent/background ≥ 3 — failing tokens fall back to defaults independently. |
| Fonts | Curated self-hosted set: `dm-sans` (default) \| `jetbrains-mono` \| `system`. No new font files, no third-party requests. |
| Logos | New org-scoped attachment uploads (`POST /org/attachments` admin, `POST /dashboards/{id}/attachments` edit); `logo_attachment_id` validated to exist in the org; dashboard logo falls back to the org logo; public pages get `GET /public/{token}/attachments/{id}`. |
| Org branding transport | Included as `org_branding` on the dashboard GET + public GET responses (no extra request). |
| Agent tools | `update_dashboard_settings`, `set_dashboard_layout`, `publish_dashboard`; `create_dashboard_widget` learns query-backed creation + `metric`/`text` configs; `update_dashboard_widget` learns `config`/`tab`/`appearance`. |

---

## Phase 1 — Backend foundations

### Task 1: Settings structs — tabs + theme (struct-first)

**Files:**
- Modify: `internal/models/dashboard.go`
- Modify: `internal/api/dashboard_handlers.go` (validation; widget tab validation in add/update)
- Modify: `internal/api/dashboard_handlers_test.go` (append `TestDashboardTabsAndTheme`)
- Modify: `web/src/types/index.ts`

**Step 1: Write the failing test**

Append to `internal/api/dashboard_handlers_test.go` (mirror `TestDashboardAppSettings`'s setup — register user, create dashboard, `put(settings)` helper):

```go
func TestDashboardTabsAndTheme(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-tabs-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash Tabs Org")
	// create dashboard + put(settings) helper copied from TestDashboardAppSettings

	// Valid tabs + theme round-trip.
	rec := put(map[string]interface{}{
		"tabs": []map[string]interface{}{
			{"id": "overview", "title": "Overview"},
			{"id": "deep-dive", "title": "Deep dive"},
		},
		"app": map[string]interface{}{
			"theme": map[string]interface{}{
				"accent": "#7c5cff", "background": "#ffffff", "surface": "#fafafa",
				"text": "#111111", "border": "#e8e8e8", "radius": 6,
				"font_family": "jetbrains-mono", "color_scheme": "light",
				"chart_palette": []string{"#7c5cff", "#22c55e", "#f59e0b"},
			},
		},
	})
	// GET → assert tabs round-trip (order preserved) and theme fields survive.

	// Invalid cases → 400: bad color ("red"), radius 40, unknown font,
	// unknown scheme, >10 tabs, duplicate tab ids, empty tab id, bad tab id charset.
}
```

Also add widget-tab validation cases: creating a widget with `layout.tab = "OVERVIEW"` (bad charset) → 400; `layout.tab = "overview"` → 201.

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestDashboardTabsAndTheme -timeout 3m`
Expected: FAIL (unknown fields / no validation).

**Step 3: Implement**

`internal/models/dashboard.go`:

```go
type DashboardSettings struct {
	AutoRefreshSeconds int                   `json:"auto_refresh_seconds,omitempty"`
	GridCols           int                   `json:"grid_cols,omitempty"`
	QueryCacheSeconds  *int                  `json:"query_cache_seconds,omitempty"`
	PublicLive         bool                  `json:"public_live,omitempty"`
	Variables          []DashboardVariable   `json:"variables,omitempty"`
	App                *DashboardAppSettings `json:"app,omitempty"`
	Tabs               []DashboardTab        `json:"tabs,omitempty"`
}

// DashboardTab is a chapter of the App surface. Widgets point at a tab via
// widgets.layout.tab; an empty or unknown tab falls back to the first tab.
type DashboardTab struct {
	ID    string `json:"id"`    // ^[a-z0-9][a-z0-9-]*$, unique, <=64 chars
	Title string `json:"title"` // <=60 runes
}

type DashboardAppSettings struct {
	// ...existing fields...
	Theme *DashboardAppTheme `json:"theme,omitempty"`
}

// DashboardAppTheme overrides the app surface's tokens. Every field is
// optional; absent values fall through to org branding, then defaults.
type DashboardAppTheme struct {
	Accent       string   `json:"accent,omitempty"`
	Background   string   `json:"background,omitempty"`
	Surface      string   `json:"surface,omitempty"`
	Text         string   `json:"text,omitempty"`
	Border       string   `json:"border,omitempty"`
	Radius       *int     `json:"radius,omitempty"`       // 0..24
	FontFamily   string   `json:"font_family,omitempty"`  // dm-sans | jetbrains-mono | system
	ChartPalette []string `json:"chart_palette,omitempty"`// <=12 hex colors
	ColorScheme  string   `json:"color_scheme,omitempty"` // auto | light | dark
}
```

And `WidgetLayout` gains `Tab string `json:"tab,omitempty"``.

Validation (`internal/api/dashboard_handlers.go`):
- Extend `validateDashboardAppSettings` with `validateAppTheme(app.Theme)`: hex colors (`#rgb`/`#rrggbb`), radius 0–24, font enum, scheme enum, palette ≤12 hex.
- New `validateDashboardTabs(tabs []models.DashboardTab) error`: ≤10; id matches `^[a-z0-9][a-z0-9-]*$` and ≤64; title non-empty ≤60 runes; ids unique.
- New `validateWidgetTab(tab string) error`: empty allowed; else same id charset/≤64.
- Update path: replace the `validateDashboardAppSettingsValue(req.Settings["app"])` call with `validateDashboardSettingsValue(req.Settings)` which decodes `app` into the struct (run existing validator) **and** `tabs` into `[]models.DashboardTab` (run `validateDashboardTabs`).
- Create path: call `validateDashboardAppSettings(req.Settings.App)` + `validateDashboardTabs(req.Settings.Tabs)`.
- Widget add (`handleAddWidget`) and update (`handleUpdateWidget` layout path): call `validateWidgetTab(req.Layout.Tab)`.

`web/src/types/index.ts`: add `DashboardTab`, `DashboardAppTheme`, `DashboardSettings.tabs`, `app.theme`, `WidgetLayout.tab`.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardTabsAndTheme|TestDashboardAppSettings' -timeout 3m`
Then: `cd web && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/models/dashboard.go internal/api/dashboard_handlers.go internal/api/dashboard_handlers_test.go web/src/types/index.ts
git commit -m "feat(api): tabs and app theme settings structs with validation"
```

---

### Task 2: `dashboard_versions` — publish, list, restore

**Files:**
- Create: `internal/database/migrations/V130__dashboard_versions.sql`
- Modify: `internal/models/dashboard.go` (version model)
- Create: `internal/api/dashboard_versions.go` (handlers + snapshot helpers + content hash)
- Modify: `internal/api/router.go` (three routes)
- Create: `internal/api/dashboard_versions_test.go`

**Step 1: Write the failing tests**

`TestDashboardPublishVersions` (mirror the helpers from `TestDashboardCRUD`; the test must create a dashboard with a query widget — reuse whatever helper `dashboard_query_test.go` uses to create connectors/widgets, or add widgets via `POST /dashboards/{id}/widgets` with `connector_id`/`query`):

- Publish with no widgets → 200 `{version: 1, content_hash: <hex>}`.
- Publish again → version 2.
- `GET /dashboards/{id}/versions` → versions newest-first, `latest.version == 2`, `has_unpublished_changes == false` right after publish.
- Change the draft (PUT settings / add a widget) → `has_unpublished_changes == true`; `draft_hash != latest.content_hash`.
- `POST /dashboards/{id}/versions/1/restore` → draft settings/widgets/title equal the v1 snapshot; versions list unchanged (restore does not publish); restoring an unknown version → 404.
- Non-editor (view-only user) → 403 on publish/versions/restore.
- Audit rows `dashboard.publish` + `dashboard.version_restore` exist (follow the audit assertions used elsewhere in the package).

`TestDashboardContentHash` (unit): same projection → equal hashes; settings/widget order changes → equal; a changed query → different.

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardPublishVersions|TestDashboardContentHash' -timeout 3m`
Expected: FAIL — routes/migration missing.

**Step 3: Implement**

Migration `V130__dashboard_versions.sql`:

```sql
CREATE TABLE IF NOT EXISTS dashboard_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    version INT NOT NULL,
    title TEXT NOT NULL,
    settings JSONB NOT NULL,
    widgets JSONB NOT NULL,
    content_hash TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (dashboard_id, version)
);
CREATE INDEX IF NOT EXISTS idx_dashboard_versions_dashboard ON dashboard_versions (dashboard_id, version DESC);
```

`internal/api/dashboard_versions.go`:

- `type snapshotWidget struct { ID, Type string; Layout models.WidgetLayout; ConnectorID, Query, Language, NotebookID, CellID, Config }` with `json` tags matching `models.Widget`'s (id/type/layout/connector_id/query/language/notebook_id/cell_id/config).
- `dashboardSnapshotFromDoc(state []byte) (*dashboardSnapshot, error)` → `dashboarddoc.Project`; refuse when `len(proj.WidgetWarnings) > 0` (return a sentinel `errSnapshotWarnings` carrying the warnings); settings = `{...proj.Settings, "variables": proj.Variables}`; widgets sorted by id; also copy `proj.Title`.
- `dashboardContentHash(title string, settings map[string]any, widgets []snapshotWidget) string` → `sha256` over a canonical encoding: settings keys sorted (use `encoding/json` of a sorted-key map — Go's `json.Marshal` on `map[string]any` already sorts keys), widgets sorted by id. Keep it one function used by publish + status.
- `loadLatestDashboardVersion(ctx, orgID, dashID) (*dashboardVersionRow, error)` — returns `pgx.ErrNoRows` when none.
- `publishDashboardVersion(ctx, orgID, dashID, userID) (version int, hash string, err error)` — load/seed doc (map `errDashboardDocNotFound` → 404), snapshot, compute hash, `INSERT ... SELECT COALESCE(MAX(version),0)+1` in a transaction; on unique-violation retry once; audit `dashboard.publish` with `{version, content_hash}`.
- Handlers:
  - `handlePublishDashboard` — `POST /api/v1/dashboards/{id}/publish`, `edit` permission, 200 `{version, content_hash, created_at}`; 422 with `{error, warnings}` on `errSnapshotWarnings`.
  - `handleListDashboardVersions` — `GET /api/v1/dashboards/{id}/versions`, `edit`; response `{versions: [{version, created_by_email, created_at, content_hash}], latest: {...}|null, has_unpublished_changes, draft_hash}` (draft hash from the doc projection with the same canonicalization; when no doc exists yet, hash the materialized rows via `dashboarddoc.Seed`? — simplest: `loadOrSeedDashboardDoc` so the draft hash always reflects a real projection). Limit 50, newest first.
  - `handleRestoreDashboardVersion` — `POST /api/v1/dashboards/{id}/versions/{version}/restore`, `edit`; load the snapshot; `UpdateMeta(state, &snap.Title, settingsWithoutVariables, variables)`; upsert every snapshot widget (`dashboarddoc.UpsertWidget` with layout incl. tab); delete current doc widgets whose ids are not in the snapshot (`dashboarddoc.DeleteWidget`); `storeAndMaterializeDashboardDoc`; audit `dashboard.version_restore`; 200 `{restored: version}`.
- Router: register the three routes with the standard auth middleware, matching neighboring dashboard routes.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardPublishVersions|TestDashboardContentHash' -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V130__dashboard_versions.sql internal/models/dashboard.go internal/api/dashboard_versions.go internal/api/dashboard_versions_test.go internal/api/router.go
git commit -m "feat(api): dashboard versions — publish, list, restore"
```

---

### Task 3: Published-mode reads — GET, public, execute/options, draft-doc ACL

**Files:**
- Modify: `internal/api/dashboard_handlers.go` (`handleGetDashboard`)
- Modify: `internal/api/dashboard_query_handlers.go` (authed execute + options; public execute + options)
- Modify: `internal/api/public_handlers.go` (`servePublicDashboard`)
- Modify: `internal/api/internal_handlers.go` (dashboard-yjs GET/PUT + collab authorize)
- Modify: `internal/api/dashboard_query_test.go` / `dashboard_versions_test.go` (append cases)

**Step 1: Write the failing tests**

- `TestDashboardGetPublished`: dashboard with a query widget, publish v1, then change the draft (add a widget / change the query). A **view-only** user's plain `GET /dashboards/{id}` returns the v1 widget set + `published_version.version == 1`; the **editor's** plain GET returns the draft; anyone with `?published=1` gets v1.
- `TestDashboardExecutePublished`: publish with SQL `SELECT 1 AS x`; change the draft widget to `SELECT 2 AS x`; `POST /execute {published: true}` → result reflects `SELECT 1`; without `published` (editor) → `SELECT 2`.
- `TestPublicDashboardPublished`: public token + `public_live`; publish v1; change the draft; `GET /public/{token}` and `POST /public/{token}/execute` reflect v1.
- `TestDashboardDocAclPublished`: with a version present, a view-only user's `GET /internal/dashboard-yjs/{id}` → 403 and `POST /internal/collab/authorize` for that user → 403; the editor's → 200. With no version, the view-only user keeps the #245 behavior (200 read-only).

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardGetPublished|TestDashboardExecutePublished|TestPublicDashboardPublished|TestDashboardDocAclPublished' -timeout 3m`
Expected: FAIL.

**Step 3: Implement**

- `handleGetDashboard`: after the permission checks, `publishedParam := r.URL.Query().Get("published") == "1"`; `if publishedParam || !editOK { if ver, err := s.loadLatestDashboardVersion(...); err == nil { substitute title/settings/widgets from the snapshot; set published_version } }`. `widgets_data` is computed from the **substituted** widget list. Response gains `PublishedVersion *struct{Version int; CreatedAt time.Time} `json:"published_version,omitempty"``.
- `handleExecuteDashboardWidget` / `handleDashboardVariableOptions`: add `Published bool `json:"published,omitempty"`` to the request structs. When true: load the latest version (404 when none — never silently fall back), decode `widgets` into `[]snapshotWidget` and find by `req.WidgetID` (404 `widget not found`), build `models.Widget` from it, and use the snapshot's `settings.variables` for `ValidateVariables`/`Interpolate`. Cache seconds: snapshot settings' `query_cache_seconds` when present else live row. All permission checks unchanged.
- Public execute/options handlers: same resolution, but **always** prefer the published snapshot when a version exists (public pages never show drafts).
- `servePublicDashboard`: when a version exists, substitute title/settings/widgets from the snapshot; add `published_version` to the response.
- `handleInternalDashboardYjsGet` / `Put`: when a version exists, require dashboard `edit` (403 otherwise) — viewers can no longer read/write the draft doc. `handleInternalCollabAuthorize`: same check for dashboard resources (deny with 403 before returning read-only).

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardGetPublished|TestDashboardExecutePublished|TestPublicDashboardPublished|TestDashboardDocAclPublished|TestDashboardYjs|TestCollabAuthorize' -timeout 3m`
Expected: PASS (adjust the `-run` set to the actual yjs/authorize test names in the package).

**Step 5: Commit**

```bash
git add internal/api/dashboard_handlers.go internal/api/dashboard_query_handlers.go internal/api/public_handlers.go internal/api/internal_handlers.go internal/api/*_test.go
git commit -m "feat(api): published-mode reads for dashboards, execute, and draft-doc ACL"
```

---

### Task 4: Org branding + logo attachments

**Files:**
- Modify: `internal/models/org.go`
- Modify: `internal/api/org_handlers.go` (branding validation in `handleUpdateOrgSettings`)
- Modify: `internal/api/attachment_handlers.go` (org + dashboard upload)
- Modify: `internal/api/router.go` (two upload routes + public logo route)
- Modify: `internal/api/dashboard_handlers.go` (logo validation + `org_branding` on GET)
- Modify: `internal/api/public_handlers.go` (`org_branding` + public logo)
- Create/append: `internal/api/org_handlers_test.go`, `internal/api/attachment_handlers_test.go`

**Step 1: Write the failing tests**

- `TestOrgBranding`: admin PUT `/org/settings` with `branding: {app_name, accent_color, logo_attachment_id, default_chart_palette}` round-trips through GET; bad accent → 400; palette >12 → 400; non-admin PUT → 403 (follow existing org-settings tests).
- `TestOrgAttachmentUpload`: admin uploads a small PNG to `POST /api/v1/org/attachments` → 201 with id; non-admin → 403; `GET /api/v1/attachments/{id}` works for an org member.
- `TestDashboardLogoAttachment`: dashboard `edit` uploads to `POST /api/v1/dashboards/{id}/attachments`; setting `settings.app.logo_attachment_id` to that id → 200; a random UUID → 400; an id from another org → 400.
- `TestDashboardGetOrgBranding`: `GET /dashboards/{id}` includes `org_branding` when set.
- `TestPublicLogo`: `GET /api/v1/public/{token}/attachments/{id}` serves the org logo; a foreign org's attachment → 404.

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestOrgBranding|TestOrgAttachmentUpload|TestDashboardLogoAttachment|TestDashboardGetOrgBranding|TestPublicLogo' -timeout 3m`
Expected: FAIL.

**Step 3: Implement**

- `models.OrgBranding { AppName string; LogoAttachmentID string; AccentColor string; DefaultChartPalette []string }` + `OrgSettings.Branding *OrgBranding`; validation mirroring the theme validators (app_name ≤60 runes; hex colors; palette ≤12).
- `POST /api/v1/org/attachments` — org admin; the existing upload body handling (`handleUploadAttachment`) factored into a helper that takes `(orgID, notebookID *string, userID)` so both routes share multipart/storage/limits code; insert with `notebook_id = NULL`.
- `POST /api/v1/dashboards/{id}/attachments` — dashboard `edit`; same helper.
- Logo validation: in the settings validation path, when `app.logo_attachment_id` is set, verify `SELECT true FROM attachments WHERE id = $1 AND org_id = $2` (400 otherwise); same for `branding.logo_attachment_id`.
- `handleGetDashboard` + `servePublicDashboard`: add `org_branding` (read `orgs.settings`; null when unset).
- `GET /api/v1/public/{token}/attachments/{id}` — validate the token's org owns the attachment (404 otherwise), stream it with the existing attachment-serving helper.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -run 'TestOrgBranding|TestOrgAttachmentUpload|TestDashboardLogoAttachment|TestDashboardGetOrgBranding|TestPublicLogo' -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/models/org.go internal/api/org_handlers.go internal/api/attachment_handlers.go internal/api/router.go internal/api/dashboard_handlers.go internal/api/public_handlers.go internal/api/*_test.go
git commit -m "feat(api): org branding and app logo attachments"
```

---

## Phase 2 — Theming (frontend)

### Task 5: `resolveAppTheme` + contrast guardrails + app-scheme CSS

**Files:**
- Create: `web/src/utils/appTheme.ts`
- Create: `web/src/utils/appTheme.test.ts`
- Modify: `web/src/styles/theme.css` (`.app-theme` scheme blocks + `--app-font`/`--app-radius`)

**Step 1: Write the failing tests**

- Precedence: dashboard accent wins over branding accent; branding wins over defaults; absent everything → no overrides.
- Mapping: `{accent, background, surface, text, border}` → `--accent`, `--bg-canvas`, `--bg-card`, `--text-primary`, `--border-light`; `radius` → `--app-radius`; `font_family` → `--app-font` (stack: DM Sans / JetBrains Mono / system-ui).
- Guardrails: text/background contrast < 4.5 → the text override is dropped (default kept) while background stays; accent/background < 3 → accent dropped; a valid pair passes both.
- Scheme: `auto` → follows the global `data-theme`; `light`/`dark` force `scheme`.
- `chartPalette`: dashboard palette wins, else branding's, else undefined; invalid entries (non-hex) are filtered.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/utils/appTheme.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement**

- `contrastRatio(fg, bg)` using WCAG relative luminance; `hexToRgb` tolerant of `#rgb`/`#rrggbb`.
- `resolveAppTheme(theme, branding, globalScheme) → { scheme: 'light'|'dark'; vars: Record<string,string>; chartPalette?: string[] }`.
- `theme.css`: add `.app-theme[data-app-scheme='dark'] { … }` and `.app-theme[data-app-scheme='light'] { … }` blocks that re-declare the token set (copy the values from the existing `[data-theme='dark']` / `:root` blocks), plus `--app-font`/`--app-radius` defaults (`inherit`/`8px`) and `.app-theme { font-family: var(--app-font); }`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/utils/appTheme.test.ts && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/utils/appTheme.ts web/src/utils/appTheme.test.ts web/src/styles/theme.css
git commit -m "feat(web): app theme resolution with contrast guardrails"
```

---

### Task 6: `AppThemeProvider` + chart palette plumbing + logo

**Files:**
- Create: `web/src/components/AppThemeProvider.tsx`
- Create: `web/src/components/AppThemeProvider.test.tsx`
- Modify: `web/src/charts/common.tsx` (module-level app theme override)
- Modify: `web/src/components/AppHeader.tsx` (logo)
- Modify: `web/src/components/AppHeader.test.tsx`
- Modify: `web/src/pages/DashboardPage.tsx` + `web/src/pages/PublicDashboardPage.tsx` (wrap + logo + org branding)

**Step 1: Write the failing tests**

- `AppThemeProvider` renders a `.app-theme` container with `data-app-scheme` and the resolved CSS vars; children render inside.
- Charts: after mount, `getCurrentPalette()` returns the app palette; after unmount, it returns the global palette again (`charts/common.tsx` unit test).
- `AppHeader` renders the logo `<img>` when `logoUrl` is set and hides it otherwise.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/AppThemeProvider.test.tsx src/components/AppHeader.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `AppThemeProvider({ theme, branding, children })`: resolves with the global scheme (observe `data-theme` like `useChartColors` does), renders `<div className="app-theme" data-app-scheme={scheme} style={vars}>{children}</div>`, and on mount/update calls `setAppChartTheme({ palette: chartPalette ?? null, scheme })`, clearing on unmount.
- `charts/common.tsx`: module-level `appChartTheme: { palette: string[]|null; scheme: 'light'|'dark'|null }` + `setAppChartTheme`; `isDarkMode()` returns the scheme override when set; `getCurrentPalette()` returns the palette override when set; `useChartColors()` recomputes when the override changes (subscribe via a tiny emitter or re-run on scheme state — keep it simple: the provider's effect sets it before children's first paint via `useLayoutEffect`).
- Viewer/public: wrap the surface in `AppThemeProvider` using `dashboard.settings.app?.theme` + `org_branding` from the response; compute `logoUrl` (`app.logo_attachment_id || branding.logo_attachment_id` → `/api/v1/attachments/{id}` authed, `/api/v1/public/{token}/attachments/{id}` public) and pass it to `AppHeader`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/AppThemeProvider.test.tsx src/components/AppHeader.test.tsx src/charts && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AppThemeProvider.tsx web/src/components/AppThemeProvider.test.tsx web/src/charts/common.tsx web/src/components/AppHeader.tsx web/src/components/AppHeader.test.tsx web/src/pages/DashboardPage.tsx web/src/pages/PublicDashboardPage.tsx
git commit -m "feat(web): app theme provider, chart palette override, and logo"
```

---

### Task 7: App settings panel — Theme section + logo control

**Files:**
- Modify: `web/src/components/AppSettingsPanel.tsx`
- Modify: `web/src/components/AppSettingsPanel.test.tsx`

**Step 1: Write the failing tests**

- Changing the accent color and saving PUTs `settings.app.theme.accent`.
- Selecting `color_scheme: dark` + `font_family: jetbrains-mono` round-trips into the save payload.
- Choosing a preset chart palette saves `chart_palette` (array of hex).
- Radius input persists as a number; clearing a color removes that key.
- Logo: uploading a file POSTs to `/api/v1/dashboards/{id}/attachments` and saves the returned id; "Use org logo" clears the dashboard logo; the panel shows the org logo as the effective fallback.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/AppSettingsPanel.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- New "Theme" section: color inputs (accent, background, surface, text, border) with a "Reset to defaults" affordance per field; radius number (0–24); font select (DM Sans / JetBrains Mono / System); scheme select (Auto / Light / Dark); chart palette: preset select (Default / Aether / Amber / Teal / Custom) + a comma-separated hex input for Custom.
- Logo control: None / Org logo / Upload… (file input → dashboard attachment upload → id); preview via `/api/v1/attachments/{id}`.
- Save merges into the existing `settings.app` PUT; omitted/empty fields are deleted from the theme object (send `theme: null` when everything is cleared).

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/AppSettingsPanel.test.tsx && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AppSettingsPanel.tsx web/src/components/AppSettingsPanel.test.tsx
git commit -m "feat(web): theme and logo controls in the app settings panel"
```

---

### Task 8: Org branding panel

**Files:**
- Modify: `web/src/pages/OrgSettingsPage.tsx`
- Modify: `web/src/pages/OrgSettingsPage.test.tsx` (create if absent — check how the page is tested today and extend that file)

**Step 1: Write the failing tests**

- The Branding section loads current `branding` values from `GET /org/settings`.
- Saving PUTs `/api/v1/org/settings` with the **full settings object** (audit retention + branding) — the handler overwrites, so a partial PUT would wipe fields.
- Uploading a logo POSTs to `/api/v1/org/attachments` and saves the id; clearing removes it.
- Non-admins don't see the section (the page already gates admin content — mirror that gate).

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/pages/OrgSettingsPage.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- A "Branding" section (SectionHeader, like the audit section): app name input, accent color input, chart palette preset select, logo upload + preview + clear.
- Save sends `{ ...currentSettings, branding: { ...} }`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/pages/OrgSettingsPage.test.tsx && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/pages/OrgSettingsPage.tsx web/src/pages/OrgSettingsPage.test.tsx
git commit -m "feat(web): org branding settings"
```

---

## Phase 3 — Tabs (frontend)

### Task 9: Tab utils + `AppTabs` + viewer/public tab bar

**Files:**
- Create: `web/src/utils/dashboardTabs.ts`
- Create: `web/src/utils/dashboardTabs.test.ts`
- Create: `web/src/components/AppTabs.tsx`
- Create: `web/src/components/AppTabs.test.tsx`
- Modify: `web/src/pages/DashboardPage.tsx`, `web/src/pages/PublicDashboardPage.tsx`

**Step 1: Write the failing tests**

- `widgetsInTab(widgets, tabs, tabId)`: no tabs → all widgets; empty/unknown widget tab → first tab; known tab → only that tab's widgets; unknown `tabId` → first tab.
- `activeTabId(tabs, requested)`: requested when valid, else first tab id, else null.
- `AppTabs`: renders one `role="tab"` per tab with `aria-selected`; click fires `onChange`; ArrowLeft/ArrowRight move selection; hidden when `tabs.length === 0`.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/utils/dashboardTabs.test.ts src/components/AppTabs.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- Utils as above (pure functions, `DashboardTab` + `Widget` types).
- `AppTabs`: chapter-styled tablist under the masthead (hairline underline on the active tab, 13px DM Sans, 36px tap targets, `role="tablist"` + arrow-key nav, no chrome).
- Viewer + public: `const tabs = settings?.tabs ?? []`; render `<AppTabs>` between the masthead and the filter strip when tabs exist; keep the active tab in the URL (`useSearchParams`, `?tab=`); render only the active tab's widgets; the refresh action still runs **all** widgets.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/utils/dashboardTabs.test.ts src/components/AppTabs.test.tsx src/pages/DashboardPage.test.tsx && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/utils/dashboardTabs.ts web/src/utils/dashboardTabs.test.ts web/src/components/AppTabs.tsx web/src/components/AppTabs.test.tsx web/src/pages/DashboardPage.tsx web/src/pages/PublicDashboardPage.tsx
git commit -m "feat(web): tab bar and tab filtering on the app surface"
```

---

### Task 10: Editor tabs — strip, grid filter, drag-to-tab, drawer select, settings section

**Files:**
- Modify: `web/src/pages/DashboardEditorPage.tsx`
- Modify: `web/src/components/WidgetConfigDrawer.tsx` + test
- Modify: `web/src/components/AppSettingsPanel.tsx` + test
- Modify: `web/src/components/dashboardCollabRuntime.ts` (`setWidgetTab`)

**Step 1: Implement**

- `dashboardCollabRuntime.ts`: `setWidgetTab(collab, widgetId, tab)` — read the widget's layout and `updateWidgetLayout` with the new tab (one Yjs write).
- Editor strip (under the header): `All` + one chip per tab (`data-tab-id` on each) + `＋` (opens the App settings panel's Tabs section). Clicking filters the grid to that tab (`All` shows everything). The active strip chip is editor-local state, not URL state.
- Drag-to-tab: `onDragStop(layout, oldItem, newItem, placeholder, e)` → `document.elementFromPoint(e.clientX, e.clientY)?.closest('[data-tab-id]')` → `setWidgetTab`. Dropping on `All` clears the tab.
- Config drawer: a "Placement" section with a `Tab` select (options: the tabs + "First tab (default)"), only rendered when tabs exist; saving calls `setWidgetTab` (live doc) or the REST widget PUT (`layout: { ...widget.layout, tab }`) in the fallback path.
- App settings panel: a "Tabs" section — rows with a title input, move up/down, delete, and an "Add tab" button. New tab ids are slugged from the title (`slugify(title)` + uniqueness suffix). Delete clears that tab's `tab` on its widgets (client-side `setWidgetTab(..., '')` per widget) before removing the tab. Saves into `settings.tabs` via the existing settings PUT.
- Guard: when `settings.tabs` is empty the strip, drawer section, and tab filtering are absent (no behavior change for existing dashboards).

**Step 2: Verify**

Run: `cd web && npm run test:run -- src/components/WidgetConfigDrawer.test.tsx src/components/AppSettingsPanel.test.tsx src/pages/DashboardEditorPage.test.tsx && npx tsc -b`
Expected: PASS.

**Step 3: Real-browser validation (mandatory)**

With the dev stack up: open a dashboard editor, add two tabs, assign widgets via the drawer select and by dropping a card onto a chip, rename/delete a tab, then open `/dashboards/:id/view` and check the tab bar switches content and syncs `?tab=`.

**Step 4: Commit**

```bash
git add web/src/pages/DashboardEditorPage.tsx web/src/components/WidgetConfigDrawer.tsx web/src/components/WidgetConfigDrawer.test.tsx web/src/components/AppSettingsPanel.tsx web/src/components/AppSettingsPanel.test.tsx web/src/components/dashboardCollabRuntime.ts
git commit -m "feat(web): tab management in the dashboard editor"
```

---

## Phase 4 — Publish (frontend)

### Task 11: Publish status, button, versions panel, restore

**Files:**
- Create: `web/src/components/DashboardVersionsPanel.tsx`
- Create: `web/src/components/DashboardVersionsPanel.test.tsx`
- Modify: `web/src/pages/DashboardEditorPage.tsx` (header status chip + Publish button + panel state)

**Step 1: Write the failing tests**

- Status chip: no versions → `Not published`; after publish → `v1`; `has_unpublished_changes` → amber dot + title.
- Publish button POSTs `/api/v1/dashboards/{id}/publish`, shows a busy state, then invalidates `['dashboard-versions', id]` + `['dashboard', id]`.
- Panel: lists versions (vN, email, relative time); `Restore` opens a confirm; confirming POSTs `/versions/{version}/restore` and invalidates both queries; the latest version is marked `Published`.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/DashboardVersionsPanel.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- Query `['dashboard-versions', id]` → `GET /api/v1/dashboards/{id}/versions` with `refetchInterval: 8000` while the editor is open.
- Header: status chip (click → panel) + `Publish` button next to the save status; hidden on mobile like other controls. Disabled while `!editingEnabled`.
- Panel: right-side drawer (reuse the drawer chrome) with the version list, restore + confirm, and a note: "Restoring copies this version into the draft — publish to make it live."

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/DashboardVersionsPanel.test.tsx src/pages/DashboardEditorPage.test.tsx && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/DashboardVersionsPanel.tsx web/src/components/DashboardVersionsPanel.test.tsx web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): publish status, button, and version history"
```

---

### Task 12: Viewer published mode + draft preview + public wiring

**Files:**
- Modify: `web/src/hooks/useDashboardDoc.ts` (add `connect?: boolean`)
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/components/QueryDataWidget.tsx` (thread `published` into execute bodies)
- Modify: `web/src/pages/PublicDashboardPage.tsx`
- Modify: `web/src/types/index.ts` (`published_version`, `org_branding` on responses)
- Modify tests: `web/src/pages/DashboardPage.test.tsx`, `web/src/hooks/useDashboardDoc.test.tsx`, `web/src/components/QueryDataWidget.test.tsx`

**Step 1: Write the failing tests**

- `useDashboardDoc(id, { connect: false })` never imports/acquires the collab runtime (spy on `getDashboardCollab`).
- Viewer with `published_version` set: does not connect (no LIVE chip), renders the REST widget list, and execute calls include `published: true`.
- Viewer without `published_version`: #245 behavior (live doc merges, no `published` flag).
- `?preview=draft` with `can_edit`: plain GET (no `published=1`), live doc connected, a small "Draft preview" badge; without `can_edit` it falls back to published.
- Public: execute bodies omit `published` (server resolves), and the response's published widgets render.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/pages/DashboardPage.test.tsx src/hooks/useDashboardDoc.test.tsx src/components/QueryDataWidget.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `useDashboardDoc`: `options.connect ?? true`; when false, skip the acquire effect (return the empty snapshot; no provider).
- `DashboardPage`: query `?published=1` unless `preview=draft` (and `can_edit`); `const published = data?.published_version != null`; `useDashboardDoc(id, { enabled: canEdit && !published, connect: !published })`; skip the live merges when published (render REST); thread `published` down to `QueryDataWidget` → `api.post(..., { widget_id, variables, connector_id, published: true })`; editors get a `Preview draft` / `View published` toggle in the masthead actions (Link to `?preview=draft` / plain path); a muted "Draft preview" chip when previewing.
- Public page: no client changes needed for resolution (server-side), but the response typing gains `published_version` + `org_branding`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/pages/DashboardPage.test.tsx src/hooks/useDashboardDoc.test.tsx src/components/QueryDataWidget.test.tsx && npx tsc -b`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/hooks/useDashboardDoc.ts web/src/hooks/useDashboardDoc.test.tsx web/src/pages/DashboardPage.tsx web/src/pages/PublicDashboardPage.tsx web/src/components/QueryDataWidget.tsx web/src/components/QueryDataWidget.test.tsx web/src/types/index.ts
git commit -m "feat(web): published viewer mode and draft preview"
```

---

## Phase 5 — Agent tooling + verification

### Task 13: Agent tools — settings, layout, publish, query-backed widgets

**Files:**
- Modify: `internal/agent/tools_manage.go` (definitions + handlers)
- Modify: `internal/agent/tools_seed.go` (seed list)
- Modify: `internal/agent/engine.go` (dashboard tools system-prompt line)
- Modify: `internal/agent/tools_manage_test.go`

**Step 1: Write the failing tests**

- `update_dashboard_settings` sets `app.title` + `tabs` and they materialize (call the tool, then read the dashboard row / doc projection).
- `set_dashboard_layout` moves a widget to a tab and sets `appearance`; a bad tab id → error result.
- `publish_dashboard` creates version 1; a second call → version 2.
- `create_dashboard_widget` with `connector_id` + `query` creates a query widget (no notebook/cell required); with `config.metric` it stores the metric config; without either it still requires notebook/cell.
- `update_dashboard_widget` merges `config` and sets `layout.tab`.

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/agent/ -run 'TestDashboard' -timeout 3m`
Expected: FAIL (unknown tools).

**Step 3: Implement**

- Mirror the existing dashboard tool handlers' structure (ToolDef with `Parameters` JSON schema + Execute → the doc helpers). Reuse the same validation functions as the REST paths (Task 1) and the publish helper from Task 2 (`publishDashboardVersion`) so behavior is identical.
- `update_dashboard_settings { dashboard_id, title?, app?, tabs? }` → `UpdateMeta` + store/materialize + audit `dashboard.update` (existing action) with the changed keys.
- `set_dashboard_layout { dashboard_id, tabs?, placements?: [{widget_id, tab}], appearance?: [{widget_id, appearance}] }` → tabs via `UpdateMeta`; placements/appearance via `UpdateLayout`/`SetWidgetConfig` per widget; one store at the end.
- `publish_dashboard { dashboard_id }` → shared publish helper; result carries the version + hash.
- Extend `create_dashboard_widget` schema: `connector_id`, `query`, `config` optional; require `(connector_id && query) || (notebook_id && cell_id)`; `type` enum unchanged.
- Extend `update_dashboard_widget`: `config` (shallow merge into the widget's config), `tab`.
- Update the engine's dashboard tools hint line to list the new tools.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/agent/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/agent/tools_manage.go internal/agent/tools_seed.go internal/agent/engine.go internal/agent/tools_manage_test.go
git commit -m "feat(agent): dashboard settings, layout, publish, and query-backed widgets"
```

---

### Task 14: E2E + visual snapshots

**Files:**
- Create: `e2e/dashboard-publish.spec.ts`
- Create: `e2e/snapshots/dashboard-publish.spec.ts-snapshots/` (generated)
- Modify: `e2e/dashboard-app.spec.ts` only if the wave-2 surface changes its baseline (tabs/theme absent there → should be untouched)

**Step 1: Write the spec**

Follow `e2e/dashboard-filters.spec.ts`'s helpers (Postgres connector + dashboard with a query widget). Scenarios:

1. **Publish flow**: create a dashboard + query widget; open `/view` (viewer sees it); edit the draft title/widget; reload `/view` → the draft change is **not** visible; publish from the editor; reload `/view` → visible; the editor shows `Unpublished changes` after another draft edit; restore v1 → the draft reverts (and the app still shows the last published until the next publish).
2. **Tabs**: add two tabs + assign two widgets; `/view` shows the tab bar, switching tabs changes the visible widget and syncs `?tab=`.
3. **Theme**: set accent + dark scheme in the App settings panel; screenshot `/view` (`await expect(page).toHaveScreenshot('dashboard-app-themed.png')`).
4. **Draft preview**: as the editor, `?preview=draft` shows the draft + the badge; the plain path shows published.

**Step 2: Run**

```bash
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test dashboard-publish.spec.ts --update-snapshots
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test dashboard-publish.spec.ts --repeat-each=2
```
Expected: PASS, stable snapshot.

**Step 3: Commit**

```bash
git add e2e/dashboard-publish.spec.ts e2e/snapshots
git commit -m "test(e2e): publish flow, tabs, theming, and draft preview"
```

---

### Task 15: Docs + full verification

**Files:**
- Modify: `DESIGN.md` (app theming token set; tabs; publish semantics — mark approved 2026-10-11)
- Modify: `AGENTS.md` (dashboard paragraph: `dashboard_versions`, published reads, tabs, theming, logo endpoints)
- Modify: `FRONTEND.md` (dashboard app section: tabs + theme + publish)

**Step 1: Update docs**

Keep each edit tight. Document: the snapshot contents + `published: true` resolution, the viewer-defaults-to-published rule and `?preview=draft`, the draft-doc ACL tightening, the tab fallback semantics, the theme token precedence + guardrails, and the logo endpoints.

**Step 2: Full verification**

```bash
task check
cd web && npx tsc -b && npm run build && npm run test:run
cd relay && npm run build
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test dashboard-app.spec.ts dashboard-publish.spec.ts dashboard.spec.ts dashboard-filters.spec.ts
```

**Step 3: Commit + PR**

```bash
git add DESIGN.md AGENTS.md FRONTEND.md
git commit -m "docs: tabs, publish, and app theming"
git push -u origin feat/data-apps-wave-2
gh pr create --title "feat: data apps wave 2 — tabs, publish, and theming" --body "…"
```

---

## Out of scope (later plans)

- Wave 3: sanitized HTML/CSS panel (`html` widget, DOMPurify, sandboxed frame).
- Wave 4: custom components + full custom pages, org `custom_code_enabled`.
- Distribution: embed route, presentation mode, print/PDF.
- Org font uploads; scheduled snapshots; signed embedding; saved views.
