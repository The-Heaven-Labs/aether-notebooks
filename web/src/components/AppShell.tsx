import { useState, useEffect } from 'react'
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'
import { ShortcutsModal } from './ShortcutsModal'
import { AgentPanel } from './AgentPanel'

interface Props {
  children: React.ReactNode
  noPadding?: boolean
}

export function AppShell({ children, noPadding }: Props) {
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showGlobalAgent, setShowGlobalAgent] = useState(false)

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
        <main id="main-content" style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' } : {}) }}>{children}</main>
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
