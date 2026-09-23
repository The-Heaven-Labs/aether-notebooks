import type { SSOProvider, SSODebugCapture } from '../types'

// Latest-wins redacted IDP payload shown beneath a provider row while its
// Debug Claims flag is on. Shared by the platform admin and org SSO settings.
export function DebugClaimsPanel({ provider, capture, loading, error }: {
  provider: SSOProvider
  capture: SSODebugCapture | null
  loading: boolean
  error: string | null
}) {
  if (loading) return <div style={styles.debugPanel}>Loading capture…</div>
  if (error) {
    return (
      <div style={styles.debugPanel}>
        <span style={{ color: 'var(--error)' }}>{error}</span>
      </div>
    )
  }
  if (!capture) {
    return (
      <div style={styles.debugPanel}>
        No capture yet — have a user log in through this provider while Debug Claims is enabled.
      </div>
    )
  }

  const prefix = provider.group_prefix || ''
  const parsedGroups = capture.parsed_groups ?? []
  const filtered = prefix ? parsedGroups.filter(g => g.startsWith(prefix)) : parsedGroups
  const scopes = capture.granted_scopes ?? []

  return (
    <div style={styles.debugPanel}>
      <div style={styles.debugMeta}>
        Captured {new Date(capture.captured_at).toLocaleString()} ·{' '}
        {capture.email || capture.subject}
        {capture.name ? ` (${capture.name})` : ''}
      </div>
      <div style={styles.debugRow}>
        <span style={styles.debugLabel}>Granted scopes</span>
        <span>{scopes.length > 0 ? scopes.join(', ') : '(none reported)'}</span>
      </div>

      <div style={styles.debugSectionTitle}>Groups claim</div>
      <div style={styles.debugRow}>
        <span style={styles.debugLabel}>Claim</span>
        <code style={styles.debugCode}>{capture.groups_claim}</code>
      </div>
      <div style={styles.debugRow}>
        <span style={styles.debugLabel}>Raw value</span>
        <code style={styles.debugCode}>{formatDebugValue(capture.groups_claim_value)}</code>
      </div>
      <div style={styles.debugRow}>
        <span style={styles.debugLabel}>Parsed groups</span>
        <span>{parsedGroups.length > 0 ? parsedGroups.join(', ') : '(none)'}</span>
      </div>
      <div style={styles.debugRow}>
        <span style={styles.debugLabel}>After prefix filter</span>
        <span>
          {prefix
            ? (filtered.length > 0 ? filtered.join(', ') : `(none match "${prefix}")`)
            : 'No group prefix configured'}
        </span>
      </div>

      {capture.user_info_error && (
        <div style={styles.debugError}>UserInfo error: {capture.user_info_error}</div>
      )}

      <div style={styles.debugSectionTitle}>ID token claims</div>
      <pre style={styles.debugPre}>{JSON.stringify(capture.id_token_claims ?? {}, null, 2)}</pre>

      <div style={styles.debugSectionTitle}>UserInfo claims</div>
      {capture.user_info_claims ? (
        <pre style={styles.debugPre}>{JSON.stringify(capture.user_info_claims, null, 2)}</pre>
      ) : (
        <div style={styles.debugMuted}>
          {provider.get_user_info
            ? 'No UserInfo claims captured.'
            : 'UserInfo endpoint is disabled for this provider.'}
        </div>
      )}
    </div>
  )
}

function formatDebugValue(value: unknown): string {
  if (value === undefined) return '(absent)'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}

const styles: Record<string, React.CSSProperties> = {
  debugPanel: {
    marginTop: 10,
    padding: '12px 14px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    background: 'var(--bg-secondary)',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    fontSize: 12,
    color: 'var(--text-primary)',
  },
  debugMeta: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  debugRow: {
    display: 'flex',
    gap: 8,
    alignItems: 'baseline',
  },
  debugLabel: {
    minWidth: 130,
    color: 'var(--text-muted)',
    flexShrink: 0,
  },
  debugCode: {
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
    overflowWrap: 'anywhere',
  },
  debugSectionTitle: {
    marginTop: 6,
    fontSize: 11,
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
    color: 'var(--text-muted)',
  },
  debugPre: {
    margin: 0,
    padding: '8px 10px',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    maxHeight: 260,
    overflow: 'auto',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-all',
  },
  debugError: {
    color: 'var(--error)',
    fontSize: 12,
  },
  debugMuted: {
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
}
