import { useState, useEffect, useRef } from 'react'
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff, ChevronRight, Clock, X, SeparatorHorizontal, Copy } from 'lucide-react'
import { Compartment, EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { format } from 'sql-formatter'
import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'
import { yCollab } from 'y-codemirror.next'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { OutputRenderer } from './OutputRenderer'
import type { Cell, Connector } from '../types'

// ── Yjs collaboration cache (shared across all cells in a notebook) ───────────

const RELAY_URL = import.meta.env.VITE_RELAY_URL || 'ws://localhost:3001'

interface NotebookCollab {
  doc: Y.Doc
  provider: HocuspocusProvider
  refCount: number
  synced: boolean
}
const collabCache = new Map<string, NotebookCollab>()

function getOrCreateCollab(notebookId: string): NotebookCollab {
  const existing = collabCache.get(notebookId)
  if (existing) { existing.refCount++; return existing }

  const doc = new Y.Doc()
  const token = localStorage.getItem('hnb_token') ?? ''
  const userName = localStorage.getItem('hnb_user_name') ?? ''
  const userEmail = localStorage.getItem('hnb_user_email') ?? ''

  const provider = new HocuspocusProvider({
    url: RELAY_URL,
    name: notebookId,
    document: doc,
    token,
    onAuthenticationFailed: () => console.warn('[yjs] Relay auth failed'),
  })

  provider.awareness?.setLocalStateField('user', {
    name: userName || userEmail || 'Anonymous',
    email: userEmail,
    color: `hsl(${Math.abs(hashStr(userEmail || userName)) % 360}, 70%, 55%)`,
  })

  const entry: NotebookCollab = { doc, provider, refCount: 1, synced: false }
  provider.on('synced', ({ state }: { state: boolean }) => { if (state) entry.synced = true })
  collabCache.set(notebookId, entry)
  return entry
}

function releaseCollab(notebookId: string) {
  const entry = collabCache.get(notebookId)
  if (!entry) return
  entry.refCount--
  if (entry.refCount <= 0) {
    try { entry.provider.destroy() } catch { /* ignore */ }
    entry.doc.destroy()
    collabCache.delete(notebookId)
  }
}

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return h
}

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w-]/g, '')
}

const markdownComponents = {
  h1: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h1 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h1>
    )
  },
  h2: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h2 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h2>
    )
  },
  h3: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h3 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h3>
    )
  },
  h4: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h4 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h4>
    )
  },
  h5: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h5 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h5>
    )
  },
  h6: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
    const text = children?.toString() || ''
    const id = slugify(text)
    return (
      <h6 id={id} {...props}>
        <a href={`#${id}`} style={styles.headerAnchor}>#</a>
        {children}
      </h6>
    )
  },
}

// ── Types ─────────────────────────────────────────────────────────────────────

export interface SaveState {
  saving: boolean
  savedAt: Date | null
  error: string | null
}

interface Props {
  cell: Cell
  connectors: Connector[]
  notebookId: string
  onRun: (cellId: string) => void
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onSave?: (cellId: string, source: string) => void
  onAssignConnector: (cellId: string, connectorId: string) => void
  onClearConnector?: (cellId: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  onDuplicate?: () => void
  running?: boolean
  saveState?: SaveState
  runAt?: Date
  onUpdateCellMeta?: (updates: Partial<Pick<Cell, 'source_visible' | 'cell_collapsed' | 'slide_break' | 'title' | 'description' | 'slug'>>) => void
  onShowHistory?: () => void
  onFocus?: (cellId: string) => void
}

// ── Helper ────────────────────────────────────────────────────────────────────

function fmtTime(date: Date): string {
  const diffSec = Math.floor((Date.now() - date.getTime()) / 1000)
  if (diffSec < 5) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// ── CodeEditorView ────────────────────────────────────────────────────────────
// Embeds the full CodeMirror + Yjs editor (behaviour identical to former CodeCell)

interface CodeEditorProps {
  cell: Cell
  notebookId: string
  onRun: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  collapsed: boolean
}

function CodeEditorView({ cell, notebookId, onRun, onSourceChange, collapsed }: CodeEditorProps) {
  const editorRef = useRef<HTMLDivElement>(null)
  const onRunRef = useRef(onRun)
  const onSourceChangeRef = useRef(onSourceChange)
  onRunRef.current = onRun
  onSourceChangeRef.current = onSourceChange
  const collabCompartment = useRef(new Compartment())

  useEffect(() => {
    if (!editorRef.current) return

    const collab = getOrCreateCollab(notebookId)
    const ytext = collab.doc.getText(`cell:${cell.id}`)
    const compartment = collabCompartment.current

    const cellKeymap = keymap.of([
      { key: 'Mod-Enter', run: () => { onRunRef.current(cell.id); return true } },
      {
        key: 'Mod-Shift-f',
        run: (view) => {
          const raw = view.state.doc.toString()
          try {
            const formatted = format(raw, { language: 'sql', tabWidth: 2 })
            collab.doc.transact(() => { ytext.delete(0, ytext.length); ytext.insert(0, formatted) })
            onSourceChangeRef.current(cell.id, formatted)
          } catch { /* leave as-is */ }
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
            '.cm-editor': { background: '#f7f7f7' },
            '.cm-gutters': { display: 'none' },
          }),
          compartment.of([]),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onSourceChangeRef.current(cell.id, update.state.doc.toString())
          }),
        ],
      }),
      parent: editorRef.current,
    })

    const attachCollab = () => {
      if (ytext.length === 0) {
        collab.doc.transact(() => ytext.insert(0, view.state.doc.toString()))
      } else if (ytext.toString() !== view.state.doc.toString()) {
        view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: ytext.toString() } })
      }
      view.dispatch({ effects: compartment.reconfigure(yCollab(ytext, collab.provider.awareness)) })
    }

    let onSynced: (({ state }: { state: boolean }) => void) | null = null
    if (collab.synced) {
      attachCollab()
    } else {
      onSynced = ({ state }: { state: boolean }) => { if (state) attachCollab() }
      collab.provider.on('synced', onSynced)
    }

    return () => {
      if (onSynced) collab.provider.off('synced', onSynced)
      view.destroy()
      releaseCollab(notebookId)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cell.id, notebookId, collapsed])

  return <div ref={editorRef} style={styles.codeEditor} />
}

// ── MarkdownView ──────────────────────────────────────────────────────────────
// View mode: rendered markdown. Click to enter edit mode (plain textarea).

interface MarkdownViewProps {
  cell: Cell
  onSourceChange: (cellId: string, source: string) => void
  onSave?: (cellId: string, source: string) => void
}

function MarkdownView({ cell, onSourceChange, onSave }: MarkdownViewProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(cell.source)

  // Keep draft in sync if source changes externally (e.g. history restore)
  useEffect(() => { setDraft(cell.source) }, [cell.source])

  if (editing) {
    return (
      <textarea
        style={styles.mdEditor}
        value={draft}
        onChange={(e) => {
          setDraft(e.target.value)
          onSourceChange(cell.id, e.target.value)
        }}
        onBlur={() => {
          setEditing(false)
          onSave?.(cell.id, draft)
        }}
        autoFocus
        placeholder="Write markdown…"
      />
    )
  }

  return (
    <div style={styles.mdRendered} onClick={() => setEditing(true)}>
      {cell.source
        ? <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{cell.source}</ReactMarkdown>
        : <span style={styles.mdPlaceholder}>Click to add content…</span>
      }
    </div>
  )
}

// ── Cell ──────────────────────────────────────────────────────────────────────

export function Cell({
  cell,
  connectors,
  notebookId,
  onRun,
  onDelete,
  onSourceChange,
  onSave,
  onAssignConnector,
  onClearConnector,
  onMoveUp,
  onMoveDown,
  onSwitchType,
  running = false,
  saveState,
  runAt,
  onUpdateCellMeta,
  onShowHistory,
  onFocus,
}: Props) {
  const [hovered, setHovered] = useState(false)
  const [connectorOpen, setConnectorOpen] = useState(false)

  const isCode = cell.type === 'code'
  const sourceVisible = cell.source_visible ?? true
  const connector = connectors.find((c) => c.id === cell.connector_id)

  // ── Collapsed ───────────────────────────────────────────────────────────────

  if (cell.cell_collapsed) {
    return (
      <div style={styles.collapsed}>
        <button
          style={styles.expandTrigger}
          onClick={() => onUpdateCellMeta?.({ cell_collapsed: false })}
        >
          <ChevronRight size={11} />
          <span style={styles.cellTypeTag}>{isCode ? 'SQL' : 'MD'}</span>
          <span style={styles.collapsedTitle}>
            {cell.title || (isCode ? 'Untitled query' : 'Untitled note')}
          </span>
        </button>
      </div>
    )
  }

  // ── Normal ──────────────────────────────────────────────────────────────────

  return (
    <div
      style={styles.cell}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onClick={() => onFocus?.(cell.id)}
    >
      {/* ── Meta bar ── */}
      <div style={styles.metaBar}>
        <div style={styles.metaLeft}>
          <span style={styles.cellTypeTag}>{isCode ? 'SQL' : 'MD'}</span>

          {/* Connector: badge → inline select on click */}
          {isCode && (
            connectorOpen ? (
              <select
                style={styles.connectorSelect}
                value={cell.connector_id ?? ''}
                onChange={(e) => {
                  if (e.target.value === '') onClearConnector?.(cell.id)
                  else onAssignConnector(cell.id, e.target.value)
                  setConnectorOpen(false)
                }}
                onBlur={() => setConnectorOpen(false)}
                autoFocus
              >
                <option value="">— inherit from notebook —</option>
                {connectors.map((c) => (
                  <option key={c.id} value={c.id}>{c.name}</option>
                ))}
              </select>
            ) : (
              <button
                style={styles.connectorBadge}
                onClick={(e) => { e.stopPropagation(); setConnectorOpen(true) }}
                title="Click to change connector"
              >
                {connector?.name ?? 'no connector'}
              </button>
            )
          )}

          {/* Title */}
          {cell.title !== undefined && (
            <input
              style={styles.titleInput}
              value={cell.title}
              onChange={(e) => onUpdateCellMeta?.({ title: e.target.value })}
              onClick={(e) => e.stopPropagation()}
              placeholder="Untitled"
            />
          )}
        </div>

        {/* Hover toolbar */}
        <div style={{ ...styles.actions, opacity: hovered ? 1 : 0 }}>
          {isCode && (
            <button
              style={styles.actionBtn}
              onClick={(e) => { e.stopPropagation(); onRun(cell.id) }}
              disabled={running}
              title="Run (Ctrl+Enter)"
            >
              {running
                ? <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />
                : <Play size={11} />
              }
            </button>
          )}
          <button style={styles.actionBtn} onClick={onSwitchType} title={isCode ? 'Switch to MD' : 'Switch to SQL'}>
            {isCode ? 'MD' : 'SQL'}
          </button>
          {onMoveUp && <button style={styles.actionBtn} onClick={onMoveUp}><ChevronUp size={11} /></button>}
          {onMoveDown && <button style={styles.actionBtn} onClick={onMoveDown}><ChevronDown size={11} /></button>}
          {onDuplicate && (
            <button style={styles.actionBtn} onClick={onDuplicate} title="Duplicate cell">
              <Copy size={12} />
            </button>
          )}
          <button
            style={styles.actionBtn}
            onClick={() => onUpdateCellMeta?.({ source_visible: !sourceVisible })}
            title={sourceVisible ? 'Hide source' : 'Show source'}
          >
            {sourceVisible ? <EyeOff size={11} /> : <Eye size={11} />}
          </button>
          <button
            style={styles.actionBtn}
            onClick={() => onUpdateCellMeta?.({ cell_collapsed: true })}
            title="Collapse"
          >
            <ChevronRight size={11} />
          </button>
          <button style={styles.actionBtn} onClick={onShowHistory} title="History">
            <Clock size={11} />
          </button>
          <button
            type="button"
            title={cell.slide_break ? 'Remove slide break' : 'Start new slide here'}
            style={{ ...styles.actionBtn, color: cell.slide_break ? 'var(--accent)' : '#bbb' }}
            onClick={() => onUpdateCellMeta?.({ slide_break: !cell.slide_break })}
          >
            <SeparatorHorizontal size={13} />
          </button>
          <button
            style={{ ...styles.actionBtn, ...styles.actionBtnDelete }}
            onClick={(e) => { e.stopPropagation(); onDelete(cell.id) }}
            title="Delete"
          >
            <X size={11} />
          </button>
        </div>
      </div>

      {/* ── Editor ── */}
      {sourceVisible && (
        isCode
          ? <CodeEditorView
              cell={cell}
              notebookId={notebookId}
              onRun={onRun}
              onSourceChange={onSourceChange}
              collapsed={false}
            />
          : <MarkdownView cell={cell} onSourceChange={onSourceChange} onSave={onSave} />
      )}

      {/* ── Output ── */}
      {isCode && cell.outputs.length > 0 && (
        <div style={styles.outputWrap}>
          <OutputRenderer outputs={cell.outputs} />
        </div>
      )}

      {/* ── Footer ── */}
      {(saveState || runAt) && (
        <div style={styles.footer}>
          <span style={saveState?.error ? styles.footerError : styles.footerMuted}>
            {saveState?.saving
              ? 'Saving…'
              : saveState?.error
                ? saveState.error
                : saveState?.savedAt
                  ? `Saved ${fmtTime(saveState.savedAt)}`
                  : ''}
          </span>
          {runAt && <span style={styles.footerMuted}>Ran {fmtTime(runAt)}</span>}
        </div>
      )}
    </div>
  )
}

// ── Styles ────────────────────────────────────────────────────────────────────

const styles: Record<string, React.CSSProperties> = {
  // Cell container — full card with border
  cell: {
    background: '#fff',
    border: '1px solid #e8e8e8',
    borderRadius: 4,
    overflow: 'hidden',
  },

  // Meta bar
  metaBar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 16px',
    gap: 8,
    minHeight: 32,
  },
  metaLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flex: 1,
    minWidth: 0,
  },
  cellTypeTag: {
    fontSize: 9,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    letterSpacing: '0.1em',
    color: '#bbb',
    textTransform: 'uppercase' as const,
    flexShrink: 0,
    userSelect: 'none',
  },
  connectorBadge: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: '#aaa',
    background: 'none',
    border: 'none',
    padding: 0,
    cursor: 'pointer',
    flexShrink: 0,
  },
  connectorSelect: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: '#555',
    border: '1px solid #ddd',
    borderRadius: 3,
    padding: '1px 4px',
    background: '#fff',
    outline: 'none',
  },
  titleInput: {
    flex: 1,
    border: 'none',
    outline: 'none',
    fontSize: 12,
    fontWeight: 500,
    color: '#222',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
    minWidth: 0,
  },

  // Hover toolbar
  actions: {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    transition: 'opacity 0.12s',
    flexShrink: 0,
  },
  actionBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 6px',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 600,
    color: '#999',
    background: 'none',
    border: 'none',
    borderRadius: 3,
    cursor: 'pointer',
    lineHeight: 1,
  },
  actionBtnDelete: {
    color: '#ccc',
  },

  // Code editor area
  codeEditor: {
    borderTop: '1px solid #ebebeb',
    borderBottom: '1px solid #ebebeb',
  },

  // Markdown view mode
  mdRendered: {
    padding: '14px 20px',
    fontSize: 14,
    lineHeight: 1.75,
    color: '#222',
    fontFamily: 'var(--font-sans)',
    borderTop: '1px solid #ebebeb',
    borderBottom: '1px solid #ebebeb',
    cursor: 'text',
    minHeight: 48,
  },
  mdPlaceholder: {
    color: '#bbb',
    fontStyle: 'italic',
    fontSize: 13,
  },
  headerAnchor: {
    color: '#ccc',
    textDecoration: 'none',
    marginRight: 8,
    opacity: 0,
    transition: 'opacity 0.15s',
    fontFamily: 'var(--font-mono)',
    fontSize: '0.85em',
  },

  // Markdown edit mode
  mdEditor: {
    display: 'block',
    width: '100%',
    padding: '14px 20px',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    lineHeight: 1.7,
    color: '#333',
    background: '#f7f7f7',
    border: 'none',
    borderTop: '1px solid #ebebeb',
    borderBottom: '1px solid #ebebeb',
    outline: 'none',
    resize: 'vertical',
    minHeight: 100,
    boxSizing: 'border-box',
  },

  // Output
  outputWrap: {
    borderTop: '1px solid #ebebeb',
  },

  // Footer
  footer: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: '3px 16px',
    minHeight: 20,
  },
  footerMuted: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: '#bbb',
  },
  footerError: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: '#c0392b',
  },

  // Collapsed
  collapsed: {
    background: '#fff',
    border: '1px solid #e8e8e8',
    borderRadius: 4,
    padding: '4px 16px',
  },
  expandTrigger: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    padding: '2px 0',
    color: '#aaa',
  },
  collapsedTitle: {
    fontSize: 12,
    fontFamily: 'var(--font-sans)',
    color: '#aaa',
    fontStyle: 'italic',
  },
}
