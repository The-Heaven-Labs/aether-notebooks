import type React from 'react'
import type { ResultSet } from '../types'

export type ChartType =
  | 'bar' | 'line' | 'area' | 'scatter'
  | 'pie' | 'donut'
  | 'timeline'
  | 'hierarchy_tree'
  | 'big_number'
  | 'map'
  | 'sankey'
  | 'funnel'
  | 'heatmap'
  | 'histogram'

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
  // Big number
  valueColumn?: string
  label?: string
  prefix?: string
  suffix?: string
  decimalPlaces?: number
  // Shared
  title?: string
  showLegend?: boolean
  showGrid?: boolean
  showLabels?: boolean
  skipEmpty?: boolean
  seriesColors?: Record<string, string>
  dataZoom?: boolean
  smooth?: boolean
  // Bar
  barWidth?: string
  barCategoryGap?: string
  barMode?: 'grouped' | 'stacked' | 'horizontal'
  // Line/Area
  connectNulls?: boolean
  areaMode?: 'area' | 'stacked'
  // Sankey
  nodeAlign?: 'justify' | 'left' | 'right'
  nodeWidth?: number
  nodeGap?: number
  // Funnel
  categoryColumn?: string
  funnelSort?: 'ascending' | 'descending' | 'none'
  // Heatmap
  yAxisColumn?: string
  // Histogram
  binCount?: number
  // Scatter
  colorColumn?: string
  sizeColumn?: string
  // Pie/Donut
  roseType?: 'radius' | 'area'
  startAngle?: number
  padAngle?: number
  // Timeline-specific
  maxLabelLength?: number
  showConnectors?: boolean
  showTimeDeltas?: boolean
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
  data?: { columns: { name: string; type?: string }[]; rows: unknown[][] }
  groupValues?: string[]
}

/**
 * Chart positioning convention for ECharts-based charts:
 *
 * All charts must follow this layout pattern to prevent title/legend/grid overlap:
 *
 *   title: config.title ? {
 *     text: config.title, left: 'center', top: 8,
 *     textStyle: { fontSize: 14, color: colors.text }
 *   } : undefined,
 *
 *   legend: config.showLegend !== false ? {
 *     top: config.title ? 32 : 0,
 *     textStyle: { fontSize: 11, color: colors.textMuted }
 *   } : undefined,
 *
 *   grid: {
 *     top: config.title ? 56 : config.showLegend !== false ? 30 : 8,
 *     right: 16, bottom: config.dataZoom ? 32 : 8, left: 16,
 *     containLabel: true,
 *   }
 *
 * For chart types without a grid (pie, sankey, map geo), adjust the
 * series center/top position to sit below the title+legend stack:
 *
 *   pie center: [x, config.title ? '58%' : '50%']
 *   tree series top: config.title ? '16%' : '8%'
 */
export interface ChartModule {
  Component: React.FC<ChartProps>
  ConfigPanel: React.FC<ConfigPanelProps>
  defaultConfig: Partial<ChartConfig>
  detectColumns: (columns: ResultSet['columns'], rows: ResultSet['rows']) => Partial<ChartConfig>
  requirements: { minColumns: number; needsTime?: boolean; needsParentChild?: boolean }
}
