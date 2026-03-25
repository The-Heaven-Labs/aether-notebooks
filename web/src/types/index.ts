export interface User {
  user_id: string
  org_id: string
  role: string
}

export interface Parameter {
  name: string
  type: 'string' | 'number' | 'date' | 'daterange'
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
}

export interface Dashboard {
  id: string
  org_id: string
  title: string
  settings: DashboardSettings
  public_token?: string
  created_at: string
  updated_at: string
  widgets: Widget[]
}

export interface DashboardSettings {
  auto_refresh_seconds?: number
}

export interface Widget {
  id: string
  dashboard_id: string
  notebook_id: string
  cell_id: string
  type: 'chart' | 'table' | 'text' | 'metric'
  layout: WidgetLayout
  config: Record<string, unknown>
}

export interface WidgetLayout {
  row: number
  col: number
  width: number
  height: number
}

export interface Schedule {
  id: string
  notebook_id: string
  cron_expression: string
  enabled: boolean
  next_run_at?: string
  last_run_at?: string
}
