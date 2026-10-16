import { useState, useEffect, useMemo, createContext, useContext, lazy, Suspense } from 'react'
import { useLocation, matchPath } from 'react-router-dom'
import { Bot } from 'lucide-react'
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'
import { api } from '../api/client'

// Heavy, interaction-gated pieces of the shell. Keeping them lazy means pages
// don't pay for the agent chat UI, the shortcuts dialog, the search palette,
// or the markdown renderer until the user actually opens them.
const AgentPanel = lazy(() => import('./AgentPanel').then((m) => ({ default: m.AgentPanel })))
const ShortcutsModal = lazy(() => import('./ShortcutsModal').then((m) => ({ default: m.ShortcutsModal })))
const SearchPalette = lazy(() => import('./SearchPalette').then((m) => ({ default: m.SearchPalette })))
const MotdMarkdown = lazy(() => import('./MotdMarkdown'))

/**
 * True when a key event originates in a text-entry control, where global
 * chords (Ctrl/Cmd+K, Ctrl/Cmd+J) must not fire over the user's typing.
 */
function isTextEntryTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el) return false
  return el.tagName === 'INPUT'
    || el.tagName === 'TEXTAREA'
    || el.tagName === 'SELECT'
    || el.isContentEditable
}

interface Props {
  children: React.ReactNode
  noPadding?: boolean
  /**
   * Chrome-free rendering for the App surface (/dashboards/:id/view): no
   * TopBar, Sidebar, skip link, or MOTD banners. The global agent FAB/panel
   * is still rendered so the AI assistant is reachable there; the search
   * palette and the `?` shortcuts modal stay unavailable on the bare surface.
   * Every shell hook above still runs (rules of hooks); only the shell markup
   * is skipped. Supersedes `noPadding`.
   */
  bare?: boolean
}

const AgentPanelLayoutContext = createContext<number>(0)

export function useAgentPanelLayoutVersion() {
  return useContext(AgentPanelLayoutContext)
}

export function AppShell({ children, noPadding, bare }: Props) {
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showSearch, setShowSearch] = useState(false)
  const [showGlobalAgent, setShowGlobalAgent] = useState(() => {
    try {
      return localStorage.getItem('aether:agentDocked:__global__') === 'true'
    } catch { return false }
  })
  const [globalAgentMinimized, setGlobalAgentMinimized] = useState(false)
  const [globalAgentDocked, setGlobalAgentDocked] = useState(() => {
    try {
      const saved = localStorage.getItem('aether:agentDocked:__global__')
      return saved === 'true'
    } catch { /* ignore */ }
    return false
  })
  const [globalAgentWidth, setGlobalAgentWidth] = useState(() => {
    try {
      const saved = localStorage.getItem('aether:agentPanelWidth:__global__')
      if (saved) return Math.max(320, Math.min(600, parseInt(saved, 10)))
    } catch { /* ignore */ }
    return 460
  })
  const [globalAgentHeight, setGlobalAgentHeight] = useState(() => {
    try {
      const saved = localStorage.getItem('aether:agentPanelHeight:__global__')
      if (saved) return Math.max(200, Math.min(800, parseInt(saved, 10)))
    } catch { /* ignore */ }
    return 640
  })
  const [viewportHeight, setViewportHeight] = useState(() =>
    typeof window !== 'undefined' ? window.innerHeight : 800
  )
  const [layoutVersion, setLayoutVersion] = useState(0)

  useEffect(() => {
    const handle = () => setViewportHeight(window.innerHeight)
    window.addEventListener('resize', handle)
    return () => window.removeEventListener('resize', handle)
  }, [])
  const [motds, setMotds] = useState<Array<{id: string; title: string; content: string; visibility: string; pages: string[]}>>([])
  const [dismissedMotds, setDismissedMotds] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem('aether:dismissed_motds')
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
    // The app surface renders no MOTD banners, so skip the fetch entirely.
    if (bare) return
    api.get<Array<{id: string; title: string; content: string; visibility: string; pages: string[]}>>('/api/v1/motd')
      .then(setMotds)
      .catch(() => {})
  }, [bare])

  const dismissMotd = (id: string) => {
    setDismissedMotds(prev => {
      const next = new Set(prev)
      next.add(id)
      const stored = Object.fromEntries([...next].map(k => [k, Date.now()]))
      localStorage.setItem('aether:dismissed_motds', JSON.stringify(stored))
      return next
    })
  }

  const location = useLocation()

  // The chat page renders its own full-page chat; a second (global) panel would
  // fight it over singleton storage keys and the session WebSocket.
  const isChatPage = matchPath('/chats/:id', location.pathname) !== null

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
    // The shortcuts modal stays off the bare App surface, so its key stays
    // unbound there.
    if (bare) return
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
  }, [bare])

  useEffect(() => {
    // Ctrl/Cmd+J toggles the global agent panel; it moved off Ctrl/Cmd+K when
    // search took that chord. The agent keeps its /chats suppression (that
    // page owns a full-page chat) and the typing guard.
    //
    // The agent panel is also available in bare mode (App surface), so the
    // bare guard is intentionally omitted. The /chats page still owns its own
    // full-page chat and skips the global panel.
    if (isChatPage) return
    const handler = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.key !== 'j') return
      if (isTextEntryTarget(e.target)) return
      e.preventDefault()
      setShowGlobalAgent(v => !v)
      setGlobalAgentMinimized(false)
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [isChatPage])

  useEffect(() => {
    // Ctrl/Cmd+K opens the search palette. Search is global — unlike the agent
    // chord it stays enabled on /chats — but `bare` mode strips the palette,
    // so the chord stays unbound there.
    if (bare) return
    const handler = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.key !== 'k') return
      if (isTextEntryTarget(e.target)) return
      e.preventDefault()
      setShowSearch(true)
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [bare])

  // The notebook Chats drawer's "New chat" action opens the panel; the panel
  // itself listens for the same event to reset the session (and start a fresh
  // one attached to the current notebook when an agent is selected).
  useEffect(() => {
    const handler = () => {
      if (isChatPage) return
      setShowGlobalAgent(true)
      setGlobalAgentMinimized(false)
    }
    window.addEventListener('aether:new-agent-chat', handler)
    return () => window.removeEventListener('aether:new-agent-chat', handler)
  }, [isChatPage])

  useEffect(() => {
    localStorage.setItem('aether:agentPanelWidth:__global__', String(globalAgentWidth))
  }, [globalAgentWidth])

  useEffect(() => {
    setLayoutVersion(v => v + 1)
  }, [showGlobalAgent, globalAgentMinimized, globalAgentDocked])

  // All hooks above: the bare branch below is the last statement before the
  // shell markup, so hook order is stable across both modes. The bare App
  // surface keeps a main landmark plus the agent overlay; everything else is
  // shell chrome.
  const agentOverlay = (
    <>
      {/* Global Agent FAB (floating action button). On the bare App surface
          minimizing restores the FAB instead of the minimized bar so the
          chrome stays minimal; non-bare keeps the separate minimized bar. */}
      {!isChatPage && (!showGlobalAgent || (bare && globalAgentMinimized)) && (
        <button
          style={fabStyles.fab}
          onClick={() => { setShowGlobalAgent(true); setGlobalAgentMinimized(false) }}
          title="Open AI Agent (Ctrl+J)"
        >
          <Bot size={20} />
        </button>
      )}

      {/* Single AgentPanel instance — never unmounts across dock/undock */}
      {!isChatPage && showGlobalAgent && !globalAgentMinimized && (
        <div style={globalAgentStyles.floatingWrapper}>
          <div style={{
            ...(globalAgentDocked ? globalAgentStyles.docked : globalAgentStyles.modal),
            top: globalAgentDocked ? 52 : Math.max(52, viewportHeight - globalAgentHeight - 8),
            height: globalAgentDocked ? viewportHeight - 52 : globalAgentHeight,
            width: globalAgentWidth,
            right: globalAgentDocked ? 0 : 24,
          }}>
            {!globalAgentDocked && (
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
                    try { localStorage.setItem('aether:agentPanelHeight:__global__', String(lastClamped)) } catch {}
                    document.removeEventListener('mousemove', onMove)
                    document.removeEventListener('mouseup', onUp)
                  }
                  document.addEventListener('mousemove', onMove)
                  document.addEventListener('mouseup', onUp)
                }}
              />
            )}
            <Suspense fallback={<div style={globalAgentStyles.panelLoading}>Loading agent…</div>}>
              <AgentPanel
                notebookId={currentNotebookId}
                pageContext={currentPageContext}
                width={globalAgentWidth}
                onResize={(w) => { setGlobalAgentWidth(w); try { localStorage.setItem('aether:agentPanelWidth:__global__', String(w)) } catch {} }}
                onClose={() => { setShowGlobalAgent(false); try { localStorage.setItem('aether:agentDocked:__global__', 'false') } catch {} }}
                onMinimize={() => setGlobalAgentMinimized(true)}
                onDock={() => { setGlobalAgentDocked(d => { const next = !d; try { localStorage.setItem('aether:agentDocked:__global__', String(next)) } catch {}; return next; }) }}
                docked={globalAgentDocked}
              />
            </Suspense>
          </div>
        </div>
      )}

      {/* Minimized agent bar — hidden on the bare App surface, where minimize
          restores the FAB instead. */}
      {!isChatPage && showGlobalAgent && globalAgentMinimized && !bare && (
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
  )

  if (bare) return (
    <>
      <main id="main-content">{children}</main>
      {agentOverlay}
    </>
  )

  return (
    <AgentPanelLayoutContext.Provider value={layoutVersion}>
    <div className="app-shell" style={styles.root}>
      <a href="#main-content" className="skip-link">Skip to content</a>
      <TopBar
        onShowShortcuts={() => setShowShortcuts(true)}
        onOpenSearch={() => setShowSearch(true)}
      />
      <div style={{
        ...styles.body,
        ...(showGlobalAgent && !globalAgentMinimized && globalAgentDocked && !isChatPage ? { paddingRight: globalAgentWidth } : {}),
      }}>
        <Sidebar />
        <main id="main-content" style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'auto', display: 'flex', flexDirection: 'column' } : {}) }}>
          {visibleMotds.map(motd => (
            <div key={motd.id} style={motdStyles.banner}>
              <div style={motdStyles.bannerContent}>
                {motd.title && <strong style={motdStyles.bannerTitle}>{motd.title}:</strong>}
                <Suspense fallback={null}>
                  <MotdMarkdown content={motd.content} />
                </Suspense>
              </div>
              <button onClick={() => dismissMotd(motd.id)} style={motdStyles.bannerClose} title="Dismiss">×</button>
            </div>
          ))}
          {children}
        </main>
      </div>
      {showShortcuts && (
        <Suspense fallback={null}>
          <ShortcutsModal onClose={() => setShowShortcuts(false)} />
        </Suspense>
      )}

      {/* Search palette: mounted on first open so its chunk stays off the
          shell's critical path, same as the shortcuts modal. The palette owns
          its input, reset, and Escape; AppShell only holds the open bit. */}
      {showSearch && (
        <Suspense fallback={null}>
          <SearchPalette open onClose={() => setShowSearch(false)} />
        </Suspense>
      )}

      {agentOverlay}
    </div>
    </AgentPanelLayoutContext.Provider>
  )
}

const globalAgentStyles: Record<string, React.CSSProperties> = {
  floatingWrapper: {
    position: 'fixed', inset: 0, zIndex: 1500,
    pointerEvents: 'none',
  },
  panelLoading: {
    flex: 1,
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    color: 'var(--text-muted)',
    fontSize: 13,
  },
  modal: {
    position: 'fixed', zIndex: 1501,
    borderRadius: 8, overflow: 'hidden',
    border: '1px solid var(--border)',
    boxShadow: '0 16px 48px rgba(0,0,0,0.3)',
    background: 'var(--bg-primary)',
    display: 'flex', flexDirection: 'column',
    pointerEvents: 'auto',
    transformOrigin: 'bottom right',
    // Intentional: agent panel float/dock/minimize morph. The panel's
    // geometry genuinely changes; FLIP would need measured layout.
    transition: 'top 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), right 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), height 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), border-radius 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), box-shadow 0.7s cubic-bezier(0.05, 0.7, 0.1, 1)',
  },
  docked: {
    position: 'fixed', zIndex: 1501,
    border: 'none',
    borderLeft: '1px solid var(--border)',
    borderRadius: 0,
    boxShadow: 'none',
    overflow: 'hidden',
    background: 'var(--bg-primary)',
    display: 'flex', flexDirection: 'column',
    pointerEvents: 'auto',
    transformOrigin: 'bottom right',
    // Intentional: agent panel float/dock/minimize morph. The panel's
    // geometry genuinely changes; FLIP would need measured layout.
    transition: 'top 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), right 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), height 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), border-radius 0.7s cubic-bezier(0.05, 0.7, 0.1, 1), box-shadow 0.7s cubic-bezier(0.05, 0.7, 0.1, 1)',
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
    background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)',
    border: 'none', cursor: 'pointer',
    boxShadow: '0 4px 16px rgba(0,0,0,0.25)',
    transition: 'transform 0.15s, box-shadow 0.15s',
  },
}

const styles: Record<string, React.CSSProperties> = {
  // Height (100vh with a 100dvh upgrade) lives on .app-shell in theme.css so
  // dynamic-viewport browsers don't scroll a 100vh-tall shell out of view.
  root: { display: 'flex', flexDirection: 'column', overflow: 'hidden', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0 },
  main: { flex: 1, overflow: 'auto', padding: '32px', minHeight: 0 },
}

const motdStyles: Record<string, React.CSSProperties> = {
  banner: {
    background: 'var(--warning-light)',
    borderBottom: '1px solid var(--warning-border)',
    borderLeft: '1px solid var(--accent)',
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
