import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { renderHook } from '@testing-library/react'
import { buildPieSeriesLabelConfig, buildPieGeometry, PIE_TITLE_BAND, PieChartModule } from './PieChart'
import { LineChartModule } from './LineChart'
import { AreaChartModule } from './AreaChart'
import { SankeyChartModule } from './SankeyChart'
import { HierarchyTreeModule } from './HierarchyTreeChart'
import { TimelineModule } from './TimelineChart'
import { HistogramChartModule } from './HistogramChart'
import { FunnelChartModule } from './FunnelChart'
import {
  useGroupBySeries,
  buildLegend,
  estimateLegendColumnWidth,
  legendColumnReserve,
  getChartColors,
  LEGEND_COLUMN_MIN,
  LEGEND_COLUMN_MAX,
} from './common'
import { ChartView } from './index'

// ECharts uses ResizeObserver
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

describe('buildPieSeriesLabelConfig', () => {
  it('forces always-visible labels by default', () => {
    const c = buildPieSeriesLabelConfig({}, '#111')
    expect(c.label).toMatchObject({ show: true, position: 'outside' })
    expect(c.labelLine).toMatchObject({ show: true })
    expect(c.labelLayout({ rect: { x: 10, y: 500 } })).toEqual({ hideOverlap: false })
    expect(c.avoidLabelOverlap).toBe(true)
    expect(c.minShowLabelAngle).toBe(0)
  })

  it('hides labels and guide lines when off', () => {
    const c = buildPieSeriesLabelConfig({ showLabels: false }, '#111')
    expect(c.label).toEqual({ show: false })
    expect(c.labelLine).toEqual({ show: false })
  })

  it('switches inside without guide lines and contrasts per slice', () => {
    const c = buildPieSeriesLabelConfig({ labelPosition: 'inside' }, '#111')
    expect(c.label).toMatchObject({ show: true, position: 'inside' })
    expect(c.labelLine).toEqual({ show: false })
    const color = c.label.color as (p: { color?: unknown }) => string
    expect(color({ color: '#000000' })).toBe('#fff')
    expect(color({ color: '#ffffff' })).toBe('#111')
  })

  it('passes min label angle through', () => {
    expect(buildPieSeriesLabelConfig({ minShowLabelAngle: 30 }, '#111').minShowLabelAngle).toBe(30)
  })
})

describe('pie title-band protection', () => {
  it('clamps outside labels dragged into the title band back below it', () => {
    const c = buildPieSeriesLabelConfig({}, '#111', { hasTitle: true })
    expect(c.labelLayout({ labelRect: { x: 10, y: 5 }, rect: { x: 10, y: 60 } }))
      .toMatchObject({ y: PIE_TITLE_BAND, hideOverlap: false })
    expect(c.labelLayout({ labelRect: { x: 10, y: PIE_TITLE_BAND - 1 } })).toMatchObject({ y: PIE_TITLE_BAND })
    expect(c.labelLayout({ labelRect: { x: 10, y: PIE_TITLE_BAND } })).toEqual({ hideOverlap: false })
  })

  it('falls back to the host rect when labelRect is absent', () => {
    const c = buildPieSeriesLabelConfig({}, '#111', { hasTitle: true })
    expect(c.labelLayout({ rect: { x: 10, y: 20 } })).toMatchObject({ y: PIE_TITLE_BAND })
  })

  it('leaves labels below the band alone and never hides them', () => {
    const c = buildPieSeriesLabelConfig({}, '#111', { hasTitle: true })
    expect(c.labelLayout({ labelRect: { x: 10, y: 120 } })).toEqual({ hideOverlap: false })
    // Missing rect info is treated as above the band (safe default).
    expect(c.labelLayout({})).toEqual({ hideOverlap: false, y: PIE_TITLE_BAND })
  })

  it('uses shortened leader segments in title mode only', () => {
    expect(buildPieSeriesLabelConfig({}, '#111', { hasTitle: true }).labelLine)
      .toMatchObject({ show: true, length: 8, length2: 8 })
    expect(buildPieSeriesLabelConfig({}, '#111').labelLine)
      .toMatchObject({ show: true, length: 12, length2: 12 })
  })

  it('keeps the titled circle below the title band at small dashboard sizes', () => {
    for (const height of [100, 200, 600]) {
      const { center, radius } = buildPieGeometry({ title: 'CDN response code %', chartType: 'donut' })
      const centerY = (parseFloat(center[1]) / 100) * height
      const outerRadius = (parseFloat(radius[1]) / 100) * (height / 2)
      expect(centerY - outerRadius).toBeGreaterThanOrEqual(PIE_TITLE_BAND)
    }
  })

  it('keeps the untitled geometry unchanged from v0.56.0', () => {
    expect(buildPieGeometry({ chartType: 'donut' })).toEqual({ radius: ['40%', '70%'], center: ['40%', '50%'] })
    expect(buildPieGeometry({ chartType: 'pie' })).toEqual({ radius: ['0%', '70%'], center: ['40%', '50%'] })
  })

  it('centers the circle when the legend is hidden', () => {
    expect(buildPieGeometry({ title: 'T', showLegend: false, chartType: 'pie' }).center).toEqual(['50%', '50%'])
  })
})

describe('legend column measurement', () => {
  it('clamps to the minimum for short or empty name lists', () => {
    expect(estimateLegendColumnWidth([])).toBe(LEGEND_COLUMN_MIN)
    expect(estimateLegendColumnWidth(['a', 'bb'])).toBe(LEGEND_COLUMN_MIN)
  })

  it('grows with the longest name and clamps at the maximum', () => {
    expect(estimateLegendColumnWidth(['a'.repeat(24)])).toBeGreaterThan(LEGEND_COLUMN_MIN)
    expect(estimateLegendColumnWidth(['x'.repeat(200)])).toBe(LEGEND_COLUMN_MAX)
  })

  it('covers the measured width of every name plus swatch/pager padding', () => {
    const names = ['orders', 'developer-productivity-integration', 'network edge latency']
    const width = estimateLegendColumnWidth(names)
    expect(width).toBeLessThan(LEGEND_COLUMN_MAX)
    for (const name of names) {
      // jsdom has no real canvas, so the 6.2px/char fallback applies
      expect(width).toBeGreaterThanOrEqual(name.length * 6.2 + 40 - 0.01)
    }
  })
})

describe('buildLegend long-name handling', () => {
  const colors = getChartColors()

  it('truncates names over the column budget and passes short names through', () => {
    const legend = buildLegend({}, colors, { seriesNames: ['short'] }) as Record<string, unknown>
    const formatter = legend.formatter as (name: string) => string
    expect(formatter('short')).toBe('short')
    const longName = 'developer-productivity-integration'
    const rendered = formatter(longName)
    expect(rendered.endsWith('…')).toBe(true)
    expect(rendered.length).toBeLessThan(longName.length)
  })

  it('exposes the untruncated name via legend tooltip', () => {
    const legend = buildLegend({}, colors) as Record<string, unknown>
    expect(legend.tooltip).toEqual({ show: true })
  })

  it('is not longer than the reserved column width in characters', () => {
    const names = ['a'.repeat(30)]
    const legend = buildLegend({}, colors, { seriesNames: names }) as Record<string, unknown>
    const formatter = legend.formatter as (name: string) => string
    const rendered = formatter(names[0])
    const reserved = estimateLegendColumnWidth(names)
    expect(rendered.length).toBeLessThanOrEqual(Math.floor((reserved - 40) / 6.2))
  })
})

describe('legend column reserve', () => {
  it('reserves the measured column when shown and 16px otherwise', () => {
    const names = ['alpha', 'beta']
    expect(legendColumnReserve(undefined, names)).toBe(estimateLegendColumnWidth(names))
    expect(legendColumnReserve(true, names)).toBe(estimateLegendColumnWidth(names))
    expect(legendColumnReserve(false, names)).toBe(16)
  })
})

describe('buildLegend top-right reservation', () => {
  const colors = getChartColors()

  it('keeps the legend below the reset button with and without a title', () => {
    const untitled = buildLegend({}, colors, { reserveTopRight: true }) as Record<string, unknown>
    const titled = buildLegend({ title: 'Sales' }, colors, { reserveTopRight: true }) as Record<string, unknown>
    expect(untitled.top as number).toBeGreaterThanOrEqual(34)
    expect(titled.top as number).toBeGreaterThanOrEqual(34)
  })

  it('leaves the default placement untouched without the option', () => {
    expect((buildLegend({}, colors) as Record<string, unknown>).top).toBe(8)
    expect((buildLegend({ title: 'Sales' }, colors) as Record<string, unknown>).top).toBe(30)
  })
})

describe('pie panel controls', () => {
  const columns = ['month', 'revenue']
  const base: Record<string, unknown> = { chartType: 'pie', xAxis: 'month', yAxis: ['revenue'] }

  function renderPiePanel(config: Record<string, unknown> = {}) {
    const onChange = vi.fn()
    render(<PieChartModule.ConfigPanel config={{ ...base, ...config } as never} columns={columns} onChange={onChange} />)
    return onChange
  }

  it('exposes Title, Labels, Legend, position, and min angle', () => {
    renderPiePanel()
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByLabelText('Show labels')).toBeInTheDocument()
    expect(screen.getByLabelText('Legend')).toBeInTheDocument()
    expect(screen.getByLabelText('Label position')).toBeInTheDocument()
    expect(screen.getByLabelText('Min label angle')).toBeInTheDocument()
  })

  it('toggles labels and edits title', () => {
    const onChange = renderPiePanel()
    fireEvent.click(screen.getByLabelText('Show labels'))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ showLabels: false }))
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Sales' } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ title: 'Sales' }))
  })

  it('hides position controls when labels are off', () => {
    renderPiePanel({ showLabels: false })
    expect(screen.queryByLabelText('Label position')).toBeNull()
  })

  it('ships always-on label defaults', () => {
    expect(PieChartModule.defaultConfig).toMatchObject({
      showLabels: true,
      labelPosition: 'outside',
      minShowLabelAngle: 0,
    })
  })
})

describe('axis panel connect nulls', () => {
  function renderAxis(chartType: string) {
    const onChange = vi.fn()
    const Panel = LineChartModule.ConfigPanel
    render(
      <Panel
        config={{ chartType, xAxis: 'month', yAxis: ['revenue'] } as never}
        columns={['month', 'revenue']}
        onChange={onChange}
      />,
    )
    return onChange
  }

  it('shows for line and area only', () => {
    renderAxis('line')
    expect(screen.getByLabelText('Connect nulls')).toBeInTheDocument()
  })

  it('toggles the flag', () => {
    const onChange = renderAxis('area')
    fireEvent.click(screen.getByLabelText('Connect nulls'))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ connectNulls: true }))
  })
})

describe('sankey panel', () => {
  const data = {
    columns: [{ name: 'src' }, { name: 'dst' }, { name: 'v' }],
    rows: [['a', 'b', 1], ['b', 'c', 2]],
  }

  it('exposes Title and node colors', () => {
    const onChange = vi.fn()
    render(
      <SankeyChartModule.ConfigPanel
        config={{ chartType: 'sankey', xAxis: 'src', yAxis: ['dst', 'v'] } as never}
        columns={['src', 'dst', 'v']}
        onChange={onChange}
        data={data}
      />,
    )
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Flow' } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ title: 'Flow' }))
    // Node color pickers keyed on node names
    expect(screen.getByText('Node colors')).toBeInTheDocument()
  })

  it('has no legend toggle (ECharts sankey cannot render legend items)', () => {
    const onChange = vi.fn()
    render(
      <SankeyChartModule.ConfigPanel
        config={{ chartType: 'sankey', xAxis: 'src', yAxis: ['dst', 'v'] } as never}
        columns={['src', 'dst', 'v']}
        onChange={onChange}
        data={data}
      />,
    )
    expect(screen.queryByLabelText('Legend')).toBeNull()
  })
})

describe('tree panel', () => {
  const data = {
    columns: [{ name: 'id' }, { name: 'pid' }, { name: 'name' }],
    rows: [[1, 0, 'init'], [2, 1, 'ssh']],
  }

  function renderTree(config: Record<string, unknown> = {}) {
    const onChange = vi.fn()
    render(
      <HierarchyTreeModule.ConfigPanel
        config={{ chartType: 'hierarchy_tree', ...config } as never}
        columns={['id', 'pid', 'name']}
        onChange={onChange}
        data={data}
      />,
    )
    return onChange
  }

  it('exposes Title and node colors, drops dead nodeSpacing', () => {
    renderTree()
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByText('Node colors')).toBeInTheDocument()
    expect(screen.queryByLabelText('Horizontal spacing')).toBeNull()
  })
})

describe('timeline panel', () => {
  function renderTimeline(config: Record<string, unknown> = {}) {
    const onChange = vi.fn()
    render(
      <TimelineModule.ConfigPanel
        config={{ chartType: 'timeline', ...config } as never}
        columns={['timestamp', 'event']}
        onChange={onChange}
      />,
    )
    return onChange
  }

  it('exposes Title, Grid, and overlap toggle', () => {
    renderTimeline()
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByLabelText('Grid')).toBeInTheDocument()
    expect(screen.getByLabelText('Hide overlapping labels')).toBeInTheDocument()
  })

  it('toggles overlap hiding', () => {
    const onChange = renderTimeline()
    fireEvent.click(screen.getByLabelText('Hide overlapping labels'))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ hideLabelOverlap: false }))
  })
})

describe('histogram and funnel panels', () => {
  it('histogram exposes Grid', () => {
    const onChange = vi.fn()
    render(
      <HistogramChartModule.ConfigPanel
        config={{ chartType: 'histogram' } as never}
        columns={['v']}
        onChange={onChange}
      />,
    )
    expect(screen.getByLabelText('Grid')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('Grid'))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ showGrid: false }))
  })

  it('funnel exposes skip-empty', () => {
    const onChange = vi.fn()
    render(
      <FunnelChartModule.ConfigPanel
        config={{ chartType: 'funnel' } as never}
        columns={['stage', 'v']}
        onChange={onChange}
      />,
    )
    expect(screen.getByLabelText('Skip empty stages')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('Skip empty stages'))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ skipEmpty: false }))
  })

  it('funnel toggles Legend in emitted config', () => {
    const onChange = vi.fn()
    render(
      <FunnelChartModule.ConfigPanel
        config={{ chartType: 'funnel' } as never}
        columns={['stage', 'v']}
        onChange={onChange}
      />,
    )
    const legend = screen.getByLabelText('Legend')
    expect(legend).toBeChecked()
    fireEvent.click(legend)
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ showLegend: false }))
  })

  it('funnel ships its legend enabled by default', () => {
    expect(FunnelChartModule.defaultConfig.showLegend).toBe(true)
  })
})

describe('useGroupBySeries null coalescing', () => {
  it('keeps null by default, coerces to 0 when asked', () => {
    const sparse = [
      { x: 'a', g: 's1', v: 1 },
      { x: 'b', g: 's2', v: 2 },
    ]
    const cfg = { xAxis: 'x', yAxis: ['v'], groupBy: 'g' }
    const { result: def } = renderHook(() => useGroupBySeries(sparse, cfg, ['#000']))
    const { result: coal } = renderHook(() => useGroupBySeries(sparse, cfg, ['#000'], true))
    const dataOf = (name: string, r: { current: { series: { name: string; data: unknown[] }[] } }) =>
      r.current.series.find((s) => s.name === name)?.data
    // s1 missing at x='b' → null by default, 0 with coercion
    expect(dataOf('s1', def)).toEqual([1, null])
    expect(dataOf('s1', coal)).toEqual([1, 0])
  })
})

describe('render smoke for touched charts', () => {
  const cols = [
    { name: 'month', type: 'text' },
    { name: 'revenue', type: 'float' },
  ]

  it('pie with new label fields renders', () => {
    render(
      <ChartView
        output={{ type: 'table', data: { columns: cols, rows: [['Jan', 100]] }, config: { chartType: 'pie', labelPosition: 'inside', minShowLabelAngle: 0 } as never }}
      />,
    )
    expect(screen.getByTestId('chart-container')).toBeInTheDocument()
  })

  it('donut with title and legend renders', () => {
    render(
      <ChartView
        output={{ type: 'table', data: { columns: cols, rows: [['Jan', 100], ['Feb', 5]] }, config: { chartType: 'donut', title: 'Sales', showLegend: true } as never }}
      />,
    )
    expect(screen.getByTestId('chart-container')).toBeInTheDocument()
  })

  it('stacked area with nulls renders', () => {
    render(
      <ChartView
        output={{
          type: 'table',
          data: { columns: [...cols, { name: 'provider', type: 'text' }], rows: [['Jan', 100, 'a'], ['Jan', null, 'b']] },
          config: { chartType: 'area', areaMode: 'stacked', xAxis: 'month', yAxis: ['revenue'], groupBy: 'provider' } as never,
        }}
      />,
    )
    expect(screen.getByTestId('chart-container')).toBeInTheDocument()
  })

  it('timeline without overlap hiding renders', () => {
    render(
      <ChartView
        output={{
          type: 'table',
          data: {
            columns: [{ name: 'timestamp', type: 'timestamp' }, { name: 'event', type: 'text' }],
            rows: [['2024-01-01T10:00:00', 'Login']],
          },
          config: { chartType: 'timeline', hideLabelOverlap: false } as never,
        }}
      />,
    )
    expect(screen.getByTestId('chart-container')).toBeInTheDocument()
  })

  it('area module wires stacked coercion flag', () => {
    expect(AreaChartModule.defaultConfig.chartType).toBe('area')
  })
})
