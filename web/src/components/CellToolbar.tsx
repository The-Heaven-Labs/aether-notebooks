import type { Connector } from '../types'

interface Props {
  onRun: () => void
  onDelete: () => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  running: boolean
  cellType: 'code' | 'text'
  connectors?: Connector[]
  connectorId?: string
  onAssignConnector?: (connectorId: string) => void
  sourceVisible: boolean
  cellCollapsed: boolean
  onToggleSourceVisible: (val: boolean) => void
  onToggleCellCollapsed: (val: boolean) => void
  onShowHistory: () => void
}

export function CellToolbar({ onRun, onDelete, onMoveUp, onMoveDown, onSwitchType, running, cellType, connectors, connectorId, onAssignConnector, sourceVisible, cellCollapsed, onToggleSourceVisible, onToggleCellCollapsed, onShowHistory }: Props) {
  return (
    <div style={styles.toolbar}>
      <div style={styles.left}>
        {cellType === 'code' ? (
          <button style={styles.runBtn} onClick={onRun} disabled={running} title="Run cell (Ctrl+Enter)">
            {running ? '⏳' : '▶ Run'}
          </button>
        ) : null}
        <button
          style={styles.typeBtn}
          onClick={onSwitchType}
          title={cellType === 'code' ? 'Switch to markdown cell' : 'Switch to SQL cell'}
        >
          {cellType === 'code' ? 'SQL' : 'MD'}
          <span style={styles.switchHint}>⇄</span>
        </button>
        {cellType === 'code' && connectors && connectors.length > 0 && (
          <select
            style={{
              ...styles.connectorSelect,
              color: connectorId ? 'var(--text-primary)' : 'var(--text-muted)',
            }}
            value={connectorId ?? ''}
            onChange={(e) => onAssignConnector?.(e.target.value)}
            title="Assign connector"
          >
            <option value="" disabled>No connector</option>
            {connectors.map((c) => (
              <option key={c.id} value={c.id}>{c.name}</option>
            ))}
          </select>
        )}
      </div>
      <div style={styles.right}>
        {onMoveUp && (
          <button style={styles.iconBtn} onClick={onMoveUp} title="Move up">↑</button>
        )}
        {onMoveDown && (
          <button style={styles.iconBtn} onClick={onMoveDown} title="Move down">↓</button>
        )}
        <button
          style={styles.iconBtn}
          onClick={() => onToggleSourceVisible(!sourceVisible)}
          title={sourceVisible ? 'Hide source' : 'Show source'}
        >
          {sourceVisible ? '⊟' : '⊞'}
        </button>
        <button
          style={styles.iconBtn}
          onClick={() => onToggleCellCollapsed(!cellCollapsed)}
          title={cellCollapsed ? 'Expand cell' : 'Collapse cell'}
        >
          {cellCollapsed ? '▷' : '▽'}
        </button>
        <button
          style={styles.iconBtn}
          onClick={onShowHistory}
          title="Cell history"
        >
          ⏱
        </button>
        <button style={styles.deleteBtn} onClick={onDelete} title="Delete cell">✕</button>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '5px 10px',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
    minHeight: 36,
  },
  left: { display: 'flex', alignItems: 'center', gap: 6 },
  right: { display: 'flex', alignItems: 'center', gap: 3 },
  runBtn: {
    padding: '4px 12px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 5,
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    letterSpacing: '0.02em',
    fontFamily: 'var(--font-sans)',
  },
  typeBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 10,
    fontWeight: 700,
    color: 'var(--text-muted)',
    letterSpacing: '0.08em',
    fontFamily: 'var(--font-mono)',
    padding: '2px 7px',
    background: 'var(--border)',
    border: '1px solid transparent',
    borderRadius: 4,
    cursor: 'pointer',
    transition: 'background 0.15s, color 0.15s',
  },
  switchHint: {
    fontSize: 11,
    opacity: 0.5,
    fontFamily: 'var(--font-sans)',
  },
  connectorSelect: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    fontWeight: 600,
    padding: '2px 6px',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    outline: 'none',
    maxWidth: 180,
  },
  iconBtn: {
    padding: '3px 7px',
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    lineHeight: 1,
  },
  deleteBtn: {
    padding: '3px 7px',
    background: 'transparent',
    border: '1px solid transparent',
    borderRadius: 4,
    fontSize: 12,
    cursor: 'pointer',
    color: 'var(--text-muted)',
    lineHeight: 1,
    marginLeft: 2,
  },
}
