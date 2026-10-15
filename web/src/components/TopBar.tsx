import { useState, useRef, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { Keyboard, Menu, Search } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { isApplePlatform } from '../utils/platform'
import { Kbd } from './Kbd'
import { openMobileSidebar } from './Sidebar'

function LogoMark() {
  return (
    <svg style={{ display: 'block' }} width="28" height="28" viewBox="0 0 32 32">
      <rect x="1" y="1" width="12" height="12" rx="2" fill="#6366F1"/>
      <rect x="15" y="1" width="12" height="12" rx="2" fill="#6366F1"/>
      <rect x="1" y="15" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <rect x="8" y="15" width="5" height="5" rx="1" fill="#6366F1"/>
      <rect x="15" y="15" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <rect x="22" y="15" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <rect x="1" y="22" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <rect x="8" y="22" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <rect x="15" y="22" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <rect x="22" y="22" width="5" height="5" rx="1" fill="#6366F1" fillOpacity="0.4"/>
      <circle cx="10.5" cy="10.5" r="1.5" fill="white"/>
    </svg>
  )
}

interface TopBarProps {
  onShowShortcuts?: () => void
  /**
   * Opens the search palette. The field is the palette's second affordance
   * (Ctrl/Cmd+K is the first) and renders only when a caller wires it.
   */
  onOpenSearch?: () => void
}

export function TopBar({ onShowShortcuts, onOpenSearch }: TopBarProps) {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const isNarrow = useMediaQuery(768)

  const name = localStorage.getItem('aether_user_name') ?? ''
  const email = localStorage.getItem('aether_user_email') ?? ''
  const orgName = localStorage.getItem('aether_org_name') ?? ''
  const isPlatformAdmin = localStorage.getItem('aether_is_platform_admin') === 'true'
  const homeFolderId = sessionStorage.getItem('aether_home_folder_id')
  const initials = name ? name[0].toUpperCase() : email ? email[0].toUpperCase() : '?'
  const searchShortcut = isApplePlatform() ? '⌘K' : 'Ctrl K'

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  if (!user) return null

  return (
    <header style={styles.bar}>
      <button
        className="aether-hamburger"
        style={styles.hamburger}
        onClick={openMobileSidebar}
        aria-label="Open navigation menu"
      >
        <Menu size={18} />
      </button>
      <Link to={homeFolderId ? { pathname: '/', search: `?folder=${homeFolderId}` } : '/'} style={styles.brand}>
        <div style={styles.logo}><LogoMark /></div>
        <span style={styles.appName}>Aether</span>
      </Link>
      {onOpenSearch ? (
        isNarrow ? (
          // Icon-only below 768px: the bar has no room for the field between
          // the hamburger/brand and the org/avatar cluster.
          <button
            className="topbar-search"
            style={styles.searchCompact}
            onClick={onOpenSearch}
            title={`Search (${searchShortcut})`}
            aria-label="Search"
          >
            <Search size={16} aria-hidden="true" />
          </button>
        ) : (
          // Field-styled button: a real input would be a second search
          // implementation, so the affordance only opens the palette.
          <button
            className="topbar-search"
            style={styles.search}
            onClick={onOpenSearch}
            aria-label="Search"
          >
            <Search size={14} style={styles.searchIcon} aria-hidden="true" />
            <span style={styles.searchPlaceholder}>Search files, dashboards, SQL…</span>
            <Kbd style={styles.searchKbd}>{searchShortcut}</Kbd>
          </button>
        )
      ) : (
        <div style={styles.spacer} />
      )}
      {onShowShortcuts && (
        <button
          style={styles.shortcutsBtn}
          onClick={onShowShortcuts}
          title="Keyboard shortcuts (?)"
          aria-label="Keyboard shortcuts"
        >
          <Keyboard size={14} />
        </button>
      )}
      {isPlatformAdmin && (
        <Link to="/admin" style={styles.adminLink}>Admin</Link>
      )}
      <div style={styles.right} ref={ref}>
        <span style={styles.orgName}>{orgName}</span>
        <button style={styles.avatar} onClick={() => setOpen(o => !o)} aria-label="Profile menu">
          {initials}
        </button>
        {open && (
          <div style={styles.dropdown}>
            <div style={styles.dropdownHeader}>
              <div style={styles.dropdownName}>{name}</div>
              <div style={styles.dropdownEmail}>{email}</div>
            </div>
            <Link
              to="/profile"
              style={styles.dropdownLink}
              onClick={() => setOpen(false)}
            >
              Profile settings
            </Link>
            {user?.role === 'admin' && (
              <Link
                to="/settings"
                style={styles.dropdownLink}
                onClick={() => setOpen(false)}
              >
                Settings
              </Link>
            )}
            <Link
              to="/about"
              style={styles.dropdownLink}
              onClick={() => setOpen(false)}
            >
              About
            </Link>
            <a
              href="/docs"
              target="_blank"
              rel="noopener noreferrer"
              style={styles.dropdownLink}
              onClick={() => setOpen(false)}
            >
              API Documentation
            </a>
            <button style={styles.signOut} onClick={() => { logout(); setOpen(false) }}>
              Sign out
            </button>
          </div>
        )}
      </div>
    </header>
  )
}

const styles: Record<string, React.CSSProperties> = {
  bar: {
    height: 52,
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    display: 'flex',
    alignItems: 'center',
    padding: '0 16px 0 8px',
    gap: 12,
    flexShrink: 0,
    zIndex: 1550,
  },
  brand: { display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none' },
  logo: {
    display: 'flex', alignItems: 'center', justifyContent: 'center',
  },
  brandDivider: { width: 1, height: 20, background: 'var(--nav-border)', flexShrink: 0 },
  appName: { fontSize: 13, fontWeight: 500, color: 'var(--nav-text)', letterSpacing: '0.01em' },
  spacer: { flex: 1 },
  // Layout only: the chrome surface (wash, border, radius, hover) lives in
  // the `.topbar-search` class beside the other nav-chrome controls, since
  // inline styles can't express :hover on the dark bar.
  search: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    height: 30,
    padding: '0 10px',
    flex: 1,
    maxWidth: 'min(420px, 40vw)',
    minWidth: 0,
    margin: '0 auto',
    cursor: 'pointer',
    textAlign: 'left',
  },
  searchIcon: { color: 'var(--nav-text-muted)', flexShrink: 0 },
  searchPlaceholder: {
    flex: 1,
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontSize: 13,
    color: 'var(--nav-text-muted)',
  },
  searchKbd: { flexShrink: 0 },
  searchCompact: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 30,
    height: 30,
    padding: 0,
    marginLeft: 'auto',
    cursor: 'pointer',
  },
  orgName: { fontSize: 12, color: 'var(--nav-text)', fontWeight: 500, opacity: 0.75 },
  right: { position: 'relative', display: 'flex', alignItems: 'center', gap: 10 },
  avatar: {
    width: 30, height: 30,
    borderRadius: '50%',
    background: 'var(--accent-light)',
    border: '1.5px solid var(--accent)',
    color: 'var(--accent-hover)',
    fontSize: 13, fontWeight: 700,
    cursor: 'pointer',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
  },
  dropdown: {
    position: 'absolute', right: 0, top: 38,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    boxShadow: 'var(--shadow-md)',
    minWidth: 200,
    zIndex: 1600,
    overflow: 'hidden',
  },
  dropdownHeader: { padding: '12px 14px', borderBottom: '1px solid var(--border-light)' },
  dropdownName: { fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  dropdownEmail: { fontSize: 12, color: 'var(--text-muted)', marginTop: 2 },
  adminLink: {
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-muted)',
    textDecoration: 'none',
    padding: '4px 8px',
    borderRadius: 4,
    border: '1px solid var(--border)',
  },
  shortcutsBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'none',
    border: '1px solid var(--nav-border)',
    borderRadius: 4,
    padding: '5px 7px',
    cursor: 'pointer',
    color: 'var(--nav-text-muted)',
  },
  hamburger: {
    display: 'none', // shown via CSS media query on mobile
    alignItems: 'center',
    justifyContent: 'center',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--nav-text)',
    padding: '4px',
    borderRadius: 4,
  },
  dropdownLink: {
    display: 'block',
    padding: '10px 14px',
    fontSize: 13,
    color: 'var(--text-primary)',
    textDecoration: 'none',
    borderBottom: '1px solid var(--border-light)',
  },
  signOut: {
    width: '100%', padding: '10px 14px',
    background: 'transparent', border: 'none',
    fontSize: 13, color: 'var(--error)',
    cursor: 'pointer', textAlign: 'left',
    fontWeight: 500,
  },
}
