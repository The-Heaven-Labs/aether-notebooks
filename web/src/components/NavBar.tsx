import { Link } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'

type ActivePage = 'notebooks' | 'dashboards' | 'connectors' | 'members' | 'audit'

interface NavBarProps {
  activePage?: ActivePage
}

export function NavBar({ activePage }: NavBarProps) {
  const { logout } = useAuth()

  const linkStyle = (page: ActivePage) =>
    activePage === page ? styles.navActive : styles.navLink

  return (
    <header style={styles.header}>
      <div style={styles.headerInner}>
        <div style={styles.brand}>
          <div style={styles.logoMark}>▦</div>
          <span style={styles.brandName}>Heaven's Notebooks</span>
        </div>
        <div style={styles.headerRight}>
          {activePage === 'notebooks' ? (
            <span style={styles.navActive}>Notebooks</span>
          ) : (
            <Link to="/" style={linkStyle('notebooks')}>Notebooks</Link>
          )}
          <span style={styles.navSep}>·</span>
          {activePage === 'dashboards' ? (
            <span style={styles.navActive}>Dashboards</span>
          ) : (
            <Link to="/dashboards" style={linkStyle('dashboards')}>Dashboards</Link>
          )}
          <span style={styles.navSep}>·</span>
          {activePage === 'connectors' ? (
            <span style={styles.navActive}>Connectors</span>
          ) : (
            <Link to="/connectors" style={linkStyle('connectors')}>Connectors</Link>
          )}
          <span style={styles.navSep}>·</span>
          {activePage === 'members' ? (
            <span style={styles.navActive}>Members</span>
          ) : (
            <Link to="/members" style={linkStyle('members')}>Members</Link>
          )}
          <span style={styles.navSep}>·</span>
          {activePage === 'audit' ? (
            <span style={styles.navActive}>Audit</span>
          ) : (
            <Link to="/audit" style={linkStyle('audit')}>Audit</Link>
          )}
          <button type="button" style={styles.logoutBtn} onClick={logout}>Sign Out</button>
        </div>
      </div>
    </header>
  )
}

const styles: Record<string, React.CSSProperties> = {
  header: {
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    flexShrink: 0,
  },
  headerInner: {
    maxWidth: 1280,
    margin: '0 auto',
    padding: '0 32px',
    height: 56,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  brand: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  },
  logoMark: {
    width: 30,
    height: 30,
    background: 'var(--accent)',
    borderRadius: 7,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 16,
    color: 'white',
  },
  brandName: {
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--nav-text)',
    letterSpacing: '-0.1px',
  },
  headerRight: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
  },
  navLink: {
    fontSize: 13,
    fontWeight: 500,
    color: '#6a6260',
    textDecoration: 'none',
  },
  navActive: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--nav-text)',
  },
  navSep: {
    fontSize: 12,
    color: '#3a3630',
  },
  logoutBtn: {
    padding: '6px 14px',
    border: '1px solid #3a3630',
    borderRadius: 6,
    background: 'transparent',
    fontSize: 13,
    color: '#8a8278',
    cursor: 'pointer',
    fontWeight: 500,
    transition: 'all 0.15s',
  },
}
