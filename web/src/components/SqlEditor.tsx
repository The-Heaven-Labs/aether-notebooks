import { useEffect, useRef, useState } from 'react'
import { Compartment, EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { sql, MySQL, PostgreSQL, StandardSQL } from '@codemirror/lang-sql'
import { sqlHighlight, syntaxHighlighting } from './sqlHighlight'
import type { DashboardCollab } from './dashboardCollabRuntime'

function languageExtension(connectorType?: string) {
  if (connectorType === 'postgres') return sql({ dialect: PostgreSQL })
  if (connectorType === 'databricks') return sql({ dialect: StandardSQL })
  return sql({ dialect: MySQL })
}

/** Binds the editor to a widget's query text in a live dashboard document. */
export interface SqlEditorCollab {
  collab: DashboardCollab
  widgetId: string
}

export function SqlEditor({ value, onChange, minHeight = 160, connectorType, collab = null, editable = true, onCollabUnavailable }: {
  value: string
  onChange: (value: string) => void
  minHeight?: number
  connectorType?: string
  /**
   * When set, the editor binds to the widget's shared `Y.Text` for
   * character-level co-editing instead of owning the text locally. The
   * binding module is imported on demand.
   */
  collab?: SqlEditorCollab | null
  /** When false the editor is read-only (used while editing is paused). */
  editable?: boolean
  /**
   * Called when the shared binding cannot be loaded or applied; the caller
   * should drop the document binding and fall back to local editing + REST
   * persistence.
   */
  onCollabUnavailable?: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)
  const valueRef = useRef(value)
  const onCollabUnavailableRef = useRef(onCollabUnavailable)

  // The collab whose binding has settled — applied, or failed with the editor
  // released back to local editing. While it does not match `collab` the editor
  // stays read-only: a keystroke before y-codemirror adopts the shared text
  // would be reverted when the binding attaches.
  const [readyCollab, setReadyCollab] = useState<SqlEditorCollab | null>(null)
  const effectiveEditable = editable && (!collab || readyCollab === collab)
  const effectiveEditableRef = useRef(effectiveEditable)

  // Latest-value refs, updated after render (refs must not be written during
  // render): the view creation effect reads them without re-running per
  // keystroke, and the update listener always calls the current callback.
  useEffect(() => {
    onChangeRef.current = onChange
    valueRef.current = value
    onCollabUnavailableRef.current = onCollabUnavailable
    effectiveEditableRef.current = effectiveEditable
  })

  // The view lives in state (not only a ref) so the binding and value effects
  // below re-run when the view is recreated for a new dialect.
  const [view, setView] = useState<EditorView | null>(null)
  const editableCompartment = useRef(new Compartment())
  const collabCompartment = useRef(new Compartment())
  const collabBoundRef = useRef(false)

  // Recreate only when the dialect changes; keystrokes must not tear down the
  // editor or the cursor would reset on every character.
  useEffect(() => {
    if (!ref.current) return
    const editorView = new EditorView({
      state: EditorState.create({
        doc: valueRef.current,
        extensions: [
          languageExtension(connectorType),
          syntaxHighlighting(sqlHighlight),
          EditorView.lineWrapping,
          EditorView.updateListener.of(update => {
            if (update.docChanged) onChangeRef.current(update.state.doc.toString())
          }),
          EditorView.theme({
            '&': {
              fontFamily: 'var(--font-mono)',
              fontSize: '13px',
              border: '1px solid var(--border)',
              borderRadius: '4px',
              minHeight: `${minHeight}px`,
              background: 'var(--cm-editor-bg)',
            },
            '.cm-content': { padding: '10px 12px' },
            '.cm-line': { lineHeight: '1.6' },
            '.cm-editor': { background: 'var(--cm-editor-bg)' },
            '.cm-gutters': { display: 'none' },
            '.cm-focused': { outline: 'none' },
          }),
          editableCompartment.current.of(EditorView.editable.of(effectiveEditableRef.current)),
          collabCompartment.current.of([]),
        ],
      }),
      parent: ref.current,
    })
    viewRef.current = editorView
    setView(editorView)
    return () => {
      editorView.destroy()
      viewRef.current = null
      setView(null)
    }
    // `editable` is applied by the compartment effect below instead, so a
    // pause toggle never recreates the view.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectorType, minHeight])

  // Toggle read-only without recreating the view. The editor also stays
  // read-only until a requested shared binding has settled (`readyCollab`).
  useEffect(() => {
    view?.dispatch({ effects: editableCompartment.current.reconfigure(EditorView.editable.of(effectiveEditable)) })
  }, [view, effectiveEditable])

  // Apply external value changes without recreating the view. Once bound to a
  // shared document the Yjs text is the source of truth, so external values
  // are ignored (the binding applies the shared text instead).
  useEffect(() => {
    if (!view || collabBoundRef.current) return
    const current = view.state.doc.toString()
    if (current !== value) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
    }
  }, [view, value])

  // Collaboration binding; the y-codemirror stack is imported on demand. Until
  // the binding settles the editor is read-only, so an early keystroke cannot
  // be reverted when y-codemirror adopts the shared text.
  useEffect(() => {
    if (!view || !collab) return
    let cancelled = false
    let detach: (() => void) | undefined
    collabBoundRef.current = false
    setReadyCollab(null)
    void import('./dashboardCollabEditor')
      .then((mod) => {
        if (cancelled) return
        detach = mod.attachDashboardCollabToEditor({
          view,
          compartment: collabCompartment.current,
          collab: collab.collab,
          widgetId: collab.widgetId,
        })
        collabBoundRef.current = true
        setReadyCollab(collab)
      })
      .catch(() => {
        if (cancelled) return
        // The binding could not be loaded: release the gate and hand the
        // editor back to local editing; the caller drops its doc binding.
        setReadyCollab(collab)
        onCollabUnavailableRef.current?.()
      })
    return () => {
      cancelled = true
      detach?.()
      collabBoundRef.current = false
    }
  }, [view, collab])

  return <div ref={ref} />
}
