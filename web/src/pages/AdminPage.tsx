import { useState, useEffect } from 'react'

interface Org {
  id: string; name: string; slug: string; member_count: number; created_at: string
}
interface User {
  id: string; email: string; name: string; is_platform_admin: boolean; orgs: string[]
}

export function AdminPage() {
  useEffect(() => { document.title = "Platform Admin — Heaven's Notebooks" }, [])
  const [tab, setTab] = useState<'orgs' | 'users'>('orgs')
  const [orgs, setOrgs] = useState<Org[]>([])
  const [users, setUsers] = useState<User[]>([])

  useEffect(() => {
    const token = localStorage.getItem('hnb_token')
    const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
    fetch('/api/v1/admin/orgs', { headers }).then(r => r.json()).then(d => setOrgs(d.orgs ?? []))
    fetch('/api/v1/admin/users', { headers }).then(r => r.json()).then(d => setUsers(d.users ?? []))
  }, [])

  return (
    <div style={styles.page}>
      <h1 style={styles.title}>Platform Admin</h1>
      <div style={styles.tabs} role="tablist">
        <button
          role="tab"
          aria-selected={tab === 'orgs'}
          style={tab === 'orgs' ? styles.tabActive : styles.tab}
          onClick={() => setTab('orgs')}
        >
          Orgs
        </button>
        <button
          role="tab"
          aria-selected={tab === 'users'}
          style={tab === 'users' ? styles.tabActive : styles.tab}
          onClick={() => setTab('users')}
        >
          Users
        </button>
      </div>

      {tab === 'orgs' && (
        <table style={styles.table}>
          <thead><tr>
            <th style={styles.th}>Name</th>
            <th style={styles.th}>Slug</th>
            <th style={styles.th}>Members</th>
            <th style={styles.th}>Created</th>
          </tr></thead>
          <tbody>
            {orgs.map(o => (
              <tr key={o.id}>
                <td style={styles.td}>{o.name}</td>
                <td style={styles.td}>{o.slug}</td>
                <td style={styles.td}>{o.member_count}</td>
                <td style={styles.td}>{new Date(o.created_at).toLocaleDateString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {tab === 'users' && (
        <table style={styles.table}>
          <thead><tr>
            <th style={styles.th}>Email</th>
            <th style={styles.th}>Name</th>
            <th style={styles.th}>Platform Admin</th>
            <th style={styles.th}>Orgs</th>
          </tr></thead>
          <tbody>
            {users.map(u => (
              <tr key={u.id}>
                <td style={styles.td}>{u.email}</td>
                <td style={styles.td}>{u.name}</td>
                <td style={styles.td}>{u.is_platform_admin ? 'Yes' : 'No'}</td>
                <td style={styles.td}>{u.orgs.join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: { padding: 24 },
  title: { fontSize: 20, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' },
  tabs: { display: 'flex', gap: 2, marginBottom: 16, borderBottom: '1px solid var(--border)' },
  tab: { padding: '8px 16px', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 14 },
  tabActive: { padding: '8px 16px', background: 'none', border: 'none', borderBottom: '2px solid var(--accent)', cursor: 'pointer', color: 'var(--text-primary)', fontSize: 14, fontWeight: 600 },
  table: { width: '100%', borderCollapse: 'collapse' },
  th: { textAlign: 'left', padding: '8px 12px', fontSize: 12, color: 'var(--text-muted)', borderBottom: '1px solid var(--border)', background: 'var(--bg-card)' },
  td: { padding: '10px 12px', fontSize: 13, color: 'var(--text-primary)', borderBottom: '1px solid var(--border)' },
}
