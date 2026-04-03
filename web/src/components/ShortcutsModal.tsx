import type React from 'react'
import { Modal } from './Modal'

interface Props { onClose: () => void }

const SHORTCUTS = [
  { key: 'Shift+Enter', action: 'Run focused cell' },
  { key: 'B', action: 'Add code cell' },
  { key: 'A', action: 'Add code cell' },
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
    <Modal title="Keyboard Shortcuts" onClose={onClose}>
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
    </Modal>
  )
}

const styles: Record<string, React.CSSProperties> = {
  table: { width: '100%', borderCollapse: 'collapse', padding: '8px 20px' },
  key: { padding: '8px 20px 8px', width: 160 },
  kbd: { fontFamily: 'var(--font-mono)', fontSize: 11, background: '#f5f5f5', border: '1px solid #e8e8e8', borderRadius: 3, padding: '2px 6px' },
  action: { padding: '8px 20px 8px 0', fontSize: 13, color: 'var(--text-secondary)' },
}
