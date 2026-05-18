import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Folder, FolderContents } from '../types'
import { useAuth } from '../hooks/useAuth'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorBanner } from '../components/ErrorBanner'
import { FolderTree } from '../components/FolderTree'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { TwoPanelLayout } from '../components/TwoPanelLayout'
import { Folder as FolderIcon, BookOpen, LayoutDashboard, Database, Home } from 'lucide-react'

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = 'folder' | 'notebook' | 'connector' | 'dashboard'

interface MenuTarget {
  type: ResourceType
  id: string
  name: string
}

interface RenameTarget {
  type: 'folder' | 'notebook'
  id: string
  currentName: string
}

interface MoveTarget {
  type: ResourceType
  id: string
  name: string
}

interface PermissionsTarget {
  type: ResourceType
  id: string
  name: string
}

// ─── ContextMenu ─────────────────────────────────────────────────────────────

interface ContextMenuProps {
  target: MenuTarget
  onRename: (t: RenameTarget) => void
  onMove: (t: MoveTarget) => void
  onPermissions: (t: PermissionsTarget) => void
  onDelete: (type: ResourceType, id: string) => void
  onEdit: (type: ResourceType, id: string) => void
  onClose: () => void
}

function ContextMenu({ target, onRename, onMove, onPermissions, onDelete, onEdit, onClose }: ContextMenuProps) {
  const ref = useRef<HTMLDivElement>(null)
  const canRename = target.type === 'folder' || target.type === 'notebook'
  const canDelete = target.type === 'folder' || target.type === 'notebook'

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose()
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [onClose])

  return (
    <div ref={ref} style={ms.menu}>
      {canRename && (
        <button style={ms.item} onClick={() => {
          onRename({ type: target.type as 'folder' | 'notebook', id: target.id, currentName: target.name })
          onClose()
        }}>Rename</button>
      )}
      <button style={ms.item} onClick={() => {
        onMove({ type: target.type, id: target.id, name: target.name })
        onClose()
      }}>Move to…</button>
      {target.type === 'connector' ? (
        <>
          <button style={ms.item} onClick={() => { onEdit(target.type, target.id); onClose() }}>Edit</button>
          <button style={ms.item} onClick={() => { onPermissions({ type: target.type, id: target.id, name: target.name }); onClose() }}>Permissions</button>
        </>
      ) : (
        <button style={ms.item} onClick={() => {
          onPermissions({ type: target.type, id: target.id, name: target.name })
          onClose()
        }}>Permissions</button>
      )}
      {canDelete ? (
        <button style={{ ...ms.item, color: 'var(--error)' }} onClick={() => {
          onDelete(target.type, target.id)
          onClose()
        }}>Delete</button>
      ) : (
        <button style={{ ...ms.item, color: 'var(--error)' }} onClick={() => {
          if (target.type === 'connector') { onEdit(target.type, target.id) }
          onClose()
        }}>Delete</button>
      )}
    </div>
  )
}

// ─── InlineRename ─────────────────────────────────────────────────────────────

interface InlineRenameProps {
  initialValue: string
  onConfirm: (name: string) => void
  onCancel: () => void
}

function InlineRename({ initialValue, onConfirm, onCancel }: InlineRenameProps) {
  const [value, setValue] = useState(initialValue)
  const confirmed = useRef(false)

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' && value.trim()) {
      confirmed.current = true
      onConfirm(value.trim())
    }
    if (e.key === 'Escape') onCancel()
  }

  return (
    <input
      style={s.renameInput}
      value={value}
      autoFocus
      onChange={(e) => setValue(e.target.value)}
      onKeyDown={handleKeyDown}
      onBlur={() => {
        if (confirmed.current) return
        if (value.trim()) onConfirm(value.trim()); else onCancel()
      }}
    />
  )
}

// ─── MoveModal ────────────────────────────────────────────────────────────────

interface MoveModalProps {
  target: MoveTarget
  onConfirm: (destFolderID: string | null) => void
  onClose: () => void
}

function MoveModal({ target, onConfirm, onClose }: MoveModalProps) {
  const [pickerFolderID, setPickerFolderID] = useState<string | null>(null)
  const [pickerAncestors, setPickerAncestors] = useState<Array<{ id: string; name: string }>>([])

  const { data, isLoading } = useQuery<FolderContents>({
    queryKey: ['move-picker', pickerFolderID ?? 'root'],
    queryFn: () => pickerFolderID
      ? api.get<FolderContents>(`/api/v1/folders/${pickerFolderID}`)
      : api.get<FolderContents>('/api/v1/folders'),
  })

  function navigateTo(folder: { id: string; name: string }) {
    setPickerAncestors(prev => [...prev, folder])
    setPickerFolderID(folder.id)
  }

  function navigateToAncestor(idx: number) {
    if (idx < 0) {
      setPickerAncestors([])
      setPickerFolderID(null)
    } else {
      const ancestor = pickerAncestors[idx]
      setPickerAncestors(prev => prev.slice(0, idx + 1))
      setPickerFolderID(ancestor.id)
    }
  }

  return (
    <div style={ms.backdrop} onClick={onClose}>
      <div style={ms.modal} onClick={(e) => e.stopPropagation()}>
        <div style={ms.modalHeader}>
          <span style={ms.modalTitle}>Move "{target.name}" to folder</span>
          <button style={ms.closeBtn} onClick={onClose}>×</button>
        </div>

        {/* Breadcrumb */}
        <div style={ms.pickerCrumb}>
          <button style={ms.crumbLink} onClick={() => navigateToAncestor(-1)}>Root</button>
          {pickerAncestors.map((a, idx) => (
            <span key={a.id} style={{ display: 'flex', alignItems: 'center' }}>
              <span style={{ color: 'var(--text-muted)', margin: '0 4px' }}>/</span>
              <button style={ms.crumbLink} onClick={() => navigateToAncestor(idx)}>{a.name}</button>
            </span>
          ))}
        </div>

        <div style={ms.folderList}>
          {isLoading && <div style={ms.loadingText}>Loading…</div>}
          {!isLoading && data && data.folders.length === 0 && (
            <div style={ms.emptyText}>No subfolders here.</div>
          )}
          {data?.folders.map((f) => (
            <button key={f.id} style={ms.folderRow} onClick={() => navigateTo({ id: f.id, name: f.name })}>
              <FolderIcon size={14} style={{ color: 'var(--accent)', flexShrink: 0, marginRight: 8 }} />
              <span style={{ flex: 1, textAlign: 'left', fontSize: 13 }}>{f.name}</span>
              <span style={ms.drillArrow}>›</span>
            </button>
          ))}
        </div>

        <div style={ms.modalFooter}>
          <button style={ms.moveHereBtn} onClick={() => onConfirm(pickerFolderID)}>
            Move here
          </button>
          <button style={s.cancelBtn} onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  )
}

// ─── MetaLine ─────────────────────────────────────────────────────────────────

function MetaLine({ createdBy, createdAt, updatedAt }: { createdBy: string; createdAt: string; updatedAt?: string }) {
  const created = new Date(createdAt)
  const updated = updatedAt ? new Date(updatedAt) : null
  const fmt = (d: Date) => isNaN(d.getTime()) ? '' : d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
  const showUpdated = updated && !isNaN(updated.getTime()) && updated.getTime() - created.getTime() > 60_000
  return (
    <span style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 2, display: 'block', lineHeight: 1.4 }}>
      {createdBy} · {fmt(created)}{showUpdated && <> · Updated {fmt(updated!)}</>}
    </span>
  )
}

// ─── HomePage ────────────────────────────────────────────────────────────────

export function HomePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const folderID = searchParams.get('folder')
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { user } = useAuth()
  const [filter, setFilter] = useState<'all' | 'mine'>('all')
  const [searchQuery, setSearchQuery] = useState('')

  const filterItems = <T extends { created_by: string }>(items: T[]): T[] =>
    filter === 'mine' ? items.filter(i => i.created_by === user?.user_id) : items

  const { data: recentItems = [] } = useQuery<Array<{
    id: string; type: string; name: string; updated_at: string
  }>>({
    queryKey: ['recent'],
    queryFn: () => api.get('/api/v1/recent'),
  })

  const [creating, setCreating] = useState<null | 'folder' | 'notebook' | 'dashboard'>(null)
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)

  // Context menu
  const [openMenu, setOpenMenu] = useState<MenuTarget | null>(null)
  const [menuPos, setMenuPos] = useState<{ top: number; left: number }>()

  // Rename
  const [renaming, setRenaming] = useState<RenameTarget | null>(null)

  // Move modal
  const [moving, setMoving] = useState<MoveTarget | null>(null)

  // Permissions (stub — Task 13 will wire this up)
  const [permissionsTarget, setPermissionsTarget] = useState<PermissionsTarget | null>(null)

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

  const { data: members = [] } = useQuery<Array<{ user_id: string; name: string }>>({
    queryKey: ['members'],
    queryFn: () => api.get('/api/v1/members'),
  })
  const memberName = (userId: string) =>
    members.find(m => m.user_id === userId)?.name ?? userId.slice(0, 8)

  useEffect(() => {
    const name = data?.folder?.name
    document.title = name ? `${name} — hnb` : "Files — hnb"
  }, [data?.folder?.name])

  // ── Mutations ──

  const createFolder = useMutation({
    mutationFn: (name: string) =>
      api.post<Folder>('/api/v1/folders', { name, ...(folderID ? { parent_id: folderID } : {}) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
      setCreating(null)
      setNewName('')
    },
    onError: (e: Error) => setError(e.message),
  })

  const createNotebook = useMutation({
    mutationFn: (title: string) =>
      api.post<{ id: string }>('/api/v1/notebooks', { title, ...(folderID ? { folder_id: folderID } : {}) }),
    onSuccess: (nb) => navigate(`/notebooks/${nb.id}`),
    onError: (e: Error) => setError(e.message),
  })

  const createDashboard = useMutation({
    mutationFn: (title: string) =>
      api.post<{ id: string }>('/api/v1/dashboards', { title, ...(folderID ? { folder_id: folderID } : {}) }),
    onSuccess: (d) => navigate(`/dashboards/${d.id}`),
    onError: (e: Error) => setError(e.message),
  })

  const deleteFolder = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/folders/${id}?force=true`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
    },
    onError: (e: Error) => setError(e.message),
  })

  const deleteNotebook = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/notebooks/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
    },
    onError: (e: Error) => setError(e.message),
  })

  const renameFolder = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      api.put(`/api/v1/folders/${id}`, { name }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
      setRenaming(null)
    },
    onError: (e: Error) => setError(e.message),
  })

  const renameNotebook = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api.put(`/api/v1/notebooks/${id}`, { title }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
      setRenaming(null)
    },
    onError: (e: Error) => setError(e.message),
  })

  const moveItem = useMutation({
    mutationFn: ({ type, id, destFolderID }: { type: ResourceType; id: string; destFolderID: string | null }) => {
      if (type === 'folder') {
        return api.put(`/api/v1/folders/${id}`, { parent_id: destFolderID })
      } else if (type === 'notebook') {
        return api.put(`/api/v1/notebooks/${id}`, { folder_id: destFolderID })
      } else if (type === 'connector') {
        return api.put(`/api/v1/connectors/${id}`, { folder_id: destFolderID })
      } else {
        return api.put(`/api/v1/dashboards/${id}`, { folder_id: destFolderID })
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
      setMoving(null)
    },
    onError: (e: Error) => setError(e.message),
  })

  // ── Handlers ──

  const q = searchQuery.trim().toLowerCase()
  const searchFolders = q
    ? (data?.folders ?? []).filter(f => f.name.toLowerCase().includes(q))
    : data?.folders ?? []
  const searchNotebooks = q
    ? (data?.notebooks ?? []).filter(nb => nb.title.toLowerCase().includes(q))
    : data?.notebooks ?? []
  const searchConnectors = q
    ? (data?.connectors ?? []).filter(c => c.name.toLowerCase().includes(q))
    : data?.connectors ?? []
  const searchDashboards = q
    ? (data?.dashboards ?? []).filter(d => d.title.toLowerCase().includes(q))
    : data?.dashboards ?? []

  const isEmpty = data &&
    filterItems(searchFolders).length === 0 &&
    filterItems(searchNotebooks).length === 0 &&
    filterItems(searchConnectors).length === 0 &&
    filterItems(searchDashboards).length === 0

  const handleCreate = () => {
    if (!newName.trim()) return
    if (creating === 'folder') createFolder.mutate(newName.trim())
    else if (creating === 'notebook') createNotebook.mutate(newName.trim())
    else if (creating === 'dashboard') createDashboard.mutate(newName.trim())
  }

  function handleMenuOpen(e: React.MouseEvent, target: MenuTarget) {
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const menuHeight = 160 // approximate; menu items are ~36px each × 4 items
    const top = rect.bottom + 4 + menuHeight > window.innerHeight
      ? rect.top - menuHeight - 4
      : rect.bottom + 4
    setMenuPos({ top, left: rect.left })
    setOpenMenu(target)
  }

  function handleDelete(type: ResourceType, id: string) {
    if (type === 'folder') deleteFolder.mutate(id)
    else if (type === 'notebook') deleteNotebook.mutate(id)
    // connectors / dashboards: no-op (TODO: implement via their own pages)
  }

  function handleRenameConfirm(newValue: string) {
    if (!renaming) return
    if (renaming.type === 'folder') {
      renameFolder.mutate({ id: renaming.id, name: newValue })
    } else {
      renameNotebook.mutate({ id: renaming.id, title: newValue })
    }
  }

  function handleMoveConfirm(destFolderID: string | null) {
    if (!moving) return
    moveItem.mutate({ type: moving.type, id: moving.id, destFolderID })
  }

  function handlePermissions(t: PermissionsTarget) {
    setPermissionsTarget(t)
  }

  function handleMoveFolder(folder: Folder) {
    setMoving({ type: 'folder', id: folder.id, name: folder.name })
  }

  function handlePermissionsFolder(folder: Folder) {
    setPermissionsTarget({ type: 'folder', id: folder.id, name: folder.name })
  }

  function handleEdit(type: ResourceType, id: string) {
    if (type === 'connector') navigate(`/connectors?edit=${id}`)
  }

  return (
    <AppShell>
      <TwoPanelLayout
        leftPanel={
          <FolderTree
            onSelectFolder={(id) => setSearchParams(id ? { folder: id } : {})}
            selectedFolderId={folderID}
            onMoveFolder={handleMoveFolder}
            onPermissionsFolder={handlePermissionsFolder}
          />
        }
        rightPanel={
          <div style={{ maxWidth: 1280, margin: '0 auto', position: 'relative' }}>
            {/* Filter pills */}
            <div style={{ display: 'flex', gap: 6, marginBottom: 8 }}>
              {(['all', 'mine'] as const).map(f => (
                <button
                  key={f}
                  onClick={() => setFilter(f)}
                  style={{
                    padding: '4px 12px',
                    borderRadius: 20,
                    fontSize: 12,
                    fontWeight: 500,
                    cursor: 'pointer',
                    border: 'none',
                    background: filter === f ? 'var(--accent)' : 'var(--accent-light)',
                    color: filter === f ? '#fff' : 'var(--accent)',
                  }}
                >
                  {f === 'all' ? 'All' : 'Created by me'}
                </button>
              ))}
            </div>

            {/* Search bar */}
            <div style={{ marginBottom: 12 }}>
              <input
                style={s.searchInput}
                type="search"
                placeholder="Search by name…"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                aria-label="Search files"
              />
            </div>

            {/* Breadcrumb — only when in a folder */}
            {folderID && (
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
            )}

            {/* Recent section — root only, no active search */}
            {!folderID && !searchQuery && recentItems.length > 0 && (
              <section style={{ ...s.section, marginBottom: 20 }}>
                <div style={s.sectionLabel}>Recent</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {recentItems.slice(0, 5).map((item) => {
                    const href = item.type === 'notebook'
                      ? `/notebooks/${item.id}`
                      : item.type === 'dashboard'
                      ? `/dashboards/${item.id}`
                      : `/connectors`
                    const icon = item.type === 'notebook'
                      ? <BookOpen size={12} style={{ flexShrink: 0 }} />
                      : item.type === 'dashboard'
                      ? <LayoutDashboard size={12} style={{ flexShrink: 0 }} />
                      : <Database size={12} style={{ flexShrink: 0 }} />
                    return (
                      <button
                        key={`${item.type}-${item.id}`}
                        style={s.recentChip}
                        onClick={() => navigate(href)}
                        title={item.name}
                      >
                        {icon}
                        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const, maxWidth: 160 }}>
                          {item.name}
                        </span>
                      </button>
                    )
                  })}
                </div>
              </section>
            )}

            {/* Toolbar */}
            <div style={s.toolbar}>
              <button style={s.newBtn} onClick={() => { setCreating('folder'); setNewName('') }}>
                + New Folder
              </button>
              <button style={s.newBtn} onClick={() => { setCreating('notebook'); setNewName('') }}>
                + New Notebook
              </button>
              <button style={s.newBtn} onClick={() => { setCreating('dashboard'); setNewName('') }}>
                + New Dashboard
              </button>
            </div>

            {/* Inline create form */}
            {creating && (
              <div style={s.createForm}>
                <input
                  style={s.input}
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder={
                    creating === 'folder' ? 'Folder name…'
                    : creating === 'notebook' ? 'Notebook title…'
                    : 'Dashboard title…'
                  }
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

            {isLoading && <div style={{ padding: 32, color: 'var(--text-muted)', fontSize: 14 }}>Loading…</div>}

            {!isLoading && isEmpty && !creating && (
              <EmptyState
                icon={<FolderIcon size={32} />}
                title="This folder is empty"
                text="Create a folder or notebook to get started."
                action={{ label: '+ New Notebook', onClick: () => setCreating('notebook') }}
              />
            )}

            {/* Folders */}
            {data && filterItems(searchFolders).length > 0 && (
              <section style={s.section}>
                <div style={s.sectionLabel}>Folders</div>
                <div style={s.folderGrid}>
                  {filterItems(searchFolders).map((f) => (
                    <div key={f.id} style={s.folderCard} className="card-hover">
                      {renaming?.id === f.id ? (
                        <div style={{ flex: 1, padding: '4px 8px' }}>
                          <InlineRename
                            initialValue={renaming.currentName}
                            onConfirm={handleRenameConfirm}
                            onCancel={() => setRenaming(null)}
                          />
                        </div>
                      ) : (
                        <button style={s.folderBtn} onClick={() => setSearchParams({ folder: f.id })}>
                          <FolderIcon size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                          <div style={{ flex: 1, minWidth: 0 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                              <span style={s.folderName}>{f.name}</span>
                              {f.is_home && <span style={s.badge}>home</span>}
                            </div>
                            <MetaLine createdBy={memberName(f.created_by)} createdAt={f.created_at} updatedAt={f.updated_at} />
                          </div>
                        </button>
                      )}
                      <button
                        style={s.menuBtn}
                        title="More options"
                        onClick={(e) => handleMenuOpen(e, { type: 'folder', id: f.id, name: f.name })}
                      >⋯</button>
                      {openMenu?.id === f.id && menuPos && (
                        <div style={{ position: 'fixed', top: menuPos.top, left: menuPos.left, zIndex: 1000 }}>
                          <ContextMenu
                            target={openMenu}
                            onRename={setRenaming}
                            onMove={setMoving}
                            onPermissions={handlePermissions}
                            onDelete={handleDelete}
                            onEdit={handleEdit}
                            onClose={() => setOpenMenu(null)}
                          />
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </section>
            )}

            {/* Notebooks */}
            {data && filterItems(searchNotebooks).length > 0 && (
              <section style={s.section}>
                <div style={s.sectionLabel}>Notebooks</div>
                <div style={s.list}>
                  {filterItems(searchNotebooks).map((nb) => (
                    <div key={nb.id} style={s.item}>
                      {renaming?.id === nb.id ? (
                        <div style={{ flex: 1 }}>
                          <InlineRename
                            initialValue={renaming.currentName}
                            onConfirm={handleRenameConfirm}
                            onCancel={() => setRenaming(null)}
                          />
                        </div>
                      ) : (
                        <Link to={`/notebooks/${nb.id}`} style={s.itemLink}>
                          <BookOpen size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                          <div style={{ flex: 1, minWidth: 0 }}>
                            <span style={s.itemName}>{nb.title}</span>
                            <MetaLine createdBy={memberName(nb.created_by)} createdAt={nb.created_at} updatedAt={nb.updated_at} />
                          </div>
                        </Link>
                      )}
                      <div style={{ position: 'relative' }}>
                        <button
                          style={s.menuBtn}
                          title="More options"
                          onClick={(e) => handleMenuOpen(e, { type: 'notebook', id: nb.id, name: nb.title })}
                        >⋯</button>
                        {openMenu?.id === nb.id && menuPos && (
                          <div style={{ position: 'fixed', top: menuPos.top, left: menuPos.left, zIndex: 1000 }}>
                            <ContextMenu
                              target={openMenu}
                              onRename={setRenaming}
                              onMove={setMoving}
                              onPermissions={handlePermissions}
                              onDelete={handleDelete}
                              onEdit={handleEdit}
                              onClose={() => setOpenMenu(null)}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            )}

            {/* Connectors */}
            {data && filterItems(searchConnectors).length > 0 && (
              <section style={s.section}>
                <div style={s.sectionLabel}>Connectors</div>
                <div style={s.list}>
                  {filterItems(searchConnectors).map((c) => (
                    <div key={c.id} style={s.item}>
                      <Link to={`/connectors?edit=${c.id}`} style={s.itemLink}>
                        <Database size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                            <span style={s.itemName}>{c.name}</span>
                            {c.is_default && <span style={s.badge}>default</span>}
                          </div>
                          <MetaLine createdBy={memberName(c.created_by)} createdAt={c.created_at} />
                        </div>
                      </Link>
                      <div style={{ position: 'relative' }}>
                        <button
                          style={s.menuBtn}
                          title="More options"
                          onClick={(e) => handleMenuOpen(e, { type: 'connector', id: c.id, name: c.name })}
                        >⋯</button>
                        {openMenu?.id === c.id && menuPos && (
                          <div style={{ position: 'fixed', top: menuPos.top, left: menuPos.left, zIndex: 1000 }}>
                            <ContextMenu
                              target={openMenu}
                              onRename={setRenaming}
                              onMove={setMoving}
                              onPermissions={handlePermissions}
                              onDelete={handleDelete}
                              onEdit={handleEdit}
                              onClose={() => setOpenMenu(null)}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            )}

            {/* Dashboards */}
            {data && filterItems(searchDashboards).length > 0 && (
              <section style={s.section}>
                <div style={s.sectionLabel}>Dashboards</div>
                <div style={s.list}>
                  {filterItems(searchDashboards).map((d) => (
                    <div key={d.id} style={s.item}>
                      <Link to={`/dashboards/${d.id}`} style={s.itemLink}>
                        <LayoutDashboard size={15} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <span style={s.itemName}>{d.title}</span>
                          <MetaLine createdBy={memberName(d.created_by)} createdAt={d.created_at} updatedAt={d.updated_at} />
                        </div>
                      </Link>
                      <div style={{ position: 'relative' }}>
                        <button
                          style={s.menuBtn}
                          title="More options"
                          onClick={(e) => handleMenuOpen(e, { type: 'dashboard', id: d.id, name: d.title })}
                        >⋯</button>
                        {openMenu?.id === d.id && menuPos && (
                          <div style={{ position: 'fixed', top: menuPos.top, left: menuPos.left, zIndex: 1000 }}>
                            <ContextMenu
                              target={openMenu}
                              onRename={setRenaming}
                              onMove={setMoving}
                              onPermissions={handlePermissions}
                              onDelete={handleDelete}
                              onEdit={handleEdit}
                              onClose={() => setOpenMenu(null)}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            )}

            {/* Move modal */}
            {moving && (
              <MoveModal
                target={moving}
                onConfirm={handleMoveConfirm}
                onClose={() => setMoving(null)}
              />
            )}

            {/* Permissions panel */}
            {permissionsTarget && (
              <PermissionsPanel
                resourceType={permissionsTarget.type}
                resourceId={permissionsTarget.id}
                resourceName={permissionsTarget.name}
                parentFolderId={folderID ?? undefined}
                onClose={() => setPermissionsTarget(null)}
              />
            )}
          </div>
        }
      />
    </AppShell>
  )
}

// ─── Styles ──────────────────────────────────────────────────────────────────

const s: Record<string, React.CSSProperties> = {
  breadcrumb: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2, marginBottom: 20 },
  crumbBtn: { display: 'flex', alignItems: 'center', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--accent)', fontSize: 13, fontWeight: 500, padding: '2px 6px', borderRadius: 3 },
  sep: { color: 'var(--text-muted)', margin: '0 2px', fontSize: 13 },
  toolbar: { display: 'flex', gap: 10, marginBottom: 20 },
  newBtn: { padding: '7px 14px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  createForm: { display: 'flex', gap: 10, marginBottom: 20, padding: 16, background: 'var(--bg-card)', borderRadius: 4, border: '1px solid var(--border)', alignItems: 'center' },
  input: { flex: 1, padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 14, outline: 'none', background: 'var(--bg-input)', color: 'var(--text-primary)' },
  createBtn: { padding: '7px 14px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '7px 14px', border: '1px solid var(--border)', borderRadius: 4, background: 'none', fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  section: { marginBottom: 28 },
  sectionLabel: { fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase' as const, color: 'var(--text-muted)', marginBottom: 8 },
  folderGrid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 8 },
  folderCard: { display: 'flex', alignItems: 'center', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 4, overflow: 'visible', transition: 'border-color 0.15s', position: 'relative' },
  folderBtn: { flex: 1, display: 'flex', alignItems: 'center', gap: 7, padding: '9px 12px', background: 'none', border: 'none', cursor: 'pointer', textAlign: 'left' as const, minWidth: 0 },
  folderName: { fontSize: 13, fontWeight: 500, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const, flex: 1 },
  badge: { fontSize: 10, fontWeight: 700, background: 'var(--accent-light)', color: 'var(--accent)', borderRadius: 3, padding: '1px 5px', letterSpacing: '0.04em', flexShrink: 0 },
  menuBtn: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 18, padding: '0 8px', flexShrink: 0, lineHeight: 1, letterSpacing: 1 },
  list: { display: 'flex', flexDirection: 'column', gap: 6 },
  item: { display: 'flex', alignItems: 'center', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 4, padding: '8px 14px', gap: 10 },
  itemLink: { flex: 1, display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none', minWidth: 0 },
  itemName: { fontSize: 14, fontWeight: 500, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const, flex: 1 },
  renameInput: { width: '100%', padding: '5px 8px', border: '1px solid var(--accent)', borderRadius: 3, fontSize: 13, outline: 'none' },
  searchInput: { width: '100%', maxWidth: 320, padding: '7px 12px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, outline: 'none', background: 'var(--bg-input)', color: 'var(--text-primary)' },
  recentChip: { display: 'inline-flex', alignItems: 'center', gap: 5, padding: '4px 10px', borderRadius: 20, fontSize: 12, fontWeight: 500, border: '1px solid var(--border)', background: 'var(--bg-card)', color: 'var(--text-primary)', cursor: 'pointer' },
}

// ─── Context menu + modal styles ─────────────────────────────────────────────

const ms: Record<string, React.CSSProperties> = {
  menu: { background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 6, boxShadow: 'var(--shadow-md)', minWidth: 160, padding: '4px 0', display: 'flex', flexDirection: 'column' },
  item: { background: 'none', border: 'none', cursor: 'pointer', textAlign: 'left', padding: '8px 16px', fontSize: 13, color: 'var(--text-primary)', width: '100%' },
  backdrop: { position: 'fixed', inset: 0, background: 'var(--bg-overlay)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 2000 },
  modal: { background: 'var(--bg-card)', borderRadius: 8, boxShadow: 'var(--shadow-lg)', width: 400, maxHeight: '80vh', display: 'flex', flexDirection: 'column', overflow: 'hidden' },
  modalHeader: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 18px', borderBottom: '1px solid var(--border)' },
  modalTitle: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  closeBtn: { background: 'none', border: 'none', cursor: 'pointer', fontSize: 20, color: 'var(--text-muted)', lineHeight: 1, padding: '0 4px' },
  pickerCrumb: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', padding: '8px 16px', borderBottom: '1px solid var(--border-light)', fontSize: 12, gap: 2 },
  crumbLink: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--accent)', fontSize: 12, fontWeight: 500, padding: '2px 4px', borderRadius: 3 },
  folderList: { flex: 1, overflowY: 'auto', padding: '8px 0', minHeight: 120, maxHeight: 300 },
  folderRow: { display: 'flex', alignItems: 'center', width: '100%', background: 'none', border: 'none', cursor: 'pointer', padding: '9px 16px', textAlign: 'left', borderBottom: '1px solid var(--border-light)', color: 'var(--text-primary)' },
  drillArrow: { color: 'var(--text-muted)', fontSize: 16, marginLeft: 4 },
  loadingText: { padding: '16px', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' },
  emptyText: { padding: '16px', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' },
  modalFooter: { display: 'flex', gap: 10, padding: '12px 16px', borderTop: '1px solid var(--border)', justifyContent: 'flex-end' },
  moveHereBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
}
