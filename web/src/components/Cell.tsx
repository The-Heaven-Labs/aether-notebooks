import { useState, useEffect, useRef, useMemo, useCallback, memo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff, ChevronRight, Clock, X, SeparatorHorizontal, Copy, Link, Check, LayoutDashboard } from 'lucide-react'
import { Compartment, EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { sql, PostgreSQL, MySQL } from '@codemirror/lang-sql'
import { javascript } from '@codemirror/lang-javascript'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'
import { format } from 'sql-formatter'
import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'
import { yCollab, ySyncFacet, YSyncConfig } from 'y-codemirror.next'
import { OutputRenderer } from './OutputRenderer'
import { MarkdownView } from './MarkdownCell'
import type { Cell, Connector } from '../types'
import type { ChartConfig } from '../charts'

// Normalize chart config from backend (handles legacy key names from earlier create_chart calls)
function normalizeChartConfig(raw: unknown): ChartConfig | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const obj = raw as Record<string, unknown>
  return {
    chartType: (obj.chartType ?? obj.type) as ChartConfig['chartType'] | undefined,
    xAxis: (obj.xAxis ?? obj.x_column) as string | undefined,
    yAxis: (obj.yAxis ?? obj.y_columns) as string[] | undefined,
    title: obj.title as string | undefined,
    showLegend: obj.showLegend as boolean | undefined,
    showGrid: obj.showGrid as boolean | undefined,
    showLabels: obj.showLabels as boolean | undefined,
    skipEmpty: obj.skipEmpty as boolean | undefined,
    seriesColors: obj.seriesColors as Record<string, string> | undefined,
    // Timeline fields
    timeColumn: obj.timeColumn as string | undefined,
    endTimeColumn: obj.endTimeColumn as string | undefined,
    labelColumn: obj.labelColumn as string | undefined,
    groupBy: obj.groupBy as string | undefined,
    // Hierarchy tree fields
    idColumn: obj.idColumn as string | undefined,
    parentIdColumn: obj.parentIdColumn as string | undefined,
    metricColumns: obj.metricColumns as string[] | undefined,
    layout: obj.layout as 'top-down' | 'left-to-right' | undefined,
    nodeSpacing: obj.nodeSpacing as number | undefined,
  }
}

const sqlHighlight = HighlightStyle.define([
  { tag: tags.keyword, class: 'cm-keyword' },
  { tag: tags.string, class: 'cm-string' },
  { tag: tags.comment, class: 'cm-comment' },
  { tag: tags.function(tags.name), class: 'cm-function' },
  { tag: tags.number, class: 'cm-number' },
])

// ── Yjs collaboration cache (shared across all cells in a notebook) ───────────

const RELAY_URL = import.meta.env.VITE_RELAY_URL || 'ws://localhost:3001'

export interface NotebookCollab {
  doc: Y.Doc
  provider: HocuspocusProvider
  refCount: number
  synced: boolean
}
export const collabCache = new Map<string, NotebookCollab>()

export function getOrCreateCollab(notebookId: string): NotebookCollab {
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
  window.dispatchEvent(new CustomEvent('hnb-collab', { detail: { notebookId } }))
  return entry
}

export function updateCellFocus(notebookId: string, cellId: string | null) {
  const collab = collabCache.get(notebookId)
  if (!collab?.provider.awareness) return
  collab.provider.awareness.setLocalStateField('focus', {
    cellId,
    scrollTop: null,
    updatedAt: Date.now(),
  })
}

export function updateCellScroll(notebookId: string, scrollTop: number | null) {
  const collab = collabCache.get(notebookId)
  if (!collab?.provider.awareness) return
  collab.provider.awareness.setLocalStateField('focus', {
    cellId: collab.provider.awareness.getLocalState()?.focus?.cellId ?? null,
    scrollTop,
    updatedAt: Date.now(),
  })
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

export function slugify(text: string): string | null {
  if (!text?.trim()) return null
  return text
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w-]/g, '')
}

export function getFirstHeadingSlug(source: string): string | null {
  const match = source.match(/^#{1,6}\s+(.+)$/m)
  if (!match) return null
  return slugify(match[1])
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
  onMoveUp?: (cellId: string) => void
  onMoveDown?: (cellId: string) => void
  onSwitchType?: (cellId: string) => void
  onDuplicate?: (cellId: string) => void
  running?: boolean
  saveState?: SaveState
  runAt?: Date
  metrics?: { connect_time_ms: number; query_time_ms: number; render_time_ms: number; total_time_ms: number }
  onUpdateCellMeta?: (cellId: string, updates: Partial<Pick<Cell, 'source_visible' | 'outputs_hidden' | 'cell_collapsed' | 'slide_break' | 'title' | 'slug' | 'limit'>>) => void
  onChartConfigChange?: (cellId: string, config: ChartConfig) => void
  onViewModeChange?: (cellId: string, viewMode: 'table' | 'chart') => void
  onShowHistory?: (cellId: string) => void
  onFocus?: (cellId: string) => void
  onEditStart?: () => void
  onEditEnd?: () => void
  onAddToDashboard?: (cellId: string) => void
  focused?: boolean
  index?: number
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

// Module-level store for EditorView instances, keyed by cell ID
const editorViews = new Map<string, EditorView>()

/** Move cursor to end of document for a given cell's editor */
export function focusCellEditorEnd(cellId: string) {
  const view = editorViews.get(cellId)
  if (view) {
    const len = view.state.doc.length
    view.dispatch({ selection: { anchor: len, head: len } })
    view.focus()
  }
}

interface CodeEditorProps {
  cell: Cell
  notebookId: string
  onRun: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  collapsed: boolean
  connector?: Connector
  index?: number
  onEditStart?: () => void
  onEditEnd?: () => void
  readOnly?: boolean
}

function languageExtension(cell: Cell, connector?: Connector) {
  if (cell.language === 'javascript') return javascript()
  // Choose SQL dialect based on connector type
  const connType = connector?.type
  if (connType === 'clickhouse') return sql({ dialect: MySQL })
  if (connType === 'postgres') return sql({ dialect: PostgreSQL })
  // Default: MySQL dialect covers a broad set of keywords including SHOW, TABLES, etc.
  return sql({ dialect: MySQL })
}

function CodeEditorView({ cell, notebookId, onRun, onSourceChange, collapsed, connector, index, onEditStart, onEditEnd, readOnly }: CodeEditorProps) {
  const editorRef = useRef<HTMLDivElement>(null)
  const onRunRef = useRef(onRun)
  const onSourceChangeRef = useRef(onSourceChange)
  const onEditStartRef = useRef(onEditStart)
  const onEditEndRef = useRef(onEditEnd)
  onRunRef.current = onRun
  onSourceChangeRef.current = onSourceChange
  onEditStartRef.current = onEditStart
  onEditEndRef.current = onEditEnd
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
          languageExtension(cell, connector),
          syntaxHighlighting(sqlHighlight),
          EditorView.theme({
            '&': { fontFamily: 'var(--font-mono)', fontSize: '13px' },
            '.cm-content': { padding: '14px 16px', minHeight: '72px' },
            '.cm-line': { lineHeight: '1.65' },
            '.cm-focused': { outline: 'none' },
            '.cm-editor': { background: 'var(--cm-editor-bg)' },
            '.cm-gutters': { display: 'none' },
            // Fix cursor visibility: use text color so it's always visible in any theme
            '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--text-primary)' },
          }),
          EditorView.editable.of(!readOnly),
          EditorView.contentAttributes.of({
            'aria-label': cell.title
              ? `SQL editor: ${cell.title}`
              : `SQL editor cell ${index !== undefined ? index + 1 : ''}`,
          }),
          compartment.of([]),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onSourceChangeRef.current(cell.id, update.state.doc.toString())
            if (update.focusChanged) {
              updateCellFocus(notebookId, update.view.hasFocus ? cell.id : null)
            }
          }),
        ],
      }),
      parent: editorRef.current,
    })
    editorViews.set(cell.id, view)

    // Detect CodeMirror focus/blur for edit mode tracking
    const cmEditor = view.dom
    const handleFocus = () => onEditStartRef.current?.()
    const handleBlur = () => onEditEndRef.current?.()
    cmEditor.addEventListener('focusin', handleFocus)
    cmEditor.addEventListener('focusout', handleBlur)

    const attachCollab = () => {
      const editorContent = view.state.doc.toString()
      const yjsContent = ytext.toString()
      const ySyncConfig = new YSyncConfig(ytext, collab.provider.awareness)
      if (yjsContent.length === 0 || yjsContent !== editorContent) {
        // Sync editor content into Yjs (database is authoritative), using the
        // same config as origin so yCollab's observer ignores this change.
        collab.doc.transact(() => {
          if (ytext.length > 0) ytext.delete(0, ytext.length)
          ytext.insert(0, editorContent)
        }, ySyncConfig)
      }
      // Activate yCollab with our config last (overrides yCollab's internal one)
      // so the observer's origin guard matches our transact origin above.
      view.dispatch({ effects: compartment.reconfigure([
        ...yCollab(ytext, collab.provider.awareness),
        ySyncFacet.of(ySyncConfig),
      ]) })
    }

    let onSynced: (({ state }: { state: boolean }) => void) | null = null
    if (collab.synced) {
      attachCollab()
    } else {
      onSynced = ({ state }: { state: boolean }) => { if (state) attachCollab() }
      collab.provider.on('synced', onSynced)
    }

    return () => {
      editorViews.delete(cell.id)
      cmEditor.removeEventListener('focusin', handleFocus)
      cmEditor.removeEventListener('focusout', handleBlur)
      if (onSynced) collab.provider.off('synced', onSynced)
      view.destroy()
      releaseCollab(notebookId)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cell.id, notebookId, collapsed])

  // Sync external source changes (e.g. from agent update_cell) into Yjs
  const lastSourceRef = useRef(cell.source)
  useEffect(() => {
    if (cell.source !== lastSourceRef.current && cell.source !== undefined) {
      lastSourceRef.current = cell.source
      const collab = getOrCreateCollab(notebookId)
      const ytext = collab.doc.getText(`cell:${cell.id}`)
      if (ytext.toString() !== cell.source) {
        collab.doc.transact(() => {
          ytext.delete(0, ytext.length)
          ytext.insert(0, cell.source)
        })
      }
    }
  }, [cell.source, cell.id, notebookId])

  return <div ref={editorRef} style={styles.codeEditor} />
}

function generateTitlePlaceholder(source: string, isCode: boolean): string {
  if (!source?.trim()) {
    return isCode ? 'e.g., Monthly active users' : 'e.g., Analysis summary'
  }
  const firstLine = source.trim().split('\n')[0].trim()
  if (!firstLine) {
    return isCode ? 'e.g., Monthly active users' : 'e.g., Analysis summary'
  }
  if (isCode) {
    if (firstLine.length <= 40) return firstLine
    return firstLine.slice(0, 37) + '…'
  } else {
    const cleaned = firstLine.replace(/^#+\s*/, '').trim()
    if (cleaned.length > 0) return cleaned.slice(0, 40)
    return 'e.g., Analysis summary'
  }
}

// ── Cell ──────────────────────────────────────────────────────────────────────

export const Cell = memo(function Cell({
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
  onDuplicate,
  running = false,
  saveState,
  runAt,
  metrics,
  onUpdateCellMeta,
  onChartConfigChange,
  onViewModeChange,
  onShowHistory,
  onFocus,
  onEditStart,
  onEditEnd,
  onAddToDashboard,
  focused = false,
  index,
}: Props) {
  const [hovered, setHovered] = useState(false)
  const [connectorOpen, setConnectorOpen] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState(cell.title ?? '')

  const isCode = cell.type === 'code'
  const sourceVisible = cell.source_visible ?? true
  const outputHidden = cell.outputs_hidden ?? false
  const connector = connectors.find((c) => c.id === cell.connector_id)
  const chartConfig = useMemo(() => normalizeChartConfig(cell.metadata?.chart), [cell.metadata?.chart])
  const handleChartConfigChange = useCallback(
    (cfg: ChartConfig) => onChartConfigChange?.(cell.id, cfg),
    [cell.id, onChartConfigChange],
  )
  const viewMode = cell.metadata?.viewMode as 'table' | 'chart' | undefined
  const handleViewModeChange = useCallback(
    (vm: 'table' | 'chart') => onViewModeChange?.(cell.id, vm),
    [cell.id, onViewModeChange],
  )

  // ── Collapsed ───────────────────────────────────────────────────────────────

  if (cell.cell_collapsed) {
    return (
      <div
        id={'cell-' + cell.id}
        style={{
          ...styles.collapsed,
          borderLeft: `3px solid ${isCode ? 'var(--accent)' : 'var(--success)'}`,
        }}>
        <button
          style={styles.expandTrigger}
          onClick={() => onUpdateCellMeta?.(cell.id, { cell_collapsed: false, source_visible: true })}
        >
          <ChevronRight size={11} />
          <span style={styles.cellTypeTag}>{isCode ? 'SQL' : 'MD'}</span>
          <span style={styles.collapsedTitle}>
            {cell.title || generateTitlePlaceholder(cell.source, isCode)}
          </span>
        </button>
      </div>
    )
  }

  // ── Normal ──────────────────────────────────────────────────────────────────

  return (
    <>
      {/* ── Slide break indicator ── */}
      {cell.slide_break && (
        <div style={styles.slideBreakIndicator}>
          <span style={styles.slideBreakLabel}>Joined with previous slide</span>
        </div>
      )}
      <div
        id={'cell-' + cell.id}
        style={{
          ...styles.cell,
          borderLeft: `3px solid ${isCode ? 'var(--accent)' : 'var(--success)'}`,
          ...(focused ? {
            boxShadow: `0 0 0 1px ${isCode ? 'var(--accent)' : 'var(--success)'}, 0 2px 8px ${isCode ? 'rgba(99,102,241,0.15)' : 'rgba(34,197,94,0.15)'}`,
          } : {}),
        }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onClick={() => onFocus?.(cell.id)}
    >
      {/* ── Meta bar ── */}
      <div style={{
        ...styles.metaBar,
        background: isCode ? 'var(--bg-cell-code)' : 'var(--bg-cell-text)',
      }}>
        <div style={styles.metaLeft}>
          {index !== undefined && <span style={styles.cellNumber}>{index + 1}</span>}
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
                <option value="">Inherit from notebook</option>
                {connectors.map((c) => (
                  <option key={c.id} value={c.id} disabled={c.can_use === false}>
                    {c.name}{c.can_use === false ? ' (view only)' : ''}
                  </option>
                ))}
              </select>
            ) : (
              <button
                style={styles.connectorBadge}
                onClick={(e) => { e.stopPropagation(); setConnectorOpen(true) }}
                title="Click to change connector"
              >
                {connector?.name ?? 'Select connector'}
              </button>
            )
          )}

          {/* LIMIT selector for code cells */}
          {isCode && (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <span style={{ fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>Row limit:</span>
              <select
                style={styles.limitSelect}
                value={cell.limit == null ? 'null' : String(cell.limit)}
                onChange={(e) => {
                  const val = e.target.value
                  onUpdateCellMeta?.(cell.id, { limit: val === 'null' ? null : parseInt(val) })
                }}
                onClick={(e) => e.stopPropagation()}
              >
                <option value="null">Unlimited</option>
                <option value="1000">LIMIT 1000</option>
                <option value="100">LIMIT 100</option>
                <option value="10">LIMIT 10</option>
              </select>
            </span>
          )}

          {/* Title */}
          {editingTitle ? (
            <input
              style={styles.titleInput}
              value={titleDraft}
              onChange={(e) => setTitleDraft(e.target.value)}
              onBlur={() => {
                setEditingTitle(false)
                if (titleDraft !== (cell.title ?? '')) {
                  onUpdateCellMeta?.(cell.id, { title: titleDraft })
                }
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                if (e.key === 'Escape') { setTitleDraft(cell.title ?? ''); setEditingTitle(false) }
              }}
              onClick={(e) => e.stopPropagation()}
              autoFocus
            />
          ) : cell.title ? (
            <div
              style={styles.titleRendered}
              onClick={(e) => { e.stopPropagation(); setTitleDraft(cell.title ?? ''); setEditingTitle(true) }}
              title="Click to edit title (supports markdown)"
            >
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{cell.title}</ReactMarkdown>
            </div>
          ) : (
            <input
              style={styles.titleInput}
              value={cell.title ?? ''}
              onChange={(e) => onUpdateCellMeta?.(cell.id, { title: e.target.value })}
              onClick={(e) => e.stopPropagation()}
              placeholder={generateTitlePlaceholder(cell.source, isCode)}
            />
          )}
        </div>

        {/* Hover toolbar */}
        <div style={{ ...styles.actions, opacity: hovered ? 1 : 0 }} role="toolbar" aria-label="Cell actions">
          {isCode && (() => {
            const hasConnector = !!(cell.connector_id || connectors.length > 0)
            return (
              <button
                style={{ ...styles.actionBtn, opacity: hasConnector ? 1 : 0.4 }}
                onClick={(e) => { e.stopPropagation(); onRun(cell.id) }}
                disabled={running}
                title={hasConnector ? 'Run (Ctrl+Enter)' : 'Select a connector first'}
                aria-label="Run cell (Ctrl+Enter)"
              >
                {running
                  ? <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />
                  : <Play size={11} />
                }
              </button>
            )
          })()}
          <button style={styles.actionBtn} onClick={() => onSwitchType(cell.id)} title={isCode ? 'Convert to Markdown cell' : 'Convert to SQL cell'} aria-label={isCode ? 'Convert to Markdown cell' : 'Convert to SQL cell'}>
            {isCode ? 'MD' : 'SQL'}
          </button>
          {onMoveUp && <button style={styles.actionBtn} onClick={() => onMoveUp(cell.id)} aria-label="Move cell up"><ChevronUp size={11} /></button>}
          {onMoveDown && <button style={styles.actionBtn} onClick={() => onMoveDown(cell.id)} aria-label="Move cell down"><ChevronDown size={11} /></button>}
          {onDuplicate && (
            <button style={styles.actionBtn} onClick={() => onDuplicate(cell.id)} title="Duplicate cell" aria-label="Duplicate cell">
              <Copy size={12} />
            </button>
          )}
          {(() => {
            if (index === undefined) return null
            const idx = index + 1
            const titleSlug = cell.slug ?? slugify(cell.title ?? '')
            const mdSlug = getFirstHeadingSlug(cell.source)
            const baseSlug = isCode ? (titleSlug ?? `cell-${idx}`) : (mdSlug ?? titleSlug ?? `cell-${idx}`)
            const anchorSlug = `cell-${idx}-${baseSlug}`
            if (copiedId === cell.id) {
              return (
                <span style={styles.copiedBadge}>
                  <Check size={10} />
                  Copied!
                </span>
              )
            }
            return (
              <button
                style={styles.actionBtn}
                onClick={(e) => {
                  e.stopPropagation()
                  const url = `${window.location.origin}/notebooks/${notebookId}#${anchorSlug}`
                  navigator.clipboard.writeText(url)
                  setCopiedId(cell.id)
                  setTimeout(() => setCopiedId(null), 2000)
                }}
                title="Copy link to cell"
                aria-label="Copy link to cell"
              >
                <Link size={11} />
              </button>
            )
          })()}
          <button
            style={styles.actionBtn}
            onClick={() => onUpdateCellMeta?.(cell.id, { source_visible: !sourceVisible })}
            title={sourceVisible ? 'Hide source' : 'Show source'}
            aria-label={sourceVisible ? 'Hide source' : 'Show source'}
          >
            {sourceVisible ? <EyeOff size={11} /> : <Eye size={11} />}
          </button>
          {isCode && (
            <button
              style={styles.actionBtn}
              onClick={() => onUpdateCellMeta?.(cell.id, { outputs_hidden: !outputHidden })}
              title={outputHidden ? 'Show output' : 'Hide output'}
              aria-label={outputHidden ? 'Show output' : 'Hide output'}
            >
              {outputHidden ? <EyeOff size={11} /> : <Eye size={11} />}
            </button>
          )}
          <button
            style={styles.actionBtn}
            onClick={() => onUpdateCellMeta?.(cell.id, { cell_collapsed: true })}
            title="Collapse"
            aria-label="Collapse cell"
          >
            <ChevronRight size={11} />
          </button>
          <button style={styles.actionBtn} onClick={() => onShowHistory(cell.id)} title="History" aria-label="Cell history">
            <Clock size={11} />
          </button>
          {onAddToDashboard && (
            <button
              style={styles.actionBtn}
              onClick={(e) => { e.stopPropagation(); onAddToDashboard(cell.id) }}
              title="Add to dashboard"
              aria-label="Add to dashboard"
            >
              <LayoutDashboard size={11} />
            </button>
          )}
          <button
            type="button"
            title={cell.slide_break
              ? 'Joined with previous slide. Click to unmerge and start a new slide here.'
              : 'By default each cell starts a new slide. Click to merge this cell with the previous slide.'}
            aria-label={cell.slide_break
              ? 'Unmerge from previous slide (start a new slide here)'
              : 'Merge with previous slide'}
            style={{ ...styles.actionBtn, color: cell.slide_break ? 'var(--accent)' : 'var(--text-muted)' }}
            onClick={() => onUpdateCellMeta?.(cell.id, { slide_break: !cell.slide_break })}
          >
            {cell.slide_break ? <Link size={13} /> : <SeparatorHorizontal size={13} />}
          </button>
          <button
            style={{ ...styles.actionBtn, ...styles.actionBtnDelete }}
            onClick={(e) => { e.stopPropagation(); onDelete(cell.id) }}
            title="Delete"
            aria-label="Delete cell"
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
              connector={connector}
              index={index}
              onEditStart={onEditStart}
              onEditEnd={onEditEnd}
              readOnly={!onSave}
            />
          : <MarkdownView cell={cell} notebookId={notebookId} onSourceChange={onSourceChange} onSave={onSave} onEditStart={onEditStart} onEditEnd={onEditEnd} />
      )}

      {/* ── Output ── */}
      {isCode && cell.outputs.length > 0 && !(cell as any).outputs_hidden && (
        <div style={styles.outputWrap}>
          <OutputRenderer
            outputs={cell.outputs}
            cellId={cell.id ?? undefined}
            chartConfig={chartConfig}
            onChartConfigChange={handleChartConfigChange}
            viewMode={viewMode}
            onViewModeChange={handleViewModeChange}
          />
        </div>
      )}

      {/* ── Footer ── */}
      {(saveState || runAt || metrics) && (
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
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {metrics && (
              <span
                style={styles.footerMuted}
                title={`Connect: ${metrics.connect_time_ms}ms, Query: ${metrics.query_time_ms}ms, Render: ${metrics.render_time_ms}ms`}
              >
                ⏱ {(metrics.total_time_ms / 1000).toFixed(1)}s
              </span>
            )}
            {runAt && <span style={styles.footerMuted}>Ran {fmtTime(runAt)}</span>}
          </div>
        </div>
      )}
    </div>
    </>
  )
})

// ── Styles ────────────────────────────────────────────────────────────────────

const styles: Record<string, React.CSSProperties> = {
  // Cell container — full card with border
  cell: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
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
  cellNumber: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    color: 'var(--text-muted)',
    flexShrink: 0,
    userSelect: 'none',
    marginRight: 6,
  },
  cellTypeTag: {
    fontSize: 9,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    letterSpacing: '0.1em',
    color: 'var(--text-muted)',
    textTransform: 'uppercase' as const,
    flexShrink: 0,
    userSelect: 'none',
  },
  connectorBadge: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    background: 'none',
    border: 'none',
    padding: 0,
    cursor: 'pointer',
    flexShrink: 0,
  },
  connectorSelect: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 3,
    padding: '1px 4px',
    background: 'var(--bg-card)',
    outline: 'none',
  },
  limitSelect: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    border: '1px solid var(--border)',
    borderRadius: 3,
    padding: '1px 4px',
    background: 'var(--bg-card)',
    outline: 'none',
  },
  titleInput: {
    flex: 1,
    border: 'none',
    outline: 'none',
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-primary)',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
    minWidth: 0,
    caretColor: 'var(--text-primary)',
  },
  titleRendered: {
    flex: 1,
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
    minWidth: 0,
    cursor: 'pointer',
    lineHeight: 1.4,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
    '& p': { margin: 0, display: 'inline' },
    '& strong': { fontWeight: 700 },
    '& em': { fontStyle: 'italic' },
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
    color: 'var(--text-muted)',
    background: 'none',
    border: 'none',
    borderRadius: 3,
    cursor: 'pointer',
    lineHeight: 1,
  },
  actionBtnDelete: {
    color: 'var(--error)',
  },
  copiedBadge: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 3,
    padding: '3px 6px',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 600,
    color: 'var(--success)',
    background: 'var(--success-light)',
    border: '1px solid var(--success)',
    borderRadius: 3,
    whiteSpace: 'nowrap',
  },
  timing: {
    display: 'inline-flex',
    alignItems: 'center',
    padding: '3px 6px',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 600,
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
  },

  // Code editor area
  codeEditor: {
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
  },

  // Output
  outputWrap: {
    borderTop: '1px solid var(--border-light)',
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
    color: 'var(--text-muted)',
  },
  footerError: {
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    color: 'var(--error-full)',
  },

  // Collapsed
  collapsed: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
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
    color: 'var(--text-muted)',
  },
  collapsedTitle: {
    fontSize: 12,
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },

  // Slide break indicator
  slideBreakIndicator: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '4px 0',
    position: 'relative' as const,
  },
  slideBreakLabel: {
    fontSize: 9,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    letterSpacing: '0.1em',
    textTransform: 'uppercase' as const,
    color: 'var(--accent)',
    background: 'var(--bg-primary)',
    padding: '0 12px',
    position: 'relative' as const,
    zIndex: 1,
  },
}
