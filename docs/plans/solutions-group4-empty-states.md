# Group 4+8: Empty States, Onboarding & Polish — Design Solutions

## Issue 1: No empty state illustrations on Connectors/Agents/Models/Skills/MCPs

### Current Implementation

All five pages use inline `<td colSpan>` elements with plain text for empty states inside their respective `StyledTable` components:

- **ConnectorsPage.tsx** (line ~265): `"No connectors yet. Add one to connect to your databases."`
- **AgentsPage.tsx** (line ~183): Has a slightly richer empty state with bold title + description text, but still inline `<td>` with no icon
- **ModelsPage.tsx** (line ~193): `"No model configs yet. Add one to configure AI providers for agents."`
- **SkillsPage.tsx** (line ~167): `"No skills yet. Skills are reusable AI behaviors you can attach to agents."`
- **MCPPage.tsx** (line ~149): `"No MCP servers configured yet. Create one to extend agent capabilities."`

The existing `EmptyState` component (`web/src/components/EmptyState.tsx`) already supports an `icon` prop (rendered in a 56×56 tile), a `title`, `text`, and an `action` button — but none of these pages use it.

### Proposed Fix

**Replace inline empty-state `<td>` content with the existing `<EmptyState>` component**, placed outside the table when the list is empty. This avoids the awkward full-span table cell and provides a visually consistent, centered empty state.

#### Implementation Plan

1. **Create a page-level wrapper pattern**: When `items.length === 0 && !isLoading`, render `<EmptyState>` instead of the `<StyledTable>`.

2. **Add contextual icons** using emoji or Lucide icons (already available in the project):
   - Connectors: `<Database size={28} />` (or `🔌`)
   - Agents: `<Bot size={28} />` (or `🤖`)
   - Models: `<Brain size={28} />` (or `🧠`)
   - Skills: `<Zap size={28} />` (or `⚡`)
   - MCPs: `<Server size={28} />` (or `🔗`)

3. **Add action buttons** that trigger the "New" form:
   ```tsx
   <EmptyState
     icon={<Database size={28} />}
     title="No connectors yet"
     text="Add a connector to link your databases and start querying."
     action={{ label: '+ New Connector', onClick: () => setCreating(true) }}
   />
   ```

4. **Remove the inline empty `<tr>`** from each page's `<StyledTable>`.

#### Code Change Pattern (same for all 5 pages)

```tsx
// Before (inside StyledTable tbody):
{connectors.length === 0 && (
  <tr><td colSpan={6} style={...}>No connectors yet...</td></tr>
)}

// After (replace entire StyledTable with conditional):
{connectors.length === 0 && !isLoading ? (
  <EmptyState
    icon={<Database size={28} />}
    title="No connectors yet"
    text="Add a connector to link your databases and start querying."
    action={{ label: '+ New Connector', onClick: () => setCreating(true) }}
  />
) : (
  <StyledTable headers={...}>
    {connectors.map(...)}
  </StyledTable>
)}
```

#### Dependencies
- `EmptyState` component already exists — no new component needed
- Lucide icons already imported across the project (check each page for existing imports)
- No API or backend changes

#### Effort: ~30 min (5 pages × 6 min each)

---

## Issue 2: Chart view shows empty state with no guidance

### Current Implementation

In `ChartView.tsx` (line ~113):
```tsx
if (columns.length < 2) {
  return <p style={styles.empty}>Need at least 2 columns to chart</p>
}
```

This is a bare `<p>` tag with minimal styling — no guidance on what to do, no visual affordance, and no prompt to configure the chart. The `styles.empty` is just centered muted text.

Additionally, when the chart renders successfully, the "Configure" button is small and easy to miss. New users may not realize they can customize chart type, axes, colors, etc.

### Proposed Fix

**Replace the bare `<p>` with a guided empty state that explains the requirement and suggests next steps.** Also add a subtle hint near the Configure button on first render.

#### Implementation

1. **Improve the `< 2 columns` empty state**:
```tsx
if (columns.length < 2) {
  return (
    <div style={styles.emptyGuidance}>
      <div style={styles.emptyIcon}>📊</div>
      <p style={styles.emptyTitle}>Not enough data to chart</p>
      <p style={styles.emptyText}>
        Charts need at least 2 columns — one for the X axis and one or more for Y values.
        Run a query that returns multiple columns, then switch to chart view.
      </p>
    </div>
  )
}
```

2. **Auto-open config panel when chart first loads with default settings** (optional enhancement): When the chart renders for the first time with all defaults, briefly flash/highlight the Configure button to draw attention. A simpler alternative: add a small tooltip-like hint text below the chart on first render.

3. **Add a subtle "Configure chart" hint** below the chart when config is all defaults:
```tsx
{!showConfig && isDefaultConfig && (
  <p style={styles.configHint}>
    💡 Click "Configure" to change chart type, axes, and colors
  </p>
)}
```

#### New styles:
```tsx
emptyGuidance: {
  padding: '32px 24px',
  textAlign: 'center',
  display: 'flex',
  flexDirection: 'column',
  alignItems: 'center',
  gap: 8,
},
emptyIcon: { fontSize: 32, marginBottom: 4 },
emptyTitle: { fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 },
emptyText: { fontSize: 13, color: 'var(--text-muted)', margin: 0, maxWidth: 360, lineHeight: 1.5 },
configHint: { fontSize: 11, color: 'var(--text-muted)', textAlign: 'center', marginTop: 4, fontStyle: 'italic' },
```

#### Dependencies
- No new components needed
- No API changes
- `isDefaultConfig` can be derived: `!cfg.chartType || cfg.chartType === 'bar'` and no custom colors/axes set

#### Effort: ~15 min

---

## Issue 3: OIDC provider form has no "Test" button

### Current Implementation

In `OrgSettingsPage.tsx`, the `ProviderForm` component (line ~60) has fields for Name, Client ID, Client Secret, Discovery URL, Allowed Domains, and Enabled. The form actions only include "Save/Add Provider" and "Cancel" — there's no way to validate the OIDC configuration before committing it.

The backend has no existing test endpoint for OIDC providers (would need verification).

### Proposed Fix

**Add a "Test Connection" button to the ProviderForm that validates the OIDC discovery endpoint before saving.**

#### Implementation

1. **Add a "Test" button** next to the Save button in `ProviderForm`:
```tsx
<div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
  <button style={formStyles.testBtn} onClick={() => onTest(values)} disabled={testing || !values.discovery_url}>
    {testing ? 'Testing…' : 'Test Connection'}
  </button>
  {testResult && (
    <span style={{ fontSize: 12, color: testResult.ok ? 'var(--success)' : 'var(--error)', alignSelf: 'center' }}>
      {testResult.ok ? '✓ Discovery OK' : `✗ ${testResult.error}`}
    </span>
  )}
  <span style={{ flex: 1 }} />
  <button style={formStyles.btn} onClick={() => onSave(values)} disabled={saving}>
    {saving ? 'Saving…' : isEdit ? 'Save Changes' : 'Add Provider'}
  </button>
  <button style={formStyles.cancelBtn} onClick={onCancel} disabled={saving}>Cancel</button>
</div>
```

2. **Add test state and handler to `ProviderForm`**:
```tsx
const [testing, setTesting] = useState(false)
const [testResult, setTestResult] = useState<{ ok: boolean; error?: string } | null>(null)

const handleTest = async () => {
  setTesting(true)
  setTestResult(null)
  try {
    const res = await api.post<{ ok: boolean; error?: string }>('/api/v1/sso/test', {
      discovery_url: values.discovery_url,
      client_id: values.client_id,
      client_secret: values.client_secret,
    })
    setTestResult(res)
  } catch {
    setTestResult({ ok: false, error: 'Request failed' })
  } finally {
    setTesting(false)
  }
}
```

3. **Backend endpoint needed**: `POST /api/v1/sso/test` — fetches the discovery URL, validates it returns a valid OIDC configuration JSON with required fields (`issuer`, `authorization_endpoint`, `token_endpoint`). This is a lightweight read-only validation.

#### Backend Implementation (Go):
```go
func (s *Server) handleTestSSOProvider(w http.ResponseWriter, r *http.Request) {
    var body struct {
        DiscoveryURL string `json:"discovery_url"`
    }
    // ... decode body
    resp, err := http.Get(body.DiscoveryURL)
    // ... validate response contains issuer, authorization_endpoint, token_endpoint
    // Return { ok: true } or { ok: false, error: "..." }
}
```

#### Dependencies
- **Backend**: New endpoint `POST /api/v1/sso/test` in `internal/api/` (~30 lines)
- **Frontend**: Changes to `ProviderForm` component in `OrgSettingsPage.tsx`
- No database changes

#### Effort: ~45 min (frontend + backend)

---

## Issue 4: Profile status field has no character limit indicator

### Current Implementation

In `ProfilePage.tsx` (line ~82):
```tsx
<label style={styles.label}>
  Status <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span>
  <input style={styles.input} value={status} placeholder="e.g. On vacation"
    onChange={e => setStatus(e.target.value)} maxLength={100} />
</label>
```

The `maxLength={100}` attribute silently truncates input but gives no visual feedback about the limit or how many characters remain.

### Proposed Fix

**Add a character counter below the status input showing `current/max`.**

#### Implementation

Add a counter element below the input:
```tsx
<label style={styles.label}>
  Status <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span>
  <input style={styles.input} value={status} placeholder="e.g. On vacation"
    onChange={e => setStatus(e.target.value)} maxLength={100} />
  <span style={{
    fontSize: 11,
    color: status.length > 90 ? 'var(--error)' : 'var(--text-muted)',
    textAlign: 'right',
    marginTop: 2,
  }}>
    {status.length}/100
  </span>
</label>
```

The counter:
- Shows `{current}/100` format
- Turns red (`var(--error)`) when within 10 characters of the limit (>90)
- Right-aligned under the input
- Uses existing CSS variables — no new styles needed

#### Dependencies
- None — pure frontend change, single file
- The `maxLength={100}` already exists on the input

#### Effort: ~5 min

---

## Issue 5: No visual indicator of default connector

### Current Implementation

In `ConnectorsPage.tsx`, the default connector already has a text badge:

```tsx
<td style={cellStyle}>
  <strong>{c.name}</strong>
  {c.is_default && (
    <span style={{ fontSize: 11, background: 'var(--accent-light)', border: '1px solid var(--border)',
      borderRadius: 3, padding: '1px 6px', color: 'var(--text-secondary)', fontFamily: 'var(--font-mono)',
      marginLeft: 8 }}>
      default
    </span>
  )}
</td>
```

And the "Set default" button is hidden for the default connector (`!c.is_default`).

**Assessment**: The current implementation actually **already has** a visual indicator — the `default` badge next to the connector name. However, it's subtle (small monospace text in a muted badge). The issue description says "no badge/star" but the code shows one exists.

### Proposed Fix

**Enhance the existing default indicator to be more visually prominent** with a star icon and a more distinctive badge style.

#### Implementation

1. **Upgrade the badge** to include a star icon and use accent colors:
```tsx
{c.is_default && (
  <span style={{
    fontSize: 11,
    background: 'var(--accent-light)',
    border: '1px solid var(--accent)',
    borderRadius: 10,
    padding: '2px 8px',
    color: 'var(--accent)',
    fontWeight: 600,
    marginLeft: 8,
    display: 'inline-flex',
    alignItems: 'center',
    gap: 3,
  }}>
    <Star size={10} fill="var(--accent)" />
    Default
  </span>
)}
```

2. **Add a subtle row highlight** for the default connector (optional):
```tsx
<tr key={c.id} style={{
  ...rowStyle,
  background: c.is_default ? 'var(--accent-light)' : undefined,
}}>
```

3. **Add a tooltip** to the "Set default" button explaining what it does:
```tsx
<button ... title="Set as default connector for new notebooks">
  Set default
</button>
```

#### Dependencies
- `Star` icon from `lucide-react` (already a project dependency)
- No backend changes
- No API changes

#### Effort: ~10 min

---

## Issue 6: Dashboard widget column buttons have no labels

### Current Implementation

In `DashboardEditorPage.tsx` (line ~237):
```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
  <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Cols</span>
  {[6, 8, 12, 16, 24].map(c => (
    <button
      key={c}
      type="button"
      style={{
        padding: '3px 8px',
        fontSize: 12,
        fontWeight: 600,
        border: '1px solid var(--border)',
        borderRadius: 4,
        cursor: 'pointer',
        background: gridCols === c ? 'var(--accent)' : 'var(--bg-input)',
        color: gridCols === c ? '#fff' : 'var(--text-secondary)',
      }}
      onClick={async () => { ... }}
    >
      {c}
    </button>
  ))}
</div>
```

The buttons show only numbers (6, 8, 12, 16, 24) with a "Cols" label prefix, but no individual tooltips explaining what each number means (grid columns for the dashboard layout).

### Proposed Fix

**Add `title` attributes to each button for native browser tooltips.**

#### Implementation

Simply add a `title` prop to each button:
```tsx
<button
  key={c}
  type="button"
  title={`${c} grid columns — ${c <= 8 ? 'compact' : c <= 12 ? 'standard' : 'wide'} layout`}
  style={...}
  onClick={...}
>
  {c}
</button>
```

This gives tooltips like:
- `6 grid columns — compact layout`
- `8 grid columns — compact layout`
- `12 grid columns — standard layout`
- `16 grid columns — wide layout`
- `24 grid columns — wide layout`

#### Alternative: Add visible labels

If tooltips aren't discoverable enough, add a small descriptor below each number:
```tsx
<button ...>
  <span style={{ display: 'block', fontSize: 12, fontWeight: 600 }}>{c}</span>
  <span style={{ display: 'block', fontSize: 9, color: gridCols === c ? 'rgba(255,255,255,0.7)' : 'var(--text-muted)', marginTop: 1 }}>
    {c <= 8 ? 'sm' : c <= 12 ? 'md' : 'lg'}
  </span>
</button>
```

**Recommended approach**: Use `title` tooltips (simpler, zero layout impact). The "Cols" label + number is already reasonably clear for the target audience (developers/analysts).

#### Dependencies
- None — pure HTML `title` attribute addition
- No new components, no API changes

#### Effort: ~5 min

---

## Summary Table

| # | Issue | File(s) | Fix Complexity | Dependencies |
|---|-------|---------|----------------|--------------|
| 1 | Empty state illustrations | ConnectorsPage, AgentsPage, ModelsPage, SkillsPage, MCPPage | Low — use existing `<EmptyState>` | None |
| 2 | Chart empty state guidance | ChartView.tsx | Low — improve inline message | None |
| 3 | OIDC test button | OrgSettingsPage.tsx + new backend endpoint | Medium — needs backend | `POST /api/v1/sso/test` endpoint |
| 4 | Status char limit indicator | ProfilePage.tsx | Trivial — add counter text | None |
| 5 | Default connector indicator | ConnectorsPage.tsx | Low — enhance existing badge | `Star` from lucide-react |
| 6 | Column button tooltips | DashboardEditorPage.tsx | Trivial — add `title` attrs | None |

## Implementation Priority

1. **Issue 4** (char counter) — 5 min, zero risk
2. **Issue 6** (tooltips) — 5 min, zero risk
3. **Issue 5** (default badge) — 10 min, visual only
4. **Issue 2** (chart guidance) — 15 min, visual only
5. **Issue 1** (empty states) — 30 min, visual only
6. **Issue 3** (OIDC test) — 45 min, requires backend work

**Total estimated effort: ~1.5 hours**
