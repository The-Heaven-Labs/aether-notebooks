import { useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

interface TwoPanelLayoutProps {
  leftPanel: React.ReactNode
  rightPanel: React.ReactNode
  leftWidth?: number
}

export function TwoPanelLayout({ leftPanel, rightPanel, leftWidth = 240 }: TwoPanelLayoutProps) {
  const [collapsed, setCollapsed] = useState(() => {
    return localStorage.getItem('hnb_tree_collapsed') === 'true'
  })

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
        width: collapsed ? 0 : leftWidth,
        overflow: 'hidden',
        transition: 'width 0.2s ease',
        flexShrink: 0,
        borderRight: collapsed ? 'none' : '1px solid var(--border)',
        background: 'var(--bg-primary)',
      }}>
        {leftPanel}
      </div>

      {/* Toggle button */}
      <button
        style={{
          position: 'absolute',
          left: collapsed ? 0 : leftWidth,
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
        title={collapsed ? 'Expand tree' : 'Collapse tree'}
      >
        {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
      </button>

      {/* Right panel */}
      <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
        {rightPanel}
      </div>
    </div>
  )
}