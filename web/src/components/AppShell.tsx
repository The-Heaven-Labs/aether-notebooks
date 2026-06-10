import { useState, useEffect } from 'react'
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'
import { ShortcutsModal } from './ShortcutsModal'
import { AgentPanel } from './AgentPanel'
import { api } from '../api/client'

interface Props {
  children: React.ReactNode
  noPadding?: boolean
}

export function AppShell({ children, noPadding }: Props) {
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showGlobalAgent, setShowGlobalAgent] = useState(false)
  const [motds, setMotds] = useState<Array<{id: string; title: string; content: string}>>([])
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
    api.get<Array<{id: string; title: string; content: string}>>('/api/v1/motd')
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

  const visibleMotds = motds.filter(m => !dismissedMotds.has(m.id))

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
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        setShowGlobalAgent(v => !v)
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  return (
    <div style={styles.root}>
      <a href="#main-content" className="skip-link">Skip to content</a>
      <TopBar onShowShortcuts={() => setShowShortcuts(true)} />
      <div style={styles.body}>
        <Sidebar />
        <main id="main-content" style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' } : {}) }}>
          {visibleMotds.map(motd => (
            <div key={motd.id} style={motdStyles.banner}>
              <div style={motdStyles.bannerContent}>
                {motd.title && <strong style={motdStyles.bannerTitle}>{motd.title}:</strong>}
                <span dangerouslySetInnerHTML={{ __html: motd.content.replace(/\n/g, '<br/>') }} />
              </div>
              <button onClick={() => dismissMotd(motd.id)} style={motdStyles.bannerClose} title="Dismiss">×</button>
            </div>
          ))}
          {children}
        </main>
      </div>
      {showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}

      {showGlobalAgent && (
        <>
          <div
            style={globalAgentStyles.backdrop}
            onClick={() => setShowGlobalAgent(false)}
          />
          <div style={globalAgentStyles.modal} onClick={e => e.stopPropagation()}>
            <AgentPanel
              notebookId=""
              width={500}
              onResize={() => {}}
              onClose={() => setShowGlobalAgent(false)}
              onCellCreated={() => {}}
            />
          </div>
        </>
      )}
    </div>
  )
}

const globalAgentStyles: Record<string, React.CSSProperties> = {
  backdrop: {
    position: 'fixed', inset: 0, zIndex: 1500,
    background: 'rgba(0,0,0,0.5)',
  },
  modal: {
    position: 'fixed', zIndex: 1501,
    top: '10%', left: '50%', transform: 'translateX(-50%)',
    width: 500, maxWidth: '90vw', height: '70vh',
    borderRadius: 8, overflow: 'hidden',
    border: '1px solid var(--border)',
    boxShadow: '0 16px 48px rgba(0,0,0,0.3)',
  },
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', height: '100vh', maxHeight: '100vh', overflow: 'hidden', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0 },
  main: { flex: 1, overflow: 'auto', padding: '32px', minHeight: 0 },
}

const motdStyles: Record<string, React.CSSProperties> = {
  banner: {
    background: 'var(--accent-light)',
    borderBottom: '1px solid var(--accent)',
    padding: '8px 16px',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    fontSize: 13,
    color: 'var(--text-primary)',
  },
  bannerContent: {
    flex: 1,
    lineHeight: 1.5,
  },
  bannerTitle: {
    marginRight: 8,
  },
  bannerClose: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 18,
    padding: '0 4px',
    marginLeft: 8,
    lineHeight: 1,
  },
}
