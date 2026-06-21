import { useState, useEffect, useMemo } from 'react'
import { useLocation } from 'react-router-dom'
import { Bot } from 'lucide-react'
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'
import { ShortcutsModal } from './ShortcutsModal'
import { AgentPanel } from './AgentPanel'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from '../api/client'

interface Props {
  children: React.ReactNode
  noPadding?: boolean
}

export function AppShell({ children, noPadding }: Props) {
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showGlobalAgent, setShowGlobalAgent] = useState(() => {
    try {
      return localStorage.getItem('hnb:agentDocked:__global__') === 'true'
    } catch { return false }
  })
  const [globalAgentMinimized, setGlobalAgentMinimized] = useState(false)
  const [globalAgentDocked, setGlobalAgentDocked] = useState(() => {
    try {
      const saved = localStorage.getItem('hnb:agentDocked:__global__')
      return saved === 'true'
    } catch { /* ignore */ }
    return false
  })
  const [globalAgentWidth, setGlobalAgentWidth] = useState(() => {
    try {
      const saved = localStorage.getItem('hnb:agentPanelWidth:__global__')
      if (saved) return Math.max(320, Math.min(600, parseInt(saved, 10)))
    } catch { /* ignore */ }
    return 460
  })
  const [globalAgentHeight, setGlobalAgentHeight] = useState(() => {
    try {
      const saved = localStorage.getItem('hnb:agentPanelHeight:__global__')
      if (saved) return Math.max(200, Math.min(800, parseInt(saved, 10)))
    } catch { /* ignore */ }
    return 640
  })
  const [motds, setMotds] = useState<Array<{id: string; title: string; content: string; visibility: string; pages: string[]}>>([])
  const [dismissedMotds, setDismissedMotds] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem('hnb:dismissed_motds')
      if (stored) {
        const parsed = JSON.parse(stored)
        const now = Date.now()
        const filtered = Object.fromEntries(
          Object.entries(parsed).filter(([_, ts]) => now - (ts as number) < 86400000)
        )
        return new Set(Object.keys(filtered))
      }
    } catch {}
    return new Set()
  })

  useEffect(() => {
    api.get<Array<{id: string; title: string; content: string; visibility: string; pages: string[]}>>('/api/v1/motd')
      .then(setMotds)
      .catch(() => {})
  }, [])

  const dismissMotd = (id: string) => {
    setDismissedMotds(prev => {
      const next = new Set(prev)
      next.add(id)
      const stored = Object.fromEntries([...next].map(k => [k, Date.now()]))
      localStorage.setItem('hnb:dismissed_motds', JSON.stringify(stored))
      return next
    })
  }

  const location = useLocation()

  const currentPageContext = useMemo(() => {
    const path = location.pathname
    // /notebooks/:id — notebook page
    const nbMatch = path.match(/^\/notebooks\/([a-f0-9-]+)/)
    if (nbMatch) return { type: 'notebook' as const, id: nbMatch[1] }
    // /dashboards/:id — dashboard editor
    const dashEditMatch = path.match(/^\/dashboards\/([a-f0-9-]+)/)
    if (dashEditMatch) return { type: 'dashboard' as const, id: dashEditMatch[1] }
    // /dashboards — dashboard list
    if (path === '/dashboards') return { type: 'files' as const }
    // / — home/files
    if (path === '/') return { type: 'files' as const }
    return undefined
  }, [location.pathname])
  const currentNotebookId = currentPageContext?.type === 'notebook' ? (currentPageContext.id ?? '') : ''
  const visibleMotds = motds.filter(m => {
    if (dismissedMotds.has(m.id)) return false
    if (m.visibility === 'specific' && m.pages?.length) {
      return m.pages.some(p => location.pathname.startsWith(p))
    }
    return true
  })

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement).tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return
      if (e.key === '?') {
        e.preventDefault()
        setShowShortcuts(true)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement).tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        setShowGlobalAgent(v => !v)
        setGlobalAgentMinimized(false)
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  useEffect(() => {
    localStorage.setItem('hnb:agentPanelWidth:__global__', String(globalAgentWidth))
  }, [globalAgentWidth])

  return (
    <div style={styles.root}>
      <a href="#main-content" className="skip-link">Skip to content</a>
      <TopBar onShowShortcuts={() => setShowShortcuts(true)} />
      <div style={styles.body}>
        <Sidebar />
        <main id="main-content" style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'auto', display: 'flex', flexDirection: 'column' } : {}) }}>
          {visibleMotds.map(motd => (
            <div key={motd.id} style={motdStyles.banner}>
              <div style={motdStyles.bannerContent}>
                {motd.title && <strong style={motdStyles.bannerTitle}>{motd.title}:</strong>}
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{motd.content}</ReactMarkdown>
              </div>
              <button onClick={() => dismissMotd(motd.id)} style={motdStyles.bannerClose} title="Dismiss">×</button>
            </div>
          ))}
          {children}
        </main>
        {showGlobalAgent && !globalAgentMinimized && globalAgentDocked && (
          <AgentPanel
            notebookId={currentNotebookId}
            pageContext={currentPageContext}
            width={globalAgentWidth}
            onResize={(w) => { setGlobalAgentWidth(w); try { localStorage.setItem('hnb:agentPanelWidth:__global__', String(w)) } catch {} }}
            onClose={() => setShowGlobalAgent(false)}
            onMinimize={() => setGlobalAgentMinimized(true)}
            onDock={() => { setGlobalAgentDocked(false); try { localStorage.setItem('hnb:agentDocked:__global__', 'false') } catch {} }}
            docked
          />
        )}
      </div>
      {showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}

      <>
        {/* Global Agent FAB (floating action button) */}
          {!showGlobalAgent && (
            <button
              style={fabStyles.fab}
              onClick={() => { setShowGlobalAgent(true); setGlobalAgentMinimized(false) }}
              title="Open AI Agent (Ctrl+K)"
            >
              <Bot size={20} />
            </button>
          )}

          {/* Global Agent floating modal */}
          {showGlobalAgent && !globalAgentMinimized && !globalAgentDocked && (
            <>
              <div
                style={globalAgentStyles.backdrop}
                onClick={() => setShowGlobalAgent(false)}
              />
              <div style={{ ...globalAgentStyles.modal, width: globalAgentWidth, height: globalAgentHeight }} onClick={e => e.stopPropagation()}>
                <div
                  style={globalAgentStyles.vResizeHandle}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    const startY = e.clientY
                    const startH = globalAgentHeight
                    let lastClamped = startH
                    const onMove = (ev: MouseEvent) => {
                      const newH = startH + (startY - ev.clientY)
                      const clamped = Math.max(200, Math.min(800, newH))
                      lastClamped = clamped
                      setGlobalAgentHeight(clamped)
                    }
                    const onUp = () => {
                      try { localStorage.setItem('hnb:agentPanelHeight:__global__', String(lastClamped)) } catch {}
                      document.removeEventListener('mousemove', onMove)
                      document.removeEventListener('mouseup', onUp)
                    }
                    document.addEventListener('mousemove', onMove)
                    document.addEventListener('mouseup', onUp)
                  }}
                />
                <AgentPanel
                  notebookId={currentNotebookId}
                  pageContext={currentPageContext}
                  width={globalAgentWidth}
                  onResize={(w) => { setGlobalAgentWidth(w); try { localStorage.setItem('hnb:agentPanelWidth:__global__', String(w)) } catch {} }}
                  onClose={() => setShowGlobalAgent(false)}
                  onMinimize={() => setGlobalAgentMinimized(true)}
                  onDock={() => { setGlobalAgentDocked(true); try { localStorage.setItem('hnb:agentDocked:__global__', 'true') } catch {} }}
              docked={false}
            />
              </div>
            </>
          )}

          {/* Minimized agent bar */}
          {showGlobalAgent && globalAgentMinimized && (
            <div style={globalAgentStyles.minimizedBar} onClick={() => setGlobalAgentMinimized(false)}>
              <Bot size={16} />
              <span style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-primary)' }}>AI Agent (minimized)</span>
              <button
                style={globalAgentStyles.minimizedClose}
                onClick={e => { e.stopPropagation(); setShowGlobalAgent(false) }}
                title="Close agent"
              >
                ×
              </button>
            </div>
          )}
      </>
    </div>
  )
}

const globalAgentStyles: Record<string, React.CSSProperties> = {
  backdrop: {
    position: 'fixed', inset: 0, zIndex: 1500,
    background: 'transparent',
  },
  modal: {
    position: 'fixed', zIndex: 1501,
    bottom: 8, right: 24,
    maxWidth: 'calc(100vw - 48px)',
    maxHeight: 'calc(100vh - 16px)',
    borderRadius: 8, overflow: 'hidden',
    border: '1px solid var(--border)',
    boxShadow: '0 16px 48px rgba(0,0,0,0.3)',
    background: 'var(--bg-primary)',
    display: 'flex', flexDirection: 'column',
  },
  vResizeHandle: {
    position: 'absolute', top: 0, left: 0, right: 0,
    height: 6, cursor: 'ns-resize',
    zIndex: 10,
    background: 'transparent',
  },
  minimizedBar: {
    position: 'fixed', zIndex: 1501,
    bottom: 0, left: 0, right: 0,
    height: 44,
    display: 'flex', alignItems: 'center', gap: 8,
    padding: '0 16px',
    background: 'var(--bg-elevated)',
    borderTop: '1px solid var(--border)',
    cursor: 'pointer',
  },
  minimizedClose: {
    marginLeft: 'auto',
    background: 'none', border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 18,
    padding: '4px 8px',
    lineHeight: 1,
  },
}

const fabStyles: Record<string, React.CSSProperties> = {
  fab: {
    position: 'fixed', bottom: 24, right: 24, zIndex: 1400,
    width: 44, height: 44, borderRadius: '50%',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    background: 'var(--accent)', color: '#fff',
    border: 'none', cursor: 'pointer',
    boxShadow: '0 4px 16px rgba(0,0,0,0.25)',
    transition: 'transform 0.15s, box-shadow 0.15s',
  },
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', height: '100vh', maxHeight: '100vh', overflow: 'hidden', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0 },
  main: { flex: 1, overflow: 'auto', padding: '32px', minHeight: 0 },
}

const motdStyles: Record<string, React.CSSProperties> = {
  banner: {
    background: 'var(--warning-light)',
    borderBottom: '1px solid var(--warning-border)',
    borderLeft: '3px solid var(--accent)',
    padding: '10px 16px',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    fontSize: 13,
    fontWeight: 500,
    color: 'var(--text-primary)',
  },
  bannerContent: {
    flex: 1,
    lineHeight: 1.5,
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  bannerTitle: {
    fontWeight: 600,
    whiteSpace: 'nowrap',
  },
  bannerClose: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 20,
    padding: '0 6px',
    marginLeft: 12,
    lineHeight: 1,
    opacity: 0.7,
  },
}
