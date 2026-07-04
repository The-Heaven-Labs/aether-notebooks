import { useState, useEffect, useMemo } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { Home, LayoutDashboard, Database, Users, UserCircle, ClipboardList, ChevronLeft, ChevronRight, Bot, Brain, Wrench, Puzzle, X, Settings, Zap, Trash2 } from 'lucide-react'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { useAuth } from '../hooks/useAuth'

const ALL_NAV_ITEMS = [
  { to: '/',           title: 'Files',       icon: <Home size={16} />,              desc: 'Browse notebooks, dashboards, and connectors organized in folders' },
  { to: '/dashboards', title: 'Dashboards',  icon: <LayoutDashboard size={16} />,   desc: 'Visual dashboards built from notebook query results' },
  { to: '/connectors', title: 'Connectors',  icon: <Database size={16} />,           desc: 'Database connections (PostgreSQL, ClickHouse, OpenSearch)' },
  { to: '/members',    title: 'Members',     icon: <UserCircle size={16} />,         desc: 'Organization members and role management' },
  { to: '/groups',     title: 'Groups',      icon: <Users size={16} />,              desc: 'Permission groups for access control' },
  { to: '/audit',      title: 'Audit',       icon: <ClipboardList size={16} />,      desc: 'Audit log of actions across the organization' },
  { to: '/trash',      title: 'Trash',       icon: <Trash2 size={16} />,             desc: 'Recently deleted items — restore or permanently delete' },
  { to: '/admin',      title: 'Admin',       icon: <Settings size={16} />,           desc: 'Instance-wide settings, users, and SSO configuration' },
]

const AGENT_NAV_ITEMS = [
  { to: '/agents',  title: 'Agents',  icon: <Bot size={16} />,    desc: 'Configure AI agents with instructions, tools, and models' },
  { to: '/models',  title: 'Models',  icon: <Brain size={16} />,   desc: 'LLM provider connections and model configurations' },
  { to: '/tools',   title: 'Tools',   icon: <Wrench size={16} />,  desc: 'Custom tools (webhooks, SQL queries) for agents' },
  { to: '/skills',  title: 'Skills',  icon: <Zap size={16} />,     desc: 'Reusable prompt snippets assignable to agents' },
  { to: '/mcps',    title: 'MCPs',    icon: <Puzzle size={16} />,  desc: 'Model Context Protocol servers for agent tool integration' },
]

// Custom event to open the mobile drawer from outside (e.g. TopBar hamburger)
const MOBILE_OPEN_EVENT = 'aether-sidebar-mobile-open'
export function openMobileSidebar() {
  window.dispatchEvent(new CustomEvent(MOBILE_OPEN_EVENT))
}

export function Sidebar() {
  const { user } = useAuth()
  const isPlatformAdmin = localStorage.getItem('aether_is_platform_admin') === 'true'
  const location = useLocation()
  const isMobile = useMediaQuery(768)
  const isTablet = useMediaQuery(1024)
  const NAV_ITEMS = useMemo(() =>
    ALL_NAV_ITEMS.filter(item => {
      if (item.to === '/audit') return user?.role === 'admin'
      if (item.to === '/admin') return isPlatformAdmin
      return true
    }),
    [user?.role, isPlatformAdmin]
  )

  const [expanded, setExpanded] = useState(() => {
    return localStorage.getItem('aether_sidebar_expanded') !== 'false'
  })
  const [drawerOpen, setDrawerOpen] = useState(false)

  // On mobile: listen for the custom open event
  useEffect(() => {
    const handler = () => setDrawerOpen(true)
    window.addEventListener(MOBILE_OPEN_EVENT, handler)
    return () => window.removeEventListener(MOBILE_OPEN_EVENT, handler)
  }, [])

  // On mobile: close drawer when route changes
  useEffect(() => {
    if (isMobile) setDrawerOpen(false)
  }, [location.pathname, isMobile])

  // On tablet: force collapsed (icon rail only)
  const effectiveExpanded = isTablet ? false : expanded

  const toggle = () => {
    if (isTablet) return // Can't expand on tablet
    const next = !expanded
    setExpanded(next)
    localStorage.setItem('aether_sidebar_expanded', String(next))
  }

  const width = effectiveExpanded ? 200 : 48

  const itemStyle = (isActive: boolean) => ({
    ...styles.item,
    justifyContent: effectiveExpanded ? 'flex-start' : 'center',
    padding: effectiveExpanded ? '8px 12px' : '8px 0',
    background: isActive ? 'var(--accent-light)' : 'transparent',
    color: isActive ? 'var(--accent)' : 'var(--nav-text-muted)',
  })

  // Shared nav content
  const navContent = (
    <>
      <div style={styles.items}>
        {NAV_ITEMS.map(({ to, title, icon, desc }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            title={desc}
            style={({ isActive }) => itemStyle(isActive)}
          >
            {({ isActive }) => (
              <>
                <span style={styles.icon}>{icon}</span>
                {effectiveExpanded && (
                  <span style={styles.label}>{title}</span>
                )}
                {isActive && <span className="sr-only"> (current page)</span>}
              </>
            )}
          </NavLink>
        ))}
        <div style={styles.sectionDivider} />
        {effectiveExpanded ? (
          <div style={styles.sectionHeader}>
            <span style={styles.sectionTitle}>AI Agents</span>
          </div>
        ) : (
          <div style={{ ...styles.sectionHeader, justifyContent: 'center' }}>
            <Bot size={14} style={{ color: 'var(--text-muted)' }} />
          </div>
        )}
        {AGENT_NAV_ITEMS.map(({ to, title, icon, desc }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            title={desc}
            style={({ isActive }) => itemStyle(isActive)}
          >
            {({ isActive }) => (
              <>
                <span style={styles.icon}>{icon}</span>
                {effectiveExpanded && (
                  <span style={styles.label}>{title}</span>
                )}
                {isActive && <span className="sr-only"> (current page)</span>}
              </>
            )}
          </NavLink>
        ))}
      </div>
      {!isTablet && (
        <button style={styles.toggle} onClick={toggle} title={effectiveExpanded ? 'Collapse sidebar' : 'Expand sidebar'}>
          {effectiveExpanded ? <ChevronLeft size={14} /> : <ChevronRight size={14} />}
        </button>
      )}
    </>
  )

  // Mobile: render as drawer overlay
  if (isMobile) {
    return (
      <>
        {drawerOpen && (
          <div style={styles.overlay} onClick={() => setDrawerOpen(false)} />
        )}
        <nav style={{ ...styles.sidebar, ...styles.drawer, transform: drawerOpen ? 'translateX(0)' : 'translateX(-100%)' }}>
          <div style={styles.drawerHeader}>
            <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--nav-text)' }}>Menu</span>
            <button style={styles.drawerCloseBtn} onClick={() => setDrawerOpen(false)} aria-label="Close sidebar">
              <X size={16} />
            </button>
          </div>
          {navContent}
        </nav>
      </>
    )
  }

  // Desktop/tablet: normal sidebar
  return (
    <nav style={{ ...styles.sidebar, width }}>
      {navContent}
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
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.4)',
    zIndex: 998,
  },
  drawer: {
    position: 'fixed',
    top: 0,
    left: 0,
    bottom: 0,
    width: 220,
    zIndex: 999,
    transition: 'transform 0.25s ease',
    boxShadow: '4px 0 24px rgba(0,0,0,0.15)',
  },
  drawerHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 16px',
    borderBottom: '1px solid var(--nav-border)',
  },
  drawerCloseBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--nav-text-muted)',
    padding: 4,
    borderRadius: 4,
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
