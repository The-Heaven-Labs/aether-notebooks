import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { format } from 'sql-formatter'
import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'
import { yCollab } from 'y-codemirror.next'
import type { Cell, Connector } from '../types'
import { CellToolbar } from './CellToolbar'
import { CellHeader } from './CellHeader'
import { OutputRenderer } from './OutputRenderer'

const RELAY_URL = import.meta.env.VITE_RELAY_URL || 'ws://localhost:3001'

// Module-level cache: notebookId -> { doc, provider }
// One Y.Doc + HocuspocusProvider shared across all cells in the same notebook.
interface NotebookCollab {
  doc: Y.Doc
  provider: HocuspocusProvider
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
  const userName = localStorage.getItem('hnb_user_name') ?? ''
  const userEmail = localStorage.getItem('hnb_user_email') ?? ''

  // HocuspocusProvider speaks the Hocuspocus v2 protocol:
  // it sends an auth message (containing the JWT) before any Yjs sync frames,
  // which is what @hocuspocus/server's onAuthenticate hook expects.
  // Construction is synchronous (the WebSocket connects on the next tick),
  // so no try/catch is needed here.
  const provider = new HocuspocusProvider({
    url: RELAY_URL,
    name: notebookId,
    document: doc,
    token,
    onAuthenticationFailed: () => console.warn('[yjs] Relay auth failed — collaborative editing disabled'),
  })

  // Set user identity so remote cursors show the real name
  provider.awareness?.setLocalStateField('user', {
    name: userName || userEmail || 'Anonymous',
    email: userEmail,
    color: `hsl(${Math.abs(hashStr(userEmail || userName)) % 360}, 70%, 55%)`,
  })

  const entry: NotebookCollab = { doc, provider, refCount: 1 }
  collabCache.set(notebookId, entry)
  return entry
}

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return h
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

interface SaveState {
  saving: boolean
  savedAt: Date | null
  error: string | null
}

function fmtTime(date: Date): string {
  const now = Date.now()
  const diffMs = now - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 5) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
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
  saveState?: SaveState
  runAt?: Date
  onUpdateCellMeta?: (updates: Partial<Pick<Cell, 'source_visible' | 'cell_collapsed' | 'title' | 'description' | 'slug'>>) => void
  onShowHistory?: () => void
  onFocus?: (cellId: string) => void
}

export function CodeCell({ cell, connectors, notebookId, onRun, onDelete, onSourceChange, onAssignConnector, onMoveUp, onMoveDown, onSwitchType, running, saveState, runAt, onUpdateCellMeta, onShowHistory, onFocus }: Props) {
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
  }, [cell.id, notebookId, cell.cell_collapsed])

  if (cell.cell_collapsed) {
    return (
      <div style={styles.cellCollapsed}>
        <span style={styles.collapsedLabel}>{cell.title || 'Code cell'}</span>
        <button style={styles.expandBtn} onClick={() => onUpdateCellMeta?.({ cell_collapsed: false })}>▷ Expand</button>
      </div>
    )
  }

  return (
    <div style={styles.cell} onClick={() => onFocus?.(cell.id)}>
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
        sourceVisible={cell.source_visible ?? true}
        cellCollapsed={cell.cell_collapsed ?? false}
        onToggleSourceVisible={(v) => onUpdateCellMeta?.({ source_visible: v })}
        onToggleCellCollapsed={(v) => onUpdateCellMeta?.({ cell_collapsed: v })}
        onShowHistory={() => onShowHistory?.()}
      />
      <CellHeader
        cell={cell}
        onUpdateCell={(updates) => onUpdateCellMeta?.(updates)}
      />
      <div style={{ ...styles.editor, ...(!(cell.source_visible ?? true) ? { display: 'none' } : {}) }} ref={editorRef} />
      <OutputRenderer outputs={cell.outputs} />
      <div style={styles.statusBar}>
        <span style={saveState?.error ? styles.statusError : styles.statusSave}>
          {saveState?.saving
            ? 'Saving…'
            : saveState?.error
              ? `Save failed: ${saveState.error}`
              : saveState?.savedAt
                ? `Saved ${fmtTime(saveState.savedAt)}`
                : ''}
        </span>
        {runAt && (
          <span style={styles.statusRun}>Last run: {runAt.toLocaleTimeString()}</span>
        )}
      </div>
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
  statusBar: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: '4px 16px',
    fontSize: 11,
    minHeight: 24,
    background: '#faf9f7',
    borderTop: '1px solid var(--border-light)',
  },
  statusSave: {
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  statusError: {
    color: 'var(--error)',
    fontFamily: 'var(--font-mono)',
  },
  statusRun: {
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  cellCollapsed: { border: '1px solid var(--border)', borderRadius: 10, background: 'var(--bg-secondary)', padding: '6px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', boxShadow: 'var(--shadow-sm)' },
  collapsedLabel: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  expandBtn: { fontSize: 12, background: 'transparent', border: 'none', color: 'var(--accent)', cursor: 'pointer' },
}
