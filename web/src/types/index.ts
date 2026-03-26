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
  title?: string
  description?: string
  slug?: string
}

export interface CellVersion {
  id: string
  cell_id: string
  source: string
  created_at: string
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
  created_at: string
  config?: {
    host?: string
    port?: number
    database?: string
    user?: string
    ssl_mode?: string
  }
}

export interface Dashboard {
  id: string
  org_id: string
  title: string
  settings: { refresh_interval?: number }
  public_token?: string
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
  type: 'chart' | 'table' | 'text' | 'metric'
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
  created_at: string
}

export interface Member {
  user_id: string
  email: string
  name: string
  role: string
  joined_at: string
}
