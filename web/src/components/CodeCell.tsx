import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { format } from 'sql-formatter'
import type { Cell, Connector } from '../types'
import { CellToolbar } from './CellToolbar'
import { OutputRenderer } from './OutputRenderer'

interface Props {
  cell: Cell
  connectors: Connector[]
  onRun: (cellId: string) => void
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onAssignConnector: (cellId: string, connectorId: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  running: boolean
}

export function CodeCell({ cell, connectors, onRun, onDelete, onSourceChange, onAssignConnector, onMoveUp, onMoveDown, onSwitchType, running }: Props) {
  const editorRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onRunRef = useRef(onRun)
  const onSourceChangeRef = useRef(onSourceChange)
  onRunRef.current = onRun
  onSourceChangeRef.current = onSourceChange

  useEffect(() => {
    if (!editorRef.current) return

    const cellKeymap = keymap.of([
      {
        key: 'Mod-Enter',
        run: () => {
          onRunRef.current(cell.id)
          return true
        },
      },
      {
        key: 'Mod-Shift-f',
        run: (view) => {
          const raw = view.state.doc.toString()
          try {
            const formatted = format(raw, { language: 'sql', tabWidth: 2 })
            view.dispatch({
              changes: { from: 0, to: view.state.doc.length, insert: formatted },
            })
            onSourceChangeRef.current(cell.id, formatted)
          } catch {
            // leave as-is if formatting fails
          }
          return true
        },
      },
      ...defaultKeymap,
    ])

    const view = new EditorView({
      state: EditorState.create({
        doc: cell.source,
        extensions: [
          cellKeymap,
          sql(),
          syntaxHighlighting(defaultHighlightStyle),
          EditorView.theme({
            '&': { fontFamily: 'var(--font-mono)', fontSize: '13px' },
            '.cm-content': { padding: '14px 16px', minHeight: '72px' },
            '.cm-line': { lineHeight: '1.65' },
            '.cm-focused': { outline: 'none' },
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onSourceChangeRef.current(cell.id, update.state.doc.toString())
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
        onSwitchType={onSwitchType}
        running={running}
        connectors={connectors}
        connectorId={cell.connector_id}
        onAssignConnector={(cid) => onAssignConnector(cell.id, cid)}
      />
      <div style={styles.editor} ref={editorRef} />
      <OutputRenderer outputs={cell.outputs} />
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
  editor: {
    borderBottom: '1px solid var(--border-light)',
    background: '#fdfcfb',
  },
}
