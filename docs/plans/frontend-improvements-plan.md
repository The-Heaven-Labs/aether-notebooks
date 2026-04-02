# Frontend Improvements Implementation Plan

> **Status**: Ready to implement
> **Created**: 2026-04-01
> **Priority Order**: Phase 1 (Quick Wins) → Phase 2 (UX) → Phase 3 (Features) → Phase 4 (Components)

## Overview

This plan addresses all frontend improvements identified from:
1. IMPROVEMENTS.md backlog (30 items)
2. Visual quality analysis (hardcoded colors, accessibility, consistency)
3. Missing components and design system gaps

**Related docs**:
- IMPROVEMENTS.md - Product backlog
- FRONTEND.md - Visual design system documentation
- 2026-03-24-missing-features.md - Backend implementation plan

---

## ✅ Completed IMPROVEMENTS.md Items

These were implemented in recent commits:

- **#3** Hide/collapse cells ✅
- **#4** Keyboard shortcuts ✅
- **#6** Notebook/cell connector selector ✅
- **#7** List layout default ✅
- **#9** Database picker for connectors ✅
- **#10** Cell history panel ✅
- **#11** Image paste in markdown ✅
- **#14** Templates and snippets ✅
- **#15** Slug substitution ✅
- **#16** Charts (bar, line, area, scatter, pie, donut) ✅
- **#18** Cell-level parameters ✅
- **#19** Cell title/description visible ✅
- **#20** Markdown syntax highlighting ✅
- **#21** Live markdown preview ✅
- **#22** Presentation mode ✅
- **#23** Schema browser ✅
- **#24** Type icons in output ✅
- **#25** Dashboard widgets ✅
- **#27** Collaboration cursors (implemented, needs bug fix) ✅
- **#29** Org onboarding ✅
- **#30** Logo variants ✅

---

## 🔴 Phase 1: High Priority - Visual Quality (Quick Wins)

### Task 1: Replace Hardcoded Colors with CSS Variables

**Effort**: 1-2 hours | **Impact**: High consistency improvement

**Problem**: 54 instances of hardcoded hex colors instead of design system variables.

**Files**: 
- `web/src/styles/theme.css` (add variables)
- `MembersPage.tsx`, `DashboardEditorPage.tsx`, `NotebookPage.tsx`, `ConnectorsPage.tsx` (error colors)
- `PresentationPage.tsx` (dark theme colors)
- `LoginPage.tsx`, `OrgOnboardingPage.tsx` (error styling)
- `ChartView.tsx` (chart palette)
- `DashboardsPage.tsx`, `PublicDashboardPage.tsx` (badge backgrounds)
- `CodeCell.tsx`, `TextCell.tsx` (background tints)

**Implementation**:

1. Add to `theme.css`:
```css
/* Status colors */
--error-full: #b85c5c;
--error-light: #fdf3f3;
--error-border: #fcd0d0;
--success-full: #27ae60;
--success-light: #e8f5e9;

/* Presentation mode dark theme */
--presenter-bg: #0d0d0d;
--presenter-surface: #1a1a1a;
--presenter-border: #2a2a2a;
--presenter-text: #f0f0f0;
--presenter-muted: #888;

/* Chart palette */
--chart-1: #6366f1;
--chart-2: #22d3ee;
--chart-3: #f59e0b;
--chart-4: #10b981;
--chart-5: #ef4444;
--chart-6: #8b5cf6;
--chart-7: #ec4899;

/* Border radius scale */
--radius-xs: 4px;   /* badges, tags, type icons */
--radius-sm: 6px;   /* inputs, buttons */
--radius-md: 10px;  /* cards, cells */
--radius-lg: 12px;  /* large panels */
```

2. Replace all hardcoded instances systematically:
   - Error colors: `#c0392b` → `var(--error-full)`
   - Error backgrounds: `#fff0f0`, `#fdf3f3` → `var(--error-light)`
   - Error borders: `#fcd0d0` → `var(--error-border)`
   - Success: `#27ae60`, `#2d7d46` → `var(--success-full)`
   - Presenter: all `#0d0d0d`, `#1a1a1a`, etc → presenter variables
   - Charts: COLORS array → use CSS variables

**Testing**:
```bash
# Visual regression tests
npx playwright test --update-snapshots

# Manual: check error messages, success states, charts, presentation mode
```

**Commit**: `refactor: replace 54 hardcoded colors with CSS variables for consistency`

---

### Task 2: Add Focus States for Accessibility

**Effort**: 30 minutes | **Impact**: Accessibility compliance

**Problem**: Relying on browser defaults for focus states, inconsistent visual feedback.

**Files**: `web/src/styles/theme.css`

**Implementation**:

Add to end of `theme.css`:
```css
/* Focus visible for accessibility */
*:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

*:focus:not(:focus-visible) {
  outline: none;
}

button:focus-visible,
input:focus-visible,
select:focus-visible,
textarea:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-color: var(--accent);
}

a:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: 2px;
}
```

**Testing**:
- Press Tab through all pages
- Verify visible focus ring on all interactive elements
- Test in Chrome, Firefox, Safari

**Commit**: `feat: add consistent focus-visible states for accessibility`

---

### Task 3: Standardize Border Radius

**Effort**: 1 hour | **Impact**: Visual consistency

**Problem**: Mixed values: `3px`, `4px`, `5px`, `6px`, `7px`, `8px`, `10px`, `12px`, `14px`

**Standardize to 4 values**:
- **XS (4px)**: Badges, type icons, tags
- **SM (6px)**: Inputs, buttons, small elements
- **MD (10px)**: Cards, cells, panels
- **LG (12px)**: Large panels, modals
- **FULL (50%)**: Avatars, circles

**Implementation**:

1. Find all border-radius values:
```bash
grep -r "borderRadius:" web/src --include="*.tsx" | grep -v "50%" | sort
```

2. Replace systematically:
   - `borderRadius: 3` → `borderRadius: 4` (or use `var(--radius-xs)`)
   - `borderRadius: 5` → `borderRadius: 6` (or use `var(--radius-sm)`)
   - `borderRadius: 7` or `8` → `borderRadius: 6`
   - `borderRadius: 14` → `borderRadius: 12`

3. Prefer CSS variables for future theming:
   ```typescript
   // Instead of
   borderRadius: 6
   // Use
   borderRadius: 'var(--radius-sm)'
   ```

**Testing**:
```bash
# Visual comparison before/after
npx playwright test e2e/visual.spec.ts
```

**Commit**: `style: standardize border-radius to 4/6/10/12px scale`

---

### Task 4: Add Consistent Hover States

**Effort**: 2 hours | **Impact**: UX polish, discoverability

**Problem**: Many buttons/interactive elements lack hover feedback.

**Files**: Multiple component files

**Implementation**:

1. Add hover utilities to `theme.css`:
```css
/* Card hover */
.card-hover:hover {
  box-shadow: var(--shadow-md);
  border-color: var(--border);
  transition: box-shadow 0.15s ease, border-color 0.15s ease;
}

/* Icon button hover */
.icon-btn:hover:not(:disabled) {
  background: var(--bg-secondary);
  border-color: var(--border);
}

/* Delete button hover */
.delete-btn:hover:not(:disabled) {
  color: var(--error);
  background: var(--error-light);
}
```

2. Update component files:

**HomePage.tsx** - Add to card styles:
```typescript
card: {
  // ...existing
  transition: 'box-shadow 0.15s ease, border-color 0.15s ease',
  ':hover': {
    boxShadow: 'var(--shadow-md)',
  }
}
```

**Note**: React inline styles don't support `:hover`. Options:
- Use CSS classes instead of inline styles
- Use `onMouseEnter`/`onMouseLeave` to toggle state
- Use CSS-in-JS library (emotion, styled-components) - **not recommended** for this codebase

**Recommended approach**: Add className support:
```typescript
<div style={styles.card} className="card-hover">
```

Then in `theme.css`:
```css
.card-hover:hover {
  box-shadow: var(--shadow-md);
}
```

**Testing**:
- Hover over all interactive elements
- Verify visual feedback (opacity change, background change, shadow)
- Check: cards, buttons, nav items, table rows

**Commit**: `feat: add consistent hover states across all interactive elements`

---

## 🟡 Phase 2: Medium Priority - UX Improvements

### Task 5: Fix Collaboration Cursor Names (#27)

**Effort**: 30 minutes | **Impact**: Bug fix, collaboration UX

**Problem**: Cursors show "Anonymous" instead of real names

**Files**: `web/src/components/CodeCell.tsx` (lines 41-54)

**Investigation**:

1. Verify localStorage is set:
```typescript
// Check in browser console after login:
localStorage.getItem('hnb_user_name')
localStorage.getItem('hnb_user_email')
```

2. Check `web/src/api/client.ts` sets values on login/register

**Fix**:

Update `CodeCell.tsx`:
```typescript
const userName = localStorage.getItem('hnb_user_name') || ''
const userEmail = localStorage.getItem('hnb_user_email') || ''

// Debug log
if (!userName && !userEmail) {
  console.warn('[CodeCell] No user identity found in localStorage')
}

const provider = new HocuspocusProvider({
  url: RELAY_URL,
  name: notebookId,
  document: doc,
  token,
  onAuthenticationFailed: () => console.warn('[yjs] Relay auth failed'),
})

// Set awareness after provider is ready
provider.on('synced', () => {
  provider.awareness?.setLocalStateField('user', {
    name: userName || userEmail || 'Anonymous',
    email: userEmail,
    color: `hsl(${Math.abs(hashStr(userEmail || userName)) % 360}, 70%, 55%)`,
  })
})
```

**Testing**:
- Login as "Test User" with email "test@example.com"
- Open notebook in two tabs
- Verify cursor shows "Test User" not "Anonymous"

**Commit**: `fix: ensure collaboration cursors show real user names`

---

### Task 6: Add Notebook Title/Description Header (#1)

**Effort**: 2-3 hours | **Impact**: Major UX improvement

**Problem**: Only small title next to back button, needs prominent header

**Files**: `web/src/pages/NotebookPage.tsx`

**Implementation**:

Replace current header section (around line 360) with:

```typescript
<div style={styles.notebookHeader}>
  <div style={styles.notebookHeaderLeft}>
    <Link to="/" style={styles.backLink}>
      <ChevronLeft size={16} />
    </Link>
    <div style={styles.notebookTitleSection}>
      {editingTitle ? (
        <input
          style={styles.notebookTitleInput}
          value={titleDraft}
          onChange={(e) => setTitleDraft(e.target.value)}
          onBlur={() => {
            setEditingTitle(false)
            if (titleDraft.trim() && titleDraft !== notebook.title) {
              saveTitle.mutate(titleDraft.trim())
            }
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') e.currentTarget.blur()
            if (e.key === 'Escape') setEditingTitle(false)
          }}
          autoFocus
        />
      ) : (
        <h1 style={styles.notebookTitle} onClick={() => { setTitleDraft(notebook.title); setEditingTitle(true) }}>
          {notebook.title}
          <Pencil size={14} style={{ marginLeft: 8, opacity: 0.5 }} />
        </h1>
      )}
      <div style={styles.notebookMeta}>
        <span style={styles.notebookMetaText}>
          Last updated {fmtTime(new Date(notebook.updated_at))}
        </span>
        {notebook.description && (
          <p style={styles.notebookDescription}>{notebook.description}</p>
        )}
      </div>
    </div>
  </div>
  <div style={styles.notebookHeaderRight}>
    {/* Add: connector selector, parameters button, etc */}
  </div>
</div>
```

Add styles:
```typescript
notebookHeader: {
  display: 'flex',
  alignItems: 'flex-start',
  justifyContent: 'space-between',
  padding: '24px 32px 16px',
  borderBottom: '1px solid var(--border-light)',
  background: 'var(--bg-primary)',
},
notebookTitleInput: {
  fontSize: 24,
  fontWeight: 700,
  color: 'var(--text-primary)',
  background: 'transparent',
  border: 'none',
  borderBottom: '2px solid var(--accent)',
  outline: 'none',
  fontFamily: 'var(--font-sans)',
  minWidth: 300,
},
notebookTitle: {
  fontSize: 24,
  fontWeight: 700,
  color: 'var(--text-primary)',
  cursor: 'pointer',
  display: 'flex',
  alignItems: 'center',
},
// ... more styles
```

**Commit**: `feat: add prominent notebook title and description header section`

---

### Task 7: Standardize Error Display Component

**Effort**: 1 hour | **Impact**: Code quality, consistency

**Files**: Create `web/src/components/ErrorBanner.tsx`

**Implementation**:

```typescript
// web/src/components/ErrorBanner.tsx
import { X } from 'lucide-react'

interface Props {
  message: string
  onDismiss?: () => void
  variant?: 'error' | 'warning' | 'info'
}

export function ErrorBanner({ message, onDismiss, variant = 'error' }: Props) {
  const styles = {
    error: {
      background: 'var(--error-light)',
      border: '1px solid var(--error-border)',
      color: 'var(--error-full)',
    },
    warning: {
      background: '#fff8e6',
      border: '1px solid var(--warning)',
      color: 'var(--warning)',
    },
    info: {
      background: 'var(--accent-light)',
      border: '1px solid var(--accent)',
      color: 'var(--accent)',
    },
  }

  return (
    <div style={{ ...baseStyles, ...styles[variant] }}>
      <span>{message}</span>
      {onDismiss && (
        <button onClick={onDismiss} style={dismissBtn}>
          <X size={14} />
        </button>
      )}
    </div>
  )
}

const baseStyles: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '8px 12px',
  borderRadius: 6,
  fontSize: 13,
}

const dismissBtn: React.CSSProperties = {
  background: 'transparent',
  border: 'none',
  cursor: 'pointer',
  padding: 0,
  display: 'flex',
  opacity: 0.7,
}
```

Update all pages to use `<ErrorBanner>` instead of inline styles.

**Commit**: `refactor: create reusable ErrorBanner component, replace inline error styles`

---

### Task 8: Improve Empty States Consistency

**Effort**: 1 hour | **Impact**: UX polish

**Files**: Create `web/src/components/EmptyState.tsx`

**Implementation**:

```typescript
// web/src/components/EmptyState.tsx
import type { ReactNode } from 'react'

interface Props {
  icon?: ReactNode
  title: string
  description?: string
  action?: {
    label: string
    onClick: () => void
  }
}

export function EmptyState({ icon, title, description, action }: Props) {
  return (
    <div style={styles.container}>
      {icon && <div style={styles.iconCircle}>{icon}</div>}
      <h3 style={styles.title}>{title}</h3>
      {description && <p style={styles.description}>{description}</p>}
      {action && (
        <button style={styles.action} onClick={action.onClick}>
          {action.label}
        </button>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    textAlign: 'center',
    padding: '80px 20px',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 12,
  },
  iconCircle: {
    width: 56,
    height: 56,
    background: 'var(--bg-secondary)',
    borderRadius: 14,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: 'var(--text-muted)',
  },
  title: { fontSize: 18, fontWeight: 700, color: 'var(--text-primary)', margin: 0 },
  description: { fontSize: 14, color: 'var(--text-secondary)', margin: 0 },
  action: {
    marginTop: 8,
    padding: '8px 18px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 7,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
}
```

Update pages to use `<EmptyState>` component.

**Commit**: `refactor: create reusable EmptyState component, standardize empty displays`

---

### Task 9: Add Loading Skeletons

**Effort**: 2 hours | **Impact**: Perceived performance

**Files**: Create `web/src/components/Skeleton.tsx`

**Implementation**:

```typescript
// web/src/components/Skeleton.tsx
import type { CSSProperties } from 'react'

interface Props {
  variant?: 'text' | 'rectangular' | 'circular'
  width?: string | number
  height?: string | number
  style?: CSSProperties
}

export function Skeleton({ variant = 'text', width, height, style }: Props) {
  const baseStyle: CSSProperties = {
    display: 'block',
    backgroundColor: 'var(--bg-secondary)',
    borderRadius: variant === 'cricular' ? '50%' : variant === 'text' ? 4 : 8,
    animation: 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
    ...(width && { width: typeof width === 'number' ? `${width}px` : width }),
    ...(height && { height: typeof height === 'number' ? `${height}px` : height }),
    ...style,
  }

  return <span style={baseStyle} />
}
```

Add to `theme.css`:
```css
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
```

Create skeleton components for each page type:
```typescript
// web/src/components/NotebookSkeleton.tsx
export function NotebookSkeleton() {
  return (
    <div style={styles.card}>
      <div style={styles.header}>
        <Skeleton variant="rectangular" width={42} height={42} />
        <div style={{ flex: 1 }}>
          <Skeleton variant="text" width="60%" height={20} />
          <Skeleton variant="text" width="40%" height={14} style={{ marginTop: 4 }} />
        </div>
      </div>
    </div>
  )
}
```

**Commit**: `feat: add skeleton loading states for better perceived performance`

---

## 🟢 Phase 3: Lower Priority - Feature Enhancements

### Task 10: Markdown Anchor Links (#5)

**Effort**: 2 hours

**Files**: `web/src/components/TextCell.tsx`

**Implementation**:

Add custom components to ReactMarkdown:
```typescript
const markdownComponents = {
  h1: ({ children, ...props }: any) => {
    const text = children?.toString() || ''
    const id = text.toLowerCase().replace(/\s+/g, '-').replace(/[^\w-]/g, '')
    return (
      <h1 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h1>
    )
  },
  // Same for h2-h6
}
```

Add styles:
```typescript
headerAnchor: {
  color: 'var(--text-muted)',
  textDecoration: 'none',
  marginRight: 8,
  opacity: 0,
  transition: 'opacity 0.15s',
}
```

Add to `theme.css`:
```css
h1:hover a, h2:hover a, h3:hover a, h4:hover a, h5:hover a, h6:hover a {
  opacity: 1;
}
```

**Commit**: `feat: add anchor links to markdown headers for easy sharing`

---

### Task 11: Named Query Variables (#13)

**Effort**: 4-6 hours (backend + frontend)

**Status**: Needs backend implementation first

**Backend changes**:
- Add `variables` table: `id, org_id, notebook_id, name, query, created_at`
- Add API endpoints: `GET/POST/PUT/DELETE /api/v1/notebooks/{id}/variables`
- Execute query referenced by variable name

**Frontend changes**:
- Create `VariableManager.tsx` component
- Add variable button to notebook toolbar
- Show variable results in cell input as `{{variable_name}}`

**Detailed plan**: Create separate backend plan if prioritized

---

### Task 12: Image Customization in Markdown (#17)

**Effort**: 3 hours

**Files**: `web/src/components/TextCell.tsx`

**Implementation**:

Parse image syntax: `![alt|width=300,height=200](image.png)`

```typescript
const parseImageSyntax = (alt: string) => {
  const parts = alt.split('|')
  if (parts.length === 1) return { alt: parts[0], width: undefined, height: undefined }
  
  const altText = parts[0]
  const options = parts[1] || ''
  const widthMatch = options.match(/width=(\d+)/)
  const heightMatch = options.match(/height=(\d+)/)
  
  return {
    alt: altText,
    width: widthMatch ? parseInt(widthMatch[1]) : undefined,
    height: heightMatch ? parseInt(heightMatch[1]) : undefined,
  }
}

const markdownComponents = {
  img: ({ src, alt, ...props }: any) => {
    const { alt: altText, width, height } = parseImageSyntax(alt || '')
    return (
      <img 
        src={src} 
        alt={altText} 
        {...props}
        style={{ maxWidth: width ? `${width}px` : '100%', height: height ? `${height}px` : 'auto' }}
      />
    )
  },
}
```

**Commit**: `feat: support image width/height in markdown syntax`

---

### Task 13: Audit Log UX - Show Names Not IDs (#26)

**Effort**: 2 hours

**Files**: 
- Backend: `internal/api/audit_handlers.go`
- Frontend: `web/src/pages/AuditPage.tsx`

**Backend changes**:

Update query to join users and resource tables:
```go
rows, err := s.db.Pool.Query(ctx, `
  SELECT al.id, al.org_id, al.user_id, al.action, al.resource_type, al.resource_id, al.created_at,
         u.name as user_name, u.email as user_email,
         CASE 
           WHEN al.resource_type = 'notebook' THEN n.title
           WHEN al.resource_type = 'connector' THEN c.name
           WHEN al.resource_type = 'dashboard' THEN d.title
           ELSE NULL
         END as resource_name
  FROM audit_logs al
  JOIN users u ON u.id = al.user_id
  LEFT JOIN notebooks n ON n.id = al.resource_id
  LEFT JOIN connectors c ON c.id = al.resource_id
  LEFT JOIN dashboards d ON d.id = al.resource_id
  WHERE al.org_id = $1
  ORDER BY al.created_at DESC
  LIMIT $2 OFFSET $3
`, args...)
```

Update response struct to include `user_name`, `user_email`, `resource_name`.

**Frontend changes**:
```typescript
{entries.map((e) => (
  <tr key={e.id}>
    <td>{e.user_name || e.user_email}</td>
    <td>{e.action}</td>
    <td title={e.resource_id}>{e.resource_name || e.resource_id}</td>
    <td>{formatTimestamp(e.created_at)}</td>
  </tr>
))}
```

**Commit**: `feat: show user and resource names in audit log instead of IDs`

---

### Task 14: Profile Button/Config Menu (#28)

**Effort**: 3 hours

**Files**: `web/src/components/TopBar.tsx`, create `web/src/components/ProfileMenu.tsx`

**Implementation**:

Enhance current profile dropdown in TopBar to include:
- Profile (link to profile settings page - needs creation)
- Settings (link to settings page - needs creation)
- Sign out (existing)

See Task 6 in `2026-03-24-missing-features.md` for detailed component structure.

**Note**: Requires backend support for profile/settings endpoints if not already implemented.

**Commit**: `feat: add profile menu with profile and settings options`

---

## 🔵 Phase 4: Missing Components

### Task 15: Toast Notification System

**Effort**: 3 hours

**Installation**:
```bash
cd web && npm install react-hot-toast
```

**Setup** in `App.tsx`:
```typescript
import { Toaster } from 'react-hot-toast'

function App() {
  return (
    <>
      <AppRoutes />
      <Toaster position="top-right" />
    </>
  )
}
```

**Usage** in mutations:
```typescript
import toast from 'react-hot-toast'

const createNotebook = useMutation({
  mutationFn: (title: string) => api.post('/api/v1/notebooks', { title }),
  onSuccess: () => {
    toast.success('Notebook created')
    qc.invalidateQueries({ queryKey: ['notebooks'] })
  },
  onError: (err: Error) => {
    toast.error(err.message)
  },
})
```

**Commit**: `feat: add toast notifications system`

---

### Task 16: Modal/Dialog Component

**Effort**: 2 hours

**Files**: Create `web/src/components/Modal.tsx`

**Implementation**:
```typescript
// Full implementation in Task 16 of original plan
```

**Usage**:
```typescript
<Modal
  open={showDeleteDialog}
  onClose={() => setShowDeleteDialog(false)}
  title="Delete notebook?"
  actions={
    <>
      <button onClick={() => setShowDeleteDialog(false)}>Cancel</button>
      <button onClick={handleDelete}>Delete</button>
    </>
  }
>
  <p>Are you sure you want to delete "{notebook.title}"?</p>
</Modal>
```

**Commit**: `feat: add reusable Modal/Dialog component`

---

### Task 17: Tooltip Component

**Effort**: 1 hour

**Files**: Create `web/src/components/Tooltip.tsx`

**Implementation**:
```typescript
// Full implementation in Task 17 of original plan
```

**Usage**:
```typescript
<Tooltip content="Run cell (Ctrl+Enter)">
  <button><Play size={13} /></button>
</Tooltip>
```

**Commit**: `feat: add Tooltip component for icon-only buttons`

---

## Testing Strategy

### Visual Regression Tests

Run before and after each task:
```bash
npx playwright test --update-snapshots
```

### Manual Testing Checklist

For each task, verify:
- [ ] Desktop Chrome/Firefox/Safari
- [ ] Keyboard navigation
- [ ] Screen reader (VoiceOver/NVDA)
- [ ] High contrast mode
- [ ] Zoom 200%

---

## Execution Timeline

**Week 1**: Phase 1 - Quick Wins
- Day 1-2: CSS Variables + Focus States + Border Radius
- Day 3: Hover States
- Day 4: Collaboration Cursors fix
- Day 5: Testing + fixes

**Week 2**: Phase 2 - UX Improvements
- Day 1-2: Notebook Header redesign
- Day 3: Error Banner + Empty State
- Day 4: Loading Skeletons
- Day 5: Testing + fixes

**Week 3+**: Phase 3 - Features (as prioritized)
- Markdown Anchors
- Image Customization
- Audit Log UX
- Profile Menu

**As Needed**: Phase 4 - Components
- Toast System
- Modal Component
- Tooltip Component

---

## Success Metrics

1. **Accessibility**: 100% keyboard navigation, focus styles on all interactive elements
2. **Consistency**: Hardcode color count → 0 (all use CSS variables)
3. **Performance**: Lighthouse score >90 for all pages
4. **Code Quality**: Reusable components (ErrorBanner, EmptyState, Skeleton, Modal, Tooltip)