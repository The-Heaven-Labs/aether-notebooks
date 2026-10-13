# Data Apps — Wave 3 Implementation Plan (HTML/CSS Panels)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the `html` widget — a script-free, sanitized HTML/CSS panel for app surfaces — with the org-wide kill switch, and teach the agent tools to author panels.

**Architecture:** Approach A from `docs/plans/2026-10-11-data-apps-design.md`, wave 3 only. A widget type `html` stores `config.html` (markup) + `config.css` (stylesheet) as sibling keys; source-less like text panels. Rendering is client-side: `{{variable}}` values are HTML-escaped into the markup, the result runs through DOMPurify's strict allowlist, and the composed document renders in an iframe with `sandbox=""` plus a CSP `<meta>` (`default-src 'none'`; images only from `data:` or the app origin) — the sandbox is the security boundary, the sanitizer is defense in depth. The resolved app-theme tokens are injected into the frame as `:root` CSS variables so panels match the surface. The server validates type + size caps and enforces `orgs.settings.html_panels_enabled` (absent = enabled) on creation/conversion; disabled panels render a quiet placeholder and the dashboard/public GET responses carry the resolved `html_panels_enabled` flag.

**Tech Stack:** Go (`net/http` ServeMux, pgx), React 19 + TypeScript, DOMPurify (new dependency, approved 2026-10-11), Vitest, Playwright.

**Preconditions:**
- Branch `feat/data-apps-wave-3` is checked out from `main` (wave 2 merged as `f03008e1`, release `v0.73.0`; the app-builder track merged as `e6e906ae`, release `v0.74.0` — this plan is the data-apps design's own wave 3).
- Start infra before Go tests: `task infra:up` (the disposable per-worktree DB is handled by `scripts/go-test.sh`).
- Full dev stack before E2E/browser validation: `docker compose -f docker-compose.dev.yml up -d`.
- All Go test commands include `-timeout 3m` (repo rule).
- Wave 4 (custom components/pages) and distribution (embed/present/print) are separate plans; do not implement them here.

## Decisions locked for this wave

| Question | Decision |
|---|---|
| Widget type | `html`; V132 widens `widgets_type_check` to `('chart','table','text','metric','html')`. No new columns. |
| Config shape | `config.html` (markup string) + `config.css` (stylesheet string) as sibling keys — the design's literal `{ html, css }`. |
| Size caps | `html` ≤ 256 KiB, `css` ≤ 64 KiB (byte lengths), enforced in `internal/validate` and reused by REST + tools; far below the 1 MiB document config cap. |
| Sanitizer | DOMPurify (client-side) with an explicit tag/attribute allowlist; no `<a>`, `<form>`, `<script>`, `<style>`, `<iframe>`; `style` attribute allowed (CSS cannot execute). |
| Images | `img src` restricted to `data:image/*` and the app origin (absolute or `/`-relative) via `ALLOWED_URI_REGEXP`; everything else is stripped. |
| Sandbox | `<iframe sandbox="" referrerPolicy="no-referrer" title="HTML panel">` — scripts, forms, navigation, popups, and same-origin access all disabled. |
| CSP | A `<meta http-equiv="Content-Security-Policy">` in the frame: `default-src 'none'; img-src data: <origin>; style-src 'unsafe-inline'; font-src data: <origin>; form-action 'none'; base-uri 'none'`. |
| Interpolation | `{{variable}}` values are HTML-escaped (`& < > " '`) before the template is sanitized (new `interpolateHtml`); unknown tokens stay visible. |
| Theme | Resolved app-theme tokens (`resolved.vars`) injected into the frame as `:root` custom properties + body defaults; `AppThemeProvider` gains an `AppThemeContext`/`useAppTheme()` so `HtmlWidget` can read them. |
| Org switch | `orgs.settings.html_panels_enabled` (`*bool`, absent = enabled); admin toggle in Org Settings; served as a resolved `html_panels_enabled` bool on dashboard GET + public dashboard GET. |
| Switch enforcement | Fail closed while disabled: creating a widget with type `html`, converting to `html`, or writing panel config (`html`/`css` keys — REST and agent tools) returns 400; existing panels render a placeholder everywhere (app, public, editor). Re-enabling restores the retained config. |
| Authoring UI | Drawer gains HTML + CSS multiline editors and a live preview using the same `HtmlWidget`; the add-widget picker offers "HTML panel" only for the source-less option and only when enabled. No CodeMirror in this wave (wave 4 upgrades authoring). |
| Public pages | Panels render (static) through the same renderer; the switch is evaluated live on the public GET, so disabling hides panels on published snapshots too. |
| Agent tooling | `create_dashboard_widget` gains `type: html` (source-less allowed for this type only) + an `html` block; `set_widget_config` gains the `html` block; `update_dashboard_widget`'s type enum gains `html`; `builtinSeedRev` bumps 1 → 2. |
| Review engine | No new findings this wave (an "empty panel" finding is a later follow-up). |

---

## Phase 1 — Backend foundations

### Task 1: `html` widget type in models, document, and schema

**Files:**
- Modify: `internal/models/dashboard.go:135-140` (widget type constants)
- Modify: `internal/dashboarddoc/doc.go:144-150` (`widgetTypes` map)
- Create: `internal/database/migrations/V132__html_widget_type.sql`
- Modify: `internal/dashboarddoc/apply_test.go` (extend the existing `SetWidgetType` test)

**Step 1: Write the failing test**

Extend `TestApply_SetWidgetType` in `internal/dashboarddoc/apply_test.go` (mirror the file's chart/table cases — seed a widget, call `SetWidgetType(state, id, "html")`, project, assert `w.Type == "html"` and that unrelated fields survive). Also add a rejection case: `SetWidgetType(state, id, "custom")` returns `unknown type` (wave 4's type stays unlisted).

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/dashboarddoc/ -run TestApply_SetWidgetType -timeout 3m`
Expected: FAIL — `html` is not in `widgetTypes`.

**Step 3: Implement**

`internal/models/dashboard.go`:
```go
const (
	WidgetChart  WidgetType = "chart"
	WidgetTable  WidgetType = "table"
	WidgetText   WidgetType = "text"
	WidgetMetric WidgetType = "metric"
	WidgetHTML   WidgetType = "html"
)
```

`internal/dashboarddoc/doc.go`:
```go
var widgetTypes = map[string]bool{
	"chart":  true,
	"table":  true,
	"metric": true,
	"text":   true,
	"html":   true,
}
```

`internal/database/migrations/V132__html_widget_type.sql`:
```sql
-- V132: html widget type (data apps wave 3 — escape hatch tier 1).
-- Script-free HTML/CSS panels; rendering is sandboxed client-side.
ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('chart', 'table', 'text', 'metric', 'html'));
```

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/dashboarddoc/ -timeout 3m`
Expected: PASS. (The CHECK is exercised end-to-end by Task 3's handler test on a real DB.)

**Step 5: Commit**

```bash
git add internal/models/dashboard.go internal/dashboarddoc/doc.go internal/dashboarddoc/apply_test.go internal/database/migrations/V132__html_widget_type.sql
git commit -m "feat(api): html widget type and document support"
```

---

### Task 2: `validate.WidgetHTMLConfig` — caps + converter

**Files:**
- Modify: `internal/validate/widget_config.go`
- Modify: `internal/validate/widget_config_test.go` (append)

**Step 1: Write the failing test**

Append to `internal/validate/widget_config_test.go`:

```go
func TestValidateWidgetHTML(t *testing.T) {
	// Valid content passes.
	require.NoError(t, ValidateWidgetHTML(WidgetHTMLConfig{
		HTML: `<div class="stat">42</div>`,
		CSS:  `.stat { font-size: 28px; }`,
	}))
	// Both empty is allowed (a no-op block).
	require.NoError(t, ValidateWidgetHTML(WidgetHTMLConfig{}))

	// Oversize html / css are rejected with actionable messages.
	err := ValidateWidgetHTML(WidgetHTMLConfig{HTML: strings.Repeat("x", maxHTMLPanelBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "256 KiB")

	err = ValidateWidgetHTML(WidgetHTMLConfig{CSS: strings.Repeat("x", maxHTMLPanelCSSBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "64 KiB")

	// The converter omits empty keys and keeps set ones verbatim.
	require.Equal(t, map[string]any{}, ToStoredWidgetHTML(WidgetHTMLConfig{}))
	stored := ToStoredWidgetHTML(WidgetHTMLConfig{HTML: "<p>hi</p>", CSS: "p{}"})
	require.Equal(t, "<p>hi</p>", stored["html"])
	require.Equal(t, "p{}", stored["css"])
}
```

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/validate/ -run TestValidateWidgetHTML -timeout 3m`
Expected: FAIL — `WidgetHTMLConfig` undefined.

**Step 3: Implement**

Append to `internal/validate/widget_config.go`:

```go
// WidgetHTMLConfig is the `html` block of a widget-config tool call: a
// script-free panel's markup + stylesheet. The client sanitizes with DOMPurify
// and renders inside a sandboxed iframe; the server only bounds size.
type WidgetHTMLConfig struct {
	HTML string `json:"html"`
	CSS  string `json:"css"`
}

const (
	// maxHTMLPanelBytes caps a panel's markup; maxHTMLPanelCSSBytes its
	// stylesheet. Both sit far below dashboarddoc's 1 MiB config cap.
	maxHTMLPanelBytes    = 256 << 10
	maxHTMLPanelCSSBytes = 64 << 10
)

// ValidateWidgetHTML bounds a panel's markup and stylesheet. Empty strings
// are allowed (the drawer shows an empty state; the agent block treats a zero
// value as a no-op).
func ValidateWidgetHTML(cfg WidgetHTMLConfig) error {
	if len(cfg.HTML) > maxHTMLPanelBytes {
		return fmt.Errorf("html panel markup must be at most 256 KiB")
	}
	if len(cfg.CSS) > maxHTMLPanelCSSBytes {
		return fmt.Errorf("html panel css must be at most 64 KiB")
	}
	return nil
}

// ToStoredWidgetHTML converts a validated `html` block into the stored
// `config.html` / `config.css` keys. Unset fields are omitted; a zero block
// produces no keys.
func ToStoredWidgetHTML(cfg WidgetHTMLConfig) map[string]any {
	out := map[string]any{}
	if cfg.HTML != "" {
		out["html"] = cfg.HTML
	}
	if cfg.CSS != "" {
		out["css"] = cfg.CSS
	}
	return out
}
```

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/validate/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/validate/widget_config.go internal/validate/widget_config_test.go
git commit -m "feat(validate): html panel config caps and converter"
```

---

### Task 3: REST accepts `html` + bounded config

**Files:**
- Modify: `internal/api/dashboard_handlers.go` (both widget type switches at ~722 and ~898; caps validation on add/update)
- Modify: `internal/api/dashboard_handlers_test.go` (append `TestDashboardHTMLWidget`)

**Step 1: Write the failing test**

Append to `internal/api/dashboard_handlers_test.go` (the file's inline `httptest` pattern — see `TestDashboardAppSettings`; `t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")` first because registration is rate-limited in Redis):

```go
func TestDashboardHTMLWidget(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-html-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash HTML Org")

	// Local request helper mirroring TestDashboardAppSettings' inline pattern
	// (JSON body, Bearer token, admin mode). Reused by Task 4's test.
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		r := httptest.NewRequest(method, path, rd)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}

	rec := do("POST", "/api/v1/dashboards", map[string]any{"title": "HTML Widgets"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	// Source-less html widget round-trips with its config.
	rec = do("POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type":   "html",
		"layout": map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
		"config": map[string]any{"html": `<div class="stat">42</div>`, "css": `.stat{font-size:28px}`},
	})
	require.Equal(t, 201, rec.Code)

	// GET returns type html + config preserved.
	// PUT type conversion to html on a text widget → 200.

	// Oversize markup → 400 naming the cap.
	big := strings.Repeat("x", 256<<10+1)
	rec = do("POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "html", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"html": big},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "256 KiB")

	// Unknown types still rejected.
	rec = do("POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 400, rec.Code)
}
```

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestDashboardHTMLWidget -timeout 3m`
Expected: FAIL — `invalid widget type`.

**Step 3: Implement**

In `handleAddWidget`, extend the type switch and validate caps right after it:

```go
switch req.Type {
case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric, models.WidgetHTML:
default:
	writeError(w, http.StatusBadRequest, "invalid widget type")
	return
}
if req.Type == models.WidgetHTML {
	if err := validate.ValidateWidgetHTML(widgetHTMLConfigFrom(req.Config)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
}
```

In `handleUpdateWidget`, extend the type switch the same way and, when `req.Config` carries string `html`/`css` keys, validate the same caps:

```go
if req.Config != nil {
	if err := validate.ValidateWidgetHTML(widgetHTMLConfigFrom(req.Config)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
}
```

Shared helper in `dashboard_handlers.go`:

```go
// widgetHTMLConfigFrom extracts panel fields from a raw config map; non-string
// values are treated as absent (the document store keeps them, the renderer
// ignores them).
func widgetHTMLConfigFrom(config map[string]interface{}) validate.WidgetHTMLConfig {
	cfg := validate.WidgetHTMLConfig{}
	if s, ok := config["html"].(string); ok {
		cfg.HTML = s
	}
	if s, ok := config["css"].(string); ok {
		cfg.CSS = s
	}
	return cfg
}
```

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestDashboardHTMLWidget -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/dashboard_handlers.go internal/api/dashboard_handlers_test.go
git commit -m "feat(api): accept html widgets with bounded panel config"
```

---

### Task 4: Org switch — setting, helper, response fields, enforcement

**Files:**
- Modify: `internal/models/org.go` (`OrgSettings`)
- Modify: `internal/api/org_handlers.go` (`orgHTMLPanelsEnabled` helper next to `orgBranding`)
- Modify: `internal/api/dashboard_handlers.go` (both response structs + rejection in add/update)
- Modify: `internal/api/dashboard_handlers_test.go` (append `TestOrgHTMLPanelsSwitch`)

**Step 1: Write the failing test**

Append to `internal/api/dashboard_handlers_test.go` (reuse Task 3's `do` helper + dashboard creation):

```go
func TestOrgHTMLPanelsSwitch(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	// register + create dashboard exactly like TestDashboardHTMLWidget, then:

	// Default: absent means enabled — dashboard GET reports true.
	rec := do("GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":true`)

	// Disable org-wide; the setting round-trips and the dashboard GET flips.
	rec = do("PUT", "/api/v1/org/settings", map[string]any{"html_panels_enabled": false})
	require.Equal(t, 200, rec.Code)
	rec = do("GET", "/api/v1/org/settings", nil)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":false`)
	rec = do("GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"html_panels_enabled":false`)

	// Creating an html widget is rejected while disabled…
	rec = do("POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "html", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")

	// …and allowed again after re-enabling (explicit true round-trips too).
	rec = do("PUT", "/api/v1/org/settings", map[string]any{"html_panels_enabled": true})
	require.Equal(t, 200, rec.Code)
	rec = do("POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "html", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 201, rec.Code)
}
```

Also assert the public dashboard GET carries `html_panels_enabled` (share the dashboard, `GET /api/v1/public/dashboards/{token}` → field present).

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestOrgHTMLPanelsSwitch -timeout 3m`
Expected: FAIL — no such field.

**Step 3: Implement**

`internal/models/org.go`:
```go
type OrgSettings struct {
	DefaultQueryTimeout int          `json:"default_query_timeout_seconds,omitempty"`
	DefaultMaxRows      int          `json:"default_max_rows,omitempty"`
	AuditRetentionDays  int          `json:"audit_retention_days,omitempty"`
	Branding            *OrgBranding `json:"branding,omitempty"`
	// HTMLPanelsEnabled gates the `html` widget type org-wide. nil (absent)
	// means enabled; an explicit false hides every panel behind a placeholder
	// and rejects creation/conversion.
	HTMLPanelsEnabled *bool `json:"html_panels_enabled,omitempty"`
}
```
Struct-first matters here: `handleUpdateOrgSettings` re-marshals the struct, so the pointer is what lets an explicit `false` survive the round-trip (`omitempty` drops nil, keeps `false`).

`internal/api/org_handlers.go` (next to `orgBranding`):
```go
// orgHTMLPanelsEnabled reports whether html panels are enabled for the org.
// Absent settings mean enabled; a read failure degrades to enabled so a
// transient DB error never blanks a surface.
func (s *Server) orgHTMLPanelsEnabled(ctx context.Context, orgID string) bool {
	var settingsJSON []byte
	if err := s.db.Pool.QueryRow(ctx, `SELECT settings FROM orgs WHERE id = $1`, orgID).Scan(&settingsJSON); err != nil {
		slog.Debug("org settings read failed", "org_id", orgID, "error", err)
		return true
	}
	var settings models.OrgSettings
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return true
	}
	return settings.HTMLPanelsEnabled == nil || *settings.HTMLPanelsEnabled
}
```

`internal/api/dashboard_handlers.go` — add `HTMLPanelsEnabled bool \`json:"html_panels_enabled"\`` to `dashboardWithWidgets` (~489) and `publicDashboardWithWidgets` (~1364), populated with `s.orgHTMLPanelsEnabled(ctx, dash.OrgID)`. In `handleAddWidget`, after the type switch:
```go
if req.Type == models.WidgetHTML && !s.orgHTMLPanelsEnabled(ctx, claims.OrgID) {
	writeError(w, http.StatusBadRequest, "html panels are disabled for this organization")
	return
}
```
In `handleUpdateWidget`, when `req.Type != nil && *req.Type == models.WidgetHTML` **or** `req.Config` carries string `html`/`css` keys, run the same check (fail-closed authoring: panel config writes are rejected while disabled).

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -timeout 3m`
Expected: PASS (including the org settings round-trip and public GET).

**Step 5: Commit**

```bash
git add internal/models/org.go internal/api/org_handlers.go internal/api/dashboard_handlers.go internal/api/dashboard_handlers_test.go
git commit -m "feat(api): org-level html panels switch"
```

---

### Task 5: Agent tools author panels

**Files:**
- Modify: `internal/agent/tools_manage.go` (`validDashboardWidgetType`; create/update/set_widget_config handlers + schemas; org gate via `ctx.DB`)
- Modify: `internal/agent/tools_seed.go` (`builtinSeedRev = 1` → `2`)
- Modify: `internal/agent/tools_manage_test.go` (append)

**Step 1: Write the failing test**

Append to `internal/agent/tools_manage_test.go` (mirror the existing `set_widget_config` / `create_dashboard_widget` tests' setup — a bare `ToolContext` with a real pool + `DashboardDocStore`):

```go
func TestWidgetHTMLTooling(t *testing.T) {
	// create_dashboard_widget with type html + html block → widget config
	// carries html/css; no source required.
	// create with connector_id + type html → error ("source-less").
	// set_widget_config html block merges over existing config.
	// Oversize html → error naming 256 KiB.
	// Org disabled (UPDATE orgs SET settings = settings || '{"html_panels_enabled":false}')
	// → create/set/convert all error with "disabled".
}
```

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/agent/ -run TestWidgetHTMLTooling -timeout 3m`
Expected: FAIL — `html` not a valid type.

**Step 3: Implement**

- `validDashboardWidgetType`: add `models.WidgetHTML`.
- New schema constant next to the others:
```go
const widgetHTMLBlockSchema = `{"type":"object","description":"HTML panel content: script-free markup + stylesheet, sanitized client-side. Use for bespoke sections (callouts, branded tiles) that widgets cannot express.","properties":{"html":{"type":"string","description":"Panel markup; scripts, links, forms, and third-party images are stripped at render"},"css":{"type":"string","description":"Panel stylesheet (≤64 KiB); can use the app theme's CSS variables like var(--accent)"}}}`
```
- `create_dashboard_widget` description: mention html panels; schema: `"type":{"enum":["chart","table","metric","text","html"]}`, append `,"html":` + `widgetHTMLBlockSchema`.
- Create handler: add `HTML *validate.WidgetHTMLConfig \`json:"html"\``; relax the source requirement — when `req.Type == "html"`, reject any notebook/cell/connector/query ("html panels are source-less") and skip the source checks; validate the block; merge via `validate.ToStoredWidgetHTML`; org gate:
```go
if req.Type == "html" && !orgHTMLPanelsEnabled(ctx.Context, ctx.DB, ctx.OrgID) {
	return nil, fmt.Errorf("html panels are disabled for this organization")
}
```
- `orgHTMLPanelsEnabled` helper in `tools_manage.go` (mirrors the API helper; absent = true).
- `set_widget_config`: add `HTML *validate.WidgetHTMLConfig`; validate; `config["html"]`/`config["css"]` merge for set keys; extend the "nothing to update" check and the audited `keys` list; org gate whenever the block is present (fail closed), then require the widget's projected type to be `html` (`"set the widget type to html first"` otherwise).
- `update_dashboard_widget`: type enum gains `html`; when converting to html, org gate.
- `tools_seed.go`: `const builtinSeedRev = 2` (descriptions/schemas changed for three builtins).

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/agent/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/agent/tools_manage.go internal/agent/tools_seed.go internal/agent/tools_manage_test.go
git commit -m "feat(agent): author html panels with the dashboard tools"
```

---

### Task 6: Guide + tool descriptions

**Files:**
- Modify: `internal/agent/guide/guide.md` (widget types intro + new `### html` section)
- Modify: `internal/agent/tools_manage.go` (descriptions only — the seed revision already covers refresh)

**Step 1: Update the guide**

- Line 25: "Widgets are `chart`, `table`, `metric`, or `text`." → add `html`.
- Add a section after `### text`:

```markdown
### html

A script-free panel for bespoke sections: your markup + stylesheet, rendered
in a sandboxed frame. Source-less like a text panel. Set it with
`set_widget_config`'s `html` block (or create with `type: "html"`):

```json
{ "html": "<div class=\"stat\">42</div>", "css": ".stat { font-size: 28px; color: var(--accent); }" }
```

- Scripts, forms, links, and third-party images are stripped; images may use
  `data:` URIs or same-origin attachments only.
- `{{variable}}` tokens interpolate the current values as escaped text.
- The app theme's CSS variables (`var(--accent)`, `var(--text-primary)`, …)
  are available inside the panel.
- Org admins can disable panels org-wide (`html_panels_enabled`); creating one
  while disabled fails — fall back to `text`.
```

**Step 2: Verify**

Run: `bash scripts/go-test.sh ./internal/agent/ -timeout 3m`
Expected: PASS (guide is embedded; no code change beyond the markdown).

**Step 3: Commit**

```bash
git add internal/agent/guide/guide.md internal/agent/tools_manage.go
git commit -m "docs(agent): html panel guide and tool descriptions"
```

---

## Phase 2 — Client renderer

### Task 7: `interpolateHtml` — HTML-safe variable interpolation

**Files:**
- Modify: `web/src/utils/interpolateText.ts`
- Modify: `web/src/utils/interpolateText.test.ts`

**Step 1: Write the failing test**

Append to `web/src/utils/interpolateText.test.ts`:

```ts
describe('interpolateHtml', () => {
  const variables: DashboardVariable[] = [
    { name: 'region', label: 'Region', type: 'text' },
    { name: 'window', label: 'Window', type: 'date_range' },
  ]

  test('escapes values for both text and attribute contexts', () => {
    const out = interpolateHtml('<div title="{{region}}">{{region}}</div>', { region: '<b>"EU" & \'west\'</b>' }, variables)
    expect(out).toBe('<div title="&lt;b&gt;&quot;EU&quot; &amp; &#39;west&#39;&lt;/b&gt;">&lt;b&gt;&quot;EU&quot; &amp; &#39;west&#39;&lt;/b&gt;</div>')
  })

  test('unknown tokens stay visible; date_range renders start → end', () => {
    expect(interpolateHtml('{{nope}}', {}, variables)).toBe('{{nope}}')
    expect(interpolateHtml('{{window}}', { window: ['2026-01-01', '2026-02-01'] }, variables)).toBe('2026-01-01 → 2026-02-01')
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/utils/interpolateText.test.ts`
Expected: FAIL — `interpolateHtml` is not exported.

**Step 3: Implement**

In `web/src/utils/interpolateText.ts`:
```ts
// Panel interpolation escapes the five HTML-significant characters: values
// may land in text or attribute contexts, and the result is sanitized later.
function escapeHtmlAttr(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

// Display-only interpolation for html panels: declared variables are
// substituted as escaped plain text before DOMPurify runs. Unknown tokens stay
// visible so authors can spot typos.
export function interpolateHtml(
  template: string,
  values: Record<string, unknown>,
  variables: DashboardVariable[],
): string {
  const declared = new Map(variables.map((v): [string, DashboardVariable] => [v.name, v]))
  return template.replace(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g, (match, name: string) => {
    const variable = declared.get(name)
    if (!variable) return match
    return escapeHtmlAttr(displayValue(values[name], variable.type))
  })
}
```

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/utils/interpolateText.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/utils/interpolateText.ts web/src/utils/interpolateText.test.ts
git commit -m "feat(web): html-safe variable interpolation"
```

---

### Task 8: Expose the resolved app theme

**Files:**
- Modify: `web/src/components/AppThemeProvider.tsx`
- Modify: `web/src/components/AppThemeProvider.test.tsx`

**Step 1: Write the failing test**

Append a case that renders a probe component using `useAppTheme()` inside the provider and asserts it receives `scheme` and `vars['--accent']`; and that the hook returns `null` outside the provider.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/AppThemeProvider.test.tsx`
Expected: FAIL — no such export.

**Step 3: Implement**

```tsx
import { createContext, useContext } from 'react'
import type { ResolvedAppTheme } from '../utils/appTheme'

/** Resolved theme for consumers that cannot read CSS variables (iframes). */
const AppThemeContext = createContext<ResolvedAppTheme | null>(null)

/** Null outside an AppThemeProvider (editor chrome / global theme applies). */
export function useAppTheme(): ResolvedAppTheme | null {
  return useContext(AppThemeContext)
}
```

Wrap the provider's children:
```tsx
<AppChartThemeContext.Provider value={chartTheme}>
  <AppThemeContext.Provider value={resolved}>
    <div className="app-theme" data-app-scheme={resolved.scheme} style={resolved.vars}>
      {children}
    </div>
  </AppThemeContext.Provider>
</AppChartThemeContext.Provider>
```

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/components/AppThemeProvider.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AppThemeProvider.tsx web/src/components/AppThemeProvider.test.tsx
git commit -m "feat(web): expose resolved app theme to panels"
```

---

### Task 9: `HtmlWidget` — sanitized, sandboxed renderer

**Files:**
- Modify: `web/package.json` (+ lockfile) — add `dompurify`
- Create: `web/src/components/HtmlWidget.tsx`
- Create: `web/src/components/HtmlWidget.test.tsx`

**Step 1: Add the dependency**

```bash
cd web && npm install dompurify
```
(DOMPurify 3.x ships its own types; if `tsc` cannot find them, add `@types/dompurify` as a dev dependency.)

**Step 2: Write the failing test**

Create `web/src/components/HtmlWidget.test.tsx` covering the pure helpers and the component (jsdom):

```ts
import { sanitizePanelHtml, buildPanelSrcDoc } from './HtmlWidget'

const ORIGIN = 'https://app.example.com'

describe('sanitizePanelHtml', () => {
  test('strips scripts, event handlers, links, and forms', () => {
    const out = sanitizePanelHtml('<script>alert(1)</script><a href="/x">link</a><form><input></form><div onclick="x">ok</div>', ORIGIN)
    expect(out).not.toContain('<script')
    expect(out).not.toContain('<a ')
    expect(out).not.toContain('<form')
    expect(out).not.toContain('onclick')
    expect(out).toContain('<div>ok</div>')
  })

  test('restricts image sources to data: and the app origin', () => {
    const out = sanitizePanelHtml(
      '<img src="https://evil.example/x.png"><img src="//evil.example/x.png"><img src="data:image/png;base64,AAA"><img src="/api/v1/attachments/1"><img src="https://app.example.com/logo.png">',
      ORIGIN,
    )
    expect(out).not.toContain('evil.example')
    expect(out).toContain('data:image/png')
    expect(out).toContain('/api/v1/attachments/1')
    expect(out).toContain('https://app.example.com/logo.png')
  })
})

describe('buildPanelSrcDoc', () => {
  test('composes CSP, theme tokens, css, and body', () => {
    const doc = buildPanelSrcDoc({
      html: '<div class="stat">42</div>',
      css: '.stat{color:var(--accent)}',
      tokens: { '--accent': '#7c5cff' },
      scheme: 'dark',
      origin: ORIGIN,
    })
    expect(doc).toContain(`img-src data: ${ORIGIN}`)
    expect(doc).toContain("default-src 'none'")
    expect(doc).toContain('--accent:#7c5cff')
    expect(doc).toContain('.stat{color:var(--accent)}')
  })

  test('neutralizes a </style> breakout in css', () => {
    const doc = buildPanelSrcDoc({ html: '', css: '</style><script>alert(1)</script>', tokens: {}, scheme: 'light', origin: ORIGIN })
    expect(doc).not.toContain('</style><script')
  })
})

describe('HtmlWidget', () => {
  test('renders a fully locked-down iframe with interpolated content', () => {
    // render <HtmlWidget html={'<p>{{region}}</p>'} css="" values={{region:'<b>x</b>'}} variables=[...] />
    // assert: iframe has sandbox="" (empty), title="HTML panel"; srcdoc contains escaped value and no raw <b>
  })

  test('disabled renders the org placeholder; empty renders the empty state', () => {})
})
```

**Step 3: Run to verify failure**

Run: `cd web && npx vitest run src/components/HtmlWidget.test.tsx`
Expected: FAIL — module not found.

**Step 4: Implement `web/src/components/HtmlWidget.tsx`**

Structure (exact code; adapt style objects to the repo's conventions):

```tsx
import DOMPurify from 'dompurify'
import { useMemo } from 'react'
import { interpolateHtml } from '../utils/interpolateText'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useAppTheme } from './AppThemeProvider'
import type { AppColorScheme } from '../utils/appTheme'
import type { DashboardVariable } from '../types'

// The strict allowlist. Deliberately absent: a, form, input, button, script,
// style, iframe, object, embed, link, meta, base, svg (svg can carry scripts).
const ALLOWED_TAGS = [
  'div', 'span', 'p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'dl', 'dt', 'dd',
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'caption',
  'img', 'strong', 'b', 'em', 'i', 'u', 's', 'small', 'code', 'pre', 'br', 'hr',
  'blockquote', 'figure', 'figcaption', 'sub', 'sup', 'section', 'header', 'footer',
  'main', 'aside', 'article', 'nav', 'mark', 'kbd', 'samp', 'var', 'abbr', 'time',
]
const ALLOWED_ATTR = [
  'class', 'style', 'title', 'alt', 'src', 'width', 'height', 'colspan', 'rowspan',
  'align', 'valign', 'dir', 'lang', 'scope', 'headers', 'datetime', 'cite', 'start', 'reversed',
]

function uriRegexp(origin: string): RegExp {
  const escaped = origin.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  // data: images, absolute same-origin URLs, or origin-relative paths. The
  // (?!\/) guard blocks protocol-relative URLs (//evil.example/x.png).
  return new RegExp(`^(?:data:image\\/(?:png|jpe?g|gif|webp|svg\\+xml);|${escaped}\\/|\\/(?!\\/))`)
}

/** Sanitizes panel markup; exported for tests. */
export function sanitizePanelHtml(html: string, origin: string): string {
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOWED_URI_REGEXP: uriRegexp(origin),
  })
}

/** Neutralizes the only sequence that can close the injected style element. */
function safeCss(css: string): string {
  return css.replace(/<\/(style)/gi, '<\\/$1')
}

/** Composes the frame document; exported for tests. */
export function buildPanelSrcDoc({ html, css, tokens, scheme, origin }: {
  html: string
  css: string
  tokens: Record<string, string>
  scheme: AppColorScheme
  origin: string
}): string {
  const csp = [
    "default-src 'none'",
    `img-src data: ${origin}`,
    "style-src 'unsafe-inline'",
    `font-src data: ${origin}`,
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ')
  const tokenCss = Object.entries(tokens).map(([k, v]) => `${k}:${v}`).join(';')
  const canvas = scheme === 'dark' ? '#141414' : '#ffffff'
  const text = scheme === 'dark' ? '#e8e8e8' : '#111111'
  return [
    '<!doctype html><html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    `<style>:root{${tokenCss}}body{margin:0;background:var(--bg-card,${canvas});color:var(--text-primary,${text});font-family:var(--app-font-family,'DM Sans',system-ui,sans-serif)}</style>`,
    `<style>${safeCss(css)}</style>`,
    `</head><body>${html}</body></html>`,
  ].join('')
}

interface HtmlWidgetProps {
  html: string
  css: string
  /** Overrides the dashboard variables context; used by tests and the public page. */
  values?: Record<string, unknown>
  variables?: DashboardVariable[]
  /** False when the org switch is off — renders a placeholder instead. */
  enabled?: boolean
}

export function HtmlWidget({ html, css, values, variables, enabled = true }: HtmlWidgetProps) {
  const context = useDashboardVariables()
  const theme = useAppTheme()
  const interpolated = useMemo(
    () => interpolateHtml(html, values ?? context.values, variables ?? context.variables),
    [html, values, variables, context.values, context.variables],
  )
  const srcDoc = useMemo(() => {
    if (!enabled || (!interpolated.trim() && !css.trim())) return ''
    return buildPanelSrcDoc({
      html: sanitizePanelHtml(interpolated, window.location.origin),
      css,
      tokens: theme?.vars ?? {},
      scheme: theme?.scheme ?? 'light',
      origin: window.location.origin,
    })
  }, [enabled, interpolated, css, theme])

  if (!enabled) return <div style={styles.empty}>HTML panels are disabled for this organization.</div>
  if (!srcDoc) return <div style={styles.empty}>Empty HTML panel — edit it in the dashboard editor.</div>
  return (
    <iframe
      title="HTML panel"
      sandbox=""
      referrerPolicy="no-referrer"
      srcDoc={srcDoc}
      style={{ width: '100%', height: '100%', border: 'none', display: 'block' }}
    />
  )
}

const styles: Record<string, React.CSSProperties> = {
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
}
```

**Step 5: Run to verify pass**

Run: `cd web && npx vitest run src/components/HtmlWidget.test.tsx`
Expected: PASS. Then `npx tsc --noEmit` to confirm the dompurify types resolve.

**Step 6: Commit**

```bash
git add web/package.json web/package-lock.json web/src/components/HtmlWidget.tsx web/src/components/HtmlWidget.test.tsx
git commit -m "feat(web): sanitized html panel renderer"
```

---

### Task 10: Wire panels into the app, public, and editor surfaces

**Files:**
- Modify: `web/src/types/index.ts` (`Widget['type']` union; `Dashboard.html_panels_enabled`)
- Modify: `web/src/utils/widgetKind.ts` (+ `widgetKind.test.ts`) — `isHTMLPanel`, badge label
- Create: `web/src/utils/htmlConfig.ts` (+ `htmlConfig.test.ts`) — `htmlConfigFrom`, mirroring `web/src/utils/textConfig.ts`
- Modify: `web/src/pages/DashboardPage.tsx:844` (render branch)
- Modify: `web/src/pages/PublicDashboardPage.tsx:237` (render branch, passing `values`/`variables` like TextWidget)
- Modify: `web/src/pages/DashboardEditorPage.tsx:868,923` (render branch)

**Step 1: Write the failing tests**

- `widgetKind.test.ts`: `isHTMLPanel` true for `{type:'html'}` with no source, false for query-backed; `widgetKindLabel` returns `'Panel'`.
- Extend `web/src/pages/DashboardPage.test.tsx` (its harness mocks the dashboard API) with a case where a widget `{type:'html', config:{html:'<p>hi</p>'}}` renders an `iframe[title="HTML panel"]`, and `dashboard.html_panels_enabled: false` renders the placeholder text.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/utils/widgetKind.test.ts src/pages/DashboardPage.test.tsx`
Expected: FAIL — no html branch.

**Step 3: Implement**

- `types/index.ts`:
```ts
type: 'chart' | 'table' | 'text' | 'metric' | 'html'
```
```ts
/** Resolved org switch for html panels (served on dashboard + public GETs). */
html_panels_enabled?: boolean
```
- `widgetKind.ts`:
```ts
/** True for a source-less html panel: no notebook cell and no query behind it. */
export function isHTMLPanel(widget: Widget): boolean {
  return widget.type === 'html' && !widget.notebook_id && !widget.connector_id
}
```
`widgetKindLabel` gains `'Panel'` for html panels (check order: query → text → html → cell).
- `DashboardPage.tsx`, `DashboardEditorPage.tsx`, `PublicDashboardPage.tsx`: add the branch between text and cell:
```tsx
) : isHTMLPanel(widget) ? (
  <HtmlWidget
    html={htmlConfigFrom(widget.config).html}
    css={htmlConfigFrom(widget.config).css}
    enabled={dashboard.html_panels_enabled !== false}
  />
) : (
```
`PublicDashboardPage.tsx` additionally passes `values={publicLive ? undefined : defaults}` and `variables={variables}` (same as its TextWidget branch); the app and editor pages rely on the variables context.
Add a `htmlConfigFrom` helper next to `textConfigFrom` (`web/src/utils/` — mirror `textConfigFrom`'s file with `{html: string, css: string}`, coercing non-strings to `''`).

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/utils/widgetKind.test.ts src/pages/DashboardPage.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/types/index.ts web/src/utils/widgetKind.ts web/src/utils/widgetKind.test.ts web/src/utils/htmlConfig.ts web/src/utils/htmlConfig.test.ts web/src/pages/DashboardPage.tsx web/src/pages/DashboardPage.test.tsx web/src/pages/PublicDashboardPage.tsx web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): html panels on the app, public, and editor surfaces"
```

---

## Phase 3 — Authoring & admin

### Task 11: Drawer editors, picker, and field help

**Files:**
- Modify: `web/src/components/WidgetConfigDrawer.tsx` (html branch + type option + `htmlPanelsEnabled` prop)
- Modify: `web/src/components/WidgetConfigDrawer.test.tsx`
- Modify: `web/src/utils/addWidgetPayload.ts` (+ test) — `AddWidgetType` gains `'html'`
- Modify: `web/src/pages/DashboardEditorPage.tsx` (pass `htmlPanelsEnabled={dashboard.html_panels_enabled !== false}`; picker wiring)
- Modify: `web/src/app-builder/fieldHelp.ts` + `fieldHelp.test.ts` (`html.html`, `html.css`)

**Step 1: Write the failing tests**

- `WidgetConfigDrawer.test.tsx`: for a source-less `html` widget, the drawer renders the "Panel HTML" and "Panel CSS" editors and a preview iframe; `htmlPanelsEnabled={false}` renders the disabled note instead of editors.
- `addWidgetPayload.test.ts`: `validTypesForSource('none', true)` includes `'html'`; `('none', false)` does not; `buildAddWidgetPayload` for html returns `{type:'html', config:{}}` with no source keys.
- `fieldHelp.test.ts`: add `'html.html'`, `'html.css'` to `REQUIRED_FIELDS`.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/WidgetConfigDrawer.test.tsx src/utils/addWidgetPayload.test.ts src/app-builder/fieldHelp.test.ts`
Expected: FAIL.

**Step 3: Implement**

- `addWidgetPayload.ts`:
```ts
export type AddWidgetType = 'table' | 'chart' | 'metric' | 'text' | 'html'

export function validTypesForSource(source: AddWidgetSource, htmlEnabled = true): AddWidgetType[] {
  if (source === 'none') return htmlEnabled ? ['text', 'html'] : ['text']
  if (source === 'cell') return ['table', 'chart']
  return ['table', 'chart', 'metric']
}
```
`buildAddWidgetPayload` for source `'none'` with `type === 'html'` returns `{ type: 'html', layout, config: {} }`.
- Drawer: prop `htmlPanelsEnabled?: boolean` (default true); `html`/`css` derived from `config`; html branch mirroring the text branch (multiline `DebouncedTextInput`s with `helpKey="html.html"` / `"html.css"`, the muted hint about escaping/sanitizing, and a 200px preview box rendering `<HtmlWidget html={html} css={css} />`); type `<option value="html">HTML panel</option>` shown only when `htmlPanelsEnabled`; when disabled and `widgetType === 'html'`, render a muted "HTML panels are disabled for this organization." note in place of the editors.
- Editor page: thread the flag into the picker (`validTypesForSource(pickerSource, dashboard.html_panels_enabled !== false)`) and into the drawer; picker descriptor for "HTML panel" ("Script-free HTML/CSS section").
- `fieldHelp.ts`:
```ts
'html.html': { label: 'Panel HTML', hint: 'Script-free markup; scripts, links, forms, and third-party images are stripped.', example: '<div class="stat">42</div>' },
'html.css': { label: 'Panel CSS', hint: 'Stylesheet for the panel; the app theme’s CSS variables are available.', example: '.stat { font-size: 28px; color: var(--accent); }' },
```
(The `fieldHelp.test.ts` guide cross-check passes because `html` and `css` appear in the guide's new section.)

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/components/WidgetConfigDrawer.test.tsx src/utils/addWidgetPayload.test.ts src/app-builder/fieldHelp.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/WidgetConfigDrawer.tsx web/src/components/WidgetConfigDrawer.test.tsx web/src/utils/addWidgetPayload.ts web/src/utils/addWidgetPayload.test.ts web/src/pages/DashboardEditorPage.tsx web/src/app-builder/fieldHelp.ts web/src/app-builder/fieldHelp.test.ts
git commit -m "feat(web): html panel authoring in the widget drawer"
```

---

### Task 12: Org settings toggle

**Files:**
- Modify: `web/src/pages/OrgSettingsPage.tsx` (+ `web/src/test/OrgSettingsPage.test.tsx`)

**Step 1: Write the failing test**

Extend `web/src/test/OrgSettingsPage.test.tsx`: with `html_panels_enabled: false` in the GET payload, the toggle renders off; flipping it PUTs `{html_panels_enabled: true}`.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/test/OrgSettingsPage.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `OrgSettingsPayload` gains `html_panels_enabled?: boolean`.
- New "App builder" section (near branding): a checkbox "Allow HTML panels" + hint ("Script-free HTML/CSS sections on app surfaces. When off, existing panels show a placeholder and new ones cannot be added."). Save via `api.put('/api/v1/org/settings', { html_panels_enabled: next })`; derive the checkbox from the loaded payload (`!== false`).

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/test/OrgSettingsPage.test.tsx`
Expected: PASS. Then browser-validate the toggle with agent-browser against the dev stack (repo rule: real-browser validation is mandatory for UI changes).

**Step 5: Commit**

```bash
git add web/src/pages/OrgSettingsPage.tsx web/src/test/OrgSettingsPage.test.tsx
git commit -m "feat(web): org switch for html panels"
```

---

## Phase 4 — Verification

### Task 13: E2E — rendering, sanitization, kill switch

**Files:**
- Create: `e2e/html-panel.spec.ts`

**Scenarios:**
1. **Render + sanitize**: create a dashboard via API with an html widget (`config.html` containing a heading, a `<script>`, and an `<img src="https://evil.example/x.png">`; `config.css` styling the heading). Open `/dashboards/{id}/view`; assert the heading is visible inside the frame (`frameLocator`), the script tag count is 0, the external image is absent, and the heading's computed color matches the CSS.
2. **Drawer authoring**: open `/dashboards/{id}`; open the widget drawer; the HTML/CSS editors and the preview frame render; typing into the HTML editor updates the preview.
3. **Kill switch**: `PUT /api/v1/org/settings {html_panels_enabled:false}` as the admin → reload the app view → placeholder text visible; `POST` widget type html → 400; re-enable → the panel renders again.

**Run:**
```bash
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test html-panel.spec.ts
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test html-panel.spec.ts --repeat-each=2
```

**Commit:**
```bash
git add e2e/html-panel.spec.ts
git commit -m "test(e2e): html panels and the org kill switch"
```

---

### Task 14: Docs + full verification + PR

**Files:**
- Modify: `AGENTS.md` (Data apps paragraph: `html` widget, org switch, `html_panels_enabled` on GETs, DOMPurify + sandbox/CSP invariants, caps)
- Modify: `DESIGN.md` (escape hatch tier 1 implemented; security boundary + guidance patterns)
- Modify: `FRONTEND.md` (HtmlWidget: states, preview, theme tokens)

**Verification:**
```bash
task check
cd web && npx tsc -b && npm run build && npm run test:run
cd relay && npm run build
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test html-panel.spec.ts dashboard-app.spec.ts dashboard-publish.spec.ts app-builder.spec.ts dashboard.spec.ts
```

Plus a browser sweep with agent-browser over: app view with a panel, drawer authoring, org toggle, public page with a panel.

Then `git push -u origin feat/data-apps-wave-3` + `gh pr create --title "feat: data apps wave 3 — HTML/CSS panels"` (base `main`; wave 2 + app-builder are already merged).

**Commit:**
```bash
git add AGENTS.md DESIGN.md FRONTEND.md
git commit -m "docs: data apps wave 3 surfaces and the html panel boundary"
```

---

## Out of scope (later)

- Wave 4: `custom` widget (allow-scripts opaque-origin sandbox, postMessage data bridge, `custom_code_enabled` default off), full custom page.
- Distribution: dashboard embedding, presentation mode, print/PDF.
- MCP allowlist additions for the new tools (existing follow-up).
- Review-engine findings for empty panels (e.g. "panel has no content").
- CodeMirror HTML/CSS authoring modes (wave 4 upgrades the drawer editor).
- Signed embedding, scheduled snapshots, saved views.

## Known limitations to record in the docs

- Panels are script-free by design; interactivity arrives with wave 4's `custom` widget.
- `style` attributes are allowed and CSP blocks subresource loads — a panel cannot fetch anything beyond `data:`/origin images, but it can still reference origin URLs that 404.
- The org switch is evaluated live, including on published snapshots; a disabled panel's config is retained and reappears when re-enabled.
- Panel content is not part of the review engine this wave.
