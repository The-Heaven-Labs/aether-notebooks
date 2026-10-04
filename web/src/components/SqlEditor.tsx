import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { sql, MySQL, PostgreSQL, StandardSQL } from '@codemirror/lang-sql'
import { sqlHighlight, syntaxHighlighting } from './sqlHighlight'

function languageExtension(connectorType?: string) {
  if (connectorType === 'postgres') return sql({ dialect: PostgreSQL })
  if (connectorType === 'databricks') return sql({ dialect: StandardSQL })
  return sql({ dialect: MySQL })
}

export function SqlEditor({ value, onChange, minHeight = 160, connectorType }: {
  value: string
  onChange: (value: string) => void
  minHeight?: number
  connectorType?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  const valueRef = useRef(value)
  valueRef.current = value

  // Recreate only when the dialect changes; keystrokes must not tear down the
  // editor or the cursor would reset on every character.
  useEffect(() => {
    if (!ref.current) return
    const view = new EditorView({
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
        ],
      }),
      parent: ref.current,
    })
    viewRef.current = view
    return () => {
      view.destroy()
      viewRef.current = null
    }
  }, [connectorType, minHeight])

  // Apply external value changes without recreating the view.
  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const current = view.state.doc.toString()
    if (current !== value) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
    }
  }, [value])

  return <div ref={ref} />
}
