import type { ChartConfig } from './types'
import { normalizeChartConfig } from './normalizeChartConfig'

export const WIDGET_OVERRIDE_FLAG = 'config_edited_from_dashboard'

export function hasWidgetOverride(widgetConfig: unknown): boolean {
  return !!widgetConfig && typeof widgetConfig === 'object' &&
    (widgetConfig as Record<string, unknown>)[WIDGET_OVERRIDE_FLAG] === true
}

/**
 * Chart appearance for a dashboard widget.
 * Unflagged widgets render the notebook cell config only (creation-time
 * snapshots in legacy widgets.config are ignored). Flagged widgets render the
 * cell config plus explicit per-widget overrides; the marker key is stripped
 * before the config reaches any chart.
 */
export function mergeWidgetChartConfig(cellChart: unknown, widgetConfig: unknown): ChartConfig {
  const cell = normalizeChartConfig(cellChart) ?? ({} as ChartConfig)
  if (!hasWidgetOverride(widgetConfig)) return cell
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { [WIDGET_OVERRIDE_FLAG]: _ignored, ...overrides } =
    widgetConfig as Record<string, unknown>
  return normalizeChartConfig({ ...cell, ...overrides }) ?? ({} as ChartConfig)
}
