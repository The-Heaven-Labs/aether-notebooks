import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
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
import { yCollab } from 'y-codemirror.next'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { OutputRenderer } from './OutputRenderer'
import type { Cell, Connector } from '../types'
import { getToken } from '../api/client'

const sqlHighlight = HighlightStyle.define([
  { tag: tags.keyword, class: 'cm-keyword' },
  { tag: tags.string, class: 'cm-string' },
  { tag: tags.comment, class: 'cm-comment' },
  { tag: tags.function(tags.name), class: 'cm-function' },
  { tag: tags.number, class: 'cm-number' },
])

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

function slugify(text: string): string | null {
  if (!text?.trim()) return null
  return text
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w-]/g, '')
}

function getFirstHeadingSlug(source: string): string | null {
  const match = source.match(/^#{1,6}\s+(.+)$/m)
  if (!match) return null
  return slugify(match[1])
}

export interface ResizableImageProps {
  src: string
  alt?: string
  width?: string
  onResize?: (src: string, newWidth: number) => void
}

export function ResizableImage({ src, alt, width, onResize }: ResizableImageProps) {
  const imgRef = useRef<HTMLImageElement>(null)
  const [blobUrl, setBlobUrl] = useState<string | null>(null)

  useEffect(() => {
    let url: string
    fetch(src, { headers: { Authorization: `Bearer ${getToken()}` } })
      .then(r => r.blob())
      .then(blob => { url = URL.createObjectURL(blob); setBlobUrl(url) })
      .catch(() => setBlobUrl(src))
    return () => { if (url) URL.revokeObjectURL(url) }
  }, [src])

  const handleMouseDown = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    const startWidth = imgRef.current?.getBoundingClientRect().width ?? (parseInt(width ?? '0') || 300)

    const onMouseMove = (ev: MouseEvent) => {
      const newWidth = Math.max(50, startWidth + (ev.clientX - startX))
      if (imgRef.current) imgRef.current.style.width = `${newWidth}px`
    }

    const onMouseUp = (ev: MouseEvent) => {
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
      const newWidth = Math.max(50, startWidth + (ev.clientX - startX))
      onResize?.(src, Math.round(newWidth))
    }

    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)
  }

  return (
    <span style={{ display: 'inline-block', position: 'relative' }} onClick={e => e.stopPropagation()}>
      <img ref={imgRef} src={blobUrl ?? ''} alt={alt} width={width} style={{ display: 'block', maxWidth: '100%' }} />
      <span
        className="img-resize-handle"
        onMouseDown={handleMouseDown}
        onClick={e => e.stopPropagation()}
        style={{
          position: 'absolute',
          right: 0,
          top: 0,
          bottom: 0,
          width: 8,
          cursor: 'ew-resize',
          background: 'rgba(0,0,0,0.15)',
        }}
      />
    </span>
  )
}

function makeMarkdownComponents(onResize: (src: string, newWidth: number) => void) {
  return {
    img: ({ src, alt, width }: React.ImgHTMLAttributes<HTMLImageElement>) => (
      <ResizableImage src={src ?? ''} alt={alt} width={width?.toString()} onResize={onResize} />
    ),
    h1: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h1 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h1>
      )
    },
    h2: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h2 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h2>
      )
    },
    h3: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h3 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h3>
      )
    },
    h4: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h4 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h4>
      )
    },
    h5: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h5 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h5>
      )
    },
    h6: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h6 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h6>
      )
    },
  }
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
  onAddToDashboard?: (cellId: string) => void
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

interface CodeEditorProps {
  cell: Cell
  notebookId: string
  onRun: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  collapsed: boolean
  connector?: Connector
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

function CodeEditorView({ cell, notebookId, onRun, onSourceChange, collapsed, connector }: CodeEditorProps) {
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
  notebookId: string
  onSourceChange: (cellId: string, source: string) => void
  onSave?: (cellId: string, source: string) => void
}

function MarkdownView({ cell, notebookId, onSourceChange, onSave }: MarkdownViewProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(cell.source)
  const [uploading, setUploading] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Keep draft in sync if source changes externally (e.g. history restore)
  useEffect(() => { setDraft(cell.source) }, [cell.source])

  const handleResize = useCallback((imgSrc: string, newWidth: number) => {
    const escaped = imgSrc.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const updated = cell.source.replace(
      new RegExp(`(<img\\b[^>]*\\bsrc="${escaped}"[^>]*?)(?:\\s+width="[^"]*")?([^>]*?>)`, 'i'),
      (_, before, after) => `${before} width="${newWidth}"${after}`,
    )
    onSave?.(cell.id, updated)
  }, [cell.source, cell.id, onSave])

  const markdownComponents = useMemo(() => makeMarkdownComponents(handleResize), [handleResize])

  const uploadImage = useCallback(async (file: File): Promise<{ id: string; filename: string }> => {
    const form = new FormData()
    form.append('file', file)
    const res = await fetch(`/api/v1/notebooks/${notebookId}/attachments`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${getToken()}` },
      body: form,
    })
    if (!res.ok) throw new Error(`upload failed: ${res.status}`)
    return res.json()
  }, [notebookId])

  const insertImageTag = useCallback((att: { id: string; filename: string }) => {
    const textarea = textareaRef.current
    const imgTag = `<img src="/api/v1/attachments/${att.id}" alt="${att.filename}" width="100%">`
    if (textarea) {
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const newDraft = draft.slice(0, start) + imgTag + draft.slice(end)
      setDraft(newDraft)
      onSourceChange(cell.id, newDraft)
      // restore cursor after the inserted tag
      requestAnimationFrame(() => {
        textarea.selectionStart = start + imgTag.length
        textarea.selectionEnd = start + imgTag.length
      })
    } else {
      const newDraft = draft + imgTag
      setDraft(newDraft)
      onSourceChange(cell.id, newDraft)
    }
  }, [draft, cell.id, onSourceChange])

  const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.files).filter(f => f.type.startsWith('image/'))
    if (files.length === 0) return
    e.preventDefault()
    setUploading(true)
    try {
      const att = await uploadImage(files[0])
      insertImageTag(att)
    } catch (err) {
      console.error('Image upload failed:', err)
    } finally {
      setUploading(false)
    }
  }, [uploadImage, insertImageTag])

  const handleFileSelect = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    // reset input so same file can be re-selected
    e.target.value = ''
    setUploading(true)
    try {
      const att = await uploadImage(file)
      insertImageTag(att)
    } catch (err) {
      console.error('Image upload failed:', err)
    } finally {
      setUploading(false)
    }
  }, [uploadImage, insertImageTag])

  if (editing) {
    return (
      <div>
        <div style={styles.mdToolbar}>
          <button
            style={styles.mdToolbarBtn}
            disabled={uploading}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => fileInputRef.current?.click()}
            title="Upload image"
          >
            {uploading ? '...' : (
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
                <circle cx="8.5" cy="8.5" r="1.5"/>
                <polyline points="21 15 16 10 5 21"/>
              </svg>
            )}
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            style={{ display: 'none' }}
            onChange={handleFileSelect}
          />
        </div>
        <textarea
          ref={textareaRef}
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
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
              setEditing(false)
              onSave?.(cell.id, draft)
            }
          }}
          onPaste={handlePaste}
          autoFocus
          placeholder="Write markdown…"
        />
      </div>
    )
  }

  return (
    <div style={styles.mdRendered} onClick={() => setEditing(true)}>
      {cell.source
        ? <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={markdownComponents}>{cell.source}</ReactMarkdown>
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
  onDuplicate,
  running = false,
  saveState,
  runAt,
  onUpdateCellMeta,
  onShowHistory,
  onFocus,
  onAddToDashboard,
  index,
}: Props) {
  const [hovered, setHovered] = useState(false)
  const [connectorOpen, setConnectorOpen] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)

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
          <input
            style={styles.titleInput}
            value={cell.title ?? ''}
            onChange={(e) => onUpdateCellMeta?.({ title: e.target.value })}
            onClick={(e) => e.stopPropagation()}
            placeholder="Untitled"
          />
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
              >
                <Link size={11} />
              </button>
            )
          })()}
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
          {onAddToDashboard && (
            <button
              style={styles.actionBtn}
              onClick={(e) => { e.stopPropagation(); onAddToDashboard(cell.id) }}
              title="Add to dashboard"
            >
              <LayoutDashboard size={11} />
            </button>
          )}
          <button
            type="button"
            title={cell.slide_break ? 'Separate into own slide' : 'Join with previous slide'}
            style={{ ...styles.actionBtn, color: cell.slide_break ? 'var(--accent)' : '#bbb' }}
            onClick={() => onUpdateCellMeta?.({ slide_break: !cell.slide_break })}
          >
            {cell.slide_break ? <Link size={13} /> : <SeparatorHorizontal size={13} />}
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
              connector={connector}
            />
          : <MarkdownView cell={cell} notebookId={notebookId} onSourceChange={onSourceChange} onSave={onSave} />
      )}

      {/* ── Output ── */}
      {isCode && cell.outputs.length > 0 && (
        <div style={styles.outputWrap}>
          <OutputRenderer outputs={cell.outputs} cellId={cell.id ?? undefined} />
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
    color: 'var(--text-muted)',
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

  // Code editor area
  codeEditor: {
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
  },

  // Markdown view mode
  mdRendered: {
    padding: '14px 20px',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
    cursor: 'text',
    minHeight: 48,
  },
  mdPlaceholder: {
    color: 'var(--text-muted)',
    fontStyle: 'italic',
    fontSize: 13,
  },
  headerAnchor: {
    color: 'var(--text-muted)',
    textDecoration: 'none',
    marginRight: 8,
    opacity: 0,
    transition: 'opacity 0.15s',
    fontFamily: 'var(--font-mono)',
    fontSize: '0.85em',
  },

  // Markdown edit toolbar
  mdToolbar: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 12px',
    borderTop: '1px solid var(--border-light)',
    background: 'var(--bg-cell-code)',
  },
  mdToolbarBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 6px',
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 3,
    cursor: 'pointer',
    lineHeight: 1,
  },

  // Markdown edit mode
  mdEditor: {
    display: 'block',
    width: '100%',
    padding: '14px 20px',
    fontFamily: 'var(--font-mono)',
    fontSize: 13,
    lineHeight: 1.7,
    color: 'var(--text-primary)',
    background: 'var(--bg-cell-code)',
    border: 'none',
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
    outline: 'none',
    resize: 'vertical',
    minHeight: 100,
    boxSizing: 'border-box',
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
}
