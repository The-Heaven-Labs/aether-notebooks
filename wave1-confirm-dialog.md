# Wave 1 — ConfirmDialog Component (Item 6)

## Status: ✅ Complete

## What was done
Created `web/src/components/ConfirmDialog.tsx` — a reusable, themed confirmation dialog component.

## Component API
```tsx
interface ConfirmDialogProps {
  open: boolean
  title: string
  message?: string
  confirmLabel?: string   // default: 'Confirm'
  cancelLabel?: string    // default: 'Cancel'
  destructive?: boolean   // red confirm button for dangerous actions
  onConfirm: () => void
  onCancel: () => void
}
```

## Features
- **Theme-aware**: Uses CSS variables (`--bg-card`, `--border`, `--accent`, `--error-full`, etc.) — works in both light and dark mode
- **Destructive mode**: `destructive={true}` turns confirm button red (`var(--error-full)`)
- **Keyboard support**: ESC key cancels the dialog
- **Backdrop click**: Clicking overlay triggers onCancel
- **Auto-focus**: Confirm button gets focus when dialog opens
- **Stop propagation**: Modal body click doesn't bubble to overlay

## Commit
- Hash: `b2cebaa`
- Message: `feat: add ConfirmDialog component (item 6)`

## Integration notes
The component is ready to replace `window.confirm()` calls across the codebase. The design doc identifies 14 locations. Example migration pattern:

```tsx
// Before:
if (confirm('Delete this cell?')) deleteCell.mutate(cid)

// After:
const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
// ...
<button onClick={() => setDeleteConfirm(cid)}>Delete</button>
<ConfirmDialog
  open={deleteConfirm !== null}
  title="Delete cell?"
  message="This cannot be undone."
  destructive
  onConfirm={() => { deleteCell.mutate(deleteConfirm!); setDeleteConfirm(null) }}
  onCancel={() => setDeleteConfirm(null)}
/>
```

## Files changed
- `web/src/components/ConfirmDialog.tsx` (new, 136 lines)
