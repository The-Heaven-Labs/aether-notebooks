import { TrendingDown, TrendingUp } from 'lucide-react'
import { computeComparison, computeMetric, formatValue, sparklinePoints, type MetricConfig } from '../utils/metric'
import type { ResultSet } from '../types'

interface Props {
  result: ResultSet
  config: MetricConfig
}

const SPARKLINE_WIDTH = 120
const SPARKLINE_HEIGHT = 28
const SPARKLINE_PAD_Y = 2

type ComparisonTone = 'up' | 'down' | 'flat'

interface ComparisonReading {
  text: string
  tone: ComparisonTone
}

/** "+" for a non-negative rendering, the number's own minus otherwise. */
function signed(text: string): string {
  return text.startsWith('-') ? text : `+${text}`
}

function comparisonReading(
  comparison: { delta: number; percent: number } | null,
  config: MetricConfig,
): ComparisonReading | null {
  if (!comparison) return null
  // Tone follows the delta (business direction), not the percent sign: with a
  // negative baseline the two can disagree (prev=-100, main=-50 → delta +50,
  // percent -50%), and the delta is the intended source of truth.
  const tone: ComparisonTone = comparison.delta > 0 ? 'up' : comparison.delta < 0 ? 'down' : 'flat'
  if ((config.comparison?.mode ?? 'percent') === 'delta') {
    return { text: signed(formatValue(comparison.delta, config.format)), tone }
  }
  // A zero baseline cannot produce a meaningful percent; a dash is honest,
  // "+0.0%" would read as "no change" when the metric actually grew.
  if (comparison.delta !== 0 && comparison.percent === 0) {
    return { text: '—', tone: 'flat' }
  }
  // A change too small to survive one decimal rounds to "+0.0%": keep it
  // neutral rather than coloring a "-0.0%" red.
  if (Math.abs(comparison.percent) < 0.05) {
    return { text: '+0.0%', tone: 'flat' }
  }
  return { text: `${signed(comparison.percent.toFixed(1))}%`, tone }
}

const toneColor: Record<ComparisonTone, string> = {
  up: 'var(--success)',
  down: 'var(--error)',
  flat: 'var(--text-muted)',
}

/** x spans the full width; y is normalized into [2, 26] (2px padding). */
function sparklinePolyline(points: Array<[number, number]>): string {
  let min = points[0][1]
  let max = points[0][1]
  for (const [, y] of points) {
    if (y < min) min = y
    if (y > max) max = y
  }
  const span = max - min
  const usable = SPARKLINE_HEIGHT - SPARKLINE_PAD_Y * 2
  return points
    .map(([, y], i) => {
      const x = (i / (points.length - 1)) * SPARKLINE_WIDTH
      const py = span === 0
        ? SPARKLINE_HEIGHT / 2
        : SPARKLINE_HEIGHT - SPARKLINE_PAD_Y - ((y - min) / span) * usable
      return `${x.toFixed(1)},${py.toFixed(1)}`
    })
    .join(' ')
}

export function MetricWidget({ result, config }: Props) {
  if (!config.value_column) {
    return (
      <div role="group" aria-label={config.label ?? 'Metric'} style={styles.root}>
        <span style={styles.hint}>Pick a value column in the widget settings.</span>
      </div>
    )
  }

  const value = computeMetric(result, config)
  const formatted = value === null ? '—' : formatValue(value, config.format)
  const reading = comparisonReading(computeComparison(result, config), config)
  const points = sparklinePoints(result, config)
  const ariaLabel = `${config.label ? `${config.label}: ` : ''}${formatted}${reading ? `, ${reading.text}` : ''}`

  return (
    <div role="group" aria-label={ariaLabel} style={styles.root}>
      {config.label && <span style={styles.label}>{config.label}</span>}
      <span style={{ ...styles.value, ...(value === null ? styles.valueMissing : null) }}>{formatted}</span>
      {reading && (
        <span style={{ ...styles.comparison, color: toneColor[reading.tone] }}>
          {reading.tone === 'up' && <TrendingUp size={12} aria-hidden="true" />}
          {reading.tone === 'down' && <TrendingDown size={12} aria-hidden="true" />}
          {reading.text}
        </span>
      )}
      {points && (
        <svg
          width="100%"
          height={SPARKLINE_HEIGHT}
          viewBox={`0 0 ${SPARKLINE_WIDTH} ${SPARKLINE_HEIGHT}`}
          preserveAspectRatio="none"
          aria-hidden="true"
          style={styles.sparkline}
        >
          <polyline points={sparklinePolyline(points)} fill="none" stroke="var(--accent)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
        </svg>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  root: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    flexDirection: 'column',
    justifyContent: 'center',
    alignItems: 'flex-start',
    gap: 4,
    padding: 16,
    textAlign: 'left',
    minWidth: 0,
    containerType: 'inline-size',
  },
  label: {
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: '0.08em',
    textTransform: 'uppercase',
    color: 'var(--text-muted)',
  },
  value: {
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
    fontSize: 'clamp(20px, 9cqw, 32px)',
    fontWeight: 700,
    letterSpacing: '-0.5px',
    lineHeight: 1.1,
    color: 'var(--text-primary)',
    whiteSpace: 'nowrap',
    maxWidth: '100%',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  valueMissing: {
    color: 'var(--text-muted)',
  },
  comparison: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    fontFamily: 'var(--font-mono)',
    fontVariantNumeric: 'tabular-nums',
    fontSize: 12,
    fontWeight: 600,
    lineHeight: 1,
  },
  sparkline: {
    display: 'block',
    width: '100%',
    marginTop: 4,
  },
  hint: {
    fontSize: 12,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
}
