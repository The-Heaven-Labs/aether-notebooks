import { useState } from 'react'
import { NavLink } from 'react-router-dom'

const NAV_ITEMS = [
  { to: '/',           title: 'Notebooks',   icon: '▦' },
  { to: '/dashboards', title: 'Dashboards',  icon: '⊞' },
  { to: '/connectors', title: 'Connectors',  icon: '⚡' },
  { to: '/members',    title: 'Members',     icon: '👥' },
  { to: '/audit',      title: 'Audit',       icon: '📋' },
]

export function Sidebar() {
  const [expanded, setExpanded] = useState(() => {
    return localStorage.getItem('hnb_sidebar_expanded') === 'true'
  })

  const toggle = () => {
    const next = !expanded
    setExpanded(next)
    localStorage.setItem('hnb_sidebar_expanded', String(next))
  }

  const width = expanded ? 200 : 48

  return (
    <nav style={{ ...styles.sidebar, width }}>
      <div style={styles.items}>
        {NAV_ITEMS.map(({ to, title, icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            title={title}
            style={({ isActive }) => ({
              ...styles.item,
              background: isActive ? 'var(--accent-light)' : 'transparent',
              color: isActive ? 'var(--accent)' : 'var(--text-muted)',
            })}
          >
            <span style={styles.icon}>{icon}</span>
            {expanded && <span style={styles.label}>{title}</span>}
          </NavLink>
        ))}
      </div>
      <button style={styles.toggle} onClick={toggle} title={expanded ? 'Collapse sidebar' : 'Expand sidebar'}>
        {expanded ? '◀' : '▶'}
      </button>
    </nav>
  )
}

const styles: Record<string, React.CSSProperties> = {
  sidebar: {
    display: 'flex',
    flexDirection: 'column',
    background: 'var(--nav-bg)',
    borderRight: '1px solid var(--nav-border)',
    flexShrink: 0,
    transition: 'width 0.2s ease',
    overflow: 'hidden',
  },
  items: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    padding: '8px 0',
    gap: 2,
  },
  item: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '8px 12px',
    textDecoration: 'none',
    borderRadius: 6,
    margin: '0 4px',
    fontSize: 13,
    fontWeight: 500,
    whiteSpace: 'nowrap',
    transition: 'background 0.15s, color 0.15s',
  },
  icon: {
    fontSize: 16,
    flexShrink: 0,
    width: 24,
    textAlign: 'center',
  },
  label: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  toggle: {
    background: 'transparent',
    border: 'none',
    padding: '12px',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 11,
    borderTop: '1px solid var(--nav-border)',
  },
}
