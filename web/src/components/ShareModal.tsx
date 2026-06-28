import { useState } from 'react'
import { api } from '../api/client'

interface ShareModalProps {
  resourceType: 'notebook' | 'dashboard'
  resourceId: string
  onClose: () => void
}

export function ShareModal({ resourceType, resourceId, onClose }: ShareModalProps) {
  const [token, setToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [revoking, setRevoking] = useState(false)

  const publicUrl = token ? `${window.location.origin}/public/${token}` : null

  async function handleShare() {
    setError(null)
    try {
      const res = await api.post<{ token: string }>(`/api/v1/${resourceType}s/${resourceId}/share`, {})
      setToken(res.token)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function handleRevoke() {
    setRevoking(true)
    setError(null)
    try {
      await api.delete(`/api/v1/${resourceType}s/${resourceId}/share`)
      setToken(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRevoking(false)
    }
  }

  async function handleCopy() {
    if (!publicUrl) return
    await navigator.clipboard.writeText(publicUrl)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div style={s.backdrop} onClick={onClose}>
      <div style={s.modal} onClick={e => e.stopPropagation()}>
        <div style={s.header}>
          <span style={s.title}>Share {resourceType}</span>
          <button style={s.closeBtn} onClick={onClose}>×</button>
        </div>
        <div style={s.body}>
          {error && <p style={{ color: 'var(--error)', fontSize: 13 }}>{error}</p>}
          {!token ? (
            <button style={s.btn} onClick={handleShare}>Generate public link</button>
          ) : (
            <>
              <div style={s.urlRow}>
                <input style={s.urlInput} value={publicUrl!} readOnly />
                <button style={s.copyBtn} onClick={handleCopy}>{copied ? 'Copied!' : 'Copy'}</button>
              </div>
              <button style={{ ...s.btn, marginTop: 12, background: 'var(--error)', color: '#fff' }} onClick={handleRevoke} disabled={revoking}>
                {revoking ? 'Revoking…' : 'Revoke public link'}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
  backdrop: { position: 'fixed', inset: 0, background: 'var(--bg-overlay)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 2000 },
  modal: { background: 'var(--bg-card)', borderRadius: 8, boxShadow: 'var(--shadow-lg)', width: 440, maxHeight: '80vh', overflow: 'hidden' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 18px', borderBottom: '1px solid var(--border)' },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  closeBtn: { background: 'none', border: 'none', cursor: 'pointer', fontSize: 20, color: 'var(--text-muted)', lineHeight: 1, padding: '0 4px' },
  body: { padding: '18px' },
  btn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  urlRow: { display: 'flex', gap: 8 },
  urlInput: { flex: 1, padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, color: 'var(--text-primary)', background: 'var(--bg-input)', fontFamily: 'var(--font-mono)' },
  copyBtn: { padding: '7px 14px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 12, cursor: 'pointer' },
}
