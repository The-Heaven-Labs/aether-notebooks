import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Cell } from '../types'
import { CellToolbar } from './CellToolbar'

interface SaveState {
  saving: boolean
  savedAt: Date | null
  error: string | null
}

function fmtTime(date: Date): string {
  const now = Date.now()
  const diffSec = Math.floor((now - date.getTime()) / 1000)
  if (diffSec < 5) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

interface Props {
  cell: Cell
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onSave?: (cellId: string, source: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  saveState?: SaveState
}

export function TextCell({ cell, onDelete, onSourceChange, onSave, onMoveUp, onMoveDown, onSwitchType, saveState }: Props) {
  const [editing, setEditing] = useState(!cell.source)

  return (
    <div style={styles.cell}>
      <CellToolbar
        cellType="text"
        onRun={() => {}}
        onDelete={() => onDelete(cell.id)}
        onMoveUp={onMoveUp}
        onMoveDown={onMoveDown}
        onSwitchType={onSwitchType}
        running={false}
        sourceVisible={cell.source_visible ?? true}
        cellCollapsed={cell.cell_collapsed ?? false}
        onToggleSourceVisible={() => {}}
        onToggleCellCollapsed={() => {}}
        onShowHistory={() => {}}
      />
      {editing ? (
        <textarea
          style={styles.textarea}
          value={cell.source}
          onChange={(e) => onSourceChange(cell.id, e.target.value)}
          onBlur={() => { setEditing(false); onSave?.(cell.id, cell.source) }}
          autoFocus
          placeholder="Write markdown here…"
        />
      ) : (
        <div style={styles.preview} onClick={() => setEditing(true)}>
          {cell.source ? (
            <div style={styles.markdownBody}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{cell.source}</ReactMarkdown>
            </div>
          ) : (
            <p style={styles.placeholder}>Click to edit…</p>
          )}
        </div>
      )}
      {saveState && (
        <div style={styles.statusBar}>
          <span style={saveState.error ? styles.statusError : styles.statusSave}>
            {saveState.saving
              ? 'Saving…'
              : saveState.error
                ? `Save failed: ${saveState.error}`
                : saveState.savedAt
                  ? `Saved ${fmtTime(saveState.savedAt)}`
                  : ''}
          </span>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  cell: {
    border: '1px solid var(--border)',
    borderRadius: 10,
    background: 'white',
    overflow: 'hidden',
    boxShadow: 'var(--shadow-sm)',
  },
  textarea: {
    width: '100%',
    minHeight: 120,
    padding: '14px 16px',
    border: 'none',
    outline: 'none',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    background: '#fefdf9',
    resize: 'vertical',
    lineHeight: 1.65,
    color: 'var(--text-primary)',
  },
  preview: {
    padding: '16px 20px',
    cursor: 'text',
    minHeight: 52,
  },
  markdownBody: {
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
  },
  placeholder: {
    color: 'var(--text-muted)',
    fontStyle: 'italic',
    fontSize: 14,
  },
  statusBar: {
    padding: '4px 16px',
    fontSize: 11,
    minHeight: 24,
    background: '#faf9f7',
    borderTop: '1px solid var(--border-light)',
    display: 'flex',
    alignItems: 'center',
  },
  statusSave: {
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  statusError: {
    color: 'var(--error)',
    fontFamily: 'var(--font-mono)',
  },
}
