export interface User {
  user_id: string
  org_id: string
  role: string
}

export interface Parameter {
  name: string
  type: 'string' | 'number' | 'boolean' | 'date' | 'daterange'
  default: string
}

export interface Notebook {
  id: string
  org_id: string
  title: string
  description: string
  connector_id?: string
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
  parameters?: Parameter[]
  owner_name?: string
  owner_email?: string
  can_edit?: boolean
  can_run?: boolean
}

export interface Cell {
  id: string
  notebook_id: string
  type: 'code' | 'text'
  language: string
  source: string
  outputs: Output[]
  position: number
  connector_id?: string
  created_at: string
  updated_at: string
  source_visible: boolean
  outputs_hidden?: boolean
  cell_collapsed: boolean
  slide_break?: boolean
  title?: string
  slug?: string
  parameters?: Parameter[]
  limit?: number | null
  metadata?: Record<string, unknown>
  agent_updated_at?: string
  duration_ms?: number
  metrics?: {
    connect_time_ms: number
    query_time_ms: number
    render_time_ms: number
    total_time_ms: number
  }
}

export interface CellVersion {
  id: string
  cell_id: string
  source: string
  created_at: string
  created_by: string
  user?: { id: string; name: string; email: string }
}

export interface CellDiffLine {
  type: 'add' | 'del' | 'ctx'
  line: string
  old_num?: number
  new_num?: number
}

export interface CellDiff {
  cell_id: string
  position: number
  title?: string
  old_source: string
  new_source: string
  diff_lines: CellDiffLine[]
  summary: string
}

export interface CellChange {
  cell_id: string
  position: number
  old_position?: number
  title?: string
}

export interface SnapshotChanges {
  title_changed: boolean
  old_title: string
  new_title: string
  cells_added: CellChange[]
  cells_deleted: CellChange[]
  cells_modified: CellChange[]
  positions_changed: CellChange[]
  cell_diffs?: CellDiff[]
}

export interface NotebookSnapshot {
  id: string
  notebook_id: string
  name: string
  title: string
  cell_sources: Record<string, string>
  created_by: string
  created_at: string
  auto: boolean
  user?: { id: string; name: string; email: string }
  changes?: SnapshotChanges
}

export interface Output {
  type: 'table' | 'error' | 'text' | 'chart'
  data: unknown
}

export interface ResultSet {
  columns: Column[]
  rows: unknown[][]
}

export interface Column {
  name: string
  type: string
}

export interface Connector {
  id: string
  name: string
  type: string
  is_default?: boolean
  folder_id?: string
  created_by?: string
  created_at: string
  updated_at?: string
  can_use?: boolean
  timeout_seconds?: number
  table_allowlist?: string[]
  table_denylist?: string[]
  config?: {
    host?: string
    port?: number
    database?: string
    user?: string
    ssl_mode?: string
    use_tls?: boolean
  }
}

export interface Dashboard {
  id: string
  org_id: string
  title: string
  settings: { refresh_interval?: number; auto_refresh_seconds?: number; grid_cols?: number }
  public_token?: string
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
  widgets?: Widget[]
  can_view_with_data?: boolean
  widgets_data?: Record<string, { cell_id: string; source: string; type: string; language: string; outputs: Output[]; metadata?: Record<string, unknown>; updated_at?: string }>
}

export interface Widget {
  id: string
  dashboard_id: string
  notebook_id: string
  cell_id: string
  type: 'chart' | 'table' | 'text' | 'metric' | 'date_picker' | 'date_range' | 'freetext' | 'number' | 'multi_select'
  layout: { row: number; col: number; width: number; height: number }
  config: Record<string, unknown>
  created_at: string
}

export interface Schedule {
  id: string
  notebook_id: string
  cron_expression: string
  enabled: boolean
  parameter_overrides: Record<string, string>
  next_run_at: string | null
  created_at: string
  updated_at: string
}

export interface AuditEntry {
  id: string
  org_id: string
  user_id: string
  user_email: string
  action: string
  resource_type: string
  resource_id: string
  resource_name: string
  resource_parent_name?: string
  metadata?: Record<string, unknown>
  created_at: string
}

export interface Member {
  user_id: string
  email: string
  name: string
  role: string
  joined_at: string
}

export interface Folder {
  id: string
  org_id: string
  parent_id?: string
  name: string
  is_home: boolean
  owner_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface FolderContents {
  folder?: Folder
  folders: (Folder & { can_edit?: boolean; can_delete?: boolean; can_share?: boolean })[]
  notebooks: (Notebook & { can_delete?: boolean; can_share?: boolean })[]
  connectors: Array<{
    id: string
    name: string
    type: string
    is_default?: boolean
    folder_id?: string
    created_by: string
    created_at: string
    updated_at?: string
    can_edit?: boolean
    can_delete?: boolean
    can_share?: boolean
  }>
  dashboards: (Dashboard & { can_edit?: boolean; can_delete?: boolean; can_share?: boolean })[]
}

export interface Group {
  id: string
  org_id: string
  name: string
  member_count: number
  created_at: string
}

export interface GroupMember {
  user_id: string
  email: string
  name: string
}

export interface ACLEntry {
  id: string
  org_id: string
  resource_type: string
  resource_id: string
  subject_type: 'user' | 'group' | 'org_role'
  subject_id: string
  actions: string[]
  created_at: string
}

export interface SSOProvider {
  id: string
  scope: string
  scopes: string[]
  groups_claim: string
  group_prefix: string
  auto_sync_groups: boolean
  get_user_info: boolean
  provisioning_mode?: 'create_org' | 'join_provider_org' | 'deny'
  default_role?: 'admin' | 'non-admin' | 'viewer'
  callback_url?: string
  org_id?: string
  name: string
  provider_type: string
  client_id: string
  discovery_url: string
  allowed_domains: string[]
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface PlatformSSOProvider extends SSOProvider {
  enabled_for_org: boolean
}

export interface SSOSettings {
  sso_password_login: boolean
}
