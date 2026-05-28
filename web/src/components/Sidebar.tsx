import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { Home, LayoutDashboard, Database, Users, UsersRound, ClipboardList, ChevronLeft, ChevronRight, Bot, Brain, Wrench, Puzzle } from 'lucide-react'

const NAV_ITEMS = [
  { to: '/',           title: 'Files',       icon: <Home size={16} /> },
  { to: '/dashboards', title: 'Dashboards',  icon: <LayoutDashboard size={16} /> },
  { to: '/connectors', title: 'Connectors',  icon: <Database size={16} /> },
  { to: '/members',    title: 'Members',     icon: <Users size={16} /> },
  { to: '/groups',     title: 'Groups',      icon: <UsersRound size={16} /> },
  { to: '/audit',      title: 'Audit',       icon: <ClipboardList size={16} /> },
]

const AGENT_NAV_ITEMS = [
  { to: '/agents',  title: 'Agents',  icon: <Bot size={16} /> },
  { to: '/models',  title: 'Models',  icon: <Brain size={16} /> },
  { to: '/skills',  title: 'Skills',  icon: <Wrench size={16} /> },
  { to: '/mcps',    title: 'MCPs',    icon: <Puzzle size={16} /> },
]

export function Sidebar() {
  const [expanded, setExpanded] = useState(() => {
    return localStorage.getItem('hnb_sidebar_expanded') !== 'false'
  })

  const toggle = () => {
    const next = !expanded
    setExpanded(next)
    localStorage.setItem('hnb_sidebar_expanded', String(next))
  }

  const width = expanded ? 200 : 48

  const itemStyle = (isActive: boolean) => ({
    ...styles.item,
    justifyContent: expanded ? 'flex-start' : 'center',
    padding: expanded ? '8px 12px' : '8px 0',
    background: isActive ? 'var(--accent-light)' : 'transparent',
    color: isActive ? 'var(--accent)' : 'var(--nav-text-muted)',
  })

  return (
    <nav style={{ ...styles.sidebar, width }}>
      <div style={styles.items}>
        {NAV_ITEMS.map(({ to, title, icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            title={title}
            style={({ isActive }) => itemStyle(isActive)}
          >
            <span style={styles.icon}>{icon}</span>
            {expanded && (
              <span style={styles.label}>{title}</span>
            )}
          </NavLink>
        ))}
        <div style={styles.sectionDivider} />
        {expanded ? (
          <div style={styles.sectionHeader}>
            <span style={styles.sectionTitle}>AI Agents</span>
          </div>
        ) : (
          <div style={{ ...styles.sectionHeader, justifyContent: 'center' }}>
            <Bot size={14} style={{ color: 'var(--text-muted)' }} />
          </div>
        )}
        {AGENT_NAV_ITEMS.map(({ to, title, icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === to}
            title={title}
            style={({ isActive }) => itemStyle(isActive)}
          >
            <span style={styles.icon}>{icon}</span>
            {expanded && (
              <span style={styles.label}>{title}</span>
            )}
          </NavLink>
        ))}
      </div>
      <button style={styles.toggle} onClick={toggle} title={expanded ? 'Collapse sidebar' : 'Expand sidebar'}>
        {expanded ? <ChevronLeft size={14} /> : <ChevronRight size={14} />}
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
    justifyContent: 'flex-start',
    gap: 10,
    padding: '8px 12px',
    textDecoration: 'none',
    borderRadius: 6,
    margin: '0 9px',
    fontSize: 13,
    fontWeight: 500,
    whiteSpace: 'nowrap',
    transition: 'background 0.15s, color 0.15s',
  },
  icon: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    width: 15,
    height: 15,
  },
  label: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  toggle: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'transparent',
    border: 'none',
    padding: '12px',
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    borderTop: '1px solid var(--nav-border)',
  },
  sectionDivider: {
    height: 1,
    background: 'var(--nav-border)',
    margin: '8px 9px',
  },
  sectionHeader: {
    display: 'flex',
    alignItems: 'center',
    padding: '4px 18px',
    marginTop: 4,
    marginBottom: 2,
  },
  sectionTitle: {
    fontSize: 10,
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
    color: 'var(--text-muted)',
  },
}
