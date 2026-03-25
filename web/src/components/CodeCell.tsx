import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { format } from 'sql-formatter'
import * as Y from 'yjs'
import { WebsocketProvider } from 'y-websocket'
import { yCollab } from 'y-codemirror.next'
import type { Cell, Connector } from '../types'
import { CellToolbar } from './CellToolbar'
import { OutputRenderer } from './OutputRenderer'

const RELAY_URL = import.meta.env.VITE_RELAY_URL || 'ws://localhost:3001'

// Module-level cache: notebookId -> { doc, provider }
// This ensures a single Y.Doc + WebsocketProvider per notebook across all cells.
interface NotebookCollab {
  doc: Y.Doc
  provider: WebsocketProvider
  refCount: number
}
const collabCache = new Map<string, NotebookCollab>()

function getOrCreateCollab(notebookId: string): NotebookCollab {
  const existing = collabCache.get(notebookId)
  if (existing) {
    existing.refCount++
    return existing
  }

  const doc = new Y.Doc()
  const token = localStorage.getItem('hnb_token') ?? ''

  let provider: WebsocketProvider
  try {
    provider = new WebsocketProvider(RELAY_URL, notebookId, doc, {
      params: { token },
    })
  } catch (err) {
    // If provider construction fails (e.g., invalid URL in test env), create a
    // disconnected stub so the editor still works without collaboration.
    console.warn('[yjs] Failed to create WebsocketProvider:', err)
    // Create provider but immediately disconnect
    provider = new WebsocketProvider(RELAY_URL, notebookId, doc, {
      connect: false,
      params: { token },
    })
  }

  const entry: NotebookCollab = { doc, provider, refCount: 1 }
  collabCache.set(notebookId, entry)
  return entry
}

function releaseCollab(notebookId: string): void {
  const entry = collabCache.get(notebookId)
  if (!entry) return
  entry.refCount--
  if (entry.refCount <= 0) {
    try {
      entry.provider.destroy()
    } catch {
      // ignore
    }
    entry.doc.destroy()
    collabCache.delete(notebookId)
  }
}

interface Props {
  cell: Cell
  connectors: Connector[]
  notebookId: string
  onRun: (cellId: string) => void
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onAssignConnector: (cellId: string, connectorId: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  running: boolean
}

export function CodeCell({ cell, connectors, notebookId, onRun, onDelete, onSourceChange, onAssignConnector, onMoveUp, onMoveDown, onSwitchType, running }: Props) {
  const editorRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onRunRef = useRef(onRun)
  const onSourceChangeRef = useRef(onSourceChange)
  onRunRef.current = onRun
  onSourceChangeRef.current = onSourceChange

  useEffect(() => {
    if (!editorRef.current) return

    // Acquire shared Y.Doc + provider for this notebook
    const collab = getOrCreateCollab(notebookId)
    const ytext = collab.doc.getText(`cell:${cell.id}`)

    // Seed the Y.Text with the cell's initial source only when it is empty
    // (i.e., first time this cell is opened or the doc is freshly synced).
    // We do this once synchronously before the editor is created so that the
    // editor's initial content matches what is in the shared doc.
    if (ytext.length === 0 && cell.source) {
      collab.doc.transact(() => {
        ytext.insert(0, cell.source)
      })
    }

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
            // Apply the formatted text through the Y.Text to keep Yjs in sync
            collab.doc.transact(() => {
              ytext.delete(0, ytext.length)
              ytext.insert(0, formatted)
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
        // doc is managed by yCollab — pass empty string; Y.Text is the source of truth
        doc: ytext.toString(),
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
          // yCollab wires Y.Text <-> CodeMirror document bidirectionally
          yCollab(ytext, collab.provider.awareness),
          // Also fire the local state callback so NotebookPage stays in sync
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

    return () => {
      view.destroy()
      releaseCollab(notebookId)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cell.id, notebookId])

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
