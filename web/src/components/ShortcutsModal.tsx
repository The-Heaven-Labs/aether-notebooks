import type React from 'react'
import { Modal } from './Modal'

interface ShortcutEntry {
  key: string
  action: string
}

interface Props {
  onClose: () => void
  extraShortcuts?: ShortcutEntry[]
}

const NOTEBOOK_SHORTCUTS: ShortcutEntry[] = [
  { key: 'Enter', action: 'Enter edit mode (cursor at end)' },
  { key: 'Escape', action: 'Exit edit mode / close modal' },
  { key: 'Shift+Enter', action: 'Run focused cell' },
  { key: 'Ctrl+Enter (in editor)', action: 'Run cell' },
  { key: 'J / ↓', action: 'Move focus down' },
  { key: 'K / ↑', action: 'Move focus up' },
  { key: 'Ctrl+↑', action: 'Move cell up' },
  { key: 'Ctrl+↓', action: 'Move cell down' },
  { key: 'B', action: 'Add code cell below' },
  { key: 'A', action: 'Add code cell above' },
  { key: 'D D', action: 'Delete cell' },
  { key: 'Shift+D', action: 'Duplicate cell' },
  { key: 'M', action: 'Convert to markdown' },
  { key: 'Y', action: 'Convert to code' },
  { key: 'Shift+M', action: 'Toggle slide break' },
  { key: 'Ctrl+Shift+F (in SQL editor)', action: 'Format SQL' },
]

const GLOBAL_SHORTCUTS: ShortcutEntry[] = [
  { key: '?', action: 'Show keyboard shortcuts' },
]

export function ShortcutsModal({ onClose, extraShortcuts }: Props) {
  return (
    <Modal title="Keyboard Shortcuts" onClose={onClose}>
      <div style={styles.body}>
        <div style={styles.section}>
          <div style={styles.sectionTitle}>Global</div>
          <table style={styles.table}>
            <tbody>
              {GLOBAL_SHORTCUTS.map(({ key, action }) => (
                <tr key={key}>
                  <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                  <td style={styles.action}>{action}</td>
                </tr>
              ))}
              {extraShortcuts?.map(({ key, action }) => (
                <tr key={`extra-${key}`}>
                  <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                  <td style={styles.action}>{action}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionTitle}>Notebook Editor</div>
          <table style={styles.table}>
            <tbody>
              {NOTEBOOK_SHORTCUTS.map(({ key, action }) => (
                <tr key={key}>
                  <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                  <td style={styles.action}>{action}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </Modal>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { padding: '8px 0' },
  section: { marginBottom: 16 },
  sectionTitle: { fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '8px 20px 4px' },
  table: { width: '100%', borderCollapse: 'collapse' },
  key: { padding: '6px 20px 6px', width: 200 },
  kbd: { fontFamily: 'var(--font-mono)', fontSize: 11, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 3, padding: '2px 6px', color: 'var(--text-primary)' },
  action: { padding: '6px 20px 6px 0', fontSize: 13, color: 'var(--text-secondary)' },
}
