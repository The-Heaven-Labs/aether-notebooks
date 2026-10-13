import { useEffect, useRef, useState } from 'react'
import { Compartment, EditorState } from '@codemirror/state'
import type { Extension } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { sqlHighlight, syntaxHighlighting } from './sqlHighlight'

export interface CodeMirrorFieldProps {
  /** Accessible label for the editor wrapper (also the test target). */
  label: string
  language: 'html' | 'css' | 'javascript'
  value: string
  onSave: (next: string) => void
  /** When false the editor is read-only (used while editing is paused). */
  editable?: boolean
  minHeight?: number
}

/** Lazy grammar imports keep the drawer's initial bundle small. */
async function languageExtension(language: CodeMirrorFieldProps['language']): Promise<Extension> {
  if (language === 'html') {
    const { html } = await import('@codemirror/lang-html')
    return html()
  }
  if (language === 'css') {
    const { css } = await import('@codemirror/lang-css')
    return css()
  }
  const { javascript } = await import('@codemirror/lang-javascript')
  return javascript()
}

/**
 * A small CodeMirror 6 field over the same primitives as `SqlEditor` (repo
 * theme, no gutters, wrapping) with the same 600 ms debounce + unmount flush
 * semantics as `DebouncedTextInput`: typing saves a trailing edit even when
 * the drawer (or the section) closes inside the debounce window.
 */
export function CodeMirrorField({ label, language, value, onSave, editable = true, minHeight = 120 }: CodeMirrorFieldProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const draftRef = useRef(value)
  // Last draft handed to onSave — guards the unmount flush against saving the
  // same draft twice when the debounce timer already fired.
  const savedRef = useRef(value)
  const valueRef = useRef(value)
  const saveRef = useRef(onSave)
  const timerRef = useRef<number | null>(null)
  const editableRef = useRef(editable)
  const editableCompartment = useRef(new Compartment())
  const viewRef = useRef<EditorView | null>(null)
  const [view, setView] = useState<EditorView | null>(null)

  // Latest-value refs, updated after render: the view creation effect reads
  // them without re-running per keystroke.
  useEffect(() => {
    saveRef.current = onSave
    valueRef.current = value
    editableRef.current = editable
  })

  useEffect(() => {
    let disposed = false
    const flush = () => {
      if (timerRef.current != null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
      const pending = draftRef.current
      if (pending !== savedRef.current) {
        savedRef.current = pending
        saveRef.current(pending)
      }
    }

    void languageExtension(language).then((lang) => {
      if (disposed || !ref.current) return
      const editorView = new EditorView({
        state: EditorState.create({
          doc: valueRef.current,
          extensions: [
            lang,
            syntaxHighlighting(sqlHighlight),
            EditorView.lineWrapping,
            EditorView.updateListener.of(update => {
              if (!update.docChanged) return
              draftRef.current = update.state.doc.toString()
              if (timerRef.current != null) window.clearTimeout(timerRef.current)
              timerRef.current = window.setTimeout(() => {
                timerRef.current = null
                savedRef.current = draftRef.current
                saveRef.current(draftRef.current)
              }, 600)
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
            editableCompartment.current.of(EditorView.editable.of(editableRef.current)),
          ],
        }),
        parent: ref.current,
      })
      viewRef.current = editorView
      setView(editorView)
    })

    return () => {
      disposed = true
      flush()
      viewRef.current?.destroy()
      viewRef.current = null
    }
    // `editable` is applied by the compartment effect below; only the grammar
    // recreates the view (a keystroke must never reset the cursor).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [language])

  // Toggle read-only without recreating the view.
  useEffect(() => {
    if (!view) return
    view.dispatch({
      effects: editableCompartment.current.reconfigure(EditorView.editable.of(editable)),
    })
  }, [view, editable])

  return <div ref={ref} aria-label={label} />
}
