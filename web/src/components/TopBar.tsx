import { useState, useRef, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

function LogoMark() {
  return (
    <svg style={{ display: 'block' }} width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path d="M3 14V4L9 13V4M9 13V4l6 10V4" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
    </svg>
  )
}

export function TopBar() {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const name = localStorage.getItem('hnb_user_name') ?? ''
  const email = localStorage.getItem('hnb_user_email') ?? ''
  const orgName = localStorage.getItem('hnb_org_name') ?? ''
  const isPlatformAdmin = localStorage.getItem('hnb_is_platform_admin') === 'true'
  const initials = name ? name[0].toUpperCase() : email ? email[0].toUpperCase() : '?'

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
      <Link to="/" style={styles.brand}>
        <div style={styles.logo}><LogoMark /></div>
        <div style={styles.brandDivider} />
        <span style={styles.appName}>Heaven's Notebooks</span>
      </Link>
      <div style={styles.spacer} />
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
    zIndex: 10,
  },
  brand: { display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none' },
  logo: {
    width: 36, height: 36,
    background: 'var(--accent)',
    borderRadius: 9,
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontSize: 14, color: 'white',
  },
  brandDivider: { width: 1, height: 20, background: 'var(--nav-border)', flexShrink: 0 },
  appName: { fontSize: 13, fontWeight: 500, color: 'var(--text-muted)', letterSpacing: '0.01em' },
  spacer: { flex: 1 },
  orgName: { fontSize: 12, color: 'var(--text-muted)', fontWeight: 500 },
  right: { position: 'relative', display: 'flex', alignItems: 'center', gap: 10 },
  avatar: {
    width: 30, height: 30,
    borderRadius: '50%',
    background: 'var(--accent-light)',
    border: '1.5px solid var(--accent)',
    color: 'var(--accent)',
    fontSize: 13, fontWeight: 700,
    cursor: 'pointer',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
  },
  dropdown: {
    position: 'absolute', right: 0, top: 38,
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: '0 4px 16px rgba(0,0,0,0.12)',
    minWidth: 200,
    zIndex: 100,
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
  signOut: {
    width: '100%', padding: '10px 14px',
    background: 'transparent', border: 'none',
    fontSize: 13, color: 'var(--error)',
    cursor: 'pointer', textAlign: 'left',
    fontWeight: 500,
  },
}
