import { memo } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AppThemeProvider } from '../components/AppThemeProvider'
import { CHART_COLORS, getChartColors, useChartColors, useGroupBySeries } from './common'

// The stylesheet's built-in palettes (theme.css never sees these — charts are
// canvas and resolve colors in JS).
const LIGHT_DEFAULT_PALETTE = ['#6366f1', '#06b6d4', '#10b981', '#f59e0b', '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16']
const DARK_DEFAULT_PALETTE = ['#818cf8', '#22d3ee', '#34d399', '#fbbf24', '#fb7185', '#a78bfa', '#38bdf8', '#a3e635']

afterEach(() => {
  document.documentElement.removeAttribute('data-theme')
})

function ColorsProbe() {
  const colors = useChartColors()
  return (
    <div
      data-testid="colors"
      data-palette={colors.palette.join(',')}
      data-text={colors.text}
      data-border={colors.border}
      data-bg-card={colors.bgCard}
    />
  )
}

// Memoized on purpose: context propagation must pierce `memo` when the theme
// changes, otherwise live theme edits leave stale chart options behind.
const MemoColorsProbe = memo(function MemoColorsProbe() {
  const colors = useChartColors()
  return <div data-testid="memo-colors" data-palette={colors.palette.join(',')} />
})

function SeriesProbe({ palette }: { palette?: string[] }) {
  const { series } = useGroupBySeries(
    [{ x: 'a', g: 'one', y: 1 }],
    { xAxis: 'x', yAxis: ['y'], groupBy: 'g' },
    palette,
  )
  const color = (series[0]?.itemStyle as { color?: string } | undefined)?.color
  return <div data-testid="series" data-color={String(color)} />
}

describe('app chart theme plumbing', () => {
  it('applies the app palette and text overrides to hooks and module readers, restoring defaults on unmount', () => {
    const { unmount } = render(
      <AppThemeProvider
        theme={{
          chart_palette: ['#111111', '#222222'],
          background: '#111111',
          surface: '#1a1a1a',
          text: '#ffffff',
          border: '#2e2e2e',
        }}
      >
        <ColorsProbe />
      </AppThemeProvider>,
    )

    expect(screen.getByTestId('colors')).toHaveAttribute('data-palette', '#111111,#222222')
    expect(screen.getByTestId('colors')).toHaveAttribute('data-text', '#ffffff')
    expect(screen.getByTestId('colors')).toHaveAttribute('data-border', '#2e2e2e')
    // Module readers (the CHART_COLORS proxy, tooltip/axis helpers) see the
    // override too — they run inside canvas chart render paths.
    expect(getChartColors().palette).toEqual(['#111111', '#222222'])
    expect(getChartColors().text).toBe('#ffffff')
    expect(CHART_COLORS[0]).toBe('#111111')
    expect(CHART_COLORS.length).toBe(2)

    unmount()
    expect(getChartColors().palette).toEqual(LIGHT_DEFAULT_PALETTE)
    expect(getChartColors().text).toBe('#111')
    expect(CHART_COLORS[0]).toBe('#6366f1')
  })

  it('propagates a theme change while mounted, even through a memoized subtree', () => {
    const { rerender } = render(
      <AppThemeProvider theme={{ chart_palette: ['#111111'] }}>
        <MemoColorsProbe />
      </AppThemeProvider>,
    )
    expect(screen.getByTestId('memo-colors')).toHaveAttribute('data-palette', '#111111')

    rerender(
      <AppThemeProvider theme={{ chart_palette: ['#222222'] }}>
        <MemoColorsProbe />
      </AppThemeProvider>,
    )
    expect(screen.getByTestId('memo-colors')).toHaveAttribute('data-palette', '#222222')
    expect(CHART_COLORS[0]).toBe('#222222')
  })

  it('renders scheme defaults for a forced scheme', () => {
    render(
      <AppThemeProvider theme={{ color_scheme: 'dark' }}>
        <ColorsProbe />
      </AppThemeProvider>,
    )
    expect(screen.getByTestId('colors')).toHaveAttribute('data-palette', DARK_DEFAULT_PALETTE.join(','))
    expect(screen.getByTestId('colors')).toHaveAttribute('data-text', '#e8e8e8')
    expect(screen.getByTestId('colors')).toHaveAttribute('data-bg-card', '#1c1c1c')
  })

  it('defaults useGroupBySeries to the app palette', () => {
    render(
      <AppThemeProvider theme={{ chart_palette: ['#111111'] }}>
        <SeriesProbe />
      </AppThemeProvider>,
    )
    expect(screen.getByTestId('series')).toHaveAttribute('data-color', '#111111')
  })

  it('still lets an explicit palette argument win over the app palette', () => {
    render(
      <AppThemeProvider theme={{ chart_palette: ['#111111'] }}>
        <SeriesProbe palette={['#333333']} />
      </AppThemeProvider>,
    )
    expect(screen.getByTestId('series')).toHaveAttribute('data-color', '#333333')
  })

  it('falls back to the global data-theme when no provider is mounted', () => {
    document.documentElement.setAttribute('data-theme', 'dark')
    expect(getChartColors().palette).toEqual(DARK_DEFAULT_PALETTE)
    expect(getChartColors().text).toBe('#e8e8e8')
    expect(CHART_COLORS[0]).toBe('#818cf8')
  })
})
