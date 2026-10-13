# Data Apps — Wave 4 Implementation Plan (Custom Components & Pages)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship escape-hatch tier 2 — the `custom` widget (author HTML/CSS/JS + widget bindings, rendered in an `allow-scripts` opaque-origin sandbox with a postMessage data bridge through the existing execute pipeline) and the full custom page (`settings.app.layout: "custom"`), org-gated by `custom_code_enabled` (default off) with agent authoring.

**Architecture:** Approach from `docs/plans/2026-10-11-data-apps-design.md`, wave 4 only. A widget type `custom` stores `config = { html, css, js, bindings: [{ key, widget_id }] }`. The client composes one document — markup, the app-theme tokens, and the inline JS — served in `<iframe sandbox="allow-scripts">` with **no** `allow-same-origin` (opaque origin: no cookies, no storage, no authenticated fetch) plus a CSP `<meta>` that blocks every outbound request (`connect-src 'none'`). The sandbox is the security boundary; child→host messages (`ready`, `request_data`, `open_link`) are validated against `event.source`, and the host answers `request_data` only with data the viewer may already see: bound **query** widgets execute through the existing `POST /dashboards/{id}/execute` pipeline as the viewer (published snapshots and the public `public_live` gate included), bound **cell** widgets resolve their stored outputs. Bindings are validated to belong to the same dashboard at write time (REST/tools) and in the relay store validator's merged projection. `orgs.settings.custom_code_enabled` (absent = **disabled**) gates creation, conversion, and config writes, renders a placeholder on every surface while off, and rides dashboard + public GETs as a resolved bool.

**Tech Stack:** Go (`net/http` ServeMux, pgx), React 19 + TypeScript, CodeMirror 6 (`@codemirror/lang-html`, `@codemirror/lang-css`; `@codemirror/lang-javascript` already present), Vitest, Playwright.

**Preconditions:**
- Branch `feat/data-apps-wave-4` checked out from `main` (`fbc08693`, wave 3 merged as release `v0.75.0`).
- Start infra before Go tests: `task infra:up` (the disposable per-worktree DB is handled by `scripts/go-test.sh`).
- Full dev stack before E2E/browser validation — **recreate it from this worktree** (`docker compose -f docker-compose.dev.yml up -d` here; the previously running stack is bind-mounted to `.worktrees/data-apps-wave-3`).
- All Go test commands include `-timeout 3m` (repo rule).
- Distribution (embed/present/print) and signed embedding are separate plans; do not implement them here.

## Decisions locked for this wave

| Question | Decision |
|---|---|
| Widget type | `custom`; V133 widens `widgets_type_check` to `('chart','table','text','metric','html','custom')`. No new columns. |
| Config shape | `config.html` / `config.css` / `config.js` (strings) + `config.bindings` (array of `{key, widget_id}`, ≤20, key `^[a-z0-9][a-z0-9_-]*$` ≤64 chars, one binding per key, no duplicate keys, `widget_id` a canonical UUID). |
| Size caps | `html` ≤ 256 KiB, `css` ≤ 64 KiB, `js` ≤ 128 KiB, and `html+css+js` ≤ 256 KiB total; enforced by `validate.ValidateWidgetCustom` (REST + tools) — the stored config stays far below the 1 MiB document config cap. |
| Sandbox | `<iframe sandbox="allow-scripts" referrerPolicy="no-referrer" title="Custom component">` — no `allow-same-origin`, so the frame is an opaque origin: no cookies, no localStorage, no authenticated fetch. |
| CSP | `<meta http-equiv="Content-Security-Policy">`: `default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: <origin>; font-src data: <origin>; connect-src 'none'; form-action 'none'; base-uri 'none'`. Inline script only — no `src`, no remote anything. |
| Bridge protocol | Frame→host: `{type:'ready'}`, `{type:'request_data', key}`, `{type:'open_link', href}`. Host→frame: `{type:'init', theme, variables, bindings:[{key,status}]}`, `{type:'data', key, result}`, `{type:'error', key?, message}`. Host validates `event.source === iframe.contentWindow` and `event.origin === 'null'` (opaque frame); frame posts with `targetOrigin: '*'` (its opaque origin cannot know the host origin). Malformed/unknown messages are ignored, never thrown. |
| Bound data | A bound **query** widget executes through `POST /dashboards/{id}/execute` (draft), `?published` resolution for published mode, or `POST /api/v1/public/{token}/execute` on public pages — the viewer's identity, variables, connector selection, cache, and rate limits apply unchanged. A bound **cell** widget resolves its stored outputs (`widgets_data` when present, else the notebook fetch) — the bridge never executes cells. `request_data` before the viewer has `view_with_data` (or before `public_live` data on a public page) answers `{type:'error'}`. |
| Binding validation | Every binding's `widget_id` must be a widget of the **same dashboard**: write-time checks in REST add/update and the agent tools, plus a merged-projection check in the relay store validator (`validateDashboardDocStore`) so a browser document edit cannot introduce dangling bindings. A dangling binding found at render time (legacy data) renders as an error state for that key, never a crash. |
| Org switch | `orgs.settings.custom_code_enabled` (`*bool`, absent = **disabled** — unlike html panels). Served as a resolved `custom_code_enabled` bool on dashboard + public GETs. While off: creating a `custom` widget, converting to it, or writing `custom` config keys (REST and agent tools) fails closed; existing custom widgets render a quiet placeholder on every surface and keep their config. |
| Audit | `widget.create`/`widget.update` (already emitted) gain `metadata: {type:'custom', js_bytes:N}` on the REST and agent paths when the widget type is `custom` — a code-authoring trail without a new event name. |
| Public gating | Public pages render custom frames only when the org switch is on; data answers only when `settings.public_live` is true (rate limits unchanged). |
| Full custom page | `settings.app.layout: "custom"`: the app surface renders the **first** custom widget (by `created_at`) full-bleed with no card chrome; every other widget is a data source (hidden from the app view, badged "Data source" in the editor). No custom widget present → falls back to the grid. Enforced by review findings (info), not a publish refusal. |
| Authoring | The drawer gains HTML/CSS/JS CodeMirror 6 editors (lazy language imports, 600 ms debounce + flush-on-unmount like `DebouncedTextInput`), a binding picker (existing widgets of the dashboard), and a live `SandboxedFrame` preview (no bound data in the preview). |
| Agent tooling | New `create_custom_widget` builtin (org-gated; html/css/js/bindings block), `update_dashboard_widget`'s type enum gains `custom`, `set_widget_config` gains a `custom` block; `builtinSeedRev` 2 → 3. Agent-written code lands in the draft for human review before publish. |
| Review engine | New findings: `layout: "custom"` with no custom widget (warning), multiple custom widgets under `layout: "custom"` (info), custom widget with no bindings (info). |
| MCP allowlist | Out of scope (existing follow-up): the new tools stay out of the MCP allowlist. |

---

## Phase 1 — Backend foundations

### Task 1: `custom` widget type in models, document, and schema

**Files:**
- Modify: `internal/models/dashboard.go` (widget type constants)
- Modify: `internal/dashboarddoc/doc.go` (`widgetTypes` map)
- Create: `internal/database/migrations/V133__custom_widget_type.sql`
- Modify: `internal/dashboarddoc/apply_test.go` (extend `TestApply_SetWidgetType`)

**Step 1: Write the failing test**

Extend `TestApply_SetWidgetType` in `internal/dashboarddoc/apply_test.go` with a `custom` case mirroring the `html` one (seed a widget, `SetWidgetType(state, id, "custom")`, project, assert `w.Type == "custom"` and that unrelated fields survive). The existing `html` + `custom`-rejection subtests move: the current "unlisted custom type is rejected" subtest must be **removed** (custom is now listed) and replaced by a rejection for a genuinely unknown type (e.g. `"sankey_widget"`).

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/dashboarddoc/ -run TestApply_SetWidgetType -timeout 3m`
Expected: FAIL — `custom` is not in `widgetTypes`.

**Step 3: Implement**

`internal/models/dashboard.go`:
```go
const (
	WidgetChart  WidgetType = "chart"
	WidgetTable  WidgetType = "table"
	WidgetText   WidgetType = "text"
	WidgetMetric WidgetType = "metric"
	WidgetHTML   WidgetType = "html"
	WidgetCustom WidgetType = "custom"
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
	"custom": true,
}
```

`internal/database/migrations/V133__custom_widget_type.sql`:
```sql
-- V133: custom widget type (data apps wave 4 — escape hatch tier 2).
-- Author HTML/CSS/JS; rendering is sandboxed client-side behind the org switch.
ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check;
ALTER TABLE widgets ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('chart', 'table', 'text', 'metric', 'html', 'custom'));
```

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/dashboarddoc/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/models/dashboard.go internal/dashboarddoc/doc.go internal/dashboarddoc/apply_test.go internal/database/migrations/V133__custom_widget_type.sql
git commit -m "feat(api): custom widget type and document support"
```

---

### Task 2: `validate.WidgetCustomConfig` — caps, bindings, converter

**Files:**
- Modify: `internal/validate/widget_config.go`
- Modify: `internal/validate/widget_config_test.go` (append)

**Step 1: Write the failing test**

Append to `internal/validate/widget_config_test.go`:

```go
func TestValidateWidgetCustom(t *testing.T) {
	// Valid content passes; both strings empty is allowed (a no-op block).
	require.NoError(t, ValidateWidgetCustom(WidgetCustomConfig{
		HTML: `<div id="root"></div>`,
		CSS:  `#root { color: var(--accent); }`,
		JS:   `document.getElementById('root').textContent = 'hi'`,
		Bindings: []WidgetCustomBinding{
			{Key: "sales", WidgetID: "3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"},
		},
	}))
	require.NoError(t, ValidateWidgetCustom(WidgetCustomConfig{}))

	// Size caps name the field.
	err := ValidateWidgetCustom(WidgetCustomConfig{HTML: strings.Repeat("x", maxCustomHTMLBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "256 KiB")
	err = ValidateWidgetCustom(WidgetCustomConfig{CSS: strings.Repeat("x", maxCustomCSSBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "64 KiB")
	err = ValidateWidgetCustom(WidgetCustomConfig{JS: strings.Repeat("x", maxCustomJSBytes+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "128 KiB")

	// Bindings: key charset, duplicates, UUID, count.
	err = ValidateWidgetCustom(WidgetCustomConfig{Bindings: []WidgetCustomBinding{{Key: "Bad Key", WidgetID: "3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "binding key")
	err = ValidateWidgetCustom(WidgetCustomConfig{Bindings: []WidgetCustomBinding{
		{Key: "a", WidgetID: "3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"},
		{Key: "a", WidgetID: "2b3c4d5e-6f70-4a8a-9c5b-7e6d1b2c3a4f"},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
	err = ValidateWidgetCustom(WidgetCustomConfig{Bindings: []WidgetCustomBinding{{Key: "a", WidgetID: "nope"}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "widget id")
	err = ValidateWidgetCustom(WidgetCustomConfig{Bindings: make([]WidgetCustomBinding, 21)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "20")

	// The converter omits empty keys and stores bindings verbatim.
	require.Equal(t, map[string]any{}, ToStoredWidgetCustom(WidgetCustomConfig{}))
	stored := ToStoredWidgetCustom(WidgetCustomConfig{
		HTML: "<p>hi</p>", CSS: "p{}", JS: "1",
		Bindings: []WidgetCustomBinding{{Key: "sales", WidgetID: "3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"}},
	})
	require.Equal(t, "<p>hi</p>", stored["html"])
	require.Equal(t, []map[string]any{{"key": "sales", "widget_id": "3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"}}, stored["bindings"])
}

func TestWidgetCustomBindingIDs(t *testing.T) {
	ids := WidgetCustomBindingIDs(map[string]any{
		"bindings": []any{
			map[string]any{"key": "a", "widget_id": "3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"},
			map[string]any{"key": "b", "widget_id": "not-a-uuid"},
			"junk",
		},
	})
	require.Equal(t, []string{"3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f"}, ids)
}
```

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/validate/ -run 'TestValidateWidgetCustom|TestWidgetCustomBindingIDs' -timeout 3m`
Expected: FAIL — `WidgetCustomConfig` undefined.

**Step 3: Implement**

Append to `internal/validate/widget_config.go` (uuid is already a dependency of the module; import `github.com/google/uuid` — the file does not import it yet, add it):

```go
// WidgetCustomConfig is the `custom` block of a widget-config tool call and
// the REST config shape: a sandboxed component's markup, stylesheet, script,
// and its data bindings. Rendering is client-side in an allow-scripts
// opaque-origin frame; the server bounds size and binding shape.
type WidgetCustomConfig struct {
	HTML     string                `json:"html"`
	CSS      string                `json:"css"`
	JS       string                `json:"js"`
	Bindings []WidgetCustomBinding `json:"bindings"`
}

// WidgetCustomBinding wires one bridge key to a widget of the same dashboard.
type WidgetCustomBinding struct {
	Key      string `json:"key"`
	WidgetID string `json:"widget_id"`
}

const (
	// maxCustomHTMLBytes / maxCustomCSSBytes / maxCustomJSBytes cap the
	// component bundle; maxCustomBundleBytes caps the sum so one widget can
	// never approach dashboarddoc's 1 MiB config cap.
	maxCustomHTMLBytes   = 256 << 10
	maxCustomCSSBytes    = 64 << 10
	maxCustomJSBytes     = 128 << 10
	maxCustomBundleBytes = 256 << 10
	maxCustomBindings    = 20
	maxCustomBindingKey  = 64
)

// customBindingKeyPattern mirrors the tab-id charset: lowercase ids agents
// and the picker can generate deterministically.
var customBindingKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidateWidgetCustom bounds a component's bundle and its bindings. Empty
// strings are allowed (the drawer shows an empty state; the agent block treats
// a zero value as a no-op). Binding widget existence is checked by the
// caller, which has the database.
func ValidateWidgetCustom(cfg WidgetCustomConfig) error {
	if len(cfg.HTML) > maxCustomHTMLBytes {
		return fmt.Errorf("custom html must be at most 256 KiB")
	}
	if len(cfg.CSS) > maxCustomCSSBytes {
		return fmt.Errorf("custom css must be at most 64 KiB")
	}
	if len(cfg.JS) > maxCustomJSBytes {
		return fmt.Errorf("custom js must be at most 128 KiB")
	}
	if total := len(cfg.HTML) + len(cfg.CSS) + len(cfg.JS); total > maxCustomBundleBytes {
		return fmt.Errorf("custom bundle must be at most 256 KiB")
	}
	if len(cfg.Bindings) > maxCustomBindings {
		return fmt.Errorf("custom components support at most 20 bindings")
	}
	seen := make(map[string]bool, len(cfg.Bindings))
	for _, b := range cfg.Bindings {
		if len(b.Key) == 0 || len(b.Key) > maxCustomBindingKey || !customBindingKeyPattern.MatchString(b.Key) {
			return fmt.Errorf("binding key %q must be lowercase letters, digits, dashes or underscores (max 64)", b.Key)
		}
		if seen[b.Key] {
			return fmt.Errorf("duplicate binding key %q", b.Key)
		}
		seen[b.Key] = true
		if u, err := uuid.Parse(b.WidgetID); err != nil || u.String() != b.WidgetID {
			return fmt.Errorf("binding %q widget id %q is not a valid id", b.Key, b.WidgetID)
		}
	}
	return nil
}

// ToStoredWidgetCustom converts a validated `custom` block into the stored
// `config` keys. Unset fields are omitted; a zero block produces no keys.
func ToStoredWidgetCustom(cfg WidgetCustomConfig) map[string]any {
	out := map[string]any{}
	if cfg.HTML != "" {
		out["html"] = cfg.HTML
	}
	if cfg.CSS != "" {
		out["css"] = cfg.CSS
	}
	if cfg.JS != "" {
		out["js"] = cfg.JS
	}
	if len(cfg.Bindings) > 0 {
		bindings := make([]map[string]any, 0, len(cfg.Bindings))
		for _, b := range cfg.Bindings {
			bindings = append(bindings, map[string]any{"key": b.Key, "widget_id": b.WidgetID})
		}
		out["bindings"] = bindings
	}
	return out
}

// WidgetCustomConfigFrom extracts the custom fields from a raw stored config.
// Non-string values are treated as absent; malformed bindings entries are
// dropped (the renderer shows them as missing keys, never crashes).
func WidgetCustomConfigFrom(config map[string]any) WidgetCustomConfig {
	cfg := WidgetCustomConfig{}
	if s, ok := config["html"].(string); ok {
		cfg.HTML = s
	}
	if s, ok := config["css"].(string); ok {
		cfg.CSS = s
	}
	if s, ok := config["js"].(string); ok {
		cfg.JS = s
	}
	if raw, ok := config["bindings"].([]any); ok {
		for _, entry := range raw {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			key, _ := m["key"].(string)
			widgetID, _ := m["widget_id"].(string)
			if key == "" || widgetID == "" {
				continue
			}
			cfg.Bindings = append(cfg.Bindings, WidgetCustomBinding{Key: key, WidgetID: widgetID})
		}
	}
	return cfg
}

// WidgetCustomBindingIDs returns the valid binding widget ids of a raw stored
// config — the input to the existence checks on the write paths.
func WidgetCustomBindingIDs(config map[string]any) []string {
	cfg := WidgetCustomConfigFrom(config)
	out := make([]string, 0, len(cfg.Bindings))
	for _, b := range cfg.Bindings {
		if u, err := uuid.Parse(b.WidgetID); err == nil && u.String() == b.WidgetID {
			out = append(out, b.WidgetID)
		}
	}
	return out
}
```

Add `"regexp"` and `"github.com/google/uuid"` to the file's imports.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/validate/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/validate/widget_config.go internal/validate/widget_config_test.go
git commit -m "feat(validate): custom component bundle caps and binding shape"
```

---

### Task 3: REST accepts `custom` + binding validation + audit metadata

**Files:**
- Modify: `internal/api/dashboard_handlers.go` (type switches in `handleAddWidget`/`handleUpdateWidget`, the `widgetHTMLConfigFrom` neighborhood, bindings helper)
- Modify: `internal/api/dashboard_handlers_test.go` (append `TestDashboardCustomWidget`)

**Step 1: Write the failing test**

Append to `internal/api/dashboard_handlers_test.go` (reuse `doDashboardJSON`; a registration helper like the wave-3 tests):

```go
func TestDashboardCustomWidget(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-custom-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Custom Org")
	// Enable custom code for the org (default is disabled).
	require.Equal(t, 200, doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings",
		map[string]any{"custom_code_enabled": true}).Code)

	rec := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Custom Widgets"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID := dashResp["id"].(string)

	// The custom widget binds another widget of the same dashboard: seed a
	// query widget first (a raw connector row is enough — the binding ref
	// check only needs existence on the dashboard).
	connID := uuid.NewString()
	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		SELECT $1, org_id, 'binding target', 'postgres', '\x00' FROM dashboards WHERE id = $2`,
		connID, dashID)
	require.NoError(t, err)
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "table", "connector_id": connID, "query": "SELECT 1", "language": "sql",
		"layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 201, rec.Code)
	var target map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&target))
	targetID := target["id"].(string)

	// Custom widget round-trips with its bundle + bindings.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 4, "col": 0, "width": 6, "height": 6},
		"config": map[string]any{
			"html": `<div id="root"></div>`, "css": `#root{color:red}`, "js": `document.getElementById('root').textContent='hi'`,
			"bindings": []map[string]any{{"key": "sales", "widget_id": targetID}},
		},
	})
	require.Equal(t, 201, rec.Code)
	var custom map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&custom))
	require.Equal(t, "custom", custom["type"])

	// A binding to a widget of another dashboard (or none) → 400.
	other := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Elsewhere"})
	var otherResp map[string]any
	require.NoError(t, json.NewDecoder(other.Body).Decode(&otherResp))
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 12, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"bindings": []map[string]any{{"key": "x", "widget_id": uuid.NewString()}}},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "binding")

	// Oversize js → 400 naming the cap; unknown types still rejected.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 16, "col": 0, "width": 4, "height": 4},
		"config": map[string]any{"js": strings.Repeat("x", 128<<10+1)},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "128 KiB")
}
```

Adjust details to the file's helpers at implementation time (the executor-level insert uses the test DB; `setupTestServer` exposes `DB`). Keep the assertions exactly as above.

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestDashboardCustomWidget -timeout 3m`
Expected: FAIL — `invalid widget type`.

**Step 3: Implement**

In `handleAddWidget`:

```go
	switch req.Type {
	case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric, models.WidgetHTML, models.WidgetCustom:
	default:
		writeError(w, http.StatusBadRequest, "invalid widget type")
		return
	}
	if req.Type == models.WidgetHTML {
		// (unchanged wave-3 block)
	}
	if req.Type == models.WidgetCustom {
		if err := validate.ValidateWidgetCustom(validate.WidgetCustomConfigFrom(req.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateCustomBindingRefs(ctx, claims.OrgID, dashID, req.Config); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
```

In `handleUpdateWidget`'s type switch add `models.WidgetCustom`; after the existing caps blocks add:

```go
	if req.Config != nil {
		// The custom block mirrors the html caps check: validate whatever the
		// request carries, and bindings against the *merged* config below
		// (a partial patch can introduce or keep bindings).
		if err := validate.ValidateWidgetCustom(validate.WidgetCustomConfigFrom(req.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
```

and, where the merged widget is assembled (`merged := existing` block), after `if req.Config != nil { merged.Config = req.Config }` add:

```go
	if merged.Type == string(models.WidgetCustom) {
		if err := validate.ValidateWidgetCustom(validate.WidgetCustomConfigFrom(merged.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateCustomBindingRefs(ctx, claims.OrgID, dashID, merged.Config); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
```

New helper in `dashboard_handlers.go` (next to `widgetHTMLConfigFrom`):

```go
// validateCustomBindingRefs rejects a custom widget whose bindings do not name
// widgets of this dashboard: the renderer would otherwise show permanent
// error states, and a cross-dashboard id would be a data-confusion bug (the
// execute pipeline would refuse it anyway — this fails at write time).
func (s *Server) validateCustomBindingRefs(ctx context.Context, orgID, dashID string, config map[string]interface{}) error {
	ids := validate.WidgetCustomBindingIDs(config)
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id FROM widgets WHERE dashboard_id = $1 AND id = ANY($2)`,
		dashID, ids)
	if err != nil {
		return fmt.Errorf("check binding references: %w", err)
	}
	defer rows.Close()
	found := make(map[string]bool, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("check binding references: %w", err)
		}
		found[id] = true
	}
	for _, id := range ids {
		if !found[id] {
			return fmt.Errorf("binding widget %s is not a widget of this dashboard", id)
		}
	}
	return nil
}
```

(`orgID` stays in the signature for the follow-up org-scoping of the query; the dashboard id already scopes it. Keep the parameter to match the tool-side mirror, or drop it — pick the compile-clean variant.)

Audit metadata: in `handleAddWidget` and `handleUpdateWidget`, where `s.audit.Log(...)` runs with `Action: "widget.create"`/`"widget.update"`, switch to `s.audit.Log(ctx, audit.Entry{...})` with `Metadata` when the type is custom:

```go
	entry := audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "widget.create", ResourceType: "widget", ResourceID: widget.ID,
	}
	if widget.Type == models.WidgetCustom {
		entry.Metadata = map[string]any{"type": "custom", "js_bytes": len(validate.WidgetCustomConfigFrom(widget.Config).JS)}
	}
	s.audit.Log(ctx, entry)
```

(`audit.Entry.Metadata` already exists — used by `AuditLogWithMetadata` on the agent side.)

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestDashboardCustomWidget -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/dashboard_handlers.go internal/api/dashboard_handlers_test.go
git commit -m "feat(api): accept custom widgets with validated bindings"
```

---

### Task 4: Relay store validator rejects dangling custom bindings

**Files:**
- Modify: `internal/api/dashboard_doc_service.go` (`validateDashboardDocStore`)
- Modify: `internal/api/dashboard_doc_writes_test.go` (append)

**Step 1: Write the failing test**

Append to `internal/api/dashboard_doc_writes_test.go` (the file already drives the internal PUT with a real dashboard doc; mirror its helpers):

```go
// TestDashboardDocStoreRejectsDanglingCustomBinding pins the merged-projection
// binding check: a browser document edit cannot introduce a custom widget
// whose binding names a widget outside the document.
func TestDashboardDocStoreRejectsDanglingCustomBinding(t *testing.T) {
	// ... existing fixture helpers build a dashboard with one widget and an
	// editor actor with an internal token.
	state := storedDashboardDocState(t, srv, dashID)
	// Inject a custom widget binding to a random UUID via the document store
	// (the same path a crafted relay store takes).
	injected, err := dashboarddoc.UpsertWidget(state, dashboarddoc.WidgetDoc{
		ID: uuid.NewString(), Type: "custom",
		Layout: dashboarddoc.Layout{Row: 0, Col: 0, Width: 4, Height: 4},
		Config: map[string]any{"bindings": []any{
			map[string]any{"key": "x", "widget_id": uuid.NewString()},
		}},
	})
	require.NoError(t, err)
	status := putInternalDashboardYjs(t, srv, dashID, token, injected)
	require.Equal(t, 403, status) // refused by the validator
}
```

Adapt the helper names to the file's existing `putInternalDashboardYjs`-style helper (it exists per the wave-3 notes: "the file already drives the internal PUT", around line 693/809 driving doc stores).

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestDashboardDocStoreRejectsDanglingCustomBinding -timeout 3m`
Expected: FAIL — the store is accepted (204).

**Step 3: Implement**

In `validateDashboardDocStore`, after the widget-ref notebook-view loop, add the dashboard-local binding check on the **merged** projection:

```go
	// Custom components may only bind widgets of their own dashboard: the
	// merged projection is the authority (a delta update can add a binding
	// whose target lives in an earlier struct). A dangling binding renders as
	// a permanent error and would be a data-confusion vector, so the store is
	// refused like a bad cell reference.
	for id, w := range mergedProj.Widgets {
		if w.Type != string(models.WidgetCustom) {
			continue
		}
		for _, bindingID := range validate.WidgetCustomBindingIDs(w.Config) {
			if _, ok := mergedProj.Widgets[bindingID]; !ok {
				return fmt.Errorf("%w: custom widget %s binds widget %s outside the dashboard", dashboarddoc.ErrStoreForbidden, id, bindingID)
			}
		}
	}
```

(`ErrStoreForbidden` maps to the internal PUT's 403, same as the notebook-view denial.) Add `"github.com/the-heaven-labs/aether/internal/validate"` and `"github.com/the-heaven-labs/aether/internal/models"` imports if missing.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/dashboard_doc_service.go internal/api/dashboard_doc_writes_test.go
git commit -m "feat(api): refuse dangling custom bindings in document stores"
```

---

### Task 5: Org switch — `custom_code_enabled` (default off), response fields, enforcement

**Files:**
- Modify: `internal/models/org.go` (`OrgSettings`)
- Modify: `internal/api/org_handlers.go` (`orgCustomCodeEnabled` helper)
- Modify: `internal/api/dashboard_handlers.go` (response structs + rejection in add/update)
- Modify: `internal/api/dashboard_handlers_test.go` (append `TestOrgCustomCodeSwitch`)

**Step 1: Write the failing test**

Append to `internal/api/dashboard_handlers_test.go` (reuse `doDashboardJSON`):

```go
func TestOrgCustomCodeSwitch(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-custom-switch-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Custom Switch Org")

	rec := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards", map[string]any{"title": "Switch"})
	require.Equal(t, 201, rec.Code)
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID := dashResp["id"].(string)

	// Default: absent means DISABLED — unlike html panels.
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"custom_code_enabled":false`)
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "disabled")

	// Enabling round-trips and flips the GET + public GET.
	require.Equal(t, 200, doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings",
		map[string]any{"custom_code_enabled": true}).Code)
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Contains(t, rec.Body.String(), `"custom_code_enabled":true`)
	share := doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/share", nil)
	var shareResp map[string]any
	require.NoError(t, json.NewDecoder(share.Body).Decode(&shareResp))
	rec = doDashboardJSON(t, srv, token, "GET", "/api/v1/public/"+shareResp["token"].(string), nil)
	require.Contains(t, rec.Body.String(), `"custom_code_enabled":true`)

	// Creating works now; writes to custom config keys are rejected again
	// once disabled (fail closed) while the widget keeps its config.
	rec = doDashboardJSON(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/widgets", map[string]any{
		"type": "custom", "layout": map[string]int{"row": 0, "col": 0, "width": 4, "height": 4},
	})
	require.Equal(t, 201, rec.Code)
	require.Equal(t, 200, doDashboardJSON(t, srv, token, "PUT", "/api/v1/org/settings",
		map[string]any{"custom_code_enabled": false}).Code)
	rec = doDashboardJSON(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID+"/widgets", nil)
	_ = rec // widget-update path exercised by the wave-3-style assertions below
}
```

(At implementation time replace the trailing placeholder with the same update-path assertions wave 3 used: a `PUT` carrying `{"type":"custom"}` and a `PUT` carrying `{"config":{"js":"x"}}` must both 400 with "disabled".)

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/api/ -run TestOrgCustomCodeSwitch -timeout 3m`
Expected: FAIL — no such field.

**Step 3: Implement**

`internal/models/org.go` — extend `OrgSettings`:

```go
	// CustomCodeEnabled gates the `custom` widget type org-wide. nil (absent)
	// means DISABLED (unlike HTMLPanelsEnabled): arbitrary code is opt-in.
	CustomCodeEnabled *bool `json:"custom_code_enabled,omitempty"`
```

`internal/api/org_handlers.go` (next to `orgHTMLPanelsEnabled`):

```go
// orgCustomCodeEnabled reports whether custom components are enabled for the
// org. Absent settings mean DISABLED (arbitrary code is opt-in); a read
// failure degrades to disabled so a transient DB error never enables code.
func (s *Server) orgCustomCodeEnabled(ctx context.Context, orgID string) bool {
	var settingsJSON []byte
	if err := s.db.Pool.QueryRow(ctx, `SELECT settings FROM orgs WHERE id = $1`, orgID).Scan(&settingsJSON); err != nil {
		slog.Debug("org settings read failed", "org_id", orgID, "error", err)
		return false
	}
	var settings models.OrgSettings
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return false
	}
	return settings.CustomCodeEnabled != nil && *settings.CustomCodeEnabled
}
```

`internal/api/dashboard_handlers.go` — add `CustomCodeEnabled bool \`json:"custom_code_enabled"\`` to both response structs, populated with `s.orgCustomCodeEnabled(ctx, dash.OrgID)`. In `handleAddWidget`, after the html gate:

```go
	if req.Type == models.WidgetCustom && !s.orgCustomCodeEnabled(ctx, claims.OrgID) {
		writeError(w, http.StatusBadRequest, "custom code is disabled for this organization")
		return
	}
```

In `handleUpdateWidget`: when `req.Type != nil && *req.Type == models.WidgetCustom` **or** the raw config carries any of `html`/`css`/`js`/`bindings` keys, run the same gate (fail-closed authoring, mirroring the html `hasHTMLKey || hasCSSKey` pattern; use string/array presence checks).

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/api/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/models/org.go internal/api/org_handlers.go internal/api/dashboard_handlers.go internal/api/dashboard_handlers_test.go
git commit -m "feat(api): org-level custom code switch"
```

---

### Task 6: Agent tooling gates + `create_custom_widget`

**Files:**
- Modify: `internal/agent/tools_manage.go` (`validDashboardWidgetType`, org helper, `create_custom_widget`, gates in existing handlers, schemas)
- Modify: `internal/agent/tools_seed.go` (`builtinSeedRev = 3` + entries)
- Modify: `internal/agent/tools_manage_test.go` (append `TestWidgetCustomTooling`)

**Step 1: Write the failing test**

Append to `internal/agent/tools_manage_test.go` (mirror `TestWidgetHTMLTooling`'s setup and helpers):

```go
func TestWidgetCustomTooling(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Custom Tooling")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	create := dashboardTool(t, reg, "create_custom_widget")

	// Disabled by default: the tool refuses before any write.
	args, err := json.Marshal(map[string]any{"dashboard_id": dashID, "js": "document.title='x'"})
	require.NoError(t, err)
	_, err = create(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")

	// Enable and seed a binding target via the query widget tool.
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE orgs SET settings = COALESCE(settings,'{}'::jsonb) || '{"custom_code_enabled":true}'::jsonb WHERE id=$1`, orgID)
	require.NoError(t, err)
	connID := uuid.NewString()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		VALUES ($1, $2, 'test-connector', 'postgres', $3)`, connID, orgID, []byte("encrypted"))
	require.NoError(t, err)
	qargs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID, "connector_id": connID, "query": "SELECT 1 AS x",
		"type": "table", "row": 0, "col": 0, "width": 4, "height": 4,
	})
	require.NoError(t, err)
	out, err := dashboardTool(t, reg, "create_dashboard_widget")(qargs, ctx)
	require.NoError(t, err)
	targetID := out.(map[string]any)["widget_id"].(string)

	// The tool writes the bundle + binding into the document.
	args, err = json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"html":         `<div id="root"></div>`,
		"css":          `#root{color:var(--accent)}`,
		"js":           `document.getElementById('root').textContent='ok'`,
		"bindings":     []map[string]any{{"key": "sales", "widget_id": targetID}},
		"row":          4, "col": 0, "width": 6, "height": 6,
	})
	require.NoError(t, err)
	out, err = create(args, ctx)
	require.NoError(t, err)
	widgetID := out.(map[string]any)["widget_id"].(string)

	proj := storedDashboardDoc(t, db.Pool, dashID)
	w, ok := proj.Widgets[widgetID]
	require.True(t, ok)
	require.Equal(t, "custom", w.Type)
	require.Equal(t, `<div id="root"></div>`, w.Config["html"])
	bindings, ok := w.Config["bindings"].([]any)
	require.True(t, ok)
	require.Len(t, bindings, 1)

	// A binding outside the dashboard is refused.
	args, err = json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"bindings":     []map[string]any{{"key": "x", "widget_id": uuid.NewString()}},
	})
	require.NoError(t, err)
	_, err = create(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "binding")

	// Oversize js is refused before any write.
	args, err = json.Marshal(map[string]any{"dashboard_id": dashID, "js": strings.Repeat("x", 128<<10+1)})
	require.NoError(t, err)
	_, err = create(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "128 KiB")

	// update_dashboard_widget converts to custom when enabled.
	args, err = json.Marshal(map[string]any{"dashboard_id": dashID, "widget_id": targetID, "type": "custom"})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "update_dashboard_widget")(args, ctx)
	require.NoError(t, err)
	proj = storedDashboardDoc(t, db.Pool, dashID)
	require.Equal(t, "custom", proj.Widgets[targetID].Type)

	// Disabling again fails set_widget_config's custom block closed.
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE orgs SET settings = settings || '{"custom_code_enabled":false}'::jsonb WHERE id=$1`, orgID)
	require.NoError(t, err)
	args, err = json.Marshal(map[string]any{
		"dashboard_id": dashID, "widget_id": widgetID,
		"custom": map[string]any{"js": "document.title='y'"},
	})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "set_widget_config")(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")
}
```

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/agent/ -run TestWidgetCustomTooling -timeout 3m`
Expected: FAIL — `create_custom_widget` not registered.

**Step 3: Implement**

- `validDashboardWidgetType`: add `models.WidgetCustom`.
- Mirror helper `orgCustomCodeEnabled(ctx.Context, ctx.DB, ctx.OrgID)` (same body as the API helper, absent = false).
- New tool in `RegisterManageTools` (next to `create_dashboard_widget`):

```go
	reg.Register(&ToolDef{
		Function: struct{...}{
			Name:        "create_custom_widget",
			Description: "Add a sandboxed custom component (HTML/CSS/JS + data bindings) to a dashboard, behind the org's custom-code switch. Source-less; bind existing dashboard widgets by id to feed it data through the bridge.",
			Parameters:  `{"type":"object","properties":{"dashboard_id":{"type":"string"},"html":{"type":"string"},"css":{"type":"string"},"js":{"type":"string"},"bindings":{"type":"array","items":{"type":"object","properties":{"key":{"type":"string"},"widget_id":{"type":"string"}},"required":["key","widget_id"]}},"row":{"type":"number"},"col":{"type":"number"},"width":{"type":"number"},"height":{"type":"number"}},"required":["dashboard_id"]}`,
		},
		Handler: makeCreateCustomWidgetHandler(pool),
		Timeout: 15 * time.Second,
	})
```

- `makeCreateCustomWidgetHandler(pool)` mirrors `makeCreateDashboardWidgetHandler`'s doc-store write path: permission check → org gate (`custom code is disabled for this organization`) → `validate.ValidateWidgetCustom` → binding refs exist in the dashboard (query `widgets WHERE dashboard_id=$1 AND id=ANY($2)`) → layout validation → `UpsertWidget` with `Type: "custom"`, `Config: validate.ToStoredWidgetCustom(...)` merged over any raw config → store → audit `widget.create` with `metadata {type:'custom', js_bytes}` → return `{widget_id}`.
- `update_dashboard_widget`: enum/schema description gains `custom`; when converting to custom, org gate.
- `set_widget_config`: add `Custom *validate.WidgetCustomConfig \`json:"custom"\``; validate; org gate when present; merge `ToStoredWidgetCustom` over the projected config; extend the "nothing to update" check, the audited `keys` list, and require the projected type to be `custom` ("set the widget type to custom first").
- `tools_seed.go`: `const builtinSeedRev = 3`; add the `create_custom_widget` seed entry; update the three description strings touched (`create_dashboard_widget` type enum mention, `update_dashboard_widget`, `set_widget_config`).

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/agent/ -timeout 3m`
Expected: PASS (including the seed/registry drift test).

**Step 5: Commit**

```bash
git add internal/agent/tools_manage.go internal/agent/tools_seed.go internal/agent/tools_manage_test.go
git commit -m "feat(agent): author custom components behind the org switch"
```

---

## Phase 2 — Sandboxed renderer & data bridge

### Task 7: `SandboxedFrame` — protocol, CSP, postMessage plumbing

**Files:**
- Create: `web/src/components/sandboxedFrame.ts` (pure builders + protocol types)
- Create: `web/src/components/sandboxedFrame.test.ts`
- Create: `web/src/components/SandboxedFrame.tsx`

**Step 1: Write the failing tests**

Create `web/src/components/sandboxedFrame.test.ts`:

```ts
import { describe, expect, test } from 'vitest'
import { buildCustomSrcDoc, parseHostMessage, parseFrameMessage } from './sandboxedFrame'

const ORIGIN = 'https://app.example.com'

describe('buildCustomSrcDoc', () => {
  test('composes CSP (no network), theme tokens, css, bootstrap, and the author script', () => {
    const doc = buildCustomSrcDoc({
      html: '<div id="root"></div>',
      css: '#root{color:var(--accent)}',
      js: "document.getElementById('root').textContent='hi'",
      tokens: { '--accent': '#7c5cff' },
      scheme: 'dark',
      origin: ORIGIN,
    })
    expect(doc).toContain('default-src \'none\'')
    expect(doc).toContain("connect-src 'none'")
    expect(doc).toContain("script-src 'unsafe-inline'")
    expect(doc).toContain(`img-src data: ${ORIGIN}`)
    expect(doc).toContain('--accent:#7c5cff')
    expect(doc).toContain("textContent='hi'")
  })

  test('escapes a </script> breakout in js and keeps css style-safe', () => {
    const doc = buildCustomSrcDoc({
      html: '', css: '</style><script>x</script>',
      js: "const s = '</script><script>alert(1)</script>'",
      tokens: {}, scheme: 'light', origin: ORIGIN,
    })
    expect(doc).not.toContain('</script><script>alert(1)</script>')
  })
})

describe('protocol parsers', () => {
  test('frame messages accept only the known shapes', () => {
    expect(parseFrameMessage({ type: 'ready' })).toEqual({ type: 'ready' })
    expect(parseFrameMessage({ type: 'request_data', key: 'sales' })).toEqual({ type: 'request_data', key: 'sales' })
    expect(parseFrameMessage({ type: 'open_link', href: 'https://x.test' })).toEqual({ type: 'open_link', href: 'https://x.test' })
    expect(parseFrameMessage({ type: 'request_data' })).toBeNull()
    expect(parseFrameMessage({ type: 'nope' })).toBeNull()
    expect(parseFrameMessage('junk')).toBeNull()
  })

  test('host messages tolerate junk payloads from the page', () => {
    expect(parseHostMessage({ type: 'init', theme: {}, variables: {}, bindings: [] })).not.toBeNull()
    expect(parseHostMessage({ type: 'data', key: 'sales', result: { ok: true } })).not.toBeNull()
    expect(parseHostMessage({ type: 'error', message: 'x' })).not.toBeNull()
    expect(parseHostMessage({ type: 'data', key: 7 })).toBeNull()
    expect(parseHostMessage(null)).toBeNull()
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/sandboxedFrame.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement**

`web/src/components/sandboxedFrame.ts` — pure module (no React):

```ts
import type { AppColorScheme } from '../utils/appTheme'

/** Child → host frames. Only these shapes are accepted. */
export type FrameMessage =
  | { type: 'ready' }
  | { type: 'request_data'; key: string }
  | { type: 'open_link'; href: string }

/** Host → child frames. */
export type HostMessage =
  | { type: 'init'; theme: Record<string, unknown>; variables: Record<string, unknown>; bindings: Array<{ key: string; status: 'ready' | 'unavailable' }> }
  | { type: 'data'; key: string; result: unknown }
  | { type: 'error'; key?: string; message: string }

export function parseFrameMessage(data: unknown): FrameMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  if (m.type === 'ready') return { type: 'ready' }
  if (m.type === 'request_data' && typeof m.key === 'string' && m.key.length > 0) return { type: 'request_data', key: m.key }
  if (m.type === 'open_link' && typeof m.href === 'string' && m.href.length > 0) return { type: 'open_link', href: m.href }
  return null
}

export function parseHostMessage(data: unknown): HostMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  if (m.type === 'init' && typeof m.theme === 'object' && typeof m.variables === 'object' && Array.isArray(m.bindings)) {
    return m as unknown as HostMessage
  }
  if (m.type === 'data' && typeof m.key === 'string') return m as unknown as HostMessage
  if (m.type === 'error' && typeof m.message === 'string') return m as unknown as HostMessage
  return null
}

/** The host's answer to `open_link`: http(s) only, always with noopener. */
export function safeOpenLink(href: string, open: (url: string) => void): boolean {
  let url: URL
  try {
    url = new URL(href, window.location.origin)
  } catch {
    return false
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return false
  open(url.toString())
  return true
}

/** Neutralizes the sequence that can close the injected script element. */
function safeScript(js: string): string {
  return js.replace(/<\/(script)/gi, '<\\/$1')
}

/** Neutralizes the sequence that can close the injected style element. */
function safeStyle(css: string): string {
  return css.replace(/<\/(style)/gi, '<\\/$1')
}

/**
 * Composes the sandbox document: strict CSP with `connect-src 'none'` (the
 * component cannot fetch anything), inline author script/style only, the
 * resolved theme tokens as :root variables, and a small bootstrap that
 * forwards child→host frames and applies host→child data events as
 * `window.__aether` CustomEvents the author script can subscribe to.
 */
export function buildCustomSrcDoc({ html, css, js, tokens, scheme, origin }: {
  html: string
  css: string
  js: string
  tokens: Record<string, string>
  scheme: AppColorScheme
  origin: string
}): string {
  const csp = [
    "default-src 'none'",
    "script-src 'unsafe-inline'",
    "style-src 'unsafe-inline'",
    `img-src data: ${origin}`,
    `font-src data: ${origin}`,
    "connect-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ')
  const tokenCss = Object.entries(tokens).map(([k, v]) => `${k}:${v}`).join(';')
  const canvas = scheme === 'dark' ? '#141414' : '#ffffff'
  const text = scheme === 'dark' ? '#e8e8e8' : '#111111'
  const bootstrap = `
(function(){
  var post = function(m){ try { parent.postMessage(m, '*') } catch (e) {} };
  window.addEventListener('message', function(ev){
    if (ev.data && ev.data.type) window.dispatchEvent(new CustomEvent('aether:' + ev.data.type, { detail: ev.data }));
  });
  window.aether = {
    ready: function(){ post({type:'ready'}) },
    requestData: function(key){ post({type:'request_data', key: String(key)}) },
    openLink: function(href){ post({type:'open_link', href: String(href)}) },
  };
  post({type:'ready'});
})();`
  const scrollbars = [
    "*{scrollbar-width:thin;scrollbar-color:#8888 transparent;scrollbar-color:color-mix(in srgb,var(--text-primary,#888) 25%,transparent) transparent}",
    "*::-webkit-scrollbar{width:7px;height:7px}",
    "*::-webkit-scrollbar-track{background:transparent}",
    "*::-webkit-scrollbar-thumb{background:#8888;background:color-mix(in srgb,var(--text-primary,#888) 25%,transparent);border-radius:4px}",
  ].join('')
  return [
    '<!doctype html><html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    `<style>:root{color-scheme:${scheme};${tokenCss}}html,body{margin:0;padding:0;background:var(--bg-card,${canvas});color:var(--text-primary,${text});font-family:var(--app-font-family,'DM Sans',system-ui,sans-serif)}${scrollbars}</style>`,
    `<style>${safeStyle(css)}</style>`,
    `</head><body>${html}<script>${bootstrap}${safeScript(js)}</script></body></html>`,
  ].join('')
}
```

(`sandboxedFrame.ts` imports `AppColorScheme` from `../utils/appTheme` — same pattern as `HtmlWidget`.)

`web/src/components/SandboxedFrame.tsx`:

```tsx
import { useEffect, useMemo, useRef, useState } from 'react'
import { buildCustomSrcDoc, parseFrameMessage, safeOpenLink } from './sandboxedFrame'
import type { FrameMessage, HostMessage } from './sandboxedFrame'
import { useResolvedAppTheme } from './AppThemeProvider'
import type { DashboardAppTheme, OrgBranding } from '../types'

interface SandboxedFrameProps {
  html: string
  css: string
  js: string
  /** Called with every accepted child frame (host decides the answer). */
  onMessage: (message: FrameMessage, reply: (message: HostMessage) => void) => void
  theme?: DashboardAppTheme | null
  branding?: OrgBranding | null
  title?: string
}

/**
 * The opaque-origin sandbox for custom components: `sandbox="allow-scripts"`
 * with no `allow-same-origin` (no cookies, storage, or authenticated fetch),
 * a CSP that blocks every outbound request, and a postMessage bridge whose
 * frames are validated against this iframe's contentWindow before dispatch.
 * The child's bootstrap posts `ready`; the host answers it with `init` through
 * the same `onMessage` callback (one flow, no load-event ordering races).
 */
export function SandboxedFrame({ html, css, js, onMessage, theme, branding, title = 'Custom component' }: SandboxedFrameProps) {
  const ref = useRef<HTMLIFrameElement | null>(null)
  const resolved = useResolvedAppTheme(theme, branding)
  const srcDoc = useMemo(
    () => buildCustomSrcDoc({ html, css, js, tokens: resolved.vars, scheme: resolved.scheme, origin: window.location.origin }),
    [html, css, js, resolved],
  )

  useEffect(() => {
    const handler = (event: MessageEvent) => {
      const frame = ref.current
      if (!frame || event.source !== frame.contentWindow) return
      // Opaque origins serialize to "null"; anything else is not our frame.
      if (event.origin !== 'null' && event.origin !== '') return
      const message = parseFrameMessage(event.data)
      if (!message) return
      const reply = (out: HostMessage) => {
        try { frame.contentWindow?.postMessage(out, '*') } catch { /* frame gone */ }
      }
      if (message.type === 'open_link') {
        safeOpenLink(message.href, (url) => window.open(url, '_blank', 'noopener,noreferrer'))
        return
      }
      onMessage(message, reply)
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [onMessage])

  return (
    <iframe
      ref={ref}
      title={title}
      sandbox="allow-scripts"
      referrerPolicy="no-referrer"
      srcDoc={srcDoc}
      style={{ width: '100%', height: '100%', border: 'none', display: 'block' }}
    />
  )
}
```

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/components/sandboxedFrame.test.ts && npx tsc --noEmit`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add web/src/components/sandboxedFrame.ts web/src/components/sandboxedFrame.test.ts web/src/components/SandboxedFrame.tsx
git commit -m "feat(web): sandboxed frame and custom-component protocol"
```

---

### Task 8: `CustomWidget` + binding data resolution

**Files:**
- Create: `web/src/utils/customConfig.ts` (+ `customConfig.test.ts`) — `customConfigFrom` mirroring `htmlConfigFrom`
- Create: `web/src/components/CustomWidget.tsx` (+ `CustomWidget.test.tsx`)
- Create: `web/src/components/CustomBindingQuery.tsx` (per-binding fetch child)

**Step 1: Write the failing tests**

`web/src/utils/customConfig.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { customConfigFrom } from './customConfig'

describe('customConfigFrom', () => {
  it('reads the bundle and drops malformed bindings', () => {
    expect(customConfigFrom({ html: '<p>x</p>', css: 'p{}', js: '1', bindings: [
      { key: 'sales', widget_id: 'w1' }, { key: 'bad' }, 'junk',
    ] })).toEqual({ html: '<p>x</p>', css: 'p{}', js: '1', bindings: [{ key: 'sales', widget_id: 'w1' }] })
  })
  it('coerces absent values', () => {
    expect(customConfigFrom(undefined)).toEqual({ html: '', css: '', js: '', bindings: [] })
  })
})
```

`web/src/components/CustomWidget.test.tsx` (mirror `HtmlWidget.test.tsx`'s harness; mock the frame component where convenient or drive real DOM):

```ts
describe('CustomWidget', () => {
  test('disabled renders the org placeholder', () => {
    render(<CustomWidget html="<p>x</p>" css="" js="" bindings={[]} enabled={false} />)
    expect(screen.getByText('Custom code is disabled for this organization.')).toBeInTheDocument()
  })
  test('empty renders the authoring hint', () => {
    render(<CustomWidget html="" css="" js="" bindings={[]} />)
    expect(screen.getByText(/Empty custom component/)).toBeInTheDocument()
  })
  test('renders the sandboxed frame when authored', () => {
    render(<CustomWidget html="<p>x</p>" css="" js="1" bindings={[]} />)
    expect(screen.getByTitle('Custom component')).toHaveAttribute('sandbox', 'allow-scripts')
  })
  test('answers request_data for a binding it cannot fetch with an error frame', async () => {
    // Drive the frame's postMessage with a fake event whose source is the iframe
    // contentWindow; assert an error reply is posted. (Use a mocked
    // postMessage spy on the iframe's contentWindow.)
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/utils/customConfig.test.ts src/components/CustomWidget.test.tsx`
Expected: FAIL — modules not found.

**Step 3: Implement**

`web/src/utils/customConfig.ts` — the binding-normalizing reader used by the renderer and the drawer:

```ts
export interface CustomBinding { key: string; widget_id: string }

/** Reads a custom component's bundle; malformed bindings entries are dropped. */
export function customConfigFrom(config: Record<string, unknown> | undefined): {
  html: string; css: string; js: string; bindings: CustomBinding[]
} {
  const html = typeof config?.html === 'string' ? config.html : ''
  const css = typeof config?.css === 'string' ? config.css : ''
  const js = typeof config?.js === 'string' ? config.js : ''
  const bindings: CustomBinding[] = []
  if (Array.isArray(config?.bindings)) {
    for (const entry of config.bindings) {
      if (!entry || typeof entry !== 'object') continue
      const key = (entry as Record<string, unknown>).key
      const widgetID = (entry as Record<string, unknown>).widget_id
      if (typeof key === 'string' && key && typeof widgetID === 'string' && widgetID) {
        bindings.push({ key, widget_id: widgetID })
      }
    }
  }
  return { html, css, js, bindings }
}
```

`web/src/components/CustomBindingQuery.tsx` — one bound widget's data source. It resolves as the surface dictates (props passed down):

```tsx
import { useEffect } from 'react'
import { useWidgetQuery } from '../hooks/useWidgetQuery'
import type { Widget } from '../types'

export interface BindingResult { key: string; status: 'ready' | 'unavailable'; result?: unknown; message?: string }

/**
 * Resolves one custom-component binding for the host bridge:
 *  - query widgets execute through the dashboard execute pipeline (draft,
 *    published, or the public endpoint — the caller passes the base);
 *  - cell widgets resolve their stored outputs (never executed here);
 *  - anything unresolved reports `unavailable`, which the host forwards as an
 *    error frame (the component shows its own empty state).
 */
export function CustomBindingQuery({ bindingKey, widget, values, viewerConnectorId, endpointBase, published, cellOutputs, onResult }: {
  bindingKey: string
  widget: Widget | undefined
  values: Record<string, unknown>
  viewerConnectorId?: string | null
  endpointBase?: string
  published?: boolean
  cellOutputs?: unknown[] | null
  onResult: (result: BindingResult) => void
}) {
  const isQuery = !!widget?.connector_id && !!widget?.query
  const query = useWidgetQuery({
    dashboardId: '',
    widget: widget ?? ({ id: 'missing' } as Widget),
    values,
    enabled: isQuery && !!endpointBase,
    viewerConnectorId,
    endpointBase,
    published,
  })

  useEffect(() => {
    if (!widget) {
      onResult({ key: bindingKey, status: 'unavailable', message: `binding "${bindingKey}" points at a missing widget` })
      return
    }
    if (isQuery) {
      if (query.isLoading) return
      if (query.error) {
        onResult({ key: bindingKey, status: 'unavailable', message: query.error instanceof Error ? query.error.message : 'query failed' })
        return
      }
      if (query.data) onResult({ key: bindingKey, status: 'ready', result: query.data })
      return
    }
    onResult({ key: bindingKey, status: 'ready', result: { outputs: cellOutputs ?? [] } })
  }, [bindingKey, widget, isQuery, query.isLoading, query.error, query.data, cellOutputs, onResult])

  return null
}
```

(`dashboardId` stays unused by the hook — the endpoint base already names the dashboard; verify the hook signature and pass what it needs at implementation time. If `useWidgetQuery` requires a non-empty `dashboardId` for the default endpoint base, pass the real id from the parent and keep `endpointBase` for the public case.)

`web/src/components/CustomWidget.tsx`:

```tsx
import { useCallback, useMemo, useRef, useState } from 'react'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { customConfigFrom } from '../utils/customConfig'
import type { CustomBinding } from '../utils/customConfig'
import { SandboxedFrame } from './SandboxedFrame'
import { CustomBindingQuery } from './CustomBindingQuery'
import type { BindingResult } from './CustomBindingQuery'
import type { DashboardAppTheme, OrgBranding, Widget } from '../types'

interface CustomWidgetProps {
  html: string
  css: string
  js: string
  bindings: CustomBinding[]
  /** False when the org switch is off — renders a placeholder instead. */
  enabled?: boolean
  /** Every widget of the dashboard, for binding resolution. */
  widgets: Widget[]
  /** Values override for the public page (defaults when not live). */
  values?: Record<string, unknown>
  /** Execute-endpoint base; the public page passes `/api/v1/public/{token}`. */
  endpointBase?: string
  published?: boolean
  viewerConnectorId?: string | null
  /** Stored outputs per cell id, when the surface inlines them. */
  widgetsData?: Record<string, { outputs?: unknown[] | null } | undefined>
  /** False on public pages that are not live: data requests answer errors. */
  dataEnabled?: boolean
  theme?: DashboardAppTheme | null
  branding?: OrgBranding | null
}

/**
 * A custom component: the author's bundle in an allow-scripts opaque-origin
 * sandbox, fed through the validated postMessage bridge. Bindings resolve as
 * the viewer (query widgets through the execute pipeline, cell widgets from
 * stored outputs); a `request_data` is answered only when the viewer may see
 * the data — never with credentials or tokens.
 */
export function CustomWidget({ html, css, js, bindings, enabled = true, widgets, values, endpointBase, published, viewerConnectorId, widgetsData, dataEnabled = true, theme, branding }: CustomWidgetProps) {
  const context = useDashboardVariables()
  const effectiveValues = values ?? context.values
  const [results, setResults] = useState<Record<string, BindingResult>>({})
  const resultsRef = useRef(results)
  resultsRef.current = results

  const onResult = useCallback((result: BindingResult) => {
    setResults((prev) => (prev[result.key] === result ? prev : { ...prev, [result.key]: result }))
  }, [])

  const widgetsByID = useMemo(() => {
    const map = new Map<string, Widget>()
    for (const w of widgets) map.set(w.id, w)
    return map
  }, [widgets])

  const handleMessage = useCallback((message: { type: string; key?: string }, reply: (m: unknown) => void) => {
    if (message.type === 'ready') {
      reply({
        type: 'init',
        theme: {},
        variables: effectiveValues,
        bindings: bindings.map((b) => ({
          key: b.key,
          status: resultsRef.current[b.key]?.status === 'ready' ? 'ready' : 'unavailable',
        })),
      })
      return
    }
    if (message.type !== 'request_data' || !message.key) return
    if (!dataEnabled) {
      reply({ type: 'error', key: message.key, message: 'Live data is not enabled for this page.' })
      return
    }
    const result = resultsRef.current[message.key]
    if (!result || result.status !== 'ready') {
      reply({ type: 'error', key: message.key, message: result?.message ?? 'Data is not ready yet.' })
      return
    }
    reply({ type: 'data', key: message.key, result: result.result })
  }, [bindings, dataEnabled, effectiveValues])

  if (!enabled) return <div style={styles.empty}>Custom code is disabled for this organization.</div>
  if (!html.trim() && !css.trim() && !js.trim()) {
    return <div style={styles.empty}>Empty custom component — edit it in the dashboard editor.</div>
  }
  return (
    <div style={{ width: '100%', height: '100%' }}>
      {bindings.map((binding) => (
        <CustomBindingQuery
          key={binding.key}
          bindingKey={binding.key}
          widget={widgetsByID.get(binding.widget_id)}
          values={effectiveValues}
          viewerConnectorId={viewerConnectorId}
          endpointBase={endpointBase}
          published={published}
          cellOutputs={widgetsData?.[widgetsByID.get(binding.widget_id)?.cell_id ?? '']?.outputs}
          onResult={onResult}
        />
      ))}
      <SandboxedFrame html={html} css={css} js={js} onMessage={handleMessage} theme={theme} branding={branding} />
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
}
```

Add the protocol-level test from Step 1's last case: render the component, grab the iframe, monkey-patch `contentWindow.postMessage` with a spy, dispatch a `MessageEvent` with `source: iframe.contentWindow`, and assert the error reply shape.

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/utils/customConfig.test.ts src/components/CustomWidget.test.tsx && npx tsc --noEmit`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add web/src/utils/customConfig.ts web/src/utils/customConfig.test.ts web/src/components/CustomWidget.tsx web/src/components/CustomWidget.test.tsx web/src/components/CustomBindingQuery.tsx
git commit -m "feat(web): custom component renderer with the validated data bridge"
```

---

### Task 9: Wire `custom` into the app, public, and editor surfaces (+ full custom page)

**Files:**
- Modify: `web/src/types/index.ts` (`Widget['type']`, `Dashboard.custom_code_enabled`)
- Modify: `web/src/utils/widgetKind.ts` (+ test) — `isCustomWidget`, badge label `'Component'`
- Modify: `web/src/components/dashboardCollabRuntime.ts` (`WIDGET_TYPES` + test)
- Modify: `web/src/pages/DashboardPage.tsx` (render branch + `layout: "custom"` full-bleed path)
- Modify: `web/src/pages/PublicDashboardPage.tsx` (render branch, values/variables/data gating)
- Modify: `web/src/pages/DashboardEditorPage.tsx` (both grid branches + "Data source" badge when layout custom)

**Step 1: Write the failing tests**

- `widgetKind.test.ts`: `isCustomWidget` true for `{type:'custom'}` with no source; `widgetKindLabel` returns `'Component'`.
- `dashboardCollabRuntime.test.ts`: projection keeps a `custom` widget (mirror the html case).
- `DashboardPage.test.tsx`: a custom widget in the dashboard payload renders an `iframe[title="Custom component"]`; `custom_code_enabled: false` renders the placeholder; and with `settings.app.layout: 'custom'` + a custom widget, the app renders the custom frame while a plain cell/table widget is absent from the app surface.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/utils/widgetKind.test.ts src/components/dashboardCollabRuntime.test.ts src/pages/DashboardPage.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `types/index.ts`: `'chart' | 'table' | 'text' | 'metric' | 'html' | 'custom'`; `Dashboard.custom_code_enabled?: boolean`.
- `widgetKind.ts`: `isCustomWidget(widget) { return widget.type === 'custom' && !widget.notebook_id && !widget.connector_id }`; label `'Component'` after `'Panel'`.
- `dashboardCollabRuntime.ts`: add `'custom'` to `WIDGET_TYPES`.
- `DashboardPage.tsx`: after the `isHTMLPanel` branch:
  ```tsx
  ) : isCustomWidget(widget) ? (
    <CustomWidget
      {...customConfigFrom(widget.config)}
      enabled={dashboard.custom_code_enabled === true}
      widgets={widgets}
      endpointBase={`/api/v1/dashboards/${id}`}
      published={published}
      viewerConnectorId={viewerConnectorId}
      widgetsData={dashboard.widgets_data}
      dataEnabled={canViewWithData}
      theme={app?.theme}
      branding={dashboard.org_branding}
    />
  ) : (
  ```
  Full custom page: compute `const customPage = layout === 'custom' ? widgets.find(isCustomWidget) : undefined`; when set, render only that widget full-bleed (skip the chapters/tabs path: one chapter containing only it, `WidgetFrame` with no card appearance), and keep all-widgets execution/reruns out of scope for the hidden ones (they render nothing). The fallback when no custom widget exists is today's grid.
- `PublicDashboardPage.tsx`: the same branch with `endpointBase={`/api/v1/public/${token}`}`, `values={publicLive ? undefined : defaults}`, `dataEnabled={publicLive}`, `widgetsData={widgetsData}`; `enabled={dashboard.custom_code_enabled === true}`.
- `DashboardEditorPage.tsx`: both grid branches add the CustomWidget branch (same props, `endpointBase={\`/api/v1/dashboards/${id!}\`}`, `dataEnabled={dashboard.can_view_with_data !== false}`, `cancel` from grid drag already excludes iframes? add `iframe` to the drag-cancel selector list so the component stays interactive: `dragConfig={{ ..., cancel: 'button, a, input, select, textarea, canvas, iframe, .react-resizable-handle' }}`); render a muted `Data source` chip on non-custom widgets when `liveDashboard.settings?.app?.layout === 'custom'`.

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/utils/widgetKind.test.ts src/components/dashboardCollabRuntime.test.ts src/pages/DashboardPage.test.tsx && npx tsc --noEmit`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add web/src/types/index.ts web/src/utils/widgetKind.ts web/src/utils/widgetKind.test.ts web/src/components/dashboardCollabRuntime.ts web/src/components/dashboardCollabRuntime.test.ts web/src/pages/DashboardPage.tsx web/src/pages/PublicDashboardPage.tsx web/src/pages/DashboardEditorPage.tsx web/src/pages/DashboardPage.test.tsx
git commit -m "feat(web): custom components on the app, public, and editor surfaces"
```

---

## Phase 3 — Authoring & admin

### Task 10: Drawer authoring — CodeMirror editors, binding picker, preview

**Files:**
- Modify: `web/package.json` (+ lockfile) — add `@codemirror/lang-html`, `@codemirror/lang-css`
- Create: `web/src/components/CodeMirrorField.tsx` (+ test) — debounced CodeMirror field mirroring `DebouncedTextInput`
- Modify: `web/src/components/WidgetConfigDrawer.tsx` (custom branch + type option + binding picker + preview)
- Modify: `web/src/components/WidgetConfigDrawer.test.tsx`
- Modify: `web/src/utils/addWidgetPayload.ts` (+ test) — `AddWidgetType` gains `'custom'`, `validTypesForSource(source, htmlEnabled, customEnabled)`
- Modify: `web/src/app-builder/fieldHelp.ts` + `fieldHelp.test.ts` (`custom.html`, `custom.css`, `custom.js`, `custom.bindings`)

**Step 1: Add the dependency**

```bash
cd web && npm install @codemirror/lang-html @codemirror/lang-css
```

**Step 2: Write the failing tests**

- `CodeMirrorField.test.tsx`: renders a CodeMirror editor with the given value; typing calls `onSave` after the debounce (~650 ms via fake timers); unmount flushes.
- `WidgetConfigDrawer.test.tsx`: for a source-less `custom` widget, the drawer renders the three editors + binding picker + a `SandboxedFrame` preview; `customCodeEnabled={false}` renders the disabled note; the type option "Custom component" only when enabled.
- `addWidgetPayload.test.ts`: `validTypesForSource('none', true, true)` includes `'custom'`; `('none', true, false)` does not; `buildAddWidgetPayload` for custom returns `{type:'custom', config:{}}`.
- `fieldHelp.test.ts`: add the four keys to `REQUIRED_FIELDS`.

**Step 3: Run to verify failure**

Run: `cd web && npx vitest run src/components/CodeMirrorField.test.tsx src/components/WidgetConfigDrawer.test.tsx src/utils/addWidgetPayload.test.ts src/app-builder/fieldHelp.test.ts`
Expected: FAIL.

**Step 4: Implement**

- `CodeMirrorField.tsx`: a small wrapper over `@codemirror/state`/`view` (same primitives as `SqlEditor`) with a `language: 'html' | 'css' | 'javascript'` prop that lazy-imports the grammar (`@codemirror/lang-html`/`@codemirror/lang-css`/`@codemirror/lang-javascript`), the repo's `sqlHighlight`-style theme (`oneDark`? follow `SqlEditor`'s theme exactly), `editable` prop, `minHeight`, and the same 600 ms debounce + unmount flush semantics as `DebouncedTextInput` (`aria-label` on the wrapper so tests target it).
- `WidgetConfigDrawer.tsx`: `htmlPanelsEnabled` gains a sibling `customCodeEnabled` prop (default false); derive `const custom = customConfigFrom(config)`; a source-less note for custom; the type `<option value="custom">Custom component</option>` only when `customCodeEnabled`; a `custom` editor section with `<CodeMirrorField label="Component HTML" .../>`, `Component CSS`, `Component JS`, a bindings list (each row: key input + a widget select from `dashboard.widgets` minus self, add/remove buttons; serializes to `saveWidgetConfig({ bindings })`), helper text ("The component runs sandboxed; it can only see data you bind here, through a bridge."), and a 240px preview `<SandboxedFrame html={custom.html} css={custom.css} js={custom.js} onMessage={noop} />`; disabled note when `!customCodeEnabled && widgetType === 'custom'`; `handleTypeChange` already PUTs the type.
- `addWidgetPayload.ts`: signature `validTypesForSource(source, htmlEnabled = true, customEnabled = false)`; `'none'` returns `['text', ...(htmlEnabled ? ['html'] : []), ...(customEnabled ? ['custom'] : [])]`; `buildAddWidgetPayload` returns `{type:'custom', layout, config:{}}` for custom.
- `fieldHelp.ts`:
  ```ts
  'custom.html': { label: 'Component HTML', hint: 'Markup for the sandboxed component; scripts live in Component JS.', example: '<div id="root"></div>' },
  'custom.css': { label: 'Component CSS', hint: 'Stylesheet for the component; the app theme’s CSS variables are available.', example: '#root { color: var(--accent); }' },
  'custom.js': { label: 'Component JS', hint: 'Inline script (no network); use aether.requestData(key) for bound data.', example: "aether.requestData('sales')" },
  'custom.bindings': { label: 'Data bindings', hint: 'Widgets of this dashboard the component may read through the bridge, keyed by name.', example: 'sales → Revenue by day' },
  ```

**Step 5: Run to verify pass**

Run: `cd web && npx vitest run src/components/CodeMirrorField.test.tsx src/components/WidgetConfigDrawer.test.tsx src/utils/addWidgetPayload.test.ts src/app-builder/fieldHelp.test.ts && npx tsc --noEmit`
Expected: PASS / clean.

**Step 6: Commit**

```bash
git add web/package.json web/package-lock.json web/src/components/CodeMirrorField.tsx web/src/components/CodeMirrorField.test.tsx web/src/components/WidgetConfigDrawer.tsx web/src/components/WidgetConfigDrawer.test.tsx web/src/utils/addWidgetPayload.ts web/src/utils/addWidgetPayload.test.ts web/src/app-builder/fieldHelp.ts web/src/app-builder/fieldHelp.test.ts
git commit -m "feat(web): custom component authoring in the widget drawer"
```

---

### Task 11: Org settings toggle + App settings layout control

**Files:**
- Modify: `web/src/pages/OrgSettingsPage.tsx` (+ `web/src/test/OrgSettingsPage.test.tsx`)
- Modify: `web/src/pages/DashboardEditorPage.tsx` (App settings panel: Layout select)
- Modify: `web/src/pages/DashboardEditorPage.test.tsx` (if the panel is covered there)

**Step 1: Write the failing tests**

- `OrgSettingsPage.test.tsx`: `custom_code_enabled` absent → the "Allow custom code" toggle renders **off** (default); flipping it PUTs `{custom_code_enabled: true}`; a `custom_code_enabled: true` payload renders it on.
- `DashboardEditorPage.test.tsx`: the App settings panel gains a Layout select (`grid` | `custom`) whose save writes `settings.app.layout`.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/test/OrgSettingsPage.test.tsx src/pages/DashboardEditorPage.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- Org Settings "App builder" section gains a second toggle under "Allow HTML panels":
  "Allow custom code" + hint "Sandboxed HTML/CSS/JS components on app surfaces. When off, existing components show a placeholder and new ones cannot be added." Derived `customCodeEnabled === true`; PUT via `api.put('/api/v1/org/settings', { custom_code_enabled: next })`, mirroring `handleToggleHtmlPanels` (including the `setOrgSettings` snapshot sync so a later branding save cannot revert it).
- The App settings panel (editor header → App) gains a Layout select (`Grid` default | `Custom page — one full-bleed component; other widgets become data sources`) writing `settings.app.layout` through the existing app-settings save path. Add the `app.layout` field-help key (`app.layout`) and guide coverage.

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/test/OrgSettingsPage.test.tsx src/pages/DashboardEditorPage.test.tsx`
Expected: PASS. Then browser-validate with agent-browser (repo rule): the toggle persists both ways; the Layout select flips the editor badge ("Data source" chips) and the app view renders the custom page.

**Step 5: Commit**

```bash
git add web/src/pages/OrgSettingsPage.tsx web/src/test/OrgSettingsPage.test.tsx web/src/pages/DashboardEditorPage.tsx web/src/pages/DashboardEditorPage.test.tsx web/src/app-builder/fieldHelp.ts web/src/app-builder/fieldHelp.test.ts
git commit -m "feat(web): org switch and layout control for custom code"
```

---

## Phase 4 — Review, guide, and agent docs

### Task 12: Review findings + guide section

**Files:**
- Modify: `internal/dashboardreview/review.go` (+ test)
- Modify: `internal/agent/guide/guide.md` (custom section)
- Modify: `internal/agent/tools_manage.go` (guide-touching descriptions only if needed)

**Step 1: Write the failing tests**

`internal/dashboardreview/review_test.go` — add findings coverage:
- `layout: "custom"` with zero custom widgets → warning: "the custom page has no custom component — the app falls back to the grid" (fix tool `create_custom_widget`).
- `layout: "custom"` with two custom widgets → info naming the first: "multiple custom components — only <name> renders on the custom page".
- a custom widget with no bindings → info: "the custom component has no data bindings".

**Step 2: Run to verify failure**

Run: `bash scripts/go-test.sh ./internal/dashboardreview/ -timeout 3m`
Expected: FAIL.

**Step 3: Implement**

The review input already carries settings + widgets; add the three checks with structured `widget_id`s where applicable and the fixing tool call (`create_custom_widget`, `set_widget_config`, `update_dashboard_widget`). Guide (`internal/agent/guide/guide.md`): add `### custom` after `### html` documenting the bundle, the bridge API (`aether.ready()`, `aether.requestData(key)`, `aether.openLink(href)`; `aether:data` / `aether:error` events), the org switch (`custom_code_enabled`, default off → fall back to `html`), bindings, and the full custom page. The `fieldHelp` guide cross-check test requires the new keys' trailing field names (`html`, `css`, `js`, `bindings`, `layout`) to appear in the guide — they will.

**Step 4: Run to verify pass**

Run: `bash scripts/go-test.sh ./internal/dashboardreview/ ./internal/agent/ -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dashboardreview/review.go internal/dashboardreview/review_test.go internal/agent/guide/guide.md
git commit -m "feat(review): custom component findings and the agent guide section"
```

---

## Phase 5 — E2E, docs, verification

### Task 13: E2E — bridge, isolation, kill switch, custom page, public gating

**Files:**
- Create: `e2e/custom-widget.spec.ts`

**Scenarios:**
1. **Bridge + isolation**: enable the org switch via API; create a connector + query widget + custom widget bound to it (`bindings: [{key:'sales', widget_id}]`, JS calls `aether.requestData('sales')` and writes a `[data-sales]` node when the `aether:data` event arrives); open `/dashboards/{id}/view` (draft preview as the owner); assert the bound value renders inside the frame; assert the frame made **no** outbound requests (`page.route('**/*', …)` counting requests whose frame is the iframe, or assert `connect-src` blocked behavior via a `fetch` in the author script producing an error event) and that `document.cookie` is empty inside the frame (opaque origin).
2. **Kill switch**: default off → a freshly created custom widget POST returns 400 and an existing component shows the placeholder; enabling renders it; disabling again shows the placeholder and keeps the config (re-enable → renders).
3. **Custom page**: `settings.app.layout: 'custom'` → `/dashboards/{id}/view` renders only the custom frame (the query widget is absent), and the editor shows the "Data source" chip on the query widget.
4. **Public gating**: share the dashboard; with `public_live` off the frame renders but `requestData` answers "not enabled"; turning `public_live` on (publish first for the snapshot path) feeds the data.

**Run:**
```bash
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test custom-widget.spec.ts
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test custom-widget.spec.ts --repeat-each=2
```

**Commit:**
```bash
git add e2e/custom-widget.spec.ts
git commit -m "test(e2e): custom components, bridge, and the org kill switch"
```

---

### Task 14: Docs + full verification + PR

**Files:**
- Modify: `AGENTS.md` (new "Custom components (data apps wave 4)" paragraph: type, sandbox/CSP invariants, bridge protocol, bindings validation, `custom_code_enabled` default-off, layout custom, agent tool, seed rev)
- Modify: `DESIGN.md` (tier 2 implemented: boundary, protocol, governance; the open decisions resolved)
- Modify: `FRONTEND.md` (`SandboxedFrame` + `CustomWidget`: states, protocol, theming, authoring)

**Verification:**
```bash
task check
cd web && npx tsc -b && npm run build && npm run test:run
cd relay && npm run build
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test custom-widget.spec.ts html-panel.spec.ts dashboard-app.spec.ts dashboard-publish.spec.ts app-builder.spec.ts dashboard.spec.ts
```

Plus a browser sweep with agent-browser over: a custom component on the app view (bridge data + theming), drawer authoring (three editors, binding picker, preview), the custom page, the org toggle, and the public page (gated + live).

Then `git push -u origin feat/data-apps-wave-4` + `gh pr create --title "feat: data apps wave 4 — custom components & pages"` (base `main`).

**Commit:**
```bash
git add AGENTS.md DESIGN.md FRONTEND.md
git commit -m "docs: data apps wave 4 surfaces and the custom component boundary"
```

---

## Out of scope (later)

- Distribution: dashboard embedding, presentation mode, print/PDF.
- Signed embedding with pass-through auth + RLS.
- MCP allowlist additions for the new tools (existing follow-up).
- Review findings for empty html panels (existing follow-up).
- Arbitrary dashboard-wide custom CSS; nested container sections.
- Cell execution from the bridge (bound cell widgets read stored outputs only).

## Known limitations to record in the docs

- The component is opaque-origin by design: no storage, no same-origin assets, no network — `data:` images and inline assets only.
- Cell-backed bindings read the cell's last stored outputs (no execution from the bridge).
- `layout: "custom"` renders the first custom component; other components stay data sources (review flags multiples).
- The org switch is evaluated live on every surface; disabled components keep their config and reappear when re-enabled.
- Browser documents can introduce bindings only to widgets of the same dashboard (validator-enforced); legacy dangling bindings render as per-key error states.
