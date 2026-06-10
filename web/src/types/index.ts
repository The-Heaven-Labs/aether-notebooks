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
  cell_collapsed: boolean
  slide_break?: boolean
  title?: string
  slug?: string
  parameters?: Parameter[]
  limit?: number | null
  metadata?: Record<string, unknown>
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
  created_at: string
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
  folders: Folder[]
  notebooks: Notebook[]
  connectors: Array<{
    id: string
    name: string
    type: string
    is_default?: boolean
    folder_id?: string
    created_by: string
    created_at: string
  }>
  dashboards: Dashboard[]
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
