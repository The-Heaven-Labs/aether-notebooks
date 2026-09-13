import { test, expect } from 'vitest'
import { mergeWidgetChartConfig, hasWidgetOverride, WIDGET_OVERRIDE_FLAG } from '../charts/widgetChartConfig'

test('unflagged widget config is ignored', () => {
  const merged = mergeWidgetChartConfig(
    { chartType: 'pie', showLegend: true },
    { chartType: 'bar', title: 'stale snapshot' },
  )
  expect(merged.chartType).toBe('pie')
  expect(merged.title).toBeUndefined()
})

test('flagged widget overrides win', () => {
  const merged = mergeWidgetChartConfig(
    { chartType: 'pie', showLegend: true },
    { [WIDGET_OVERRIDE_FLAG]: true, chartType: 'bar', title: 'dashboard title' },
  )
  expect(merged.chartType).toBe('bar')
  expect(merged.title).toBe('dashboard title')
  expect(merged.showLegend).toBe(true)
})

test('hasWidgetOverride is strict', () => {
  expect(hasWidgetOverride({ [WIDGET_OVERRIDE_FLAG]: true })).toBe(true)
  expect(hasWidgetOverride({ [WIDGET_OVERRIDE_FLAG]: 'true' })).toBe(false)
  expect(hasWidgetOverride({ [WIDGET_OVERRIDE_FLAG]: 1 })).toBe(false)
  expect(hasWidgetOverride({})).toBe(false)
  expect(hasWidgetOverride([])).toBe(false)
  expect(hasWidgetOverride(null)).toBe(false)
})

test('flagged lineWidth override survives normalization', () => {
  const merged = mergeWidgetChartConfig({ chartType: 'line' }, { [WIDGET_OVERRIDE_FLAG]: true, lineWidth: 4 })
  expect(merged.lineWidth).toBe(4)
})

test('override marker never reaches the chart config', () => {
  const merged = mergeWidgetChartConfig({}, { [WIDGET_OVERRIDE_FLAG]: true, chartType: 'bar' })
  expect(WIDGET_OVERRIDE_FLAG in merged).toBe(false)
  expect(hasWidgetOverride({ [WIDGET_OVERRIDE_FLAG]: true })).toBe(true)
  expect(hasWidgetOverride({})).toBe(false)
})

test('legacy chart types are normalized', () => {
  const merged = mergeWidgetChartConfig({ type: 'stacked_bar', xAxis: 'm', yAxis: ['v'] }, {})
  expect(merged.chartType).toBe('bar')
  expect(merged.barMode).toBe('stacked')
})

test('empty inputs produce an empty config', () => {
  expect(mergeWidgetChartConfig(null, null)).toEqual({})
  expect(mergeWidgetChartConfig(undefined, {})).toEqual({})
})
