import { useState, useEffect } from 'react'
import { api } from '../api/client'
import type { Member } from '../types'

interface ShareModalProps {
  resourceType: 'notebook' | 'dashboard'
  resourceId: string
  canShare?: boolean
  onClose: () => void
}

interface ShareResponse {
  token: string
  created_by?: string
  created_at?: string
}

export function ShareModal({ resourceType, resourceId, canShare = true, onClose }: ShareModalProps) {
  const [token, setToken] = useState<string | null>(null)
  const [createdBy, setCreatedBy] = useState<string | null>(null)
  const [createdAt, setCreatedAt] = useState<string | null>(null)
  const [creatorName, setCreatorName] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [revoking, setRevoking] = useState(false)

  const publicUrl = token ? `${window.location.origin}/public/${token}` : null

  // Resolve creator name from members
  useEffect(() => {
    if (!createdBy) { setCreatorName(null); return }
    api.get<Member[]>('/api/v1/members').then(members => {
      const m = members.find(m => m.user_id === createdBy)
      setCreatorName(m ? (m.name || m.email) : createdBy.slice(0, 8))
    }).catch(() => setCreatorName(createdBy.slice(0, 8)))
  }, [createdBy])

  // Check for existing token on mount (GET — never creates)
  useEffect(() => {
    (async () => {
      try {
        const res = await api.get<ShareResponse | undefined>(`/api/v1/${resourceType}s/${resourceId}/share`)
        if (res) {
          setToken(res.token)
          setCreatedBy(res.created_by ?? null)
          setCreatedAt(res.created_at ?? null)
        }
      } catch {
        // No existing token — user will need to generate one
      } finally {
        setLoading(false)
      }
    })()
  }, [resourceType, resourceId])

  async function handleShare() {
    setError(null)
    try {
      const res = await api.post<ShareResponse>(`/api/v1/${resourceType}s/${resourceId}/share`, {})
      setToken(res.token)
      setCreatedBy(res.created_by ?? null)
      setCreatedAt(res.created_at ?? null)
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
      setCreatedBy(null)
      setCreatedAt(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRevoking(false)
    }
  }

  async function handleCopy() {
    if (!publicUrl) return
    try {
      await navigator.clipboard.writeText(publicUrl)
    } catch {
      // Fallback: select the input and execCommand
      const input = document.querySelector<HTMLInputElement>(`[data-public-url]`)
      if (input) { input.select(); document.execCommand('copy') }
    }
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
          {loading && <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Checking existing link…</p>}
          {error && <p style={{ color: 'var(--error)', fontSize: 13 }}>{error}</p>}
          {!loading && !token && canShare && (
            <button style={s.btn} onClick={handleShare}>Generate public link</button>
          )}
          {!loading && !token && !canShare && (
            <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>No public link has been created for this {resourceType}.</p>
          )}
          {!loading && token && (
            <>
              <div style={s.urlRow}>
                <input style={s.urlInput} value={publicUrl!} readOnly data-public-url />
                <button style={s.copyBtn} onClick={handleCopy}>{copied ? 'Copied!' : 'Copy'}</button>
              </div>
              {(creatorName || createdAt) && (
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 8 }}>
                  Created by {creatorName ?? 'Unknown'}{createdAt ? ` · ${new Date(createdAt).toLocaleString()}` : ''}
                </div>
              )}
              {canShare && (
                <button style={{ ...s.btn, marginTop: 12, background: 'var(--error)', color: '#fff' }} onClick={handleRevoke} disabled={revoking}>
                  {revoking ? 'Revoking…' : 'Revoke public link'}
                </button>
              )}
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
