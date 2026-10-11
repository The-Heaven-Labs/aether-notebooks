import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { interpolateText } from '../utils/interpolateText'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import type { DashboardVariable } from '../types'

/**
 * Reads a text panel's markdown out of a widget config. Absent or malformed
 * `config.text.markdown` yields `''` so callers can render the empty hint.
 */
// The public page (Task 15) imports this from the component module by design;
// the component itself still hot-reloads cleanly.
// eslint-disable-next-line react-refresh/only-export-components
export function textConfigFrom(config: Record<string, unknown> | undefined): { markdown: string } {
  const text = config?.text
  if (text && typeof text === 'object') {
    const markdown = (text as Record<string, unknown>).markdown
    if (typeof markdown === 'string') return { markdown }
  }
  return { markdown: '' }
}

interface TextWidgetProps {
  markdown: string
  /** Overrides the dashboard variables context; used by tests and the public page. */
  values?: Record<string, unknown>
  variables?: DashboardVariable[]
}

/**
 * Text panel: markdown with `{{variable}}` interpolation.
 *
 * Deliberately renders without `rehypeRaw` and keeps react-markdown's default
 * `urlTransform`: raw HTML (in the markdown or in interpolated values) shows
 * as literal text, so interpolation can never become a rendering surface.
 */
export function TextWidget({ markdown, values, variables }: TextWidgetProps) {
  const context = useDashboardVariables()
  const rendered = interpolateText(markdown, values ?? context.values, variables ?? context.variables)

  if (!rendered.trim()) {
    return <div style={styles.empty}>Empty text panel — edit it in the dashboard editor.</div>
  }

  return (
    <div style={styles.markdown}>
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{rendered}</ReactMarkdown>
    </div>
  )
}

// Same treatment as DashboardPage/DashboardEditorPage's cell-linked text
// cells, so a text panel reads exactly like a markdown cell.
const styles: Record<string, React.CSSProperties> = {
  markdown: {
    padding: '16px',
    fontSize: 14,
    color: 'var(--text-primary)',
    lineHeight: 1.6,
    overflow: 'auto',
    height: '100%',
    wordBreak: 'break-word',
  },
  empty: {
    padding: '16px',
    fontSize: 13,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
}
