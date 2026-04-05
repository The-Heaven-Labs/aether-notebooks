import { useEffect, useState } from 'react'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Folder, FolderContents } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorBanner } from '../components/ErrorBanner'
import { Folder as FolderIcon, BookOpen, LayoutDashboard, Database, Home } from 'lucide-react'

export function HomePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const folderID = searchParams.get('folder')
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [creating, setCreating] = useState<null | 'folder' | 'notebook'>(null)
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const contentsKey = ['folder-contents', folderID ?? 'root']
  const { data, isLoading } = useQuery<FolderContents>({
    queryKey: contentsKey,
    queryFn: () => folderID
      ? api.get<FolderContents>(`/api/v1/folders/${folderID}`)
      : api.get<FolderContents>('/api/v1/folders'),
  })

  const { data: ancestors = [] } = useQuery<Array<{ id: string; name: string }>>({
    queryKey: ['folder-ancestors', folderID],
    queryFn: () => api.get(`/api/v1/folders/${folderID}/ancestors`),
    enabled: !!folderID,
  })

  useEffect(() => {
    const name = data?.folder?.name
    document.title = name ? `${name} — hnb` : "Files — hnb"
  }, [data?.folder?.name])

  const createFolder = useMutation({
    mutationFn: (name: string) =>
      api.post<Folder>('/api/v1/folders', { name, ...(folderID ? { parent_id: folderID } : {}) }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: contentsKey }); setCreating(null); setNewName('') },
    onError: (e: Error) => setError(e.message),
  })

  const createNotebook = useMutation({
    mutationFn: (title: string) =>
      api.post<{ id: string }>('/api/v1/notebooks', { title, ...(folderID ? { folder_id: folderID } : {}) }),
    onSuccess: (nb) => navigate(`/notebooks/${nb.id}`),
    onError: (e: Error) => setError(e.message),
  })

  const deleteFolder = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/folders/${id}?force=true`),
    onSuccess: () => qc.invalidateQueries({ queryKey: contentsKey }),
    onError: (e: Error) => setError(e.message),
  })

  const deleteNotebook = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/notebooks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: contentsKey }),
    onError: (e: Error) => setError(e.message),
  })

  const isEmpty = data &&
    data.folders.length === 0 &&
    data.notebooks.length === 0 &&
    data.connectors.length === 0 &&
    data.dashboards.length === 0

  const handleCreate = () => {
    if (!newName.trim()) return
    if (creating === 'folder') createFolder.mutate(newName.trim())
    else if (creating === 'notebook') createNotebook.mutate(newName.trim())
  }

  return (
    <AppShell>
      <div style={{ maxWidth: 1280, margin: '0 auto' }}>
        {/* Breadcrumb */}
        <div style={s.breadcrumb}>
          <button style={s.crumbBtn} onClick={() => setSearchParams({})}>
            <Home size={13} style={{ marginRight: 4 }} />
            Files
          </button>
          {ancestors.map((a) => (
            <span key={a.id} style={{ display: 'flex', alignItems: 'center' }}>
              <span style={s.sep}>/</span>
              <button style={s.crumbBtn} onClick={() => setSearchParams({ folder: a.id })}>
                {a.name}
              </button>
            </span>
          ))}
        </div>

        {/* Toolbar */}
        <div style={s.toolbar}>
          <button style={s.newBtn} onClick={() => { setCreating('folder'); setNewName('') }}>
            + New Folder
          </button>
          <button style={s.newBtn} onClick={() => { setCreating('notebook'); setNewName('') }}>
            + New Notebook
          </button>
        </div>

        {/* Inline create form */}
        {creating && (
          <div style={s.createForm}>
            <input
              style={s.input}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={creating === 'folder' ? 'Folder name…' : 'Notebook title…'}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleCreate()
                if (e.key === 'Escape') { setCreating(null); setNewName('') }
              }}
            />
            <button style={s.createBtn} disabled={!newName.trim()} onClick={handleCreate}>Create</button>
            <button style={s.cancelBtn} onClick={() => { setCreating(null); setNewName('') }}>Cancel</button>
          </div>
        )}

        {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

        {isLoading && <div style={{ padding: 32, color: '#aaa', fontSize: 14 }}>Loading…</div>}

        {!isLoading && isEmpty && !creating && (
          <EmptyState
            icon={<FolderIcon size={32} />}
            title="This folder is empty"
            text="Create a folder or notebook to get started."
            action={{ label: '+ New Notebook', onClick: () => setCreating('notebook') }}
          />
        )}

        {/* Folders */}
        {data && data.folders.length > 0 && (
          <section style={s.section}>
            <div style={s.sectionLabel}>Folders</div>
            <div style={s.folderGrid}>
              {data.folders.map((f) => (
                <div key={f.id} style={s.folderCard} className="card-hover">
                  <button style={s.folderBtn} onClick={() => setSearchParams({ folder: f.id })}>
                    <FolderIcon size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={s.folderName}>{f.name}</span>
                    {f.is_home && <span style={s.badge}>home</span>}
                  </button>
                  <button style={s.iconBtn} title="Delete folder" onClick={() => deleteFolder.mutate(f.id)}>×</button>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Notebooks */}
        {data && data.notebooks.length > 0 && (
          <section style={s.section}>
            <div style={s.sectionLabel}>Notebooks</div>
            <div style={s.list}>
              {data.notebooks.map((nb) => (
                <div key={nb.id} style={s.item}>
                  <Link to={`/notebooks/${nb.id}`} style={s.itemLink}>
                    <BookOpen size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={s.itemName}>{nb.title}</span>
                  </Link>
                  <button style={s.delBtn} onClick={() => deleteNotebook.mutate(nb.id)}>Delete</button>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Connectors */}
        {data && data.connectors.length > 0 && (
          <section style={s.section}>
            <div style={s.sectionLabel}>Connectors</div>
            <div style={s.list}>
              {data.connectors.map((c) => (
                <div key={c.id} style={s.item}>
                  <Link to="/connectors" style={s.itemLink}>
                    <Database size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={s.itemName}>{c.name}</span>
                    {c.is_default && <span style={s.badge}>default</span>}
                  </Link>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Dashboards */}
        {data && data.dashboards.length > 0 && (
          <section style={s.section}>
            <div style={s.sectionLabel}>Dashboards</div>
            <div style={s.list}>
              {data.dashboards.map((d) => (
                <div key={d.id} style={s.item}>
                  <Link to={`/dashboards/${d.id}`} style={s.itemLink}>
                    <LayoutDashboard size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={s.itemName}>{d.title}</span>
                  </Link>
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </AppShell>
  )
}

const s: Record<string, React.CSSProperties> = {
  breadcrumb: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2, marginBottom: 20 },
  crumbBtn: { display: 'flex', alignItems: 'center', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--accent)', fontSize: 13, fontWeight: 500, padding: '2px 6px', borderRadius: 3 },
  sep: { color: '#ccc', margin: '0 2px', fontSize: 13 },
  toolbar: { display: 'flex', gap: 10, marginBottom: 20 },
  newBtn: { padding: '7px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  createForm: { display: 'flex', gap: 10, marginBottom: 20, padding: 16, background: '#fff', borderRadius: 4, border: '1px solid #e8e8e8', alignItems: 'center' },
  input: { flex: 1, padding: '8px 12px', border: '1px solid #ddd', borderRadius: 4, fontSize: 14, outline: 'none' },
  createBtn: { padding: '7px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '7px 14px', border: '1px solid #ddd', borderRadius: 4, background: 'none', fontSize: 13, cursor: 'pointer', color: '#555' },
  section: { marginBottom: 28 },
  sectionLabel: { fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase' as const, color: '#aaa', marginBottom: 8 },
  folderGrid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 8 },
  folderCard: { display: 'flex', alignItems: 'center', background: '#fff', border: '1px solid #e8e8e8', borderRadius: 4, overflow: 'hidden', transition: 'border-color 0.15s' },
  folderBtn: { flex: 1, display: 'flex', alignItems: 'center', gap: 7, padding: '9px 12px', background: 'none', border: 'none', cursor: 'pointer', textAlign: 'left' as const, minWidth: 0 },
  folderName: { fontSize: 13, fontWeight: 500, color: '#111', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const, flex: 1 },
  badge: { fontSize: 10, fontWeight: 700, background: 'var(--accent-light)', color: 'var(--accent)', borderRadius: 3, padding: '1px 5px', letterSpacing: '0.04em', flexShrink: 0 },
  iconBtn: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--error)', fontSize: 16, padding: '0 8px', flexShrink: 0, lineHeight: 1 },
  list: { display: 'flex', flexDirection: 'column', gap: 6 },
  item: { display: 'flex', alignItems: 'center', background: '#fff', border: '1px solid #e8e8e8', borderRadius: 4, padding: '8px 14px', gap: 10 },
  itemLink: { flex: 1, display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none', minWidth: 0 },
  itemName: { fontSize: 14, fontWeight: 500, color: '#111', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const, flex: 1 },
  delBtn: { padding: '3px 8px', border: 'none', background: 'transparent', color: 'var(--error)', fontSize: 12, cursor: 'pointer', flexShrink: 0 },
}
