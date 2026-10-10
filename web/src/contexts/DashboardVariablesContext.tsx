import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api/client'
import type { DashboardVariable, DashboardVariableOption } from '../types'

export type VariableValue = string | number | boolean | string[]

interface VariableOptionsState {
  options: DashboardVariableOption[]
  loading: boolean
  error: string | null
  reload: () => void
}

interface DashboardVariablesContextValue {
  variables: DashboardVariable[]
  values: Record<string, unknown>
  setValue: (name: string, value: unknown) => void
  setValues: (next: Record<string, unknown>) => void
  /** Resets every variable to its configured default. */
  resetAll: () => void
  optionState: Record<string, VariableOptionsState>
}

const Ctx = createContext<DashboardVariablesContextValue>({
  variables: [], values: {}, setValue: () => {}, setValues: () => {}, resetAll: () => {}, optionState: {},
})

function storageKey(dashboardId: string) { return `aether_dashvars_${dashboardId}` }

/** Names the viewer explicitly chose; kept apart from the values map so live
 * definition changes never overwrite an explicit choice. */
function touchedStorageKey(dashboardId: string) { return `aether_dashvars_touched_${dashboardId}` }

// A multi_select with no choices is an explicit "(none)" filter, not the
// absence of one: `IN (NULL)` matches no rows by design. It round-trips
// through the URL as an empty value (`?country=`) so shared links keep the
// exclusion instead of silently falling back to stored/default choices.
const NONE_SENTINEL = ''

function parseSearchValue(v: DashboardVariable, search: URLSearchParams): unknown {
  const all = search.getAll(v.name)
  if (!all.length) return undefined
  switch (v.type) {
    case 'multi_select':
      return all.length === 1 && all[0] === NONE_SENTINEL ? [] : all
    case 'number': { const n = Number(all[0]); return Number.isFinite(n) ? n : undefined }
    case 'boolean': return all[0] === 'true' ? true : all[0] === 'false' ? false : undefined
    case 'date_range': return all[0].includes(',') ? all[0].split(',', 2) : undefined
    default: return all[0]
  }
}

/** Deep equality for a variable's value; multi_select compares as a set. */
function sameVariableValue(v: DashboardVariable, a: unknown, b: unknown): boolean {
  if (v.type === 'multi_select') {
    const la = Array.isArray(a) ? a.map(String).sort() : []
    const lb = Array.isArray(b) ? b.map(String).sort() : []
    return la.length === lb.length && la.every((item, i) => item === lb[i])
  }
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null)
}

/** True when the current value carries the same meaning as the variable's default. */
export function isVariableDefault(v: DashboardVariable, value: unknown): boolean {
  return sameVariableValue(v, value, v.default)
}

function loadInitial(dashboardId: string, variables: DashboardVariable[], search: URLSearchParams): Record<string, unknown> {
  let stored: Record<string, unknown> = {}
  try { stored = JSON.parse(localStorage.getItem(storageKey(dashboardId)) ?? '{}') } catch { stored = {} }
  const out: Record<string, unknown> = {}
  for (const v of variables) {
    const fromUrl = parseSearchValue(v, search)
    if (fromUrl !== undefined) { out[v.name] = fromUrl; continue }
    if (stored[v.name] !== undefined) { out[v.name] = stored[v.name]; continue }
    if (v.default !== undefined) out[v.name] = v.default
  }
  return out
}

/** Explicit choices from a previous visit plus this visit's URL pins. */
function loadTouched(dashboardId: string, variables: DashboardVariable[], search: URLSearchParams): Set<string> {
  const out = new Set<string>()
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(touchedStorageKey(dashboardId)) ?? '[]')
    if (Array.isArray(stored)) for (const name of stored) if (typeof name === 'string') out.add(name)
  } catch { /* ignore malformed storage */ }
  for (const v of variables) {
    if (parseSearchValue(v, search) !== undefined) out.add(v.name)
  }
  return out
}

function persistTouched(dashboardId: string, touched: ReadonlySet<string>) {
  try { localStorage.setItem(touchedStorageKey(dashboardId), JSON.stringify([...touched])) } catch { /* ignore quota */ }
}

function serializeValue(v: DashboardVariable, value: unknown): string[] {
  if (value === undefined || value === null) return []
  switch (v.type) {
    case 'multi_select':
      if (!Array.isArray(value)) return []
      return value.length ? value.map(String) : [NONE_SENTINEL]
    case 'date_range': return Array.isArray(value) && value.length === 2 ? [`${value[0]},${value[1]}`] : []
    case 'boolean': return [String(value)]
    case 'number': return [String(value)]
    default: return [String(value)]
  }
}

export function DashboardVariablesProvider({ dashboardId, variables, children, endpointBase, storageId, viewerConnectorId }: {
  dashboardId: string
  variables: DashboardVariable[]
  children: React.ReactNode
  /** Overrides the options endpoint base, e.g. `/api/v1/public/{token}` for public dashboards. */
  endpointBase?: string
  /** Overrides the localStorage namespace, e.g. a public token so visitors don't leak values across dashboards. */
  storageId?: string
  /** Viewer's dashboard connector selection, forwarded to query-backed option loads. */
  viewerConnectorId?: string | null
}) {
  const storageNamespace = storageId ?? dashboardId
  const optionsBase = endpointBase ?? `/api/v1/dashboards/${dashboardId}`
  const [searchParams, setSearchParams] = useSearchParams()
  const [values, setValuesState] = useState<Record<string, unknown>>(() => loadInitial(storageNamespace, variables, searchParams))
  // Names the viewer explicitly chose (URL pins at mount + setter calls).
  // Persisted next to the values so explicit choices survive a reload and are
  // never overwritten when a definition's default changes live.
  const [touched] = useState<Set<string>>(() => {
    const set = loadTouched(storageNamespace, variables, searchParams)
    persistTouched(storageNamespace, set)
    return set
  })
  const [optionState, setOptionState] = useState<Record<string, VariableOptionsState>>({})
  const valuesRef = useRef(values)
  valuesRef.current = values

  const persist = useCallback((next: Record<string, unknown>) => {
    try { localStorage.setItem(storageKey(storageNamespace), JSON.stringify(next)) } catch { /* ignore quota */ }
    const params = new URLSearchParams()
    for (const v of variables) {
      const serialized = serializeValue(v, next[v.name])
      // Defaults are omitted from the URL; anything else (including an
      // explicit "(none)") is encoded so shared links reproduce this view.
      if (!isVariableDefault(v, next[v.name])) serialized.forEach(s => params.append(v.name, s))
    }
    setSearchParams(params, { replace: true })
  }, [storageNamespace, variables, setSearchParams])

  const setValue = useCallback((name: string, value: unknown) => {
    // Persist outside the state updater: calling setSearchParams while React
    // renders another component triggers a "cannot update during render"
    // warning, and updaters may run twice under StrictMode.
    touched.add(name)
    persistTouched(storageNamespace, touched)
    const next = { ...valuesRef.current, [name]: value }
    valuesRef.current = next
    setValuesState(next)
    persist(next)
  }, [persist, storageNamespace, touched])

  const setValues = useCallback((next: Record<string, unknown>) => {
    for (const name of Object.keys(next)) touched.add(name)
    persistTouched(storageNamespace, touched)
    valuesRef.current = next
    setValuesState(next)
    persist(next)
  }, [persist, storageNamespace, touched])

  const resetAll = useCallback(() => {
    // Clearing touched returns every variable to "follow the default": the
    // values below are the current defaults, and later default changes apply.
    touched.clear()
    persistTouched(storageNamespace, touched)
    const next: Record<string, unknown> = {}
    for (const v of variables) next[v.name] = v.default
    valuesRef.current = next
    setValuesState(next)
    persist(next)
  }, [variables, persist, storageNamespace, touched])

  // Live definition changes: a variable the viewer never explicitly chose
  // adopts its new default when the current value is unset or still matches
  // the previous definition's default. Explicit choices, URL pins, and values
  // diverging from the previous default are left alone. This is what makes an
  // auto re-run after a live default edit actually return the new data.
  const previousVariablesRef = useRef<DashboardVariable[] | null>(null)
  useEffect(() => {
    const previous = previousVariablesRef.current
    previousVariablesRef.current = variables
    if (previous === null) return
    const previousByName = new Map(previous.map(v => [v.name, v]))
    const current = valuesRef.current
    let next: Record<string, unknown> | null = null
    for (const v of variables) {
      if (touched.has(v.name)) continue
      const value = current[v.name]
      const previousDef = previousByName.get(v.name)
      const mayAdoptDefault = value === undefined
        || (previousDef !== undefined && sameVariableValue(previousDef, value, previousDef.default))
      if (!mayAdoptDefault) continue
      if (sameVariableValue(v, value, v.default)) continue
      next ??= { ...current }
      next[v.name] = v.default
    }
    if (next) {
      valuesRef.current = next
      setValuesState(next)
      persist(next)
    }
  }, [variables, persist, touched])

  const loadOptions = useCallback(async (name: string) => {
    setOptionState(prev => ({ ...prev, [name]: { ...(prev[name] ?? { options: [] }), loading: true, error: null } }))
    try {
      const resp = await api.post<{ options: DashboardVariableOption[] }>(
        `${optionsBase}/variables/${name}/options`,
        { variables: valuesRef.current, connector_id: viewerConnectorId ?? undefined },
      )
      setOptionState(prev => ({ ...prev, [name]: { options: resp.options, loading: false, error: null, reload: () => { void loadOptions(name) } } }))
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to load options'
      setOptionState(prev => ({ ...prev, [name]: { options: [], loading: false, error: message, reload: () => { void loadOptions(name) } } }))
    }
  }, [optionsBase, viewerConnectorId])

  // Reload query-backed options on mount and whenever a dependency changes,
  // including the viewer's connector selection (options must agree with the
  // service that widget execution will use).
  const depSignature = JSON.stringify(variables.map(v => ({
    name: v.name,
    deps: (v.depends_on ?? []).map(d => [d, values[d]]),
  })))
  useEffect(() => {
    const timer = setTimeout(() => {
      for (const v of variables) {
        if (v.options?.mode === 'query') void loadOptions(v.name)
      }
    }, 300)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [depSignature, viewerConnectorId])

  // Seed static options so controls render immediately.
  const optionStateWithStatic = useMemo(() => {
    const out = { ...optionState }
    for (const v of variables) {
      if (v.options?.mode === 'static' && !out[v.name]) {
        out[v.name] = { options: v.options.values ?? [], loading: false, error: null, reload: () => {} }
      }
    }
    return out
  }, [optionState, variables])

  const value = useMemo(
    () => ({ variables, values, setValue, setValues, resetAll, optionState: optionStateWithStatic }),
    [variables, values, setValue, setValues, resetAll, optionStateWithStatic],
  )

  return (
    <Ctx.Provider value={value}>
      {children}
    </Ctx.Provider>
  )
}

export function useDashboardVariables() { return useContext(Ctx) }
