import type { ChartConfig } from './types'

// Normalize chart config from backend (handles legacy key names from earlier create_chart calls)
export function normalizeChartConfig(raw: unknown): ChartConfig | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const obj = raw as Record<string, unknown>
  const rawType = (obj.chartType ?? obj.type) as string | undefined
  let barMode: ChartConfig['barMode']
  if (rawType === 'stacked_bar') { barMode = 'stacked' }
  else if (rawType === 'horizontal_bar') { barMode = 'horizontal' }
  let areaMode: ChartConfig['areaMode']
  if (rawType === 'stacked_area') { areaMode = 'stacked' }
  const migratedChartType = rawType === 'stacked_bar' || rawType === 'horizontal_bar' ? 'bar'
    : rawType === 'stacked_area' ? 'area'
    : rawType
  // Build result and strip undefined values so spread in ChartView doesn't
  // overwrite module defaultConfig fields with undefined.
  const result: Record<string, unknown> = {
    chartType: migratedChartType,
    xAxis: (obj.xAxis ?? obj.x_column) as string | undefined,
    yAxis: (obj.yAxis ?? obj.y_columns) as string[] | undefined,
    title: obj.title as string | undefined,
    showLegend: obj.showLegend as boolean | undefined,
    showGrid: obj.showGrid as boolean | undefined,
    showLabels: obj.showLabels as boolean | undefined,
    skipEmpty: obj.skipEmpty as boolean | undefined,
    seriesColors: obj.seriesColors as Record<string, string> | undefined,
    timeColumn: obj.timeColumn as string | undefined,
    endTimeColumn: obj.endTimeColumn as string | undefined,
    labelColumn: obj.labelColumn as string | undefined,
    groupBy: obj.groupBy as string | undefined,
    maxLabelLength: obj.maxLabelLength as number | undefined,
    showConnectors: obj.showConnectors as boolean | undefined,
    showTimeDeltas: obj.showTimeDeltas as boolean | undefined,
    hideLabelOverlap: obj.hideLabelOverlap as boolean | undefined,
    idColumn: obj.idColumn as string | undefined,
    parentIdColumn: obj.parentIdColumn as string | undefined,
    metricColumns: obj.metricColumns as string[] | undefined,
    layout: obj.layout as 'top-down' | 'left-to-right' | undefined,
    valueColumn: obj.valueColumn as string | undefined,
    label: obj.label as string | undefined,
    prefix: obj.prefix as string | undefined,
    suffix: obj.suffix as string | undefined,
    decimalPlaces: obj.decimalPlaces as number | undefined,
    barWidth: obj.barWidth as string | undefined,
    barCategoryGap: obj.barCategoryGap as string | undefined,
    barMode: (barMode ?? obj.barMode) as 'grouped' | 'stacked' | 'horizontal' | undefined,
    smooth: obj.smooth as boolean | undefined,
    connectNulls: obj.connectNulls as boolean | undefined,
    areaMode: (areaMode ?? obj.areaMode) as 'area' | 'stacked' | undefined,
    roseType: obj.roseType as 'radius' | 'area' | undefined,
    startAngle: obj.startAngle as number | undefined,
    padAngle: obj.padAngle as number | undefined,
    labelPosition: obj.labelPosition as 'outside' | 'inside' | undefined,
    minShowLabelAngle: obj.minShowLabelAngle as number | undefined,
    nodeAlign: obj.nodeAlign as 'justify' | 'left' | 'right' | undefined,
    nodeWidth: obj.nodeWidth as number | undefined,
    nodeGap: obj.nodeGap as number | undefined,
    categoryColumn: obj.categoryColumn as string | undefined,
    funnelSort: obj.funnelSort as 'ascending' | 'descending' | 'none' | undefined,
    yAxisColumn: obj.yAxisColumn as string | undefined,
    binCount: obj.binCount as number | undefined,
    colorColumn: obj.colorColumn as string | undefined,
    sizeColumn: obj.sizeColumn as string | undefined,
    mapMode: obj.mapMode as 'points' | 'choropleth' | undefined,
    dataZoom: obj.dataZoom as boolean | undefined,
    logScale: obj.logScale as boolean | undefined,
    markLines: obj.markLines as ChartConfig['markLines'],
  }
  return Object.fromEntries(Object.entries(result).filter(([, v]) => v != null)) as unknown as ChartConfig
}
