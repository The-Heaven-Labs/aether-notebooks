import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Cell } from '../types'
import { CellToolbar } from './CellToolbar'

interface Props {
  cell: Cell
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
}

export function TextCell({ cell, onDelete, onSourceChange, onMoveUp, onMoveDown, onSwitchType }: Props) {
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
      />
      {editing ? (
        <textarea
          style={styles.textarea}
          value={cell.source}
          onChange={(e) => onSourceChange(cell.id, e.target.value)}
          onBlur={() => setEditing(false)}
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
}
