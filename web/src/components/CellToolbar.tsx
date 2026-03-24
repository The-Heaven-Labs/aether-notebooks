interface Props {
  onRun: () => void
  onDelete: () => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  running: boolean
  cellType: 'code' | 'text'
}

export function CellToolbar({ onRun, onDelete, onMoveUp, onMoveDown, running, cellType }: Props) {
  return (
    <div style={styles.toolbar}>
      <div style={styles.left}>
        {cellType === 'code' && (
          <button style={styles.runBtn} onClick={onRun} disabled={running} title="Run cell">
            {running ? '⏳' : '▶'}
          </button>
        )}
      </div>
      <div style={styles.right}>
        {onMoveUp && (
          <button style={styles.iconBtn} onClick={onMoveUp} title="Move up">↑</button>
        )}
        {onMoveDown && (
          <button style={styles.iconBtn} onClick={onMoveDown} title="Move down">↓</button>
        )}
        <button style={{ ...styles.iconBtn, color: 'var(--error)' }} onClick={onDelete} title="Delete">✕</button>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '4px 10px',
    borderBottom: '1px solid var(--border)',
    background: 'rgba(255,255,255,0.5)',
  },
  left: { display: 'flex', gap: 6 },
  right: { display: 'flex', gap: 4 },
  runBtn: {
    padding: '3px 10px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    cursor: 'pointer',
  },
  iconBtn: {
    padding: '3px 7px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
}
