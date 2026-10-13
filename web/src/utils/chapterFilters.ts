import { referencedVariables, variableTokens } from './dashboardRunSignature'
import type { DashboardVariable, Widget } from '../types'
import { widgetTabId, type DashboardTabLike } from './dashboardTabs'

/** Variables referenced by a widget: its SQL plus text-panel markdown. */
function widgetRefs(widget: Widget): string[] {
  const sql = referencedVariables(widget.query)
  const markdown = (widget.config as { text?: { markdown?: string } } | undefined)?.text?.markdown
  return markdown ? [...sql, ...referencedVariables(markdown)] : sql
}

/**
 * The variables a chapter's widgets use, plus everything those variables
 * depend on transitively (options queries, depends_on), so a visible filter
 * never hides the filters that feed it. Order follows `variables`.
 *
 * Widgets are attributed to chapters exactly like the renderer does
 * (`widgetTabId`): a widget with no tab, or one naming a tab that no longer
 * exists, belongs to the first chapter. With no tabs configured there is no
 * chapter scoping, so every variable is visible.
 */
export function variablesForChapter(
  widgets: readonly Widget[],
  variables: readonly DashboardVariable[],
  tabs: readonly DashboardTabLike[],
  chapterId: string,
): DashboardVariable[] {
  if (tabs.length === 0) return [...variables]

  const byToken = new Map<string, DashboardVariable>()
  for (const variable of variables) {
    // The bare name covers text-panel interpolation (`interpolateText`
    // substitutes `{{name}}` for every type, expanding a date range to a
    // readable span); `variableTokens` adds the SQL forms (`{{name_start}}` /
    // `{{name_end}}` for date ranges).
    byToken.set(variable.name, variable)
    for (const token of variableTokens(variable)) byToken.set(token, variable)
  }
  const byName = new Map(variables.map((variable) => [variable.name, variable]))

  const included = new Map<string, DashboardVariable>()
  const queue: DashboardVariable[] = []
  const includeToken = (token: string) => {
    const variable = byToken.get(token)
    if (variable && !included.has(variable.name)) {
      included.set(variable.name, variable)
      queue.push(variable)
    }
  }
  const includeVariable = (variable: DashboardVariable) => {
    if (!included.has(variable.name)) {
      included.set(variable.name, variable)
      queue.push(variable)
    }
  }

  for (const widget of widgets) {
    if (widgetTabId(widget, tabs) !== chapterId) continue
    for (const token of widgetRefs(widget)) includeToken(token)
  }

  // Close over options queries and depends_on links to a fixpoint.
  for (let i = 0; i < queue.length; i++) {
    const variable = queue[i]
    if (variable.options?.mode === 'query') {
      for (const token of referencedVariables(variable.options.query?.sql)) includeToken(token)
    }
    for (const name of variable.depends_on ?? []) {
      const target = byName.get(name)
      if (target) includeVariable(target)
    }
  }

  return variables.filter((variable) => included.has(variable.name))
}
