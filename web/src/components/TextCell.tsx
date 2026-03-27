import { useEffect, useRef } from 'react'
import { ChevronRight } from 'lucide-react'
import { EditorState } from '@codemirror/state'
import { EditorView, ViewPlugin, Decoration, keymap, WidgetType } from '@codemirror/view'
import type { DecorationSet } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { markdown } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import ReactDOM from 'react-dom/client'
import type { Cell } from '../types'
import { CellToolbar } from './CellToolbar'

// ── Live markdown preview: replace completed lines with rendered HTML ──

class MarkdownLineWidget extends WidgetType {
  content: string
  private root: ReturnType<typeof ReactDOM.createRoot> | null = null
  constructor(content: string) { super(); this.content = content }
  eq(other: MarkdownLineWidget) { return other.content === this.content }
  toDOM() {
    const div = document.createElement('div')
    div.className = 'cm-md-preview'
    div.style.cssText = 'padding:0 16px;font-size:14px;line-height:1.75;min-height:22px'
    this.root = ReactDOM.createRoot(div)
    this.root.render(
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{this.content}</ReactMarkdown>
    )
    return div
  }
  destroy() {
    this.root?.unmount()
    this.root = null
  }
}

function buildMarkdownDecorations(view: EditorView): DecorationSet {
  const { state } = view
  const { head } = state.selection.main
  const activeLine = state.doc.lineAt(head).number
  const widgets: import('@codemirror/state').Range<Decoration>[] = []

  for (let i = 1; i <= state.doc.lines; i++) {
    if (i === activeLine) continue
    const line = state.doc.line(i)
    if (line.text.trim() === '') continue
    widgets.push(
      Decoration.replace({
        widget: new MarkdownLineWidget(line.text),
        inclusive: true,
      }).range(line.from, line.to)
    )
  }
  return Decoration.set(widgets, true)
}

const markdownPreviewPlugin = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet
    lastActiveLine: number
    constructor(view: EditorView) {
      this.lastActiveLine = view.state.doc.lineAt(view.state.selection.main.head).number
      this.decorations = buildMarkdownDecorations(view)
    }
    update(update: import('@codemirror/view').ViewUpdate) {
      if (update.docChanged) {
        this.lastActiveLine = update.view.state.doc.lineAt(update.view.state.selection.main.head).number
        this.decorations = buildMarkdownDecorations(update.view)
      } else if (update.selectionSet) {
        const newActiveLine = update.view.state.doc.lineAt(update.view.state.selection.main.head).number
        if (newActiveLine !== this.lastActiveLine) {
          this.lastActiveLine = newActiveLine
          this.decorations = buildMarkdownDecorations(update.view)
        }
      }
    }
  },
  { decorations: (v) => v.decorations }
)

// ── Image paste handler ──

const imagePasteExtension = EditorView.domEventHandlers({
  paste(event, view) {
    const items = Array.from(event.clipboardData?.items ?? [])
    const imageItem = items.find((i) => i.type.startsWith('image/'))
    if (!imageItem) return false
    event.preventDefault()
    const file = imageItem.getAsFile()
    if (!file) return false
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = reader.result as string
      const md = `![pasted image](${dataUrl})`
      const { from } = view.state.selection.main
      view.dispatch({
        changes: { from, to: from, insert: md },
        selection: { anchor: from + md.length },
      })
    }
    reader.readAsDataURL(file)
    return true
  },
})

// ── TextCell component ──

interface SaveState {
  saving: boolean
  savedAt: Date | null
  error: string | null
}

function fmtTime(date: Date): string {
  const diffSec = Math.floor((Date.now() - date.getTime()) / 1000)
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
  onUpdateCellMeta?: (updates: Partial<Pick<Cell, 'source_visible' | 'cell_collapsed' | 'title' | 'description' | 'slug'>>) => void
  onShowHistory?: () => void
  saveState?: SaveState
}

export function TextCell({ cell, onDelete, onSourceChange, onSave, onMoveUp, onMoveDown, onSwitchType, onUpdateCellMeta, onShowHistory, saveState }: Props) {
  const editorRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onSourceChangeRef = useRef(onSourceChange)
  onSourceChangeRef.current = onSourceChange
  const onSaveRef = useRef(onSave)
  onSaveRef.current = onSave

  useEffect(() => {
    if (!editorRef.current) return
    const view = new EditorView({
      state: EditorState.create({
        doc: cell.source,
        extensions: [
          keymap.of(defaultKeymap),
          markdown({ codeLanguages: languages }),
          syntaxHighlighting(defaultHighlightStyle),
          markdownPreviewPlugin,
          imagePasteExtension,
          EditorView.theme({
            '&': { fontFamily: 'var(--font-mono)', fontSize: '13px' },
            '.cm-content': { padding: '14px 16px', minHeight: '80px' },
            '.cm-line': { lineHeight: '1.65' },
            '.cm-focused': { outline: 'none' },
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onSourceChangeRef.current(cell.id, update.state.doc.toString())
            }
          }),
          EditorView.domEventHandlers({
            blur: (_, view) => {
              onSaveRef.current?.(cell.id, view.state.doc.toString())
              return false
            },
          }),
        ],
      }),
      parent: editorRef.current,
    })
    viewRef.current = view
    return () => view.destroy()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cell.id, cell.cell_collapsed])

  if (cell.cell_collapsed) {
    return (
      <div style={styles.collapsed}>
        <span style={styles.collapsedLabel}>{cell.title || 'Markdown cell'}</span>
        <button style={styles.expandBtn} onClick={() => onUpdateCellMeta?.({ cell_collapsed: false })}><ChevronRight size={13} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} /> Expand</button>
      </div>
    )
  }

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
        onToggleSourceVisible={(v) => onUpdateCellMeta?.({ source_visible: v })}
        onToggleCellCollapsed={(v) => onUpdateCellMeta?.({ cell_collapsed: v })}
        onShowHistory={() => onShowHistory?.()}
      />
      <div ref={editorRef} style={(cell.source_visible ?? true) ? undefined : { display: 'none' }} />
      {saveState && (
        <div style={styles.statusBar}>
          <span style={saveState.error ? styles.statusError : styles.statusSave}>
            {saveState.saving ? 'Saving…' : saveState.error ? `Save failed: ${saveState.error}` : saveState.savedAt ? `Saved ${fmtTime(saveState.savedAt)}` : ''}
          </span>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  cell: { border: '1px solid var(--border)', borderRadius: 10, background: 'white', overflow: 'hidden', boxShadow: 'var(--shadow-sm)' },
  collapsed: { border: '1px solid var(--border)', borderRadius: 10, background: 'var(--bg-secondary)', padding: '6px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' },
  collapsedLabel: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  expandBtn: { fontSize: 12, background: 'transparent', border: 'none', color: 'var(--accent)', cursor: 'pointer' },
  statusBar: { padding: '4px 16px', fontSize: 11, minHeight: 24, background: '#faf9f7', borderTop: '1px solid var(--border-light)', display: 'flex', alignItems: 'center' },
  statusSave: { color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' },
  statusError: { color: 'var(--error)', fontFamily: 'var(--font-mono)' },
}
