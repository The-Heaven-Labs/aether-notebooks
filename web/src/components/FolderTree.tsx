import { useState, useEffect } from 'react'
import { ChevronRight, ChevronDown, Folder as FolderIcon, FolderOpen } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Folder, FolderContents } from '../types'

interface FolderTreeProps {
  onSelectFolder: (folderId: string | null) => void
  selectedFolderId: string | null
}

export function FolderTree({ onSelectFolder, selectedFolderId }: FolderTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    try {
      const saved = localStorage.getItem('hnb_tree_expanded')
      return saved ? new Set(JSON.parse(saved)) : new Set()
    } catch {
      return new Set()
    }
  })

  const { data: folderData } = useQuery<FolderContents>({
    queryKey: ['folder-tree-root'],
    queryFn: () => api.get<FolderContents>('/api/v1/folders'),
  })

  const [childrenMap, setChildrenMap] = useState<Record<string, Folder[]>>({})
  const [allFolders, setAllFolders] = useState<Folder[]>([])

  // Fetch root folders + home folders separately
  const { data: homeData } = useQuery({
    queryKey: ['folder-home'],
    queryFn: () => api.get('/api/v1/home'),
  })

  // Initialize allFolders from root folders + all home folders + their sub_folders
  useEffect(() => {
    const rootFolders = folderData?.folders ?? []
    const homeFolders: Folder[] = (homeData ?? []).map((h: any) => ({
      id: h.id,
      org_id: '',
      parent_id: null as string | null,
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
      <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8 }}>
        Folders
      </div>
      {rootFolders.map(f => (
        <TreeNodeComponent
          key={f.id}
          folder={f}
          children={childrenMap[f.id] ?? []}
          expanded={expanded}
          onToggle={toggleFolder}
          onSelect={onSelectFolder}
          selectedFolderId={selectedFolderId}
          depth={0}
        />
      ))}
    </div>
  )
}

interface TreeNodeComponentProps {
  folder: Folder
  children: Folder[]
  expanded: Set<string>
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  selectedFolderId: string | null
  depth: number
}

function TreeNodeComponent({ folder, children, expanded, onToggle, onSelect, selectedFolderId, depth }: TreeNodeComponentProps) {
  const hasChildren = children.length > 0
  const isExpanded = expanded.has(folder.id)
  const isSelected = selectedFolderId === folder.id

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
        if (!isSelected) e.currentTarget.style.background = 'var(--bg-secondary)'
      }}
      onMouseLeave={(e) => {
        if (!isSelected) e.currentTarget.style.background = 'transparent'
      }}
    >
        {hasChildren ? (
          <button
            style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', display: 'flex', color: 'var(--text-muted)' }}
            onClick={(e) => { e.stopPropagation(); onToggle(folder.id) }}
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
        <span style={{ fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{folder.name}</span>
      </div>
      {isExpanded && hasChildren && children.map(child => (
        <TreeNodeComponent
          key={child.id}
          folder={child}
          children={childrenMap[child.id] ?? []}
          expanded={expanded}
          onToggle={onToggle}
          onSelect={onSelect}
          selectedFolderId={selectedFolderId}
          depth={depth + 1}
        />
      ))}
    </div>
  )
}
