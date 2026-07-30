import { useMemo, useState, useEffect, useCallback } from 'react'
import * as echarts from 'echarts/core'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, useChartColors, useRowsAsObjects, ChartTypeSelect } from './common'
import { ConfigHint } from './ConfigHint'
import { useGroupValues } from './AxisConfigPanel'

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

const COUNTRY_ALIASES: Record<string, string> = {
  'us': 'United States of America',
  'usa': 'United States of America',
  'united states': 'United States of America',
  'uk': 'United Kingdom',
  'great britain': 'United Kingdom',
  'ru': 'Russia',
  'russian federation': 'Russia',
  'cn': 'China',
  'kr': 'South Korea',
  'south korea': 'South Korea',
  'jp': 'Japan',
  'de': 'Germany',
  'fr': 'France',
  'it': 'Italy',
  'es': 'Spain',
  'nl': 'Netherlands',
  'be': 'Belgium',
  'ch': 'Switzerland',
  'se': 'Sweden',
  'no': 'Norway',
  'dk': 'Denmark',
  'fi': 'Finland',
  'pl': 'Poland',
  'at': 'Austria',
  'ie': 'Ireland',
  'pt': 'Portugal',
  'cz': 'Czech Republic',
  'czech republic': 'Czech Republic',
  'hu': 'Hungary',
  'gr': 'Greece',
  'ro': 'Romania',
  'ua': 'Ukraine',
  'tr': 'Turkey',
  'au': 'Australia',
  'nz': 'New Zealand',
  'in': 'India',
  'br': 'Brazil',
  'za': 'South Africa',
  'eg': 'Egypt',
  'ng': 'Nigeria',
  'ke': 'Kenya',
  'ar': 'Argentina',
  'cl': 'Chile',
  'co': 'Colombia',
  'mx': 'Mexico',
  'ca': 'Canada',
  'il': 'Israel',
  'ae': 'United Arab Emirates',
  'sa': 'Saudi Arabia',
  'sg': 'Singapore',
  'hk': 'Hong Kong',
  'tw': 'Taiwan',
  'th': 'Thailand',
  'vn': 'Vietnam',
  'my': 'Malaysia',
  'id': 'Indonesia',
  'ph': 'Philippines',
  'gb': 'United Kingdom',
}

function normalizeCountry(name: string): string {
  const key = name.trim().toLowerCase()
  return COUNTRY_ALIASES[key] ?? name.trim()
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

  const matchedLatCol = config.yAxis?.[0] ?? data.columns.find(c => /^lat/i.test(c.name))?.name ?? ''
  const matchedLonCol = (config.xAxis && (/^lon/i.test(config.xAxis) || /^lng/i.test(config.xAxis))) ? config.xAxis : data.columns.find(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))?.name ?? ''
  const matchedCountryCol = (config.xAxis && !/^lon/i.test(config.xAxis) && !/^lng/i.test(config.xAxis)) ? config.xAxis : data.columns.find(c => /^country/i.test(c.name) || /^nation/i.test(c.name))?.name ?? ''
  const hasLatOrLon = !!(matchedLatCol || matchedLonCol)
  const hasCountry = !!matchedCountryCol
  const mapMode = config.mapMode ?? (hasCountry && !hasLatOrLon ? 'choropleth' : 'points')
  const isChoropleth = mapMode === 'choropleth'

  const countryCol = isChoropleth ? matchedCountryCol : ''
  const latCol = !isChoropleth ? matchedLatCol : ''
  const lonCol = !isChoropleth ? matchedLonCol : ''
  const rawValCol = config.yAxis?.[1] ?? config.valueColumn ?? ''
  const valCol = rawValCol && data.columns.some(c => c.name === rawValCol) ? rawValCol : ''
  const labelCol = config.labelColumn ?? ''
  const groupByCol = config.groupBy
  const hasGroupBy = !isChoropleth && !!(groupByCol && chartData.some(row => groupByCol in row))

  const allPts = useMemo(() =>
    chartData
      .map(d => ({
        lon: !isChoropleth ? Number(d[lonCol]) : NaN,
        lat: !isChoropleth ? Number(d[latCol]) : NaN,
        val: valCol ? Number(d[valCol]) : 1,
        name: labelCol ? String(d[labelCol] ?? '') : '',
        country: isChoropleth ? String(d[countryCol] ?? '') : '',
        group: groupByCol ? String(d[groupByCol] ?? '') : '',
        color: config.seriesColors?.point ?? CHART_COLORS[0],
      }))
      .filter(d => isChoropleth ? d.country : (!isNaN(d.lon) && !isNaN(d.lat))),
    [chartData, isChoropleth, countryCol, latCol, lonCol, valCol, labelCol, groupByCol, config.seriesColors]
  )

  const groupList = useMemo(() => {
    if (!hasGroupBy) return []
    return [...new Set(allPts.map(p => p.group).filter(Boolean))] as string[]
  }, [hasGroupBy, allPts])

  const groupPalette = useMemo(() => {
    if (!hasGroupBy) return null
    const map: Record<string, string> = {}
    groupList.forEach((g, i) => { map[g] = config.seriesColors?.[g] ?? colors.palette[i % colors.palette.length] })
    return map
  }, [hasGroupBy, groupList, config.seriesColors, colors.palette])

  const choroplethData = useMemo(() => {
    if (!isChoropleth) return []
    const map = new Map<string, number>()
    for (const pt of allPts) {
      const name = normalizeCountry(pt.country)
      map.set(name, (map.get(name) ?? 0) + pt.val)
    }
    return Array.from(map.entries()).map(([name, val]) => ({ name, value: val }))
  }, [isChoropleth, allPts])

  const choroplethMin = useMemo(() => choroplethData.length ? Math.min(...choroplethData.map(d => d.value)) : 0, [choroplethData])
  const choroplethMax = useMemo(() => choroplethData.length ? Math.max(...choroplethData.map(d => d.value)) : 1, [choroplethData])

  const maxVal = useMemo(() => Math.max(...allPts.map(p => p.val), 1), [allPts])

  const tooltipFmt = useCallback((params: any) => {
    if (isChoropleth) {
      return `${params.name}<br/>${valCol || 'Value'}: ${params.data?.value ?? params.value ?? ''}`
    }
    if (!params.data) return ''
    const lon = params.data.value?.[0] ?? params.data?.[0]
    const lat = params.data.value?.[1] ?? params.data?.[1]
    const pt = allPts.find(p => p.lon === lon && p.lat === lat)
    const name = pt?.name || `(${Number(lon)?.toFixed(1)}, ${Number(lat)?.toFixed(1)})`
    const groupInfo = pt?.group ? ` (${pt.group})` : ''
    return valCol ? `${name}${groupInfo}<br/>${valCol}: ${pt?.val ?? ''}` : `${name}${groupInfo}`
  }, [allPts, valCol, isChoropleth])

  const option = useMemo(() => {
    if (allPts.length === 0) return {}

    const dark = document.documentElement.getAttribute('data-theme') === 'dark'

    if (!geoReady || !mapRegistered) {
      if (isChoropleth) {
        return {
          title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
          xAxis: { type: 'category' as const, data: choroplethData.map(d => d.name), axisLabel: { rotate: 45, fontSize: 10, color: colors.textMuted }, axisLine: { show: false } },
          yAxis: { type: 'value' as const, ...getAxisStyle() },
          series: [{ type: 'bar' as const, data: choroplethData.map(d => d.value), itemStyle: { color: CHART_COLORS[0] } }],
          tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
        }
      }
      return {}
    }

    const lowColor = config.seriesColors?.low ?? '#e0f3f8'
    const highColor = config.seriesColors?.high ?? '#045a8d'

    const geo = {
      map: 'world' as const,
      roam: true,
      zoom: 1.2,
      center: [10, 20] as [number, number],
      itemStyle: {
        areaColor: dark ? '#3d3d3d' : '#f0f0f0',
        borderColor: dark ? '#555' : '#d0d0d0',
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
    }

    if (isChoropleth) {
      return {
        tooltip: { trigger: 'item' as const, ...getTooltipStyle(), formatter: tooltipFmt },
        title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
        visualMap: {
          min: choroplethMin,
          max: choroplethMax,
          left: 16,
          bottom: 16,
          textStyle: { fontSize: 10, color: colors.textMuted },
          inRange: { color: [lowColor, highColor] },
          calculable: true,
        },
        geo: { ...geo, label: { show: config.showLabels ?? false, fontSize: 10, color: colors.text } },
        series: [{
          type: 'map' as const,
          map: 'world',
          geoIndex: 0,
          roam: true,
          data: choroplethData,
          selectedMode: false,
          emphasis: {
            label: { show: true, fontSize: 11, color: dark ? '#fff' : '#111' },
            itemStyle: { areaColor: dark ? '#505050' : '#c8c8c8' },
          },
        }],
      }
    }

    const buildPointsSeries = (coords: 'geo' | 'cartesian') => {
      if (hasGroupBy) {
        return groupList.map((group) => {
          const pts = allPts.filter(p => p.group === group)
          const maxGrpVal = Math.max(...pts.map(p => p.val), 1)
          const grpColor = groupPalette?.[group] ?? CHART_COLORS[0]
          return {
            name: group,
            type: 'scatter' as const,
            coordinateSystem: coords === 'geo' ? 'geo' as const : undefined,
            data: pts.map(p => coords === 'geo'
              ? { value: [p.lon, p.lat, p.val], name: p.name }
              : [p.lon, p.lat]),
            symbolSize: valCol ? (v: number[]) => Math.max(5, Math.min(28, ((v[2] ?? 1) / maxGrpVal) * 28)) : 10,
            itemStyle: { color: grpColor, opacity: 0.7 },
            label: {
              show: config.showLabels,
              formatter: (pp: any) => pp.data?.name ?? '',
              fontSize: 10,
              color: colors.text,
              position: 'right' as const,
            },
          }
        })
      }
      return [{
        type: 'scatter' as const,
        coordinateSystem: coords === 'geo' ? 'geo' as const : undefined,
        data: allPts.map(p => ({
          value: [p.lon, p.lat, p.val],
          name: p.name,
          itemStyle: { color: p.color },
        })),
        symbolSize: valCol ? (v: number[]) => Math.max(5, Math.min(28, ((v[2] ?? 1) / maxVal) * 28)) : 10,
        itemStyle: { color: allPts[0]?.color ?? CHART_COLORS[0], opacity: 0.7 },
        label: {
          show: config.showLabels,
          formatter: (pp: any) => pp.data?.name ?? '',
          fontSize: 10,
          color: colors.text,
          position: 'right' as const,
        },
      }]
    }

    const builtSeries = buildPointsSeries('geo')
    const seriesColors = hasGroupBy
      ? groupList.map(g => groupPalette?.[g] ?? CHART_COLORS[0])
      : undefined
    return {
      tooltip: { trigger: 'item' as const, ...getTooltipStyle(), formatter: tooltipFmt },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
      color: seriesColors,
      legend: hasGroupBy ? { show: true, top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : { show: false },
      geo,
      series: builtSeries,
    }
  }, [allPts, maxVal, choroplethData, choroplethMin, choroplethMax, geoReady, isChoropleth, countryCol, latCol, lonCol, valCol, labelCol, config.title, config.showLabels, config.showLegend, config.seriesColors, colors, tooltipFmt])

  return <EChartsContainer option={option} height={400} notMerge showReset />
}

function MapConfigPanel({ config, columns, onChange, data }: ConfigPanelProps) {
  const hasLatOrLon = columns.some(c => /^lat/i.test(c) || /^lon/i.test(c) || /^lng/i.test(c))
  const hasCountry = columns.some(c => /^country/i.test(c) || /^nation/i.test(c))
  const mapMode = config.mapMode ?? (hasCountry && !hasLatOrLon ? 'choropleth' : 'points')
  const isChoropleth = mapMode === 'choropleth'
  const localGroupValues = useGroupValues(config, columns, data)
  const groupValues = localGroupValues
  const hasGroupByCfg = !isChoropleth && !!(config.groupBy && groupValues.length > 0)

  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Title</div>
        <input
          aria-label="Chart title"
          style={{ ...styles.select, width: '100%' }}
          type="text"
          value={config.title ?? ''}
          onChange={e => onChange({ ...config, title: e.target.value || undefined })}
          placeholder="Map title"
        />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'map'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Map mode</div>
        <select
          aria-label="Map mode"
          style={styles.select}
          value={mapMode}
          onChange={e => onChange({ ...config, mapMode: e.target.value as any })}
        >
          <option value="points">Point markers (lat/lon)</option>
          <option value="choropleth">Country regions (choropleth)</option>
        </select>
      </div>
      {isChoropleth ? (
        <>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Country column</div>
            <select
              aria-label="Country column"
              style={styles.select}
              value={config.xAxis ?? ''}
              onChange={e => onChange({ ...config, xAxis: e.target.value })}
            >
              {columns.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <ConfigHint>Column with country names or ISO codes</ConfigHint>
          </div>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Value column (optional)</div>
            <select
              aria-label="Value column"
              style={styles.select}
              value={config.yAxis?.[1] ?? ''}
              onChange={e => onChange({ ...config, yAxis: [config.yAxis?.[0] ?? '', e.target.value] })}
            >
              <option value="">Count (1 per row)</option>
              {columns.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <ConfigHint>Numeric column to aggregate per country</ConfigHint>
          </div>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Color range</div>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input
                type="color"
                value={config.seriesColors?.low ?? '#e0f3f8'}
                onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, low: e.target.value } })}
                style={styles.colorInput}
                title="Low value color"
              />
              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>→</span>
              <input
                type="color"
                value={config.seriesColors?.high ?? '#045a8d'}
                onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, high: e.target.value } })}
                style={styles.colorInput}
                title="High value color"
              />
            </div>
            <ConfigHint>Gradient from low to high values</ConfigHint>
          </div>
          <label style={styles.checkbox}>
            <input
              type="checkbox"
              checked={config.showLabels ?? false}
              onChange={e => onChange({ ...config, showLabels: e.target.checked })}
            />
            Show country labels
          </label>
        </>
      ) : (
        <>
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
              value={lonColDisplayValue(config, columns)}
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
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Group by</div>
            <select
              aria-label="Group by"
              style={styles.select}
              value={config.groupBy ?? ''}
              onChange={e => onChange({ ...config, groupBy: e.target.value || undefined })}
            >
              <option value="">— None —</option>
              {columns.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <ConfigHint>Split points into colored groups by this column</ConfigHint>
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
          <label style={styles.checkbox}>
            <input
              type="checkbox"
              checked={config.showLegend !== false}
              onChange={e => onChange({ ...config, showLegend: e.target.checked })}
            />
            Show legend
          </label>
          <ConfigHint>Display group legend (only with Group by)</ConfigHint>
          {hasGroupByCfg && (
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Group colors</div>
              <div style={styles.colorRow}>
                {groupValues.map((group, i) => {
                  const defaultColor = CHART_COLORS[i % CHART_COLORS.length]
                  const currentColor = config.seriesColors?.[group] ?? defaultColor
                  return (
                    <label key={group} style={styles.colorLabel}>
                      <input
                        type="color"
                        value={currentColor}
                        onChange={e => {
                          const newColors = { ...config.seriesColors, [group]: e.target.value }
                          onChange({ ...config, seriesColors: newColors })
                        }}
                        style={styles.colorInput}
                      />
                      <span style={styles.colorText}>{group.substring(0, 10)}</span>
                    </label>
                  )
                })}
              </div>
              <ConfigHint>Color for each group value</ConfigHint>
            </div>
          )}
          {!hasGroupByCfg && (
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Marker color</div>
              <input
                type="color"
                value={config.seriesColors?.point ?? CHART_COLORS[0]}
                onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, point: e.target.value } })}
                style={styles.colorInput}
              />
              <ConfigHint>Color for point markers on the map</ConfigHint>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function lonColDisplayValue(config: any, columns: string[]): string {
  const val = config.xAxis ?? columns.find(c => /^lon/i.test(c) || /^lng/i.test(c)) ?? ''
  return val
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
  colorInput: { width: 28, height: 24, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer' },
  colorRow: { display: 'flex', flexWrap: 'wrap', gap: 8 },
  colorLabel: { display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: 'var(--text-muted)' },
  colorText: { maxWidth: 80, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
}

export const MapChartModule: ChartModule = {
  Component: MapChartComponent,
  ConfigPanel: MapConfigPanel,
  defaultConfig: { chartType: 'map', showLabels: false, showLegend: false, showGrid: false },
  detectColumns: (columns) => {
    const hasLat = columns.some(c => /^lat/i.test(c.name))
    const hasLon = columns.some(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))
    const hasCountry = columns.some(c => /^country/i.test(c.name) || /^nation/i.test(c.name))
    if (hasCountry && !hasLat && !hasLon) {
      const countryCol = columns.find(c => /^country/i.test(c.name) || /^nation/i.test(c.name))
      const valCol = columns.find(c => /(value|count|amount|sales|revenue|total|population)$/i.test(c.name))
      return {
        mapMode: 'choropleth',
        xAxis: countryCol?.name,
        yAxis: valCol ? [valCol.name] : [],
      }
    }
    const latCol = columns.find(c => /^lat/i.test(c.name))
    const lonCol = columns.find(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))
    return {
      xAxis: lonCol?.name,
      yAxis: latCol ? [latCol.name] : [],
    }
  },
  requirements: { minColumns: 1 },
}
