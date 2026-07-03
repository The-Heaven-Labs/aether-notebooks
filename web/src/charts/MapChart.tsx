import { useMemo, useState, useEffect, useCallback } from 'react'
import * as echarts from 'echarts/core'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, useChartColors, useRowsAsObjects, ChartTypeSelect } from './common'
import { ConfigHint } from './ConfigHint'

const WORLD_GEO_URL = 'https://raw.githubusercontent.com/johan/world.geo.json/master/countries.geo.json'

let mapRegistered = false
let mapPromise: Promise<boolean> | null = null

function ensureWorldMap(): Promise<boolean> {
  if (mapRegistered) return Promise.resolve(true)
  if (mapPromise) return mapPromise
  mapPromise = fetch(WORLD_GEO_URL)
    .then(r => r.ok ? r.json() : Promise.reject(new Error(`${r.status}`)))
    .then(geojson => {
      echarts.registerMap('world', geojson)
      mapRegistered = true
      return true
    })
    .catch(() => false)
  return mapPromise
}

function MapChartComponent({ data, config }: ChartProps) {
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()
  const [geoReady, setGeoReady] = useState(mapRegistered)

  useEffect(() => {
    if (!mapRegistered) {
      ensureWorldMap().then(ok => { if (ok) setGeoReady(true) })
    }
  }, [])

  const latCol = config.yAxis?.[0] ?? data.columns.find(c => /^lat/i.test(c.name))?.name ?? ''
  const lonCol = config.xAxis ?? data.columns.find(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))?.name ?? ''
  const valCol = config.yAxis?.[1] ?? ''
  const labelCol = config.labelColumn ?? ''

  const tooltipFmt = useCallback((params: any) => {
    if (!params.data) return ''
    const lon = params.data.value?.[0] ?? params.data?.[0]
    const lat = params.data.value?.[1] ?? params.data?.[1]
    const pt = allPts.find(p => p.lon === lon && p.lat === lat)
    const name = pt?.name || `(${Number(lon)?.toFixed(1)}, ${Number(lat)?.toFixed(1)})`
    return valCol ? `${name}<br/>${valCol}: ${pt?.val ?? ''}` : name
  }, [])

  const allPts = useMemo(() =>
    chartData
      .map(d => ({
        lon: Number(d[lonCol]),
        lat: Number(d[latCol]),
        val: valCol ? Number(d[valCol]) : 1,
        name: labelCol ? String(d[labelCol] ?? '') : '',
        color: config.seriesColors?.point ?? CHART_COLORS[0],
      }))
      .filter(d => !isNaN(d.lon) && !isNaN(d.lat)),
    [chartData, latCol, lonCol, valCol, labelCol, config.seriesColors]
  )

  const maxVal = useMemo(() => Math.max(...allPts.map(p => p.val), 1), [allPts])

  const option = useMemo(() => {
    if (allPts.length === 0) return {}

    const scatterData = allPts.map(p => ({
      value: [p.lon, p.lat, p.val],
      name: p.name,
      itemStyle: { color: p.color },
    }))

    if (geoReady && mapRegistered) {
      const dark = document.documentElement.getAttribute('data-theme') === 'dark'
      return {
        tooltip: { trigger: 'item' as const, ...getTooltipStyle(), formatter: tooltipFmt },
        title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
        geo: {
          map: 'world',
          roam: true,
          zoom: 1.2,
          center: [10, 20] as [number, number],
          itemStyle: {
            areaColor: dark ? '#2a2a2a' : '#f0f0f0',
            borderColor: dark ? '#3a3a3a' : '#d0d0d0',
          },
          emphasis: {
            itemStyle: {
              areaColor: dark ? '#404040' : '#d8d8d8',
              borderColor: dark ? '#555' : '#aaa',
            },
            label: {
              show: true,
              color: dark ? '#e8e8e8' : '#111',
              fontSize: 11,
            },
          },
          label: { show: false },
        },
        series: [{
          type: 'scatter' as const,
          coordinateSystem: 'geo' as const,
          data: scatterData,
          symbolSize: (val: number[]) => Math.max(5, Math.min(28, ((val[2] ?? 1) / maxVal) * 28)),
          label: {
            show: config.showLabels,
            formatter: (pp: any) => pp.data?.name ?? '',
            fontSize: 10,
            color: colors.text,
            position: 'right' as const,
          },
            }],
      }
    }

    // Fallback: scatter on plain axes
    return {
      tooltip: { trigger: 'item' as const, ...getTooltipStyle(), formatter: tooltipFmt },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
      grid: { top: config.title ? 46 : 20, right: 16, bottom: 8, left: 16, containLabel: true },
      dataZoom: [
        { type: 'inside' as const, xAxisIndex: 0, filterMode: 'none' },
        { type: 'inside' as const, yAxisIndex: 0, filterMode: 'none' },
      ],
      xAxis: { type: 'value' as const, name: lonCol || 'Longitude', min: -180, max: 180, ...getAxisStyle() },
      yAxis: { type: 'value' as const, name: latCol || 'Latitude', min: -90, max: 90, ...getAxisStyle() },
      series: [{
        type: 'scatter' as const,
        data: allPts.map(p => [p.lon, p.lat]),
        symbolSize: (val: number[]) => {
          const pt = allPts.find(p => p.lon === val[0] && p.lat === val[1])
          return Math.max(5, Math.min(28, ((pt?.val ?? 1) / maxVal) * 28))
        },
        itemStyle: { color: allPts[0]?.color ?? CHART_COLORS[0], opacity: 0.85 },
        label: {
          show: config.showLabels,
          formatter: (pp: any) => allPts.find(p => p.lon === pp.data?.[0] && p.lat === pp.data?.[1])?.name ?? '',
          fontSize: 10,
          color: colors.text,
          position: 'right' as const,
        },
      }],
    }
  }, [allPts, maxVal, geoReady, latCol, lonCol, valCol, labelCol, config.title, config.showLabels, config.seriesColors, colors, tooltipFmt])

  return <EChartsContainer option={option} height={400} notMerge showReset />
}

function MapConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'map'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Latitude column</div>
        <select
          aria-label="Latitude column"
          style={styles.select}
          value={config.yAxis?.[0] ?? ''}
          onChange={e => onChange({ ...config, yAxis: [e.target.value, config.yAxis?.[1] ?? ''].filter(Boolean) })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column with latitude values (-90 to 90)</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Longitude column</div>
        <select
          aria-label="Longitude column"
          style={styles.select}
          value={config.xAxis ?? ''}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column with longitude values (-180 to 180)</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Value column (optional)</div>
        <select
          aria-label="Value column"
          style={styles.select}
          value={config.yAxis?.[1] ?? ''}
          onChange={e => onChange({ ...config, yAxis: [config.yAxis?.[0] ?? '', e.target.value].filter(Boolean) })}
        >
          <option value="">None</option>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Numeric column to control marker size</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Label column (optional)</div>
        <select
          aria-label="Label column"
          style={styles.select}
          value={config.labelColumn ?? ''}
          onChange={e => onChange({ ...config, labelColumn: e.target.value })}
        >
          <option value="">None</option>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column for marker text labels</ConfigHint>
      </div>
      <label style={styles.checkbox}>
        <input
          type="checkbox"
          checked={config.showLabels ?? false}
          onChange={e => onChange({ ...config, showLabels: e.target.checked })}
        />
        Show labels
      </label>
      <ConfigHint>Display text labels next to markers</ConfigHint>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Marker color</div>
        <input
          type="color"
          value={config.seriesColors?.point ?? CHART_COLORS[0]}
          onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, point: e.target.value } })}
          style={{ width: 32, height: 32, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'none' }}
        />
        <ConfigHint>Color for point markers on the map</ConfigHint>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}

export const MapChartModule: ChartModule = {
  Component: MapChartComponent,
  ConfigPanel: MapConfigPanel,
  defaultConfig: { chartType: 'map', showLabels: false, showLegend: false, showGrid: false },
  detectColumns: (columns) => {
    const latCol = columns.find(c => /^lat/i.test(c.name))
    const lonCol = columns.find(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))
    return {
      xAxis: lonCol?.name,
      yAxis: latCol ? [latCol.name] : [],
    }
  },
  requirements: { minColumns: 2 },
}
