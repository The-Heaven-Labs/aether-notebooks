import { formatValue, type ValueFormat } from './valueFormat'
import type { ResultSet } from '../types'

export type MetricAggregation =
  | 'none' | 'sum' | 'avg' | 'min' | 'max' | 'median'
  | 'count' | 'count_distinct' | 'first'

export interface MetricConfig {
  value_column?: string
  aggregation?: MetricAggregation
  label?: string
  format?: ValueFormat
  comparison?: { column?: string; mode?: 'delta' | 'percent' }
  sparkline?: { x_column?: string; y_column?: string }
}

export function metricConfigFrom(config: Record<string, unknown> | undefined): MetricConfig {
  const m = config?.metric
  return (m && typeof m === 'object' ? m : {}) as MetricConfig
}

function columnValues(result: ResultSet, column: string): unknown[] {
  const idx = result.columns.findIndex(c => c.name === column)
  if (idx < 0) return []
  return result.rows.map(row => row[idx])
}

export function computeMetric(result: ResultSet, cfg: MetricConfig): number | null {
  const column = cfg.value_column
  if (!column || !result.rows.length) return null
  const idx = result.columns.findIndex(c => c.name === column)
  if (idx < 0) return null
  const raw = result.rows.map(row => row[idx])
  const nums = raw.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const agg = cfg.aggregation ?? 'none'
  switch (agg) {
    case 'none':
    case 'first':
      return typeof raw[0] === 'number' ? raw[0] : null
    case 'count':
      return raw.length
    case 'count_distinct':
      return new Set(raw.map(v => String(v))).size
    default:
      if (!nums.length) return null
      switch (agg) {
        case 'sum': return nums.reduce((a, b) => a + b, 0)
        case 'avg': return nums.reduce((a, b) => a + b, 0) / nums.length
        case 'min': return Math.min(...nums)
        case 'max': return Math.max(...nums)
        case 'median': {
          const sorted = [...nums].sort((a, b) => a - b)
          const mid = Math.floor(sorted.length / 2)
          return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2
        }
        default: return null
      }
  }
}

export function computeComparison(result: ResultSet, cfg: MetricConfig): { delta: number; percent: number } | null {
  const column = cfg.comparison?.column
  const main = computeMetric(result, cfg)
  if (!column || main === null) return null
  const prev = columnValues(result, column).find((v): v is number => typeof v === 'number')
  if (prev === undefined) return null
  const delta = main - prev
  return { delta, percent: prev === 0 ? 0 : (delta / prev) * 100 }
}

export function sparklinePoints(result: ResultSet, cfg: MetricConfig): Array<[number, number]> | null {
  const { x_column, y_column } = cfg.sparkline ?? {}
  if (!x_column || !y_column || result.rows.length < 2) return null
  const xi = result.columns.findIndex(c => c.name === x_column)
  const yi = result.columns.findIndex(c => c.name === y_column)
  if (xi < 0 || yi < 0) return null
  const rows = result.rows.filter(row => typeof row[yi] === 'number')
  if (rows.length < 2) return null
  // x is only used for ordering: numeric values sort numerically, date-like
  // strings sort by parsed time, anything else keeps the row order.
  const xs = rows.map(row => row[xi])
  const numeric = xs.every(v => typeof v === 'number')
  const dateLike = !numeric && xs.every(v => !Number.isNaN(new Date(String(v)).getTime()))
  if (numeric) rows.sort((a, b) => (a[xi] as number) - (b[xi] as number))
  else if (dateLike) rows.sort((a, b) => new Date(String(a[xi])).getTime() - new Date(String(b[xi])).getTime())
  return rows.map((row, i) => [i, row[yi] as number])
}

export { formatValue }
