import { test, expect } from 'vitest'
import { buildLegend, getChartColors } from '../charts/common'

const colors = getChartColors()

test('hidden legend returns show:false', () => {
  expect(buildLegend({ showLegend: false }, colors)).toEqual({ show: false })
})

test('legend is a vertical scroll pager docked right', () => {
  const legend = buildLegend({}, colors) as Record<string, unknown>
  expect(legend.type).toBe('scroll')
  expect(legend.orient).toBe('vertical')
  expect(legend.right).toBe(10)
  expect(legend.top).toBe(8)
  expect(legend.height).toBe('85%')
  expect((legend.textStyle as Record<string, unknown>).color).toBe(colors.textMuted)
})

test('legend drops below a title band and shrinks its box', () => {
  const legend = buildLegend({ title: 'Orders' }, colors) as Record<string, unknown>
  expect(legend.top).toBe(30)
  expect(legend.height).toBe('50%')
})

test('scrolling legend is themed', () => {
  const legend = buildLegend({}, colors) as Record<string, unknown>
  expect(legend.pageIconColor).toBe(colors.textMuted)
  expect((legend.pageTextStyle as Record<string, unknown>).color).toBe(colors.textMuted)
})
