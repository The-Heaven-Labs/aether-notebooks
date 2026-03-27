import { useState, useRef, useEffect } from 'react'
import { useAuth } from '../hooks/useAuth'

// ── Logo options — swap LOGO_VARIANT (1–5) to preview each one ──────────────
const LOGO_VARIANT = 1

function LogoMark() {
  const s: React.CSSProperties = { display: 'block' }
  if (LOGO_VARIANT === 1) return ( // Stacked data rows — "query results"
    <svg style={s} width="18" height="18" viewBox="0 0 18 18" fill="none">
      <rect x="1" y="2" width="16" height="3" rx="1.5" fill="white"/>
      <rect x="1" y="7.5" width="11" height="3" rx="1.5" fill="white" fillOpacity="0.7"/>
      <rect x="1" y="13" width="7" height="3" rx="1.5" fill="white" fillOpacity="0.45"/>
    </svg>
  )
  if (LOGO_VARIANT === 2) return ( // Notebook cell with run dot — "cell execution"
    <svg style={s} width="18" height="18" viewBox="0 0 18 18" fill="none">
      <rect x="1.5" y="1.5" width="15" height="15" rx="3" stroke="white" strokeWidth="1.5"/>
      <path d="M6 6h6M6 9h4" stroke="white" strokeWidth="1.5" strokeLinecap="round"/>
      <circle cx="13" cy="13" r="2.5" fill="white"/>
    </svg>
  )
  if (LOGO_VARIANT === 3) return ( // Diamond data node — "data relationships"
    <svg style={s} width="18" height="18" viewBox="0 0 18 18" fill="none">
      <circle cx="9" cy="9" r="2.5" fill="white"/>
      <circle cx="9" cy="2" r="1.5" fill="white" fillOpacity="0.6"/>
      <circle cx="9" cy="16" r="1.5" fill="white" fillOpacity="0.6"/>
      <circle cx="2" cy="9" r="1.5" fill="white" fillOpacity="0.6"/>
      <circle cx="16" cy="9" r="1.5" fill="white" fillOpacity="0.6"/>
      <path d="M9 4v3M9 11v3M4 9h3M11 9h3" stroke="white" strokeWidth="1" strokeOpacity="0.5"/>
    </svg>
  )
  if (LOGO_VARIANT === 4) return ( // Stylised "N" lettermark — minimal wordmark
    <svg style={s} width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path d="M3 14V4L9 13V4M9 13V4l6 10V4" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
    </svg>
  )
  // LOGO_VARIANT === 5: Stacked layers — "notebook pages / sheets"
  return (
    <svg style={s} width="18" height="18" viewBox="0 0 18 18" fill="none">
      <rect x="4" y="11" width="12" height="5" rx="1.5" fill="white" fillOpacity="0.35"/>
      <rect x="2" y="7" width="12" height="5" rx="1.5" fill="white" fillOpacity="0.6"/>
      <rect x="1" y="3" width="12" height="5" rx="1.5" fill="white"/>
      <path d="M4 5h5M4 7h3" stroke="var(--accent)" strokeWidth="1" strokeLinecap="round"/>
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
      <div style={styles.brand}>
        <div style={styles.logo}><LogoMark /></div>
        <span style={styles.appName}>hnb</span>
      </div>
      <span style={styles.orgName}>{orgName}</span>
      <div style={styles.right} ref={ref}>
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
    height: 44,
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    display: 'flex',
    alignItems: 'center',
    padding: '0 16px',
    gap: 12,
    flexShrink: 0,
    zIndex: 10,
  },
  brand: { display: 'flex', alignItems: 'center', gap: 8 },
  logo: {
    width: 26, height: 26,
    background: 'var(--accent)',
    borderRadius: 6,
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontSize: 14, color: 'white',
  },
  appName: { fontSize: 14, fontWeight: 700, color: 'var(--nav-text)' },
  orgName: { flex: 1, fontSize: 12, color: 'var(--text-muted)', fontWeight: 500 },
  right: { position: 'relative' },
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
  signOut: {
    width: '100%', padding: '10px 14px',
    background: 'transparent', border: 'none',
    fontSize: 13, color: 'var(--error)',
    cursor: 'pointer', textAlign: 'left',
    fontWeight: 500,
  },
}
