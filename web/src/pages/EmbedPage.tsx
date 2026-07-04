import { useMemo } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { api } from '../api/client'
import { OutputRenderer } from '../components/OutputRenderer'
import { makeMarkdownComponents } from '../components/MarkdownCell'
import type { Notebook, Output, Parameter } from '../types'
import type { ChartConfig } from '../charts'

interface PublicCell {
  id: string
  position: number
  type: string
  language?: string
  source: string
  outputs: Output[]
  parameters?: Parameter[]
  metadata?: Record<string, unknown>
  outputs_hidden: boolean
}

export function EmbedPage() {
  const mdComponents = useMemo(() => makeMarkdownComponents(() => {}, true), [])
  const { token, cellId } = useParams<{ token: string; cellId: string }>()
  const [searchParams] = useSearchParams()
  const embedView = searchParams.get('view') as 'table' | 'chart' | null

  const { data, isLoading, error } = useQuery({
    queryKey: ['embed', token, cellId],
    queryFn: () => api.get<{ type: string; notebook: Notebook; cells: PublicCell[] }>(`/api/v1/public/${token}`),
    enabled: !!token,
  })

  if (isLoading) {
    return <div style={s.wrapper}><div style={s.loading} /></div>
  }
  if (error || !data) {
    return <div style={s.wrapper}><div style={s.error}>Unable to load content</div></div>
  }

  const cell = data.cells.find(c => c.id === cellId)
  if (!cell) {
    return <div style={s.wrapper}><div style={s.error}>Cell not found</div></div>
  }

  const meta = cell.metadata ?? {}
  const chartConfig = meta.chart as ChartConfig | undefined
  const viewMode = embedView ?? (meta.viewMode as 'table' | 'chart' | undefined)

  if (cell.type === 'text') {
    return (
      <div style={s.wrapper}>
        <div style={s.mdContainer}>
          <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={mdComponents}>
            {cell.source}
          </ReactMarkdown>
        </div>
      </div>
    )
  }

  if (cell.outputs_hidden || cell.outputs.length === 0) {
    return <div style={s.wrapper}><div style={s.error}>No output available</div></div>
  }

  return (
    <div style={s.wrapper}>
      <div style={s.outputContainer}>
        <OutputRenderer
          outputs={cell.outputs}
          chartConfig={chartConfig}
          viewMode={viewMode}
          fixedView={embedView ?? (viewMode === 'chart' ? 'chart' : undefined)}
          hideExport
        />
      </div>
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
  wrapper: {
    width: '100%',
    height: '100vh',
    overflow: 'hidden',
    background: 'var(--bg-primary)',
  },
  loading: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    minHeight: 120,
  },
  error: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    minHeight: 60,
    color: 'var(--text-muted)',
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
  },
  outputContainer: {
    width: '100%',
    height: '100%',
    minHeight: 0,
  },
  mdContainer: {
    padding: '12px 16px',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
    overflow: 'auto',
    height: '100%',
  },
}
