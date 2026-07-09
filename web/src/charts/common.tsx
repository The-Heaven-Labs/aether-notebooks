import { memo, useRef, useEffect, useMemo, useState } from 'react'
import * as echarts from 'echarts'
import { useTheme } from '../contexts/ThemeContext'

/**
 * Recursive helper to compare two ECharts options by JSON (used for memoization).
 */
function optionsEqual(a: any, b: any): boolean {
  try { return JSON.stringify(a) === JSON.stringify(b) } catch { return false }
}

/**
 * Returns chart colors based on the current theme.
 */
export function getChartColors(): string[] {
  if (typeof document === 'undefined') return defaultColors
  const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg-card').trim()
  const isDark = bg.startsWith('#') && parseInt(bg.slice(1), 16) < 0x808080
  return isDark ? darkColors : defaultColors
}

const defaultColors = [
  '#5470c6', '#91cc75', '#fac858', '#ee6666', '#73c0de',
  '#3ba272', '#fc8452', '#9a60b4', '#ea7ccc',
]

const darkColors = [
  '#7ec7f2', '#9fe080', '#fadc6e', '#f58b8b', '#8ed3e9',
  '#6bc48a', '#fda574', '#b88ad4', '#f09ad4',
]

type TextCommon = {
  color: string
}

type AxisCommon = {
  axisLine?: { lineStyle?: { color: string } }
  axisLabel?: { color: string }
  splitLine?: { lineStyle?: { color: string } }
  nameTextStyle?: { color: string }
}

/**
 * Applies theme-aware styling to an ECharts option.
 */
export function useThemedOption<T extends echarts.EChartsCoreOption>(option: T | null): T | null {
  const theme = useTheme()

  return useMemo(() => {
    if (!option) return null
    if (typeof document === 'undefined') return option

    const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg-card').trim()
    const text = getComputedStyle(document.documentElement).getPropertyValue('--text-primary').trim()
    const muted = getComputedStyle(document.documentElement).getPropertyValue('--text-muted').trim()
    const border = getComputedStyle(document.documentElement).getPropertyValue('--border').trim()
    const gridBg = getComputedStyle(document.documentElement).getPropertyValue('--bg-secondary').trim()
    const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim()

    const textStyle: TextCommon = { color: text }
    const axisCommon: AxisCommon = {
      axisLine: { lineStyle: { color: border } },
      axisLabel: { color: muted },
      splitLine: { lineStyle: { color: border } },
      nameTextStyle: { color: text },
    }

    return {
      ...option,
      backgroundColor: 'transparent',
      title: merge(option.title, { textStyle }),
      legend: merge(option.legend, {
        textStyle,
        pageTextStyle: textStyle,
        inactiveColor: muted,
      }),
      tooltip: merge(option.tooltip, {
        backgroundColor: bg,
        borderColor: border,
        textStyle: { color: text },
      }),
      xAxis: arrayMerge(option.xAxis, axisCommon),
      yAxis: arrayMerge(option.yAxis, axisCommon),
      series: option.series,
      visualMap: merge(option.visualMap, {
        textStyle,
        inRange: option.visualMap?.inRange,
        calculable: true,
      }),
    }
  }, [option, theme])
}

function merge<T extends object | undefined>(target: T, source: T): T {
  if (!target) return source
  if (Array.isArray(target)) return target.map((item, i) => ({ ...item, ...source })) as T
  if (typeof target === 'object') return { ...target, ...source }
  return target
}

function arrayMerge<T>(target: T | T[] | undefined, source: T): T | T[] | undefined {
  if (!target) return source
  if (Array.isArray(target)) return target.map((item) => ({ ...item, ...source })) as T[]
  if (typeof target === 'object') return { ...target, ...source }
  return target
}

interface EChartsContainerProps {
  option: echarts.EChartsCoreOption
  height?: number
  onChartReady?: (chart: echarts.ECharts) => void
  notMerge?: boolean
  showReset?: boolean
}

// Walk tree nodes to collect collapsed names or apply state
export function walkTree(nodes: any[], fn: (node: any) => void) {
  for (const node of nodes) {
    fn(node)
    if (node.children) walkTree(node.children, fn)
  }
}

function applyCollapsedToTree(data: any, collapsed: Set<string>): any {
  if (!data) return data
  if (Array.isArray(data)) {
    return data.map(n => applyCollapsedToTree(n, collapsed))
  }
  if (data.children) {
    data = { ...data, children: data.children.map((n: any) => applyCollapsedToTree(n, collapsed)) }
  }
  if (collapsed.has(data.name)) {
    data = { ...data, collapsed: true }
  }
  return data
}

export const EChartsContainer = memo(function EChartsContainer({ option, onChartReady, notMerge = true, showReset = false }: EChartsContainerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const wrapperRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)
  const [height, setHeight] = useState(200)

  const handleReset = () => {
    chartRef.current?.dispatchAction({ type: 'restore' })
  }

  useEffect(() => {
    const el = containerRef.current?.parentElement
    if (!el) return
    setHeight(el.clientHeight || 200)
    const ro = new ResizeObserver(([entry]) => {
      setHeight(entry.contentRect.height)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  useEffect(() => {
    if (!containerRef.current || height <= 0) return
    if (!chartRef.current) {
      chartRef.current = echarts.init(containerRef.current, undefined, {
        renderer: 'canvas',
      })
      onChartReady?.(chartRef.current)
    }
    const themedOption = {
      ...option,
      backgroundColor: 'transparent',
    }

    let finalOption: any = themedOption
    if (chartRef.current) {
      try {
        const currentOpt = chartRef.current.getOption() as any
        const curSeries = currentOpt?.series?.[0]
        const newSeries = (themedOption as any)?.series?.[0]
        if (curSeries?.type === 'tree' && newSeries?.type === 'tree' && curSeries.data && newSeries.data) {
          const collapsed = new Set<string>()
          const curData = Array.isArray(curSeries.data) ? curSeries.data : [curSeries.data]
          walkTree(curData, n => { if (n.collapsed) collapsed.add(n.name) })
          if (collapsed.size > 0) {
            finalOption = {
              ...themedOption,
              series: [{ ...newSeries, data: applyCollapsedToTree(newSeries.data, collapsed) }],
            }
          }
        }
      } catch { /* ignore */ }
    }

    chartRef.current.setOption(finalOption, { notMerge })

    const ro = new ResizeObserver(() => {
      chartRef.current?.resize()
    })
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [option, height])

  useEffect(() => {
    return () => {
      chartRef.current?.dispose()
      chartRef.current = null
    }
  }, [])

  return (
    <div ref={wrapperRef} style={{ position: 'relative', height: '100%' }}>
      {height > 0 && (
        <div data-testid="chart-container" ref={containerRef} style={{ height, width: '100%' }} />
      )}
      {showReset && (
        <button
          onClick={handleReset}
          style={resetBtnStyle}
          title="Reset zoom/pan"
          aria-label="Reset zoom and pan to initial view"
        >
          ↺ Reset
        </button>
      )}
    </div>
  )
})
