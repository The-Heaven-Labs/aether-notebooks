import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { sql, MySQL } from '@codemirror/lang-sql'
import { javascript } from '@codemirror/lang-javascript'
import { sqlHighlight, syntaxHighlighting } from './sqlHighlight'

function languageExtension(language?: string) {
  if (language === 'javascript') return javascript()
  return sql({ dialect: MySQL })
}

export function ReadOnlyCode({ source, language, size = 'normal' }: { source: string; language?: string; size?: 'normal' | 'large' }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!ref.current) return
    const view = new EditorView({
      state: EditorState.create({
        doc: source,
        extensions: [
          languageExtension(language),
          syntaxHighlighting(sqlHighlight),
          EditorView.editable.of(false),
          EditorView.theme({
            '&': { fontFamily: 'var(--font-mono)', fontSize: size === 'large' ? '16px' : '13px' },
            '.cm-content': { padding: size === 'large' ? '24px' : '14px 16px' },
            '.cm-line': { lineHeight: '1.65' },
            '.cm-editor': { background: 'var(--cm-editor-bg)' },
            '.cm-gutters': { display: 'none' },
            '.cm-focused': { outline: 'none' },
          }),
        ],
      }),
      parent: ref.current,
    })
    return () => view.destroy()
  }, [source, language, size])

  return <div ref={ref} />
}
