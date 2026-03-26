import type React from 'react'

interface Props { onClose: () => void }

const SHORTCUTS = [
  { key: 'Shift+Enter', action: 'Run focused cell' },
  { key: 'B', action: 'Add cell below' },
  { key: 'A', action: 'Add cell above' },
  { key: 'D D', action: 'Delete cell' },
  { key: 'J / ↓', action: 'Move focus down' },
  { key: 'K / ↑', action: 'Move focus up' },
  { key: 'M', action: 'Convert to markdown' },
  { key: 'Y', action: 'Convert to code' },
  { key: '?', action: 'Show this modal' },
  { key: 'Ctrl+Enter (in editor)', action: 'Run cell' },
  { key: 'Ctrl+Shift+F (in SQL editor)', action: 'Format SQL' },
  { key: 'Escape (in editor)', action: 'Exit cell edit mode' },
]

export function ShortcutsModal({ onClose }: Props) {
  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>Keyboard Shortcuts</span>
          <button style={styles.close} onClick={onClose}>✕</button>
        </div>
        <table style={styles.table}>
          <tbody>
            {SHORTCUTS.map(({ key, action }) => (
              <tr key={key}>
                <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                <td style={styles.action}>{action}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 },
  modal: { background: 'white', borderRadius: 12, boxShadow: '0 8px 32px rgba(0,0,0,0.2)', minWidth: 400, maxHeight: '80vh', overflow: 'auto' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 20px', borderBottom: '1px solid var(--border-light)' },
  title: { fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'transparent', border: 'none', fontSize: 14, cursor: 'pointer', color: 'var(--text-muted)' },
  table: { width: '100%', borderCollapse: 'collapse', padding: '8px 20px' },
  key: { padding: '8px 20px 8px', width: 160 },
  kbd: { fontFamily: 'var(--font-mono)', fontSize: 12, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 6px' },
  action: { padding: '8px 20px 8px 0', fontSize: 13, color: 'var(--text-secondary)' },
}
