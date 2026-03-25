import { useState, useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ResultSet } from '../types'

interface Props {
  rs: ResultSet
}

type ChartType = 'bar' | 'line' | 'scatter'

export function ChartView({ rs }: Props) {
  const numericCols = rs.columns.filter((c) =>
    ['integer', 'float', 'unknown'].includes(c.type),
  )
  const categoryCols = rs.columns.filter((c) => !['integer', 'float'].includes(c.type))

  const [chartType, setChartType] = useState<ChartType>('bar')
  const [xCol, setXCol] = useState(categoryCols[0]?.name ?? rs.columns[0]?.name ?? '')
  const [yCol, setYCol] = useState(numericCols[0]?.name ?? rs.columns[1]?.name ?? '')

  const xIdx = rs.columns.findIndex((c) => c.name === xCol)
  const yIdx = rs.columns.findIndex((c) => c.name === yCol)

  const xData = rs.rows.map((r) => String((r as unknown[])[xIdx] ?? ''))
  const yData = rs.rows.map((r) => {
    const v = (r as unknown[])[yIdx]
    return v === null ? null : Number(v)
  })

  const option = useMemo(() => ({
    backgroundColor: 'transparent',
    tooltip: {
      trigger: chartType === 'scatter' ? 'item' : 'axis',
      axisPointer: { type: 'shadow' },
    },
    grid: { left: 60, right: 24, top: 16, bottom: 60, containLabel: false },
    xAxis: chartType === 'scatter' ? { type: 'value', name: xCol } : {
      type: 'category',
      data: xData,
      axisLabel: {
        rotate: xData.length > 12 ? 35 : 0,
        interval: xData.length > 30 ? Math.floor(xData.length / 15) : 0,
        fontSize: 11,
        color: '#6b6258',
        overflow: 'truncate',
        width: 80,
      },
      axisLine: { lineStyle: { color: '#e3ddd5' } },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      name: yCol,
      nameTextStyle: { color: '#6b6258', fontSize: 11 },
      axisLabel: { fontSize: 11, color: '#6b6258' },
      axisLine: { show: false },
      splitLine: { lineStyle: { color: '#f0ede7', type: 'dashed' } },
    },
    series: [
      {
        type: chartType,
        data: chartType === 'scatter'
          ? rs.rows.map((r) => [(r as unknown[])[xIdx], (r as unknown[])[yIdx]])
          : yData,
        itemStyle: { color: '#7c6faa', borderRadius: chartType === 'bar' ? [3, 3, 0, 0] : 0 },
        lineStyle: { color: '#7c6faa', width: 2 },
        areaStyle: chartType === 'line' ? { color: 'rgba(124,111,170,0.08)' } : undefined,
        smooth: chartType === 'line',
        symbolSize: chartType === 'scatter' ? 6 : undefined,
      },
    ],
  }), [chartType, xData, yData, xCol, yCol, xIdx, rs.rows])

  if (rs.columns.length < 2) {
    return <p style={styles.empty}>Need at least 2 columns to chart</p>
  }

  return (
    <div style={styles.wrap}>
      <div style={styles.controls}>
        <div style={styles.controlGroup}>
          <span style={styles.controlLabel}>Type</span>
          <div style={styles.segmented}>
            {(['bar', 'line', 'scatter'] as ChartType[]).map((t) => (
              <button
                key={t}
                style={{ ...styles.segBtn, ...(chartType === t ? styles.segBtnActive : {}) }}
                onClick={() => setChartType(t)}
              >
                {t === 'bar' ? '▦ Bar' : t === 'line' ? '╱ Line' : '⬡ Scatter'}
              </button>
            ))}
          </div>
        </div>

        <div style={styles.controlGroup}>
          <span style={styles.controlLabel}>X</span>
          <select style={styles.select} value={xCol} onChange={(e) => setXCol(e.target.value)}>
            {rs.columns.map((c) => <option key={c.name} value={c.name}>{c.name}</option>)}
          </select>
        </div>

        <div style={styles.controlGroup}>
          <span style={styles.controlLabel}>Y</span>
          <select style={styles.select} value={yCol} onChange={(e) => setYCol(e.target.value)}>
            {rs.columns.map((c) => <option key={c.name} value={c.name}>{c.name}</option>)}
          </select>
        </div>
      </div>

      <ReactECharts
        option={option}
        style={{ height: 280, width: '100%' }}
        notMerge
      />
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  wrap: {
    padding: '12px 16px 4px',
    borderTop: '1px solid var(--border-light)',
    background: 'white',
  },
  controls: {
    display: 'flex',
    alignItems: 'center',
    gap: 16,
    marginBottom: 8,
    flexWrap: 'wrap',
  },
  controlGroup: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
  },
  controlLabel: {
    fontSize: 11,
    fontWeight: 700,
    color: 'var(--text-muted)',
    letterSpacing: '0.06em',
    textTransform: 'uppercase',
  },
  segmented: {
    display: 'flex',
    gap: 2,
    background: 'var(--bg-secondary)',
    padding: 2,
    borderRadius: 6,
  },
  segBtn: {
    padding: '4px 10px',
    border: 'none',
    background: 'transparent',
    borderRadius: 5,
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  segBtnActive: {
    background: 'white',
    color: 'var(--text-primary)',
    boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
  },
  select: {
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 5,
    fontSize: 12,
    color: 'var(--text-primary)',
    background: 'white',
    outline: 'none',
    fontFamily: 'var(--font-sans)',
  },
  empty: {
    padding: '16px',
    color: 'var(--text-muted)',
    fontSize: 13,
    textAlign: 'center',
  },
}
