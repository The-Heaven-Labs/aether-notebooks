import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useParams } from 'react-router-dom'
import { Sun, Moon } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { OutputRenderer } from '../components/OutputRenderer'
import { makeMarkdownComponents } from '../components/MarkdownCell'
import type { ChartConfig } from '../charts'
import type { Notebook, Cell, Output } from '../types'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

export function PresentationPage() {
  const { id } = useParams<{ id: string }>()
  const [notebook, setNotebook] = useState<NotebookWithCells | null>(null)
  const [index, setIndex] = useState(0)
  const [theme, setTheme] = useState<'light' | 'dark'>(() => {
    try {
      return (localStorage.getItem('hnb_theme') ?? 'dark') as 'light' | 'dark'
    } catch { return 'dark' }
  })
  const contentRef = useRef<HTMLDivElement>(null)
  const slideRef = useRef<HTMLDivElement>(null)
  const [slideScale, setSlideScale] = useState(1)

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  const toggleTheme = useCallback(() => {
    setTheme(prev => {
      const next = prev === 'dark' ? 'light' : 'dark'
      try { localStorage.setItem('hnb_theme', next) } catch {}
      return next
    })
  }, [])

  useEffect(() => {
    const token = localStorage.getItem('hnb_token')
    fetch(`/api/v1/notebooks/${id}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {}
    })
      .then(r => r.json())
      .then(setNotebook)
  }, [id])

  const slides = useMemo(() => {
    const cells = notebook?.cells ?? []
    return cells.reduce<Cell[][]>((acc, cell) => {
      if (cell.slide_break && acc.length > 0) acc[acc.length - 1].push(cell)
      else acc.push([cell])
      return acc
    }, [])
  }, [notebook?.cells])

  const getChartConfig = useCallback((cell: Cell): ChartConfig | undefined => {
    return cell.metadata?.chart as ChartConfig | undefined
  }, [])

  const total = slides.length
  const currentSlide = slides[index] ?? []

  const prev = useCallback(() => setIndex(i => Math.max(0, i - 1)), [])
  const next = useCallback(() => setIndex(i => Math.min(total - 1, i + 1)), [total])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next()
      if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') prev()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [next, prev])

  // Auto-scale slides to fit viewport without scrollbars
  useEffect(() => {
    const slide = slideRef.current
    const container = contentRef.current
    if (!slide || !container) return

    const recompute = () => {
      const pad = 32
      const availW = container.clientWidth - pad * 2
      const availH = container.clientHeight - pad * 2
      const natW = slide.scrollWidth
      const natH = slide.scrollHeight
      const scale = Math.min(1, availW / Math.max(natW, 1), availH / Math.max(natH, 1))
      setSlideScale(scale)
    }

    recompute()
    const ro = new ResizeObserver(recompute)
    ro.observe(slide)
    window.addEventListener('resize', recompute)
    return () => {
      ro.disconnect()
      window.removeEventListener('resize', recompute)
    }
  }, [index, notebook])

  if (!notebook || slides.length === 0) {
    return <div style={styles.loading}>Loading…</div>
  }

  return (
    <div style={styles.page}>
      <div ref={contentRef} style={styles.content}>
        <div ref={slideRef} style={{ ...styles.slideWrapper, transform: `scale(${slideScale})` }}>
          {currentSlide.map((cell, i) => (
            cell.type === 'text' ? (
              <div key={cell.id} style={styles.markdownSlide}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={makeMarkdownComponents(() => {}, true)}>
                  {cell.source}
                </ReactMarkdown>
              </div>
            ) : (
              <div
                key={cell.id}
                style={{
                  ...styles.codeSlide,
                  ...(i > 0 ? { marginTop: '2rem' } : {}),
                }}
              >
                {cell.source_visible !== false && (
                  <pre style={styles.codePre}>{cell.source}</pre>
                )}
                {cell.outputs_hidden !== true && (cell.outputs ?? []).length > 0 && (
                  <OutputRenderer
                    outputs={cell.outputs as Output[]}
                    cellId={cell.id}
                    chartConfig={getChartConfig(cell)}
                    hideExport
                  />
                )}
              </div>
            )
          ))}
        </div>
      </div>
      <div style={styles.nav}>
        <button
          style={{ ...styles.navBtn, ...(index === 0 ? styles.navBtnDisabled : {}) }}
          onClick={prev}
          disabled={index === 0}
          aria-label="Previous"
        >
          ← Previous
        </button>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={styles.progress}>{index + 1} / {total}</span>
          <button
            style={styles.themeBtn}
            onClick={toggleTheme}
            title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
            aria-label="Toggle theme"
          >
            {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
          </button>
        </div>
        <button
          style={{ ...styles.navBtn, ...(index === total - 1 ? styles.navBtnDisabled : {}) }}
          onClick={next}
          disabled={index === total - 1}
          aria-label="Next"
        >
          Next →
        </button>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  loading: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100vh',
    background: 'var(--bg-primary)',
    color: 'var(--text-muted)',
    fontSize: 16,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  page: {
    display: 'flex',
    flexDirection: 'column',
    height: '100vh',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans, system-ui)',
    overflow: 'hidden',
  },
  content: {
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
    padding: 32,
  },
  slideWrapper: {
    width: '100%',
    maxWidth: 900,
  },
  markdownSlide: {
    fontSize: 24,
    lineHeight: 1.6,
    color: 'var(--text-primary)',
  },
  codeSlide: {
    background: 'var(--bg-elevated)',
    borderRadius: 4,
    overflow: 'hidden',
  },
  codePre: {
    margin: 0,
    padding: '24px',
    fontSize: 16,
    fontFamily: 'var(--font-mono, monospace)',
    color: 'var(--text-primary)',
    whiteSpace: 'pre-wrap',
    overflowX: 'auto',
  },
  nav: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 40px',
    background: 'var(--bg-elevated)',
    borderTop: '1px solid var(--border)',
    flexShrink: 0,
  },
  navBtn: {
    padding: '10px 24px',
    background: 'var(--bg-secondary)',
    color: 'var(--text-primary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 14,
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'background 0.15s',
    fontFamily: 'inherit',
  },
  navBtnDisabled: {
    opacity: 0.35,
    cursor: 'not-allowed',
  },
  progress: {
    fontSize: 14,
    color: 'var(--text-muted)',
    fontVariantNumeric: 'tabular-nums',
  },
  themeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 32,
    height: 32,
    background: 'var(--bg-secondary)',
    color: 'var(--text-muted)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    fontFamily: 'inherit',
  },
}
