import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Cell } from '../types'
import { CellToolbar } from './CellToolbar'

interface Props {
  cell: Cell
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
}

export function TextCell({ cell, onDelete, onSourceChange, onMoveUp, onMoveDown }: Props) {
  const [editing, setEditing] = useState(!cell.source)

  return (
    <div style={styles.cell}>
      <CellToolbar
        cellType="text"
        onRun={() => {}}
        onDelete={() => onDelete(cell.id)}
        onMoveUp={onMoveUp}
        onMoveDown={onMoveDown}
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
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{cell.source}</ReactMarkdown>
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
    borderRadius: 8,
    background: 'var(--bg-cell-text)',
    overflow: 'hidden',
  },
  textarea: {
    width: '100%',
    minHeight: 120,
    padding: '12px 16px',
    border: 'none',
    outline: 'none',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    background: 'transparent',
    resize: 'vertical',
  },
  preview: {
    padding: '12px 16px',
    cursor: 'text',
    minHeight: 48,
    lineHeight: 1.7,
    fontSize: 14,
  },
  placeholder: {
    color: 'var(--text-secondary)',
    fontStyle: 'italic',
  },
}
