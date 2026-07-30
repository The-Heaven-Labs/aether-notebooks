import type { ChartModule } from './types'
import { BarChartModule } from './BarChart'
import { LineChartModule } from './LineChart'
import { AreaChartModule } from './AreaChart'
import { ScatterChartModule } from './ScatterChart'
import { PieChartModule } from './PieChart'
import { TimelineModule } from './TimelineChart'
import { HierarchyTreeModule } from './HierarchyTreeChart'
import { BigNumberModule } from './BigNumber'
import { MapChartModule } from './MapChart'
import { SankeyChartModule } from './SankeyChart'
import { FunnelChartModule } from './FunnelChart'
import { HeatmapChartModule } from './HeatmapChart'
import { HistogramChartModule } from './HistogramChart'

export const CHART_MODULES: Record<string, ChartModule> = {
  bar: BarChartModule,
  line: LineChartModule,
  area: AreaChartModule,
  scatter: ScatterChartModule,
  pie: PieChartModule,
  donut: PieChartModule,
  timeline: TimelineModule,
  hierarchy_tree: HierarchyTreeModule,
  big_number: BigNumberModule,
  map: MapChartModule,
  sankey: SankeyChartModule,
  funnel: FunnelChartModule,
  heatmap: HeatmapChartModule,
  histogram: HistogramChartModule,
}
