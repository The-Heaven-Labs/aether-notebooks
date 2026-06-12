import { useState, useEffect, useCallback, useMemo } from 'react'
import { useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { OutputRenderer } from '../components/OutputRenderer'
import { makeMarkdownComponents } from '../components/MarkdownCell'
import type { Notebook, Cell, Output } from '../types'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

export function PresentationPage() {
  const { id } = useParams<{ id: string }>()
  const [notebook, setNotebook] = useState<NotebookWithCells | null>(null)
  const [index, setIndex] = useState(0)

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
    // Each cell is its own slide by default.
    // slide_break=true means "join this cell with the previous slide".
    return cells.reduce<Cell[][]>((acc, cell) => {
      if (cell.slide_break && acc.length > 0) acc[acc.length - 1].push(cell)
      else acc.push([cell])
      return acc
    }, [])
  }, [notebook?.cells])

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

  if (!notebook || slides.length === 0) {
    return <div style={styles.loading}>Loading…</div>
  }

  return (
    <div style={styles.page}>
      <div style={styles.content}>
        <div style={styles.slideContainer}>
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
                {/* Source: shown unless source_visible is explicitly false */}
                {cell.source_visible !== false && (
                  <pre style={styles.codePre}>{cell.source}</pre>
                )}
                {/* Output: shown unless outputs_hidden is explicitly true */}
                {cell.outputs_hidden !== true && (cell.outputs ?? []).length > 0 && (
                  <OutputRenderer outputs={cell.outputs as Output[]} cellId={cell.id} />
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
        <span style={styles.progress}>{index + 1} / {total}</span>
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
    background: '#0d0d0d',
    color: '#888',
    fontSize: 16,
    fontFamily: 'var(--font-sans, system-ui)',
  },
  page: {
    display: 'flex',
    flexDirection: 'column',
    height: '100vh',
    background: '#0d0d0d',
    color: '#f0f0f0',
    fontFamily: 'var(--font-sans, system-ui)',
    overflow: 'hidden',
  },
  content: {
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'auto',
    padding: '40px',
  },
  slideContainer: {
    width: '100%',
    maxWidth: 900,
    minHeight: 200,
  },
  markdownSlide: {
    fontSize: 24,
    lineHeight: 1.6,
    color: '#f0f0f0',
  },
  codeSlide: {
    background: '#1a1a1a',
    borderRadius: 4,
    overflow: 'hidden',
  },
  codePre: {
    margin: 0,
    padding: '24px',
    fontSize: 16,
    fontFamily: 'var(--font-mono, monospace)',
    color: '#cdd6f4',
    whiteSpace: 'pre-wrap',
    overflowX: 'auto',
  },
  nav: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 40px',
    background: '#1a1a1a',
    borderTop: '1px solid #2a2a2a',
    flexShrink: 0,
  },
  navBtn: {
    padding: '10px 24px',
    background: '#2a2a2a',
    color: '#f0f0f0',
    border: '1px solid #3a3a3a',
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
    color: '#888',
    fontVariantNumeric: 'tabular-nums',
  },
}
