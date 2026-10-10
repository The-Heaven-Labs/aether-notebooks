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

export function SqlEditor({ value, onChange, minHeight = 160, connectorType, collab = null, editable = true }: {
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
}) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)
  const valueRef = useRef(value)

  // Latest-value refs, updated after render (refs must not be written during
  // render): the view creation effect reads them without re-running per
  // keystroke, and the update listener always calls the current callback.
  useEffect(() => {
    onChangeRef.current = onChange
    valueRef.current = value
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
          editableCompartment.current.of(EditorView.editable.of(editable)),
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

  // Toggle read-only without recreating the view.
  useEffect(() => {
    view?.dispatch({ effects: editableCompartment.current.reconfigure(EditorView.editable.of(editable)) })
  }, [view, editable])

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

  // Collaboration binding; the y-codemirror stack is imported on demand.
  useEffect(() => {
    if (!view || !collab) return
    let cancelled = false
    let detach: (() => void) | undefined
    collabBoundRef.current = false
    void import('./dashboardCollabEditor').then((mod) => {
      if (cancelled) return
      detach = mod.attachDashboardCollabToEditor({
        view,
        compartment: collabCompartment.current,
        collab: collab.collab,
        widgetId: collab.widgetId,
      })
      collabBoundRef.current = true
    })
    return () => {
      cancelled = true
      detach?.()
      collabBoundRef.current = false
    }
  }, [view, collab])

  return <div ref={ref} />
}
