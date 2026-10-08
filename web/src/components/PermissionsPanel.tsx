import { Fragment, useState, useEffect, useRef, useCallback, useId, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  X,
  Users,
  UsersRound,
  UserPlus,
  Search,
  Check,
  Copy,
  ShieldCheck,
  Mail,
} from 'lucide-react'
import { api } from '../api/client'
import { groupLabel } from '../utils/groupLabel'
import { chatLinkUrl } from '../utils/chatLink'
import { looksLikeEmail, normalizeEmail } from '../utils/email'
import { ConfirmModal } from './ConfirmModal'

// ─── Types ───────────────────────────────────────────────────────────────────

type ResourceType = 'folder' | 'notebook' | 'connector' | 'dashboard' | 'agent' | 'model_config' | 'skill' | 'mcp_server' | 'tool' | 'agent_session'
type SubjectType = 'user' | 'group' | 'org_role' | 'pending_user'

const ACTION_LABELS: Record<ResourceType, string[]> = {
  folder:      ['view', 'create', 'edit', 'manage', 'delete'],
  notebook:    ['view', 'run', 'edit', 'share', 'delete'],
  connector:   ['view', 'use', 'edit', 'share', 'delete'],
  dashboard:   ['view', 'edit', 'share', 'delete', 'view_with_data'],
  agent:       ['view', 'edit', 'delete'],
  model_config:['view', 'edit', 'delete'],
  skill:       ['view', 'edit', 'delete'],
  mcp_server:  ['view', 'edit', 'delete'],
  tool:        ['view', 'use', 'edit', 'delete'],
  // Sessions are read-only for non-owners: view is the only grantable action.
  agent_session: ['view'],
}

// Instrument-label badge per resource type (internal type keys stay internal).
const RESOURCE_LABELS: Record<ResourceType, string> = {
  folder:       'Folder',
  notebook:     'Notebook',
  connector:    'Connector',
  dashboard:    'Dashboard',
  agent:        'Agent',
  model_config: 'Model',
  skill:        'Skill',
  mcp_server:   'MCP Server',
  tool:         'Tool',
  agent_session:'Chat',
}

// Actions the API enforces for a resource type that have no chip in the
// panel. The save path keeps them on existing entries so an invisible
// permission is never silently revoked: notebook "create" gates adding and
// duplicating cells (handleCreateCell) and is seeded on notebook creation.
const UNRENDERED_ACTIONS: Partial<Record<ResourceType, string[]>> = {
  notebook: ['create'],
}

const ACTION_DESCRIPTIONS: Record<ResourceType, Record<string, string>> = {
  connector: {
    view:   'See connector name, type, host, and status',
    use:    'Run SQL queries against the connector',
    edit:   'Edit connector configuration and credentials',
    share:  'Share this connector with others',
    delete: 'Delete the connector permanently',
  },
  notebook: {
    view:   'See notebook content and cell outputs',
    run:    'Execute notebook cells',
    edit:   'Edit cell contents and notebook settings',
    share:  'Share this notebook with others',
    delete: 'Delete the notebook permanently',
  },
  dashboard: {
    view:           'View the dashboard',
    edit:           'Edit dashboard layout and content',
    share:          'Share this dashboard with others',
    delete:         'Delete the dashboard permanently',
    view_with_data: 'View the dashboard including underlying cell data (bypass notebook permissions)',
  },
  folder: {
    view:   'See the folder and its contents',
    create: 'Create sub-folders and items',
    edit:   'Rename or restructure the folder',
    manage: 'Manage folder-level permissions',
    share:  'Share this folder with others',
    delete: 'Delete the folder permanently',
  },
  agent: {
    view:   'See agent details and configuration',
    edit:   'Edit agent settings and prompt',
    delete: 'Delete the agent permanently',
  },
  model_config: {
    view:   'See model configuration details',
    edit:   'Edit model configuration',
    delete: 'Delete the model configuration permanently',
  },
  skill: {
    view:   'See skill details',
    edit:   'Edit skill definition',
    delete: 'Delete the skill permanently',
  },
  mcp_server: {
    view:   'See MCP server details',
    edit:   'Edit MCP server configuration',
    delete: 'Delete the MCP server permanently',
  },
  tool: {
    view:   'See tool details',
    use:    'Use this tool in an agent',
    edit:   'Edit tool configuration',
    delete: 'Delete the tool permanently',
  },
  agent_session: {
    view: 'Read the session transcript and watch it live (read-only)',
  },
}

interface AclEntry {
  id: string
  subject_type: SubjectType
  subject_id: string
  actions: string[]
  pending?: boolean
}

interface Member {
  user_id: string
  name: string
  email: string
  role: string
}

interface Group {
  id: string
  name: string
  display_name?: string | null
  source?: string | null
}

/** Owner-only notebook-viewer inheritance control for agent sessions. The
 * parent (SessionViewer) owns the state and performs the PATCH; the panel only
 * renders the toggle and surfaces save errors inline. */
export interface SessionNotebookInheritance {
  enabled: boolean
  hasNotebook: boolean
  onToggle: (next: boolean) => Promise<void>
}

/** Owner-only notebook link control for agent sessions. The parent owns the
 * state and performs the PATCH; the panel renders the picker and surfaces save
 * errors inline. Detaching clears inheritance server-side. */
export interface SessionNotebookLink {
  notebookId: string | null
  onSave: (notebookId: string | null) => Promise<void>
}

export interface PermissionsPanelProps {
  resourceType: ResourceType
  resourceId: string
  resourceName: string
  parentFolderId?: string
  canEdit?: boolean
  resourceOwnerId?: string
  sessionNotebookInheritance?: SessionNotebookInheritance
  sessionNotebookLink?: SessionNotebookLink
  onClose: () => void
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Narrows an entry's actions to those valid for the resource type, using the
 * chip set as the source of truth plus any unrendered backend-enforced
 * actions. Saves must never PUT actions the resource's write path rejects
 * (e.g. the owner's full-access actions on a read-only agent_session). */
function constrainActions(resourceType: ResourceType, entryActions: string[]): string[] {
  const allowed = new Set([
    ...ACTION_LABELS[resourceType],
    ...(UNRENDERED_ACTIONS[resourceType] ?? []),
  ])
  return entryActions.filter((action) => allowed.has(action))
}

function initials(name: string): string {
  return name
    .split(' ')
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join('')
}

function subjectKey(subjectType: SubjectType, subjectId: string): string {
  return `${subjectType}:${subjectId}`
}

function Avatar({ name, type, size = 30 }: { name: string; type: SubjectType; size?: number }) {
  return (
    <span
      aria-hidden="true"
      className={`access-avatar${type === 'user' ? ' is-user' : type === 'pending_user' ? ' is-pending' : ''}`}
      style={{ width: size, height: size, fontSize: size <= 24 ? 10 : 11 }}
    >
      {type === 'user'
        ? initials(name)
        : type === 'pending_user'
          ? <Mail size={13} />
          : type === 'group'
            ? <Users size={13} />
            : <UsersRound size={13} />}
    </span>
  )
}

// ─── Capability chips ────────────────────────────────────────────────────────

interface ActionChipsProps {
  actions: string[]
  selected: string[]
  descriptions: Record<string, string>
  disabled?: boolean
  onChange: (action: string) => void
}

/** One chip per grantable action. Selected chips are filled; `delete` carries
 * the destructive treatment so the riskiest action never blends in. */
function ActionChips({ actions, selected, descriptions, disabled = false, onChange }: ActionChipsProps) {
  return (
    <div className="access-chips">
      {actions.map((action) => {
        const on = selected.includes(action)
        return (
          <button
            key={action}
            type="button"
            className={`access-chip${action === 'delete' ? ' is-destructive' : ''}`}
            aria-pressed={on}
            disabled={disabled}
            title={descriptions[action] ?? action}
            onClick={() => onChange(action)}
          >
            {action}
          </button>
        )
      })}
    </div>
  )
}

// ─── Subject picker (inline, no floating popover) ────────────────────────────

interface PickerOption {
  key: string
  kind: SubjectType
  name: string
  secondary?: string
  section: 'People' | 'Groups' | 'Organization' | 'Pending'
}

interface SubjectPickerProps {
  options: PickerOption[]
  knownEmails: Set<string>
  onPick: (option: PickerOption) => void
  onCancel: () => void
}

function SubjectPicker({ options, knownEmails, onPick, onCancel }: SubjectPickerProps) {
  const [query, setQuery] = useState('')
  const [focusedIdx, setFocusedIdx] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const q = query.trim().toLowerCase()
  const filtered = q
    ? options.filter((o) =>
        o.name.toLowerCase().includes(q) ||
        (o.secondary ?? '').toLowerCase().includes(q))
    : options

  // Offer an explicit "add by email" action when the query looks like an email
  // that matches no person, group, or existing entry — never a global user
  // search. The email is lowercased to match the pending API's canonical form.
  const pendingEmail =
    q.length > 0 && looksLikeEmail(q) && !knownEmails.has(normalizeEmail(q))
      ? normalizeEmail(q)
      : null
  const pendingOption: PickerOption | null =
    pendingEmail && filtered.length === 0
      ? {
          key: subjectKey('pending_user', pendingEmail),
          kind: 'pending_user',
          name: pendingEmail,
          secondary: 'Pending — awaiting first login',
          section: 'Pending',
        }
      : null
  const displayOptions = pendingOption ? [...filtered, pendingOption] : filtered

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setFocusedIdx((i) => Math.min(i + 1, displayOptions.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setFocusedIdx((i) => Math.max(i - 1, -1))
    } else if (e.key === 'Enter' && focusedIdx >= 0 && displayOptions[focusedIdx]) {
      e.preventDefault()
      onPick(displayOptions[focusedIdx])
    } else if (e.key === 'Escape') {
      // Close the picker first; the dialog's window-level Escape handler
      // skips events already handled locally (defaultPrevented).
      e.preventDefault()
      e.stopPropagation()
      onCancel()
    }
  }

  return (
    <div className="access-picker">
      <div className="access-picker-search">
        <Search size={14} aria-hidden="true" className="access-picker-search-icon" />
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="access-picker-listbox"
          aria-activedescendant={focusedIdx >= 0 ? `access-picker-option-${focusedIdx}` : undefined}
          aria-label="Search people and groups"
          autoComplete="off"
          placeholder="Search people and groups…"
          value={query}
          onChange={(e) => { setQuery(e.target.value); setFocusedIdx(-1) }}
          onKeyDown={handleKeyDown}
        />
        <button
          type="button"
          className="access-icon-btn"
          onClick={onCancel}
          title="Cancel"
          aria-label="Cancel adding access"
        >
          <X size={14} />
        </button>
      </div>
      <ul id="access-picker-listbox" role="listbox" aria-label="People and groups" className="access-picker-list">
        {displayOptions.length === 0 && (
          <li className="access-picker-empty">
            {q ? <>No matches for “{query}”.</> : 'No people or groups left to add.'}
          </li>
        )}
        {displayOptions.map((option, idx) => {
          const showSection = idx === 0 || displayOptions[idx - 1].section !== option.section
          return (
            <Fragment key={option.key}>
              {showSection && (
                <li className="access-label access-picker-section" role="presentation">{option.section}</li>
              )}
              <li
                id={`access-picker-option-${idx}`}
                role="option"
                aria-selected={idx === focusedIdx}
                className={`access-picker-option${idx === focusedIdx ? ' is-focused' : ''}`}
                onMouseEnter={() => setFocusedIdx(idx)}
                onMouseDown={(e) => { e.preventDefault(); onPick(option) }}
              >
                <Avatar name={option.name} type={option.kind} size={26} />
                <span className="access-picker-option-name">{option.name}</span>
                {option.secondary && <span className="access-picker-option-meta">{option.secondary}</span>}
              </li>
            </Fragment>
          )
        })}
      </ul>
    </div>
  )
}

// ─── Component ───────────────────────────────────────────────────────────────

export function PermissionsPanel({
  resourceType,
  resourceId,
  resourceName,
  parentFolderId,
  canEdit = true,
  resourceOwnerId,
  sessionNotebookInheritance,
  sessionNotebookLink,
  onClose,
}: PermissionsPanelProps) {
  const qc = useQueryClient()
  const actions = ACTION_LABELS[resourceType]
  const titleId = useId()
  const dialogRef = useRef<HTMLDivElement>(null)

  // Draft state for unsaved ACL changes
  const [draft, setDraft] = useState<AclEntry[] | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [confirmDiscard, setConfirmDiscard] = useState(false)

  // Add-access composer
  type ComposerPhase = 'idle' | 'picking' | 'compose'
  const [composerPhase, setComposerPhase] = useState<ComposerPhase>('idle')
  const [newSubject, setNewSubject] = useState<PickerOption | null>(null)
  const [newActions, setNewActions] = useState<string[]>([])

  // Transient "Copied" state for the session chat link.
  const [linkCopied, setLinkCopied] = useState(false)
  const chatLinkInputRef = useRef<HTMLInputElement | null>(null)
  const linkCopiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Session notebook link / inheritance passthrough state
  const [inheritSaving, setInheritSaving] = useState(false)
  const [inheritError, setInheritError] = useState<string | null>(null)
  const [notebookValue, setNotebookValue] = useState<string>(sessionNotebookLink?.notebookId ?? '')
  const [notebookSaving, setNotebookSaving] = useState(false)
  const [notebookError, setNotebookError] = useState<string | null>(null)

  // Reset local state when the resource changes
  useEffect(() => {
    setDraft(null)
    setLinkCopied(false)
    setSaveError(null)
    setConfirmDiscard(false)
    setComposerPhase('idle')
    setNewSubject(null)
    setNewActions([])
  }, [resourceId])

  // Clear a pending "Copied" reset timer on unmount.
  useEffect(() => () => {
    if (linkCopiedTimerRef.current) clearTimeout(linkCopiedTimerRef.current)
  }, [])

  // The parent's saved value is authoritative after each PATCH.
  useEffect(() => {
    setNotebookValue(sessionNotebookLink?.notebookId ?? '')
  }, [resourceId, sessionNotebookLink?.notebookId])

  // ── Queries ──

  const aclKey = ['acl', resourceType, resourceId]

  const { data: aclData, isLoading: aclLoading } = useQuery<AclEntry[]>({
    queryKey: aclKey,
    queryFn: () => api.get<AclEntry[]>(`/api/v1/acl/${resourceType}/${resourceId}`),
  })

  const { data: members = [] } = useQuery<Member[]>({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
  })

  const { data: groups = [] } = useQuery<Group[]>({
    queryKey: ['groups'],
    queryFn: () => api.get<Group[]>('/api/v1/groups'),
  })

  const { data: parentAcl } = useQuery<AclEntry[]>({
    queryKey: ['acl', 'folder', parentFolderId],
    queryFn: () => api.get<AclEntry[]>(`/api/v1/acl/folder/${parentFolderId}`),
    enabled: !!parentFolderId,
  })

  const { data: parentFolder } = useQuery<{ folder?: { id: string; name: string } }>({
    queryKey: ['folder', parentFolderId],
    queryFn: () => api.get<{ folder?: { id: string; name: string } }>(`/api/v1/folders/${parentFolderId}`),
    enabled: !!parentFolderId,
  })

  const { data: linkableNotebooks = [] } = useQuery<Array<{ id: string; title: string }>>({
    queryKey: ['notebooks'],
    queryFn: () => api.get<Array<{ id: string; title: string }>>('/api/v1/notebooks'),
    enabled: !!sessionNotebookLink,
  })

  // ── Mutation ──

  const saveAcl = useMutation({
    mutationFn: (entries: Omit<AclEntry, 'id'>[]) =>
      api.put<AclEntry[]>(`/api/v1/acl/${resourceType}/${resourceId}`, { entries }),
    onSuccess: () => {
      setSaveError(null)
      setDraft(null)
      setComposerPhase('idle')
      setNewSubject(null)
      setNewActions([])
      qc.invalidateQueries({ queryKey: aclKey })
    },
    onError: (err: unknown) => {
      setSaveError(err instanceof Error ? err.message : 'Failed to save permissions')
    },
  })

  // ── Derived ──

  const allEntries = [
    ...(aclData ?? []).map((e: AclEntry) => ({ ...e, inherited: false })),
    ...(parentAcl ?? []).map((e: AclEntry) => ({ ...e, inherited: true })),
  ]

  const directEntries = allEntries.filter((e) => !e.inherited)
  const inheritedEntries = allEntries.filter((e) => e.inherited)
  const visibleEntries = draft !== null ? draft : directEntries
  const inheritedCount = inheritedEntries.length

  const visibleKeys = new Set(visibleEntries.map((e) => subjectKey(e.subject_type, e.subject_id)))

  // Emails that must not be offered as "pending": current members (even when
  // their entry is already in the draft) and any staged pending entry.
  const knownEmails = new Set<string>(members.map((m) => m.email.toLowerCase()))
  for (const e of visibleEntries) {
    if (e.subject_type === 'pending_user') knownEmails.add(e.subject_id.toLowerCase())
    if (e.subject_type === 'user') {
      const m = members.find((m) => m.user_id === e.subject_id)
      if (m) knownEmails.add(m.email.toLowerCase())
    }
  }

  const allPickerOptions: PickerOption[] = [
    ...members
      .filter((m) => m.user_id !== resourceOwnerId)
      .map((m) => ({
        key: subjectKey('user', m.user_id),
        kind: 'user' as const,
        name: m.name || m.email,
        secondary: m.name ? m.email : undefined,
        section: 'People' as const,
      })),
    ...groups
      .filter((g) => g.source !== 'system')
      .map((g) => ({
        key: subjectKey('group', g.id),
        kind: 'group' as const,
        name: groupLabel(g),
        secondary: 'Group',
        section: 'Groups' as const,
      })),
    {
      key: subjectKey('org_role', 'everyone'),
      kind: 'org_role' as const,
      name: 'Everyone',
      secondary: 'All organization members',
      section: 'Organization' as const,
    },
  ]
  const pickerOptions = allPickerOptions.filter((o) => !visibleKeys.has(o.key))

  // ── Helpers ──

  function subjectName(entry: AclEntry): string {
    if (entry.subject_type === 'pending_user') return entry.subject_id
    if (entry.subject_type === 'user') {
      const m = members.find((m) => m.user_id === entry.subject_id)
      return m ? (m.name || m.email) : entry.subject_id
    } else if (entry.subject_type === 'org_role') {
      return entry.subject_id === 'everyone' ? 'Everyone' : entry.subject_id
    } else {
      const g = groups.find((g) => g.id === entry.subject_id)
      return g ? groupLabel(g) : entry.subject_id
    }
  }

  function subjectSecondary(entry: AclEntry): string {
    if (entry.subject_type === 'pending_user') return 'Pending — awaiting first login'
    if (entry.subject_type === 'user') {
      const m = members.find((m) => m.user_id === entry.subject_id)
      return m ? m.email : 'User'
    }
    if (entry.subject_type === 'group') return 'Group'
    return 'All organization members'
  }

  function handleToggleAction(entryIndex: number, action: string) {
    const current = draft ?? aclData ?? []
    const updated = current.map((e, i) => {
      if (i !== entryIndex) return e
      const actions = e.actions.includes(action)
        ? e.actions.filter(a => a !== action)
        : [...e.actions, action]
      return { ...e, actions }
    })
    setDraft(updated)
  }

  function handleRemoveEntry(entryIndex: number) {
    const current = draft ?? aclData ?? []
    setDraft(current.filter((_, i) => i !== entryIndex))
  }

  function handlePickSubject(option: PickerOption) {
    setNewSubject(option)
    setNewActions(['view'])
    setComposerPhase('compose')
  }

  function handleAddEntry() {
    if (!newSubject || newActions.length === 0 || aclLoading) return
    const current = draft ?? aclData ?? []
    const updated: AclEntry[] = [
      ...current,
      {
        id: '',
        subject_type: newSubject.kind,
        subject_id: newSubject.key.slice(newSubject.key.indexOf(':') + 1),
        actions: newActions,
      },
    ]
    setDraft(updated)
    setNewSubject(null)
    setNewActions([])
    setComposerPhase('idle')
  }

  function toggleNewAction(action: string) {
    setNewActions((prev) =>
      prev.includes(action) ? prev.filter((a) => a !== action) : [...prev, action]
    )
  }

  function handleSave() {
    if (draft === null) return
    saveAcl.mutate(
      draft.map(({ id: _id, pending: _pending, ...rest }) => ({
        ...rest,
        actions: constrainActions(resourceType, rest.actions),
      }))
    )
  }

  async function handleToggleInheritance(next: boolean) {
    if (!sessionNotebookInheritance || inheritSaving) return
    setInheritSaving(true)
    setInheritError(null)
    try {
      await sessionNotebookInheritance.onToggle(next)
    } catch (err: unknown) {
      setInheritError(err instanceof Error ? err.message : 'Failed to update sharing')
    } finally {
      setInheritSaving(false)
    }
  }

  async function handleNotebookChange(next: string) {
    if (!sessionNotebookLink || notebookSaving || next === notebookValue) return
    const previous = notebookValue
    setNotebookValue(next)
    setNotebookSaving(true)
    setNotebookError(null)
    try {
      await sessionNotebookLink.onSave(next === '' ? null : next)
    } catch (err: unknown) {
      setNotebookValue(previous)
      setNotebookError(err instanceof Error ? err.message : 'Failed to update notebook')
    } finally {
      setNotebookSaving(false)
    }
  }

  async function handleCopyChatLink() {
    try {
      await navigator.clipboard.writeText(chatLinkUrl(resourceId))
      setLinkCopied(true)
      if (linkCopiedTimerRef.current) clearTimeout(linkCopiedTimerRef.current)
      linkCopiedTimerRef.current = setTimeout(() => setLinkCopied(false), 1500)
    } catch {
      // Clipboard API unavailable or denied (e.g. non-secure context): select
      // the input so the user can press Ctrl/Cmd+C.
      chatLinkInputRef.current?.focus()
      chatLinkInputRef.current?.select()
    }
  }

  // ── Close handling ──

  const requestClose = useCallback(() => {
    if (draft !== null) setConfirmDiscard(true)
    else onClose()
  }, [draft, onClose])

  // Keep the latest close handler without re-running the mount effect (which
  // would steal focus on every draft change).
  const requestCloseRef = useRef(requestClose)
  useEffect(() => {
    requestCloseRef.current = requestClose
  }, [requestClose])

  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    dialogRef.current?.focus()
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault()
        requestCloseRef.current()
      }
    }
    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
      previouslyFocused?.focus?.()
    }
  }, [])

  // ── Render ──

  // Portal to the body: callers embed the panel inside positioned ancestors
  // (e.g. the session viewer's fixed wrapper), and those ancestors' stacking
  // contexts would otherwise cap the dialog below the top bar.
  return createPortal(
    <>
      <div
        className="access-overlay-enter"
        style={styles.overlay}
        onClick={requestClose}
      >
        <div
          ref={dialogRef}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          tabIndex={-1}
          className="access-dialog-enter"
          style={styles.dialog}
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div style={styles.header}>
            <div style={styles.headerTop}>
              <h2 id={titleId} style={styles.title}>Permissions</h2>
              <button
                type="button"
                className="access-icon-btn"
                style={styles.closeBtn}
                onClick={requestClose}
                title="Close"
                aria-label="Close permissions dialog"
              >
                <X size={16} />
              </button>
            </div>
            <div style={styles.headerMeta}>
              <span style={styles.resourceName} title={resourceName}>{resourceName}</span>
              <span className="access-badge">{RESOURCE_LABELS[resourceType]}</span>
            </div>
          </div>

          {/* Body */}
          <div style={styles.body}>
            {resourceType === 'agent_session' && (
              <section style={styles.shareCard}>
                <div className="access-label">Session sharing</div>

                <div style={styles.shareField}>
                  <div style={styles.shareFieldHead}>
                    <span style={styles.fieldTitle}>Chat link</span>
                    <span style={styles.fieldHint}>Anyone with access can open this chat directly.</span>
                  </div>
                  <div style={styles.chatLinkRow}>
                    <input
                      aria-label="Chat link"
                      readOnly
                      ref={chatLinkInputRef}
                      value={chatLinkUrl(resourceId)}
                      onFocus={(e) => e.currentTarget.select()}
                      style={styles.chatLinkInput}
                    />
                    <button
                      type="button"
                      aria-label="Copy chat link"
                      className="access-btn-secondary"
                      onClick={() => { void handleCopyChatLink() }}
                    >
                      {linkCopied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
                    </button>
                  </div>
                </div>

                {sessionNotebookLink && canEdit && (
                  <div style={styles.shareField}>
                    <div style={styles.shareFieldHead}>
                      <span style={styles.fieldTitle}>Notebook</span>
                      <span style={styles.fieldHint}>Linked sessions appear in the notebook's Chats drawer.</span>
                    </div>
                    <select
                      aria-label="Notebook"
                      style={styles.notebookSelect}
                      value={notebookValue}
                      disabled={notebookSaving}
                      onChange={(e) => { void handleNotebookChange(e.target.value) }}
                    >
                      <option value="">No notebook</option>
                      {linkableNotebooks.map((nb) => (
                        <option key={nb.id} value={nb.id}>{nb.title}</option>
                      ))}
                    </select>
                    {notebookError && <div style={styles.errorText}>{notebookError}</div>}
                  </div>
                )}

                {sessionNotebookInheritance && (sessionNotebookLink ? notebookValue !== '' : sessionNotebookInheritance.hasNotebook) && canEdit && (
                  <label style={styles.switchRow}>
                    <span style={styles.shareFieldHead}>
                      <span style={styles.fieldTitle}>Anyone who can view this notebook</span>
                      <span style={styles.fieldHint}>
                        {inheritSaving
                          ? 'Saving…'
                          : sessionNotebookInheritance.enabled
                            ? 'Notebook viewers can read this session live, view only.'
                            : 'Only people explicitly shared below can read this session.'}
                      </span>
                    </span>
                    <input
                      type="checkbox"
                      className="access-switch"
                      checked={sessionNotebookInheritance.enabled}
                      disabled={inheritSaving}
                      onChange={(e) => { void handleToggleInheritance(e.target.checked) }}
                    />
                  </label>
                )}
                {inheritError && <div style={styles.errorText}>{inheritError}</div>}
              </section>
            )}

            {!canEdit && (
              <div style={styles.readOnlyNote}>
                You don't have permission to change access to this resource.
              </div>
            )}

            {aclLoading ? (
              <div style={styles.skeleton} aria-hidden="true">
                {[0, 1].map((i) => (
                  <div key={i} className="access-skel" style={styles.skeletonRow}>
                    <span style={styles.skeletonAvatar} />
                    <span style={{ ...styles.skeletonBar, width: 110 }} />
                    <span style={{ ...styles.skeletonBar, width: 190, marginLeft: 'auto' }} />
                  </div>
                ))}
              </div>
            ) : (
              <>
                {/* Add access composer */}
                {canEdit && (
                  <div style={styles.composer} role="group" aria-label="Add access">
                    {composerPhase === 'idle' && (
                      <button
                        type="button"
                        className="access-composer-trigger"
                        onClick={() => setComposerPhase('picking')}
                      >
                        <UserPlus size={14} className="access-composer-plus" aria-hidden="true" />
                        Add people or groups…
                      </button>
                    )}

                    {composerPhase === 'picking' && (
                      <SubjectPicker
                        options={pickerOptions}
                        knownEmails={knownEmails}
                        onPick={handlePickSubject}
                        onCancel={() => setComposerPhase('idle')}
                      />
                    )}

                    {composerPhase === 'compose' && newSubject && (
                      <div style={styles.composeRow}>
                        <div style={styles.composeSubject}>
                          <Avatar name={newSubject.name} type={newSubject.kind} size={24} />
                          <span style={styles.composeSubjectName} title={newSubject.name}>{newSubject.name}</span>
                          <button
                            type="button"
                            className="access-icon-btn"
                            title="Change person or group"
                            aria-label="Change selected person or group"
                            onClick={() => { setComposerPhase('picking'); setNewSubject(null); setNewActions([]) }}
                          >
                            <X size={12} />
                          </button>
                        </div>
                        <ActionChips
                          actions={actions}
                          selected={newActions}
                          descriptions={ACTION_DESCRIPTIONS[resourceType] ?? {}}
                          onChange={toggleNewAction}
                        />
                        <button
                          type="button"
                          className="access-btn-primary"
                          style={styles.addBtn}
                          disabled={newActions.length === 0 || saveAcl.isPending}
                          onClick={handleAddEntry}
                        >
                          Add
                        </button>
                      </div>
                    )}
                  </div>
                )}

                {/* Direct access */}
                <section style={styles.section}>
                  <div style={styles.sectionHead}>
                    <span className="access-label">Direct access</span>
                    {visibleEntries.length > 0 && (
                      <span style={styles.sectionCount}>
                        {visibleEntries.length} {visibleEntries.length === 1 ? 'entry' : 'entries'}
                      </span>
                    )}
                  </div>

                  {visibleEntries.length === 0 ? (
                    inheritedCount > 0 ? (
                      <div style={styles.sectionNote}>
                        No direct access — everyone below inherits from the folder.
                      </div>
                    ) : (
                      <div style={styles.empty}>
                        <span style={styles.emptyIcon}><ShieldCheck size={20} /></span>
                        <span style={styles.emptyTitle}>No direct access yet</span>
                        <span style={styles.emptyHint}>
                          {canEdit
                            ? 'Only you and organization admins can access this. Add people or groups to share it.'
                            : 'Only the owner and organization admins can access this.'}
                        </span>
                      </div>
                    )
                  ) : (
                    visibleEntries.map((entry, idx) => {
                      const name = subjectName(entry)
                      return (
                        <div key={entry.id || `direct-${idx}`} className="access-row" style={styles.entryRow}>
                          <Avatar name={name} type={entry.subject_type} />
                          <div style={styles.entryIdentity}>
                            <span style={styles.entryName} title={name}>{name}</span>
                            <span style={styles.entryMeta}>{subjectSecondary(entry)}</span>
                          </div>
                          <ActionChips
                            actions={actions}
                            selected={entry.actions}
                            descriptions={ACTION_DESCRIPTIONS[resourceType] ?? {}}
                            disabled={!canEdit}
                            onChange={(action) => handleToggleAction(idx, action)}
                          />
                          {canEdit && (
                            <button
                              type="button"
                              className="access-icon-btn access-remove"
                              style={styles.removeBtn}
                              title="Remove"
                              aria-label={`Remove access for ${name}`}
                              onClick={() => handleRemoveEntry(idx)}
                            >
                              <X size={14} />
                            </button>
                          )}
                        </div>
                      )
                    })
                  )}
                </section>

                {/* Inherited access */}
                {parentFolderId && (
                  <section style={styles.section}>
                    <div style={styles.sectionHead}>
                      <span className="access-label">Inherited from {parentFolder?.folder?.name ?? 'parent folder'}</span>
                      <span style={styles.readOnlyBadge}>read only</span>
                    </div>
                    {inheritedCount === 0 ? (
                      <div style={styles.sectionNote}>Nothing is inherited from this folder yet.</div>
                    ) : (
                      inheritedEntries.map((entry, idx) => {
                        const name = subjectName(entry)
                        return (
                          <div key={`inherited-${entry.id || idx}`} className="access-row access-row-inherited" style={styles.entryRow}>
                            <Avatar name={name} type={entry.subject_type} />
                            <div style={styles.entryIdentity}>
                              <span style={styles.entryName} title={name}>{name}</span>
                              <span style={styles.entryMeta}>{subjectSecondary(entry)}</span>
                            </div>
                            <ActionChips
                              actions={actions}
                              selected={entry.actions}
                              descriptions={ACTION_DESCRIPTIONS[resourceType] ?? {}}
                              disabled
                              onChange={() => {}}
                            />
                          </div>
                        )
                      })
                    )}
                  </section>
                )}
              </>
            )}
          </div>

          {/* Draft footer */}
          {draft !== null && canEdit && (
            <div style={styles.footer}>
              {saveError && <div style={styles.footerError}>{saveError}</div>}
              <div style={styles.footerRow}>
                <span style={styles.dirty}>
                  <span style={styles.dirtyDot} aria-hidden="true" />
                  Unsaved changes
                </span>
                <div style={styles.footerActions}>
                  <button
                    type="button"
                    className="access-btn-secondary"
                    style={styles.discardBtn}
                    disabled={saveAcl.isPending}
                    onClick={() => setDraft(null)}
                  >
                    Discard
                  </button>
                  <button
                    type="button"
                    className="access-btn-primary"
                    style={styles.saveBtn}
                    disabled={saveAcl.isPending || aclLoading}
                    onClick={handleSave}
                  >
                    {saveAcl.isPending ? 'Saving…' : 'Save'}
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {confirmDiscard && (
        <ConfirmModal
          title="Discard changes?"
          message="You have unsaved permission changes. Closing now will lose them."
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          destructive
          onConfirm={() => { setConfirmDiscard(false); onClose() }}
          onCancel={() => setConfirmDiscard(false)}
        />
      )}

      <style>{css}</style>
    </>,
    document.body,
  )
}

// ─── Styles ──────────────────────────────────────────────────────────────────

const css = `
.access-overlay-enter { animation: access-fade-in 0.15s ease-out; }
.access-dialog-enter { animation: access-rise-in 0.18s cubic-bezier(0.16, 1, 0.3, 1); }
@keyframes access-fade-in { from { opacity: 0; } to { opacity: 1; } }
@keyframes access-rise-in { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: none; } }
@keyframes access-skel-pulse { 0%, 100% { opacity: 0.5; } 50% { opacity: 1; } }
.access-skel { animation: access-skel-pulse 1.2s ease-in-out infinite; }

.access-label {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.access-badge {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 2px 6px;
  flex-shrink: 0;
}

.access-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  flex-shrink: 0;
  font-weight: 700;
  letter-spacing: 0.02em;
  background: color-mix(in srgb, var(--text-primary) 7%, transparent);
  border: 1px solid var(--border);
  color: var(--text-secondary);
}
.access-avatar.is-user {
  background: color-mix(in srgb, var(--accent) 14%, transparent);
  border-color: color-mix(in srgb, var(--accent) 35%, transparent);
  color: var(--accent);
}
.access-avatar.is-pending {
  background: color-mix(in srgb, var(--text-primary) 4%, transparent);
  border-style: dashed;
  color: var(--text-muted);
}

.access-chips { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; }

.access-chip {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  line-height: 1.4;
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: color 0.15s ease, border-color 0.15s ease, background-color 0.15s ease, opacity 0.15s ease;
}
.access-chip:hover:not(:disabled) {
  color: var(--text-primary);
  border-color: color-mix(in srgb, var(--text-primary) 30%, transparent);
}
.access-chip[aria-pressed="true"] {
  background: color-mix(in srgb, var(--accent) 14%, transparent);
  border-color: var(--accent);
  color: var(--text-primary);
}
.access-chip.is-destructive[aria-pressed="true"] {
  background: var(--error-light);
  border-color: var(--error-border);
  color: var(--error-text);
}
.access-chip:disabled { cursor: default; opacity: 0.6; }
.access-chip[aria-pressed="true"]:disabled { opacity: 0.85; }

.access-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  border-radius: 4px;
  color: var(--text-muted);
  cursor: pointer;
  padding: 4px;
  flex-shrink: 0;
  transition: color 0.15s ease, background-color 0.15s ease;
}
.access-icon-btn:hover { color: var(--text-primary); background: color-mix(in srgb, var(--text-primary) 6%, transparent); }
.access-icon-btn.access-remove:hover { color: var(--error-full); background: var(--error-light); }

.access-btn-primary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  background: var(--button-primary-bg);
  color: var(--button-primary-text);
  border: none;
  border-radius: 6px;
  padding: 7px 16px;
  font-family: var(--font-sans);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s ease;
}
.access-btn-primary:hover:not(:disabled) { opacity: 0.9; }
.access-btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

.access-btn-secondary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  background: transparent;
  color: var(--text-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-family: var(--font-sans);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}
.access-btn-secondary:hover:not(:disabled) { background: color-mix(in srgb, var(--text-primary) 5%, transparent); color: var(--text-primary); }
.access-btn-secondary:disabled { opacity: 0.5; cursor: not-allowed; }

.access-composer-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 12px;
  border: 1px dashed var(--border);
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted);
  font-family: var(--font-sans);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  transition: border-color 0.15s ease, color 0.15s ease, background-color 0.15s ease;
}
.access-composer-trigger:hover {
  border-color: var(--accent);
  color: var(--text-primary);
  background: color-mix(in srgb, var(--accent) 5%, transparent);
}
.access-composer-plus { color: var(--text-muted); }
.access-composer-trigger:hover .access-composer-plus { color: var(--accent); }

.access-picker {
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-card);
  overflow: hidden;
}
.access-picker-search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-light);
  background: color-mix(in srgb, var(--text-primary) 3%, transparent);
}
.access-picker-search-icon { color: var(--text-muted); flex-shrink: 0; }
.access-picker-search input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-family: var(--font-sans);
  font-size: 13px;
}
.access-picker-search input::placeholder { color: var(--text-muted); }
.access-picker-list {
  list-style: none;
  margin: 0;
  padding: 4px 0;
  max-height: 216px;
  overflow-y: auto;
}
.access-picker-section { padding: 6px 12px 3px; }
.access-picker-option {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 6px 12px;
  cursor: pointer;
}
.access-picker-option.is-focused { background: var(--bg-secondary); }
.access-picker-option-name {
  font-size: 13px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.access-picker-option-meta {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-left: auto;
}
.access-picker-empty { padding: 10px 12px; font-size: 12px; color: var(--text-muted); }

.access-switch {
  appearance: none;
  -webkit-appearance: none;
  width: 32px;
  height: 18px;
  border-radius: 9px;
  background: var(--border);
  position: relative;
  cursor: pointer;
  flex-shrink: 0;
  margin: 0;
  transition: background-color 0.15s ease;
}
.access-switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.15s ease;
}
.access-switch:checked { background: var(--accent); }
.access-switch:checked::after { transform: translateX(14px); }
.access-switch:disabled { opacity: 0.5; cursor: default; }

@media (prefers-reduced-motion: reduce) {
  .access-overlay-enter,
  .access-dialog-enter,
  .access-skel { animation: none; }
  .access-chip, .access-switch, .access-switch::after { transition: none; }
}
`

const styles: Record<string, CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'var(--bg-overlay)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
    zIndex: 2000,
  },
  dialog: {
    width: 640,
    maxWidth: '100%',
    maxHeight: '80vh',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: 'var(--shadow-lg)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    outline: 'none',
  },
  header: {
    padding: '16px 24px 14px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  headerTop: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  title: {
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--text-primary)',
    margin: 0,
  },
  closeBtn: {
    margin: '-4px -6px -4px 0',
  },
  headerMeta: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginTop: 4,
    minWidth: 0,
  },
  resourceName: {
    fontSize: 13,
    color: 'var(--text-secondary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    minWidth: 0,
  },
  body: {
    flex: '1 1 auto',
    minHeight: 0,
    overflowX: 'hidden',
    overflowY: 'auto',
    padding: '16px 24px 20px',
    display: 'flex',
    flexDirection: 'column',
  },
  shareCard: {
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    padding: '12px 14px',
    borderRadius: 6,
    border: '1px solid var(--border)',
    background: 'color-mix(in srgb, var(--text-primary) 3%, transparent)',
    marginBottom: 18,
  },
  shareField: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  shareFieldHead: {
    display: 'flex',
    flexDirection: 'column',
    gap: 1,
    minWidth: 0,
  },
  fieldTitle: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  fieldHint: {
    fontSize: 11,
    color: 'var(--text-muted)',
  },
  chatLinkRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
  },
  chatLinkInput: {
    flex: 1,
    minWidth: 0,
    padding: '6px 8px',
    background: 'var(--bg-input)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
  },
  notebookSelect: {
    width: '100%',
    padding: '6px 8px',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
  },
  switchRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
    cursor: 'pointer',
  },
  readOnlyNote: {
    fontSize: 12,
    color: 'var(--text-muted)',
    padding: '8px 12px',
    background: 'color-mix(in srgb, var(--text-primary) 4%, transparent)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    marginBottom: 16,
  },
  errorText: {
    fontSize: 12,
    color: 'var(--error-text)',
    background: 'var(--error-light)',
    border: '1px solid var(--error-border)',
    borderRadius: 4,
    padding: '8px 12px',
  },
  skeleton: {
    display: 'flex',
    flexDirection: 'column',
    gap: 14,
    padding: '10px 0',
  },
  skeletonRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  },
  skeletonAvatar: {
    width: 30,
    height: 30,
    borderRadius: '50%',
    background: 'color-mix(in srgb, var(--text-primary) 10%, transparent)',
    flexShrink: 0,
  },
  skeletonBar: {
    height: 10,
    borderRadius: 4,
    background: 'color-mix(in srgb, var(--text-primary) 10%, transparent)',
  },
  composer: {
    marginBottom: 18,
  },
  composeRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    flexWrap: 'wrap',
    padding: '8px 10px',
    border: '1px solid var(--border)',
    borderRadius: 6,
    background: 'color-mix(in srgb, var(--text-primary) 3%, transparent)',
  },
  composeSubject: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '2px 4px 2px 2px',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 20,
    flexShrink: 0,
    maxWidth: 220,
  },
  composeSubjectName: {
    fontSize: 13,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  addBtn: {
    marginLeft: 'auto',
  },
  section: {
    display: 'flex',
    flexDirection: 'column',
  },
  sectionHead: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    margin: '4px 0 4px',
  },
  sectionCount: {
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    color: 'var(--text-muted)',
  },
  sectionNote: {
    fontSize: 12,
    color: 'var(--text-muted)',
    padding: '6px 0',
  },
  entryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    flexWrap: 'wrap',
    padding: '10px 0',
    borderBottom: '1px solid var(--border-light)',
  },
  entryIdentity: {
    display: 'flex',
    flexDirection: 'column',
    gap: 1,
    minWidth: 0,
    flex: '0 1 200px',
  },
  entryName: {
    fontSize: 13,
    fontWeight: 500,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  entryMeta: {
    fontSize: 11,
    color: 'var(--text-muted)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  removeBtn: {
    marginLeft: 'auto',
  },
  empty: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    textAlign: 'center',
    gap: 4,
    padding: '22px 16px 14px',
  },
  emptyIcon: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 40,
    height: 40,
    borderRadius: 4,
    border: '1px solid color-mix(in srgb, var(--accent) 35%, transparent)',
    background: 'color-mix(in srgb, var(--accent) 12%, transparent)',
    color: 'var(--accent)',
    marginBottom: 6,
  },
  emptyTitle: {
    fontSize: 14,
    fontWeight: 700,
    color: 'var(--text-primary)',
  },
  emptyHint: {
    fontSize: 12,
    color: 'var(--text-muted)',
    maxWidth: 360,
  },
  readOnlyBadge: {
    fontFamily: 'var(--font-mono)',
    fontSize: 9,
    fontWeight: 700,
    letterSpacing: '0.06em',
    textTransform: 'uppercase',
    padding: '2px 5px',
    borderRadius: 3,
    background: 'color-mix(in srgb, var(--text-primary) 6%, transparent)',
    border: '1px solid var(--border)',
    color: 'var(--text-muted)',
  },
  footer: {
    flexShrink: 0,
    borderTop: '1px solid var(--border)',
    padding: '12px 24px',
  },
  footerError: {
    fontSize: 12,
    color: 'var(--error-text)',
    marginBottom: 8,
  },
  footerRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  dirty: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 7,
    fontSize: 12,
    color: 'var(--text-secondary)',
  },
  dirtyDot: {
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--warning)',
    flexShrink: 0,
  },
  footerActions: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  discardBtn: {
    padding: '7px 14px',
    fontSize: 13,
  },
  saveBtn: {
    padding: '7px 18px',
  },
}
