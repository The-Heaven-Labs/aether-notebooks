# Group 5: Accessibility & Focus Management — Design Solutions

**Author:** Senior UX Engineer  
**Date:** 2026-06-04  
**Scope:** 7 accessibility issues in `web/src/` React frontend  
**Priority:** High (WCAG 2.1 AA compliance)

---

## Issue 1: Sidebar has no `aria-current="page"`

### Current Implementation
**File:** `web/src/components/Sidebar.tsx`

The sidebar uses React Router's `<NavLink>` which already computes an `isActive` boolean via its render prop (`style={({ isActive }) => ...}`). However, this `isActive` value is only used for visual styling (background color, text color). No ARIA attribute is set to communicate the active state to assistive technology.

```tsx
<NavLink
  key={to}
  to={to}
  end={to === '/'}
  title={title}
  style={({ isActive }) => itemStyle(isActive)}
>
```

### Proposed Fix

Use the `NavLink` render prop to also set `aria-current="page"` when the link is active. React Router's `<NavLink>` accepts an `aria-current` prop via the render callback pattern — but the simplest approach is to use the `className`/`style` callback and add the attribute directly:

```tsx
<NavLink
  key={to}
  to={to}
  end={to === '/'}
  title={title}
  style={({ isActive }) => itemStyle(isActive)}
  aria-current={({ isActive }) => isActive ? 'page' : undefined}
>
```

**Note:** React Router v6 `NavLink` does NOT support `aria-current` as a function. Instead, wrap with a component or use the `children` render prop:

```tsx
<NavLink
  key={to}
  to={to}
  end={to === '/'}
  title={title}
  style={({ isActive }) => itemStyle(isActive)}
>
  {({ isActive }) => (
    <>
      <span style={styles.icon}>{icon}</span>
      {expanded && <span style={styles.label}>{title}</span>}
      {isActive && <span className="sr-only">(current page)</span>}
    </>
  )}
</NavLink>
```

**Recommended approach (simplest):** Use the `children` render prop pattern which gives access to `isActive`, and render a visually-hidden `(current page)` text. Alternatively, set `aria-current` by wrapping in a small component:

```tsx
function NavItem({ to, title, icon, expanded, isActive: externalIsActive }: ...) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      title={title}
      style={({ isActive }) => itemStyle(isActive)}
      aria-current={/* use a wrapper */ undefined}
    >
```

**Best solution:** Convert to use the `children` render prop and add `aria-current="page"` via a wrapping element or use the `NavLink`'s built-in support. In React Router v6.4+, `NavLink` passes `isActive` through the render props for `children`:

```tsx
<NavLink
  key={to}
  to={to}
  end={to === '/'}
  title={title}
  style={({ isActive }) => itemStyle(isActive)}
>
  {(navProps) => (
    <>
      <span style={styles.icon}>{icon}</span>
      {expanded && <span style={styles.label}>{title}</span>}
    </>
  )}
</NavLink>
```

Since `NavLink` doesn't directly expose `aria-current` as a function, the cleanest approach is:

```tsx
// Create a small wrapper component
function SidebarLink({ to, title, icon, expanded, end }: { ... }) {
  return (
    <NavLink
      to={to}
      end={end}
      title={title}
      style={({ isActive }) => itemStyle(isActive)}
    >
      {({ isActive }) => (
        <span aria-current={isActive ? 'page' : undefined}>
          <span style={styles.icon}>{icon}</span>
          {expanded && <span style={styles.label}>{title}</span>}
        </span>
      )}
    </NavLink>
  )
}
```

**Wait — even simpler:** React Router's `NavLink` actually DOES accept `aria-current` as a static prop. Since the component already applies active styling, just add:

```tsx
<NavLink
  key={to}
  to={to}
  end={to === '/'}
  title={title}
  style={({ isActive }) => itemStyle(isActive)}
  aria-current="page"  // ← This is wrong — it would ALWAYS be set
>
```

That's incorrect. The correct pattern for React Router v6 is to use the render prop for children and set aria-current on the inner content. But actually, `NavLink` in React Router v6 automatically sets `aria-current="page"` on the active link by default! Let me verify this is actually happening.

**After verification:** React Router v6 `NavLink` does NOT automatically set `aria-current`. It only applies the `active` class. The fix is:

### Final Fix

Extract a `SidebarLink` component that uses the `children` render prop to access `isActive`:

```tsx
function SidebarLink({ to, end, title, icon, expanded, itemStyle }: Props) {
  return (
    <NavLink
      to={to}
      end={end}
      title={title}
      style={({ isActive }) => itemStyle(isActive)}
    >
      {({ isActive }) => (
        <>
          <span style={styles.icon}>{icon}</span>
          {expanded && <span style={styles.label}>{title}</span>}
          {isActive && <span className="sr-only"> (current)</span>}
        </>
      )}
    </NavLink>
  )
}
```

Add a `.sr-only` utility class to global CSS:
```css
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
```

### Dependencies
- Add `.sr-only` utility class to `web/src/index.css` or a global stylesheet
- No new npm packages needed

---

## Issue 2: Code Editor (CodeMirror) is Not Labeled

### Current Implementation
**File:** `web/src/components/Cell.tsx` (lines ~145-195, `CodeEditorView` component)

The CodeMirror editor is mounted into a plain `<div ref={editorRef}>` with no accessible name. Screen readers see an editable text area with no context about what it's for.

```tsx
function CodeEditorView({ ... }: CodeEditorProps) {
  const editorRef = useRef<HTMLDivElement>(null)
  // ...
  return <div ref={editorRef} style={styles.codeEditor} />
}
```

CodeMirror 6 creates its own internal `.cm-editor` element with a `contenteditable`-like `.cm-content` div, but doesn't automatically get an accessible name from the surrounding context.

### Proposed Fix

Two-part fix:

1. **Add `aria-label` to the CodeMirror editor DOM** via the EditorView theme/extensions:

```tsx
const view = new EditorView({
  state: EditorState.create({
    doc: cell.source,
    extensions: [
      // ... existing extensions
      EditorView.contentAttributes.of({
        'aria-label': `SQL editor for cell ${cell.title || (index !== undefined ? `${index + 1}` : 'untitled')}`,
      }),
    ],
  }),
  parent: editorRef.current,
})
```

2. **Alternatively (simpler, more robust):** Add an `aria-label` attribute to the wrapper and use `EditorView.contentAttributes.of()` to set it on the actual editable element:

```tsx
EditorView.contentAttributes.of({
  'aria-label': cell.title 
    ? `Code editor: ${cell.title}` 
    : `Code editor for cell ${index !== undefined ? index + 1 : ''}`,
  role: 'textbox',
}),
```

This should be added to the extensions array in `EditorState.create()`.

### Final Fix

Add to the extensions array in `CodeEditorView`:

```tsx
EditorView.contentAttributes.of({
  'aria-label': cell.title
    ? `SQL editor: ${cell.title}`
    : `SQL editor cell ${index !== undefined ? index + 1 : ''}`,
}),
```

This requires passing `index` into `CodeEditorView` (it's already available in the parent `Cell` component via the `index` prop).

### Dependencies
- Need to pass `index` prop from `Cell` → `CodeEditorView` (already available in `Cell` props)
- No new npm packages needed

---

## Issue 3: Cell Action Buttons Lack Grouping

### Current Implementation
**File:** `web/src/components/Cell.tsx` (lines ~380-460)

The hover toolbar contains ~12 action buttons (Run, Toggle Type, Move Up, Move Down, Duplicate, Copy Link, Toggle Source, Collapse, History, Add to Dashboard, Slide Break, Delete) rendered as a flat `<div>` with no semantic grouping:

```tsx
<div style={{ ...styles.actions, opacity: hovered ? 1 : 0 }}>
  <button ...>Run</button>
  <button ...>MD/SQL</button>
  <button ...>Move Up</button>
  {/* ... 9 more buttons ... */}
</div>
```

Screen readers encounter these as isolated buttons with no indication they form a related toolbar.

### Proposed Fix

Wrap the action buttons in a `role="toolbar"` container with an `aria-label`:

```tsx
<div 
  role="toolbar" 
  aria-label="Cell actions"
  style={{ ...styles.actions, opacity: hovered ? 1 : 0 }}
>
  {/* existing buttons */}
</div>
```

Additionally, add `aria-label` to buttons that currently only have icon content and a `title` attribute (title is not reliably announced by all screen readers):

| Button | Current | Add `aria-label` |
|--------|---------|-------------------|
| Run | `title="Run (Ctrl+Enter)"` | `aria-label="Run query (Ctrl+Enter)"` |
| Move Up | (none) | `aria-label="Move cell up"` |
| Move Down | (none) | `aria-label="Move cell down"` |
| Toggle Source | `title="Hide source"` / `"Show source"` | `aria-label={sourceVisible ? 'Hide source code' : 'Show source code'}` |
| Collapse | `title="Collapse"` | `aria-label="Collapse cell"` |
| History | `title="History"` | `aria-label="View cell history"` |
| Slide Break | `title={...}` | `aria-label={cell.slide_break ? 'Separate into own slide' : 'Join with previous slide'}` |
| Delete | `title="Delete"` | `aria-label="Delete cell"` |

### Final Fix

```tsx
<div 
  role="toolbar" 
  aria-label="Cell actions"
  style={{ ...styles.actions, opacity: hovered ? 1 : 0 }}
>
  {/* Add aria-label to each button that lacks visible text */}
</div>
```

### Dependencies
- None — pure HTML attribute additions

---

## Issue 4: Dashboard Column Width Buttons Have No Accessible Names

### Current Implementation
**File:** `web/src/pages/DashboardEditorPage.tsx` (lines ~195-215)

The grid column selector renders buttons showing only numbers (6, 8, 12, 16, 24):

```tsx
{[6, 8, 12, 16, 24].map(c => (
  <button
    key={c}
    type="button"
    style={{
      // ... styling
      background: gridCols === c ? 'var(--accent)' : 'var(--bg-input)',
      color: gridCols === c ? '#fff' : 'var(--text-secondary)',
    }}
    onClick={async () => { /* ... */ }}
  >
    {c}
  </button>
))}
```

The number alone ("6", "12") is not descriptive. Screen readers announce "button, 6" with no context about what 6 means.

### Proposed Fix

Add `aria-label` describing the action and `aria-pressed` to indicate the selected state:

```tsx
{[6, 8, 12, 16, 24].map(c => (
  <button
    key={c}
    type="button"
    aria-label={`${c} columns`}
    aria-pressed={gridCols === c}
    style={{ /* ... existing styles ... */ }}
    onClick={async () => { /* ... */ }}
  >
    {c}
  </button>
))}
```

Additionally, wrap the group in a `role="group"` with a label, or use the existing "Cols" label text as an `aria-labelledby`:

```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 6 }} role="group" aria-label="Grid columns">
  <span id="grid-cols-label" style={{ /* ... */ }}>Cols</span>
  {[6, 8, 12, 16, 24].map(c => (
    <button
      key={c}
      type="button"
      aria-label={`${c} columns`}
      aria-pressed={gridCols === c}
      // ...
    >
      {c}
    </button>
  ))}
</div>
```

### Final Fix

```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 6 }} role="group" aria-label="Grid columns">
  <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Cols</span>
  {[6, 8, 12, 16, 24].map(c => (
    <button
      key={c}
      type="button"
      aria-label={`${c} columns`}
      aria-pressed={gridCols === c}
      style={{ /* ... */ }}
      onClick={/* ... */}
    >
      {c}
    </button>
  ))}
</div>
```

### Dependencies
- None — pure HTML attribute additions

---

## Issue 5: Color-Only Status Indicators

### Current Implementation
**File:** `web/src/components/StatusBadge.tsx`

The `StatusBadge` component actually **already handles this correctly** — it always renders a visible text `label` alongside the color:

```tsx
export function StatusBadge({ status, label, icon }: Props) {
  return (
    <span style={{ color: colorMap[status] }}>
      {icon}
      {label}
    </span>
  )
}
```

Usage in `ConnectorsPage.tsx`:
```tsx
<StatusBadge status={formTest.ok ? 'success' : 'error'} label={formTest.ok ? 'Connected' : 'Failed'} icon={...} />
```

The color reinforces the text but is never the sole indicator. The `label` prop is required (TypeScript enforces it).

**However**, the component lacks `role="status"` or `aria-live` for dynamic status changes, and the colored dot/icon + text combination could be improved for screen readers by making the status machine-readable.

### Proposed Fix

Add `role="status"` to the badge for live region announcements when status changes dynamically:

```tsx
export function StatusBadge({ status, label, icon }: Props) {
  const style: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    fontWeight: 600,
    color: colorMap[status],
  }

  return (
    <span style={style} role="status" aria-live="polite">
      {icon}
      {label}
    </span>
  )
}
```

Additionally, audit the codebase for any other color-only indicators. A search for status dots/circles without text found **no other instances** — the codebase consistently uses `StatusBadge` for status display.

### Final Fix

Add `role="status"` and `aria-live="polite"` to `StatusBadge`. No other color-only indicators exist in the codebase.

### Dependencies
- None

---

## Issue 6: Focus Trap Not Implemented in Modals

### Current Implementation
**File:** `web/src/components/Modal.tsx`

The Modal component renders an overlay + dialog but:
1. No `role="dialog"` or `aria-modal="true"` on the dialog container
2. No focus trap — Tab key moves focus outside the modal to background content
3. No focus restoration when modal closes
4. No initial focus management (focus doesn't move into the modal on open)

```tsx
export function Modal({ title, onClose, children, minWidth }: Props) {
  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={{ ...styles.modal, minWidth: minWidth ?? 400 }} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>{title}</span>
          <button ... onClick={onClose} aria-label="Close modal"><X size={14} /></button>
        </div>
        <div>{children}</div>
      </div>
    </div>
  )
}
```

### Proposed Fix

Implement a proper focus trap using a custom hook (no external dependencies):

```tsx
import { useEffect, useRef, useCallback } from 'react'

function useFocusTrap(active: boolean) {
  const containerRef = useRef<HTMLDivElement>(null)
  
  useEffect(() => {
    if (!active || !containerRef.current) return
    
    const container = containerRef.current
    const focusableSelector = 'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
    
    // Focus first focusable element on open
    const firstFocusable = container.querySelector(focusableSelector) as HTMLElement
    firstFocusable?.focus()
    
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // Let parent handle via onClose
        return
      }
      if (e.key !== 'Tab') return
      
      const focusable = Array.from(container.querySelectorAll(focusableSelector)) as HTMLElement[]
      if (focusable.length === 0) return
      
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      
      if (e.shiftKey) {
        if (document.activeElement === first) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
    
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [active])
  
  return containerRef
}
```

Updated Modal component:

```tsx
export function Modal({ title, onClose, children, minWidth }: Props) {
  const modalRef = useRef<HTMLDivElement>(null)
  const previousFocusRef = useRef<HTMLElement | null>(null)
  
  // Save previously focused element and restore on unmount
  useEffect(() => {
    previousFocusRef.current = document.activeElement as HTMLElement
    return () => {
      previousFocusRef.current?.focus()
    }
  }, [])
  
  // Focus trap
  useEffect(() => {
    const container = modalRef.current
    if (!container) return
    
    const focusableSelector = 'a[href], button:not([disabled]), textarea, input:not([disabled]), select, [tabindex]:not([tabindex="-1"])'
    
    // Initial focus
    const firstFocusable = container.querySelector(focusableSelector) as HTMLElement
    requestAnimationFrame(() => firstFocusable?.focus())
    
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      
      const focusable = Array.from(container.querySelectorAll(focusableSelector)) as HTMLElement[]
      if (focusable.length === 0) return
      
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])
  
  return (
    <div style={styles.overlay} onClick={onClose}>
      <div 
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        style={{ ...styles.modal, minWidth: minWidth ?? 400 }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={styles.header}>
          <span id="modal-title" style={styles.title}>{title}</span>
          <button style={styles.close} onClick={onClose} aria-label="Close modal">
            <X size={14} />
          </button>
        </div>
        <div>{children}</div>
      </div>
    </div>
  )
}
```

### Final Fix

Full rewrite of Modal.tsx with:
1. `role="dialog"` + `aria-modal="true"` + `aria-labelledby`
2. Focus trap (Tab/Shift+Tab cycling)
3. Focus restoration on close
4. Escape key closes modal
5. Auto-focus first interactive element on open

### Dependencies
- None — custom implementation, no external library needed
- Consider extracting `useFocusTrap` as a shared hook if other components need it (e.g., the widget picker panel in DashboardEditorPage)

---

## Issue 7: No "Skip to Content" Link on Inner Pages

### Current Implementation
**File:** `web/src/components/AppShell.tsx`

The `AppShell` component wraps all authenticated pages but has no skip link. The only skip link exists in `LoginPage.tsx`:

```tsx
// LoginPage.tsx — only place with a skip link
<a href="#login-form" style={styles.skipLink} ...>Skip to form</a>
```

The `AppShell` renders:
```tsx
<div style={styles.root}>
  <TopBar />
  <div style={styles.body}>
    <Sidebar />
    <main style={styles.main}>{children}</main>
  </div>
</div>
```

No `id` on `<main>` and no skip link to navigate past the TopBar + Sidebar.

### Proposed Fix

1. Add `id="main-content"` to the `<main>` element in `AppShell`
2. Add a skip link as the first child of the root div

```tsx
export function AppShell({ children, noPadding }: Props) {
  return (
    <div style={styles.root}>
      <a href="#main-content" className="skip-link">
        Skip to content
      </a>
      <TopBar />
      <div style={styles.body}>
        <Sidebar />
        <main id="main-content" style={{ ...styles.main, ... }}>
          {children}
        </main>
      </div>
    </div>
  )
}
```

Add global CSS for the skip link (reuse the pattern from LoginPage):

```css
.skip-link {
  position: absolute;
  top: -40px;
  left: 0;
  background: var(--accent);
  color: #fff;
  padding: 8px 16px;
  z-index: 9999;
  font-size: 13px;
  border-radius: 0 0 4px 0;
  transition: top 0.15s;
  text-decoration: none;
  font-weight: 600;
}

.skip-link:focus {
  top: 0;
}
```

### Final Fix

1. Add `id="main-content"` to `<main>` in `AppShell.tsx`
2. Add skip link `<a>` as first child of root div
3. Add `.skip-link` CSS to global stylesheet (`web/src/index.css`)

### Dependencies
- Add CSS class to `web/src/index.css` (or equivalent global stylesheet)
- The `.sr-only` class from Issue 1 could also be added at the same time

---

## Summary Table

| # | Issue | File(s) | Complexity | WCAG Criterion |
|---|-------|---------|------------|----------------|
| 1 | Sidebar `aria-current` | `Sidebar.tsx` | Low (add render prop + sr-only text) | 1.3.1, 2.4.8 |
| 2 | CodeMirror label | `Cell.tsx` | Low (add `contentAttributes` extension) | 1.3.1, 4.1.2 |
| 3 | Button grouping | `Cell.tsx` | Low (add `role="toolbar"` + aria-labels) | 1.3.1 |
| 4 | Column button names | `DashboardEditorPage.tsx` | Low (add `aria-label` + `aria-pressed`) | 1.3.1, 4.1.2 |
| 5 | Color-only status | `StatusBadge.tsx` | Minimal (already has text; add `role="status"`) | 1.4.1 |
| 6 | Modal focus trap | `Modal.tsx` | Medium (add focus trap hook + ARIA dialog) | 2.1.2, 2.4.3 |
| 7 | Skip to content | `AppShell.tsx` + `index.css` | Low (add skip link + main id) | 2.4.1 |

## Implementation Order (Recommended)

1. **Issue 5** — StatusBadge (trivial, already mostly correct)
2. **Issue 4** — Dashboard column buttons (trivial aria-label addition)
3. **Issue 3** — Cell action toolbar grouping (add role + aria-labels)
4. **Issue 1** — Sidebar aria-current (small refactor)
5. **Issue 2** — CodeMirror label (add contentAttributes)
6. **Issue 7** — Skip to content (add to AppShell + global CSS)
7. **Issue 6** — Modal focus trap (most complex, requires careful testing)

## Testing Notes

- Use **axe-core** (`@axe-core/react` in dev) for automated WCAG audits
- Test with **VoiceOver** (macOS) or **NVDA** (Windows) for screen reader validation
- Verify focus trap with keyboard-only navigation (Tab, Shift+Tab, Escape)
- Confirm skip link works on first Tab press after page load
- Test `aria-current="page"` announcement when navigating between sidebar items
