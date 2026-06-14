import type React from 'react'
import type { ResultSet } from '../types'

export type ChartType =
  | 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter'
  | 'pie' | 'donut'
  | 'timeline'
  | 'hierarchy_tree'

export interface ChartConfig {
  chartType: ChartType
  // Axis-based charts (bar, line, area, scatter)
  xAxis?: string
  yAxis?: string[]
  // Timeline
  timeColumn?: string
  endTimeColumn?: string
  labelColumn?: string
  groupBy?: string
  // Hierarchy tree
  idColumn?: string
  parentIdColumn?: string
  metricColumns?: string[]
  layout?: 'top-down' | 'left-to-right'
  nodeSpacing?: number
  // Shared
  title?: string
  showLegend?: boolean
  showGrid?: boolean
  showLabels?: boolean
  skipEmpty?: boolean
  seriesColors?: Record<string, string>
}

export interface ChartProps {
  data: ResultSet
  config: ChartConfig
  height?: number
}

export interface ConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
}

export interface ChartModule {
  Component: React.FC<ChartProps>
  ConfigPanel: React.FC<ConfigPanelProps>
  defaultConfig: Partial<ChartConfig>
  detectColumns: (columns: ResultSet['columns'], rows: ResultSet['rows']) => Partial<ChartConfig>
  requirements: { minColumns: number; needsTime?: boolean; needsParentChild?: boolean }
}
