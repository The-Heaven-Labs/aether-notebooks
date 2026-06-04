import { useState, useEffect, useRef } from 'react'
import { ChevronRight, ChevronDown, Folder as FolderIcon, FolderOpen } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Folder, FolderContents } from '../types'

interface FolderTreeProps {
  onSelectFolder: (folderId: string | null) => void
  selectedFolderId: string | null
  onMoveFolder?: (folder: Folder) => void
  onPermissionsFolder?: (folder: Folder) => void
}

export function FolderTree({ onSelectFolder, selectedFolderId, onMoveFolder, onPermissionsFolder }: FolderTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    try {
      const saved = localStorage.getItem('hnb_tree_expanded')
      return saved ? new Set(JSON.parse(saved)) : new Set()
    } catch {
      return new Set()
    }
  })

  const [openMenuId, setOpenMenuId] = useState<string | null>(null)
  const [menuPos, setMenuPos] = useState<{ top: number; left: number } | null>(null)

  useEffect(() => {
    if (!openMenuId) return
    function handleClick(e: MouseEvent) {
      // Check if click is inside the menu portal (fixed-positioned menu)
      const menus = document.querySelectorAll('[data-folder-menu]')
      for (const menu of menus) {
        if (menu.contains(e.target as Node)) return
      }
      setOpenMenuId(null)
    }
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpenMenuId(null)
    }
    document.addEventListener('mousedown', handleClick)
    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('mousedown', handleClick)
      document.removeEventListener('keydown', handleKey)
    }
  }, [openMenuId])

  const { data: folderData } = useQuery<FolderContents>({
    queryKey: ['folder-tree-root'],
    queryFn: () => api.get<FolderContents>('/api/v1/folders'),
  })

  const [childrenMap, setChildrenMap] = useState<Record<string, Folder[]>>({})
  const [allFolders, setAllFolders] = useState<Folder[]>([])

  // Fetch root folders + home folders separately
  const { data: homeData } = useQuery<Array<{ id: string; name: string; is_home: boolean; owner_id: string; sub_folders?: Folder[] }>>({
    queryKey: ['folder-home'],
    queryFn: () => api.get('/api/v1/home'),
  })

  // Initialize allFolders from root folders + all home folders + their sub_folders
  useEffect(() => {
    const rootFolders = folderData?.folders ?? []
    const homeFolders: Folder[] = (homeData ?? []).map((h) => ({
      id: h.id,
      org_id: '',
      parent_id: undefined,
      name: h.name,
      is_home: h.is_home,
      owner_id: h.owner_id,
      created_by: '',
      created_at: '',
      updated_at: '',
    }))

    // Build children map from home sub_folders
    const childMap: Record<string, Folder[]> = {}
    for (const home of (homeData ?? [])) {
      if (home.sub_folders) {
        childMap[home.id] = home.sub_folders
      }
    }

    setAllFolders([...rootFolders, ...homeFolders])
    setChildrenMap(childMap)
  }, [folderData, homeData])

  const fetchChildren = (folderId: string) => {
    if (childrenMap[folderId]) return // Already have children

    api.get<FolderContents>(`/api/v1/folders/${folderId}`).then(data => {
      setChildrenMap(prev => ({ ...prev, [folderId]: data.folders ?? [] }))
    }).catch(console.error)
  }

  const rootFolders = allFolders.filter(f => !f.parent_id)
  const homeFolders = rootFolders.filter(f => f.is_home)
  const orgFolders = rootFolders.filter(f => !f.is_home)

  const toggleFolder = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else {
        next.add(id)
        fetchChildren(id) // Fetch children when expanding
      }
      localStorage.setItem('hnb_tree_expanded', JSON.stringify([...next]))
      return next
    })
  }

  // Auto-expand when selected folder changes
  useEffect(() => {
    if (!selectedFolderId) return

    api.get<Array<{ id: string }>>(`/api/v1/folders/${selectedFolderId}/ancestors`)
      .then(ancestors => {
        setExpanded(prev => {
          const next = new Set(prev)
          ancestors.forEach(a => next.add(a.id))
          return next
        })
        // Also fetch children for each ancestor
        ancestors.forEach(a => fetchChildren(a.id))
      })
      .catch(console.error)
  }, [selectedFolderId])

  return (
    <div style={{ padding: '8px 0' }}>
      {/* Home folders section */}
      {homeFolders.length > 0 && (
        <div style={{ marginBottom: 16 }}>
          <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8 }}>
            Home
          </div>
          {homeFolders.map(f => (
            <TreeNodeComponent
              key={f.id}
              folder={f}
              children={childrenMap[f.id] ?? []}
              childrenMap={childrenMap}
              expanded={expanded}
              onToggle={toggleFolder}
              onSelect={onSelectFolder}
              selectedFolderId={selectedFolderId}
              depth={0}
              onMoveFolder={onMoveFolder}
              onPermissionsFolder={onPermissionsFolder}
              openMenuId={openMenuId}
              setOpenMenuId={setOpenMenuId}
              menuPos={menuPos}
              setMenuPos={setMenuPos}
            />
          ))}
        </div>
      )}

      {/* Root folders section */}
      {orgFolders.length > 0 && (
        <div>
          <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8 }}>
            Folders
          </div>
          {orgFolders.map(f => (
            <TreeNodeComponent
              key={f.id}
              folder={f}
              children={childrenMap[f.id] ?? []}
              childrenMap={childrenMap}
              expanded={expanded}
              onToggle={toggleFolder}
              onSelect={onSelectFolder}
              selectedFolderId={selectedFolderId}
              depth={0}
              onMoveFolder={onMoveFolder}
              onPermissionsFolder={onPermissionsFolder}
              openMenuId={openMenuId}
              setOpenMenuId={setOpenMenuId}
              menuPos={menuPos}
              setMenuPos={setMenuPos}
            />
          ))}
        </div>
      )}
    </div>
  )
}

interface TreeNodeComponentProps {
  folder: Folder
  children: Folder[]
  childrenMap: Record<string, Folder[]>
  expanded: Set<string>
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  selectedFolderId: string | null
  depth: number
  onMoveFolder?: (folder: Folder) => void
  onPermissionsFolder?: (folder: Folder) => void
  openMenuId: string | null
  setOpenMenuId: (id: string | null) => void
  menuPos: { top: number; left: number } | null
  setMenuPos: (pos: { top: number; left: number } | null) => void
}

function TreeNodeComponent({ folder, children, childrenMap, expanded, onToggle, onSelect, selectedFolderId, depth, onMoveFolder, onPermissionsFolder, openMenuId, setOpenMenuId, menuPos, setMenuPos }: TreeNodeComponentProps) {
  const hasChildren = children.length > 0
  const isExpanded = expanded.has(folder.id)
  const isSelected = selectedFolderId === folder.id
  const isMenuOpen = openMenuId === folder.id
  const [isHovered, setIsHovered] = useState(false)

  const toggleMenu = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (isMenuOpen) {
      setOpenMenuId(null)
    } else {
      const rect = e.currentTarget.getBoundingClientRect()
      setMenuPos({ top: rect.top, left: rect.right + 8 })
      setOpenMenuId(folder.id)
    }
  }

  const handleMoveFolder = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (onMoveFolder) { onMoveFolder(folder) }
    setOpenMenuId(null)
  }

  const handlePermissionsFolder = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (onPermissionsFolder) { onPermissionsFolder(folder) }
    setOpenMenuId(null)
  }

  return (
    <div>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        padding: '4px 12px',
        paddingLeft: 12 + depth * 16,
        cursor: 'pointer',
        background: isSelected ? 'var(--accent-light)' : 'transparent',
        gap: 4,
        transition: 'background 0.15s ease',
      }} onClick={() => onSelect(folder.id)}
      onMouseEnter={(e) => {
        setIsHovered(true)
        if (!isSelected) e.currentTarget.style.background = 'var(--bg-secondary)'
      }}
      onMouseLeave={(e) => {
        setIsHovered(false)
        if (!isSelected) e.currentTarget.style.background = 'transparent'
      }}
    >
        {hasChildren ? (
          <button
            style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', display: 'flex', color: 'var(--text-muted)' }}
            onClick={(e) => { e.stopPropagation(); onToggle(folder.id) }}
            aria-expanded={isExpanded}
            onMouseEnter={(e) => {
              e.stopPropagation()
              e.currentTarget.style.color = 'var(--accent)'
            }}
            onMouseLeave={(e) => {
              e.stopPropagation()
              e.currentTarget.style.color = 'var(--text-muted)'
            }}
          >
            {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </button>
        ) : (
          <span style={{ width: 12 }} />
        )}
        {isExpanded && hasChildren ? <FolderOpen size={14} style={{ color: 'var(--accent)' }} /> : <FolderIcon size={14} style={{ color: 'var(--accent)' }} />}
        <span style={{ fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>{folder.name}</span>
        {isHovered && (
          <button
            style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16, padding: '2px 6px', lineHeight: 1, flexShrink: 0 }}
            title="More options"
            aria-label="More options"
            onClick={toggleMenu}
          >⋯</button>
        )}
      </div>
      {isMenuOpen && menuPos && (
        <div
          data-folder-menu=""
          style={{
            position: 'fixed',
            top: menuPos.top,
            left: menuPos.left,
            background: 'var(--bg-card)',
            border: '1px solid var(--border)',
            borderRadius: 6,
            boxShadow: 'var(--shadow-md)',
            minWidth: 140,
            padding: '4px 0',
            zIndex: 1000,
          }}
        >
          {onMoveFolder && (
            <button
              style={{ display: 'block', width: '100%', textAlign: 'left', padding: '7px 12px', background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}
              onClick={handleMoveFolder}
            >Move to…</button>
          )}
          {onPermissionsFolder && (
            <button
              style={{ display: 'block', width: '100%', textAlign: 'left', padding: '7px 12px', background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}
              onClick={handlePermissionsFolder}
            >Permissions</button>
          )}
        </div>
      )}
      {isExpanded && hasChildren && children.map(child => (
        <TreeNodeComponent
          key={child.id}
          folder={child}
          children={childrenMap[child.id] ?? []}
          childrenMap={childrenMap}
          expanded={expanded}
          onToggle={onToggle}
          onSelect={onSelect}
          selectedFolderId={selectedFolderId}
          depth={depth + 1}
          onMoveFolder={onMoveFolder}
          onPermissionsFolder={onPermissionsFolder}
          openMenuId={openMenuId}
          setOpenMenuId={setOpenMenuId}
          menuPos={menuPos}
          setMenuPos={setMenuPos}
        />
      ))}
    </div>
  )
}