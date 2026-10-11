import type { DashboardVariable } from '../types'

/**
 * Auto re-run planning for live dashboards.
 *
 * The viewer page hands over from the REST snapshot to the live document on
 * first sync. From then on every document update produces a fresh widget list
 * (the projection cache is cleared on any `doc.update`), so change detection
 * must compare per-widget run signatures instead of array identity:
 *
 * - `connector_id | language | query` changed → schedule a cache-eligible
 *   re-run, debounced per widget.
 * - `notebook_id` / `cell_id` changed → invalidate the referenced data, never
 *   execute (cell widgets render stored outputs).
 * - a variable's definition/default changed → re-run only widgets whose SQL
 *   references the changed `{{token}}`.
 * - layout/title/config changed → re-render only.
 */

/** Debounce applied per widget so a collaborator typing SQL coalesces into one re-run. */
export const AUTO_RERUN_DEBOUNCE_MS = 2000

/** The subset of widget fields that participate in change detection. */
export interface RunSignatureWidget {
  id: string
  connector_id?: string | null
  language?: string | null
  query?: string | null
  notebook_id?: string | null
  cell_id?: string | null
}

/** `connector_id | language | query` — changing any part of it requires a re-run. */
export function widgetRunSignature(widget: RunSignatureWidget): string {
  return [widget.connector_id ?? '', widget.language ?? '', widget.query ?? ''].join('|')
}

/**
 * Variable tokens referenced by a widget query. Mirrors the server's
 * interpolation grammar (`internal/dashboard/variables.go`): `{{ name }}`
 * with `[a-zA-Z0-9_-]` names, whitespace tolerated.
 */
export function referencedVariables(sql: string | null | undefined): string[] {
  if (!sql) return []
  const names = new Set<string>()
  for (const match of sql.matchAll(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g)) names.add(match[1])
  return [...names]
}

export interface WidgetRunBaseline {
  signature: string
  notebookId: string | null
  cellId: string | null
}

export interface WidgetRunDiff {
  /** Widgets whose run signature changed → debounced cache-eligible re-run. */
  rerun: string[]
  /** Widgets whose cell reference changed → invalidate their data (no execute). */
  cellRefChanged: Array<{ widgetId: string; notebookId: string | null }>
  /** Widget ids no longer in the document → drop pending re-runs. */
  removed: string[]
  /** Baselines to compare against on the next document update. */
  next: Record<string, WidgetRunBaseline>
}

function baselineOf(widget: RunSignatureWidget): WidgetRunBaseline {
  return {
    signature: widgetRunSignature(widget),
    notebookId: widget.notebook_id ?? null,
    cellId: widget.cell_id ?? null,
  }
}

/**
 * Diffs the widget list against the baselines captured on the previous
 * document update. `prev === null` is the first sync (takeover): it only seeds
 * baselines, so handing over from REST never re-runs anything. Widgets that
 * are new to the document are also seeded without a run — mounting the widget
 * already fetches its data.
 */
export function diffWidgetRuns(
  prev: Record<string, WidgetRunBaseline> | null,
  widgets: readonly RunSignatureWidget[],
): WidgetRunDiff {
  const next: Record<string, WidgetRunBaseline> = {}
  const rerun: string[] = []
  const cellRefChanged: WidgetRunDiff['cellRefChanged'] = []

  for (const widget of widgets) {
    const baseline = baselineOf(widget)
    next[widget.id] = baseline
    const previous = prev?.[widget.id]
    if (!previous) continue
    if (previous.signature !== baseline.signature) rerun.push(widget.id)
    if (previous.notebookId !== baseline.notebookId || previous.cellId !== baseline.cellId) {
      cellRefChanged.push({ widgetId: widget.id, notebookId: baseline.notebookId })
    }
  }

  const removed = prev ? Object.keys(prev).filter(id => !(id in next)) : []
  return { rerun, cellRefChanged, removed, next }
}

export interface VariableDefinitionSnapshot {
  /** Canonical definition key; label edits are not execution changes. */
  key: string
  /** Tokens this variable substitutes in SQL (date ranges expand to `_start`/`_end`). */
  tokens: string[]
}

/** Token(s) a variable substitutes: `{{name}}`, or `{{name_start}}`/`{{name_end}}` for date ranges. */
export function variableTokens(variable: Pick<DashboardVariable, 'name' | 'type'>): string[] {
  return variable.type === 'date_range'
    ? [`${variable.name}_start`, `${variable.name}_end`]
    : [variable.name]
}

/**
 * Canonical definition key. Label is excluded: renaming a filter does not
 * change what any query receives, so it must not trigger re-runs.
 */
export function variableDefinitionKey(variable: DashboardVariable): string {
  return JSON.stringify({
    name: variable.name,
    type: variable.type,
    default: variable.default ?? null,
    required: variable.required ?? false,
    options: variable.options ?? null,
    depends_on: variable.depends_on ?? null,
  })
}

export interface VariableChangeDiff {
  /** Tokens whose definition/default changed (added, removed, or edited). */
  tokens: string[]
  /** Snapshot to compare against on the next document update. */
  next: Record<string, VariableDefinitionSnapshot>
}

/**
 * Diffs variable definitions against the previous snapshot. The first call
 * (`prev === null`) seeds without reporting changes. Changed, added, and
 * removed variables report both their old and new tokens, so widgets
 * referencing either side re-run (a rename surfaces the missing variable).
 */
export function diffVariableDefinitions(
  prev: Record<string, VariableDefinitionSnapshot> | null,
  variables: readonly DashboardVariable[],
): VariableChangeDiff {
  const next: Record<string, VariableDefinitionSnapshot> = {}
  const tokens = new Set<string>()

  for (const variable of variables) {
    const key = variableDefinitionKey(variable)
    const varTokens = variableTokens(variable)
    next[variable.name] = { key, tokens: varTokens }
    const previous = prev?.[variable.name]
    if (prev !== null && (!previous || previous.key !== key)) {
      varTokens.forEach(token => tokens.add(token))
      previous?.tokens.forEach(token => tokens.add(token))
    }
  }

  if (prev !== null) {
    for (const [name, previous] of Object.entries(prev)) {
      if (!(name in next)) previous.tokens.forEach(token => tokens.add(token))
    }
  }

  return { tokens: [...tokens], next }
}

/** Widget ids whose query references any of `tokens`. */
export function widgetsReferencingTokens(
  widgets: readonly RunSignatureWidget[],
  tokens: readonly string[],
): string[] {
  if (!tokens.length) return []
  const wanted = new Set(tokens)
  const out: string[] = []
  for (const widget of widgets) {
    if (referencedVariables(widget.query).some(token => wanted.has(token))) out.push(widget.id)
  }
  return out
}

export interface PerWidgetDebouncer {
  /** (Re)arms the timer for `widgetId`, replacing any pending run. */
  schedule(widgetId: string): void
  /** Drops a pending run (widget removed from the document, or unmount). */
  cancel(widgetId: string): void
  cancelAll(): void
  /** Whether a run is pending for `widgetId`. */
  isPending(widgetId: string): boolean
}

/** Coalesces a burst of changes into one `run(widgetId)` call per widget. */
export function createPerWidgetDebouncer(
  run: (widgetId: string) => void,
  delayMs = AUTO_RERUN_DEBOUNCE_MS,
): PerWidgetDebouncer {
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  return {
    schedule(widgetId) {
      const pending = timers.get(widgetId)
      if (pending !== undefined) clearTimeout(pending)
      timers.set(widgetId, setTimeout(() => {
        timers.delete(widgetId)
        run(widgetId)
      }, delayMs))
    },
    cancel(widgetId) {
      const pending = timers.get(widgetId)
      if (pending !== undefined) {
        clearTimeout(pending)
        timers.delete(widgetId)
      }
    },
    cancelAll() {
      for (const pending of timers.values()) clearTimeout(pending)
      timers.clear()
    },
    isPending(widgetId) {
      return timers.has(widgetId)
    },
  }
}
