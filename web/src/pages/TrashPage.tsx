import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { Trash2, RotateCcw, BookOpen, LayoutDashboard, Database } from 'lucide-react'
import { api } from '../api/client'
import { AppShell } from '../components/AppShell'

interface TrashItem {
  id: string
  type: 'notebook' | 'connector' | 'dashboard'
  name: string
  deleted_at: string
}

const TYPE_CONFIG: Record<string, { label: string; icon: React.ReactNode; link: (id: string) => string }> = {
  notebook: { label: 'Notebook', icon: <BookOpen size={13} />, link: (id) => `/notebooks/${id}` },
  dashboard: { label: 'Dashboard', icon: <LayoutDashboard size={13} />, link: (id) => `/dashboards/${id}` },
  connector: { label: 'Connector', icon: <Database size={13} />, link: () => '/connectors' },
}

export function TrashPage() {
  const [items, setItems] = useState<TrashItem[]>([])
  const [loading, setLoading] = useState(true)

  const fetchTrash = () => {
    setLoading(true)
    api.get<TrashItem[] | null>('/api/v1/trash')
      .then((data) => setItems(data ?? []))
      .catch(() => setItems([]))
      .finally(() => setLoading(false))
  }

  useEffect(() => { document.title = "Trash — Heaven's Notebooks"; fetchTrash() }, [])

  const handleRestore = async (item: TrashItem) => {
    try {
      await api.post('/api/v1/trash/restore', { type: item.type, id: item.id })
      setItems((prev) => prev.filter((i) => i.id !== item.id))
    } catch {}
  }

  const fmtTime = (d: string) => {
    const date = new Date(d)
    const now = Date.now()
    const diffDays = Math.floor((now - date.getTime()) / 86400000)
    const expiry = Math.max(0, 7 - diffDays)
    const removalDate = new Date(date.getTime() + 7 * 86400000)
    return {
      text: date.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }),
      expiry,
      removalDate: removalDate.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' }),
    }
  }

  return (
    <AppShell>
      <div style={{ maxWidth: 720, margin: '0 auto' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 20 }}>
          <Trash2 size={20} style={{ color: 'var(--text-muted)' }} />
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Trash</h1>
        </div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 20 }}>
          Resources in trash are automatically deleted after 7 days.
        </p>

        {loading ? (
          <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</p>
        ) : items.length === 0 ? (
          <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
            Trash is empty
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {items.map((item) => {
              const fmt = fmtTime(item.deleted_at)
              const cfg = TYPE_CONFIG[item.type]
              return (
                <div
                  key={`${item.type}-${item.id}`}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 10,
                    padding: '10px 14px', background: 'var(--bg-card)',
                    border: '1px solid var(--border)', borderRadius: 4,
                  }}
                >
                  <span style={{ color: 'var(--text-muted)', flexShrink: 0 }}>{cfg.icon}</span>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {item.name}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                      {cfg.label} · Deleted {fmt.text} · {fmt.expiry > 0 ? `Deletion in ${fmt.expiry} day${fmt.expiry === 1 ? '' : 's'} (${fmt.removalDate})` : `Pending deletion (${fmt.removalDate})`}
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => handleRestore(item)}
                    style={{
                      display: 'flex', alignItems: 'center', gap: 4, flexShrink: 0,
                      fontSize: 12, padding: '5px 10px',
                      background: 'var(--bg-input)', color: 'var(--text-primary)',
                      border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
                    }}
                  >
                    <RotateCcw size={12} /> Restore
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </AppShell>
  )
}
