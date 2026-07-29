import { useState, useEffect } from 'react'
import { AppShell } from '../components/AppShell'
import { api } from '../api/client'

interface VersionInfo {
  version: string
  commit: string
  buildDate: string
}

export function AboutPage() {
  useEffect(() => { document.title = "About — Aether Notebooks" }, [])
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.get<VersionInfo>('/api/v1/version')
      .then(setInfo)
      .catch(() => setError('Could not fetch version info'))
  }, [])

  return (
    <AppShell>
      <div style={styles.body}>
        <h1 style={styles.title}>About Aether Notebooks</h1>
        <p style={styles.subtitle}>Collaborative SQL/data notebook platform</p>
        <div style={styles.card}>
          {error ? (
            <p style={styles.error}>{error}</p>
          ) : info ? (
            <dl style={styles.dl}>
              <dt style={styles.dt}>Version</dt>
              <dd style={styles.dd}>{info.version}</dd>
              <dt style={styles.dt}>Commit</dt>
              <dd style={styles.dd}><code style={styles.code}>{info.commit}</code></dd>
              <dt style={styles.dt}>Build Date</dt>
              <dd style={styles.dd}>{info.buildDate}</dd>
            </dl>
          ) : (
            <p style={styles.loading}>Loading…</p>
          )}
        </div>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { maxWidth: 640, margin: '0 auto', padding: '48px 40px', width: '100%' },
  title: { fontSize: 24, fontWeight: 700, color: 'var(--text-primary)', margin: '0 0 4px' },
  subtitle: { fontSize: 14, color: 'var(--text-muted)', margin: '0 0 32px' },
  card: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    padding: '24px',
  },
  dl: { margin: 0, display: 'flex', flexDirection: 'column', gap: 16 },
  dt: { fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' },
  dd: { fontSize: 14, color: 'var(--text-primary)', margin: 0 },
  code: { fontSize: 13, fontFamily: 'var(--font-mono)', color: 'var(--accent)' },
  loading: { fontSize: 13, color: 'var(--text-muted)' },
  error: { fontSize: 13, color: 'var(--error)' },
}
