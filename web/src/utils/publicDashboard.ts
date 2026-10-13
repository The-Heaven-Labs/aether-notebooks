import { textConfigFrom } from './textConfig'
import { htmlConfigFrom } from './htmlConfig'
import { isHTMLPanel, isQueryWidget, isTextPanel } from './widgetKind'
import type { DashboardVariable, Widget } from '../types'

/** The public payload's inlined cell data, keyed by cell id. */
export interface PublicWidgetCellData {
  outputs?: readonly unknown[] | null
}

/**
 * The public page's widget filter: query widgets only render when the owner
 * opted into live queries; cell widgets need stored outputs to render at all;
 * source-less text panels render unless their source markdown is empty (the
 * editor's "empty text panel" hint is wrong on a public surface); html panels
 * render statically unless both the markup and stylesheet are blank.
 */
export function visibleWidgets(
  widgets: Widget[],
  widgetsData: Record<string, PublicWidgetCellData | undefined>,
  publicLive: boolean,
): Widget[] {
  return widgets.filter((w) => {
    if (isQueryWidget(w)) return publicLive
    if (isTextPanel(w)) return textConfigFrom(w.config).markdown.trim().length > 0
    if (isHTMLPanel(w)) {
      const panel = htmlConfigFrom(w.config)
      return panel.html.trim().length > 0 || panel.css.trim().length > 0
    }
    return !!(w.cell_id && widgetsData[w.cell_id]?.outputs?.length)
  })
}

/**
 * A public text panel's interpolation values: each variable's configured
 * default, preserving falsy defaults (0, false, []). Variables without a
 * default stay out so `interpolateText` can decide.
 */
export function variableDefaults(variables: DashboardVariable[]): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const v of variables) {
    if (v.default !== undefined) out[v.name] = v.default
  }
  return out
}
