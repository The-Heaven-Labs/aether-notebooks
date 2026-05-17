import { useState, useEffect } from 'react'
import { ChevronLeft, ChevronRight, Menu } from 'lucide-react'

interface TwoPanelLayoutProps {
  leftPanel: React.ReactNode
  rightPanel: React.ReactNode
  leftWidth?: number
}

export function TwoPanelLayout({ leftPanel, rightPanel, leftWidth = 240 }: TwoPanelLayoutProps) {
  const [collapsed, setCollapsed] = useState(() => {
    return localStorage.getItem('hnb_tree_collapsed') === 'true'
  })

  const [isMobile, setIsMobile] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => {
    const check = () => setIsMobile(window.innerWidth < 768)
    check()
    window.addEventListener('resize', check)
    return () => window.removeEventListener('resize', check)
  }, [])

  const toggle = () => {
    setCollapsed(prev => {
      const next = !prev
      localStorage.setItem('hnb_tree_collapsed', String(next))
      return next
    })
  }

  return (
    <div style={{ display: 'flex', flex: 1, overflow: 'hidden', position: 'relative' }}>
      {/* Left panel */}
      <div style={{
        width: isMobile ? 0 : (collapsed ? 0 : leftWidth),
        overflow: 'hidden',
        transition: 'width 0.2s ease',
        flexShrink: 0,
        borderRight: isMobile ? 'none' : (collapsed ? 'none' : '1px solid var(--border)'),
        background: 'var(--bg-primary)',
      }}>
        {leftPanel}
      </div>

      {/* Toggle button (desktop only) */}
      {!isMobile && (
        <button
          style={{
            position: 'fixed',
            left: collapsed ? 32 : 248,
            top: '50%',
            transform: 'translateY(-50%)',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            borderRadius: 4,
            padding: 4,
            cursor: 'pointer',
            zIndex: 10,
            display: 'flex',
            transition: 'left 0.2s ease',
          }}
          onClick={toggle}
          title={collapsed ? 'Expand folder tree' : 'Collapse folder tree'}
        >
          {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
        </button>
      )}

      {/* Right panel */}
      <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
        {rightPanel}
      </div>

      {/* Mobile drawer overlay */}
      {isMobile && drawerOpen && (
        <>
          <div
            style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', zIndex: 100 }}
            onClick={() => setDrawerOpen(false)}
          />
          <div style={{
            position: 'fixed',
            left: 0,
            top: 0,
            bottom: 0,
            width: 280,
            background: 'var(--bg-primary)',
            zIndex: 101,
            overflow: 'auto',
            boxShadow: 'var(--shadow-md)',
          }}>
            <div style={{ display: 'flex', justifyContent: 'flex-end', padding: 8 }}>
              <button
                onClick={() => setDrawerOpen(false)}
                style={{
                  background: 'var(--bg-secondary)',
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  padding: '4px 8px',
                  cursor: 'pointer',
                  fontSize: 18,
                }}
              >
                ×
              </button>
            </div>
            {leftPanel}
          </div>
        </>
      )}

      {/* Floating menu button on mobile */}
      {isMobile && !drawerOpen && (
        <button
          style={{
            position: 'fixed',
            bottom: 20,
            left: 20,
            zIndex: 50,
            background: 'var(--accent)',
            color: '#fff',
            border: 'none',
            borderRadius: '50%',
            width: 48,
            height: 48,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            boxShadow: 'var(--shadow-md)',
            cursor: 'pointer',
          }}
          onClick={() => setDrawerOpen(true)}
        >
          <Menu size={20} />
        </button>
      )}
    </div>
  )
}
