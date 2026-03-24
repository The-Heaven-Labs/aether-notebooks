import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { Cell } from '../types'
import { CellToolbar } from './CellToolbar'
import { OutputRenderer } from './OutputRenderer'

interface Props {
  cell: Cell
  onRun: (cellId: string) => void
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  running: boolean
}

export function CodeCell({ cell, onRun, onDelete, onSourceChange, onMoveUp, onMoveDown, running }: Props) {
  const editorRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)

  useEffect(() => {
    if (!editorRef.current) return

    const view = new EditorView({
      state: EditorState.create({
        doc: cell.source,
        extensions: [
          keymap.of(defaultKeymap),
          sql(),
          EditorView.theme({
            '&': { fontFamily: 'var(--font-mono)', fontSize: '13px' },
            '.cm-content': { padding: '10px 14px', minHeight: '60px' },
            '.cm-line': { lineHeight: '1.6' },
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onSourceChange(cell.id, update.state.doc.toString())
            }
          }),
        ],
      }),
      parent: editorRef.current,
    })
    viewRef.current = view
    return () => view.destroy()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cell.id])

  return (
    <div style={styles.cell}>
      <CellToolbar
        cellType="code"
        onRun={() => onRun(cell.id)}
        onDelete={() => onDelete(cell.id)}
        onMoveUp={onMoveUp}
        onMoveDown={onMoveDown}
        running={running}
      />
      <div style={styles.editor} ref={editorRef} />
      <OutputRenderer outputs={cell.outputs} />
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  cell: {
    border: '1px solid var(--border)',
    borderRadius: 8,
    background: 'var(--bg-cell-code)',
    overflow: 'hidden',
  },
  editor: { borderBottom: cell => cell ? '1px solid var(--border)' : 'none' } as never,
}
