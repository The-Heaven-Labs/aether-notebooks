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
  optionState: Record<string, VariableOptionsState>
}

const Ctx = createContext<DashboardVariablesContextValue>({
  variables: [], values: {}, setValue: () => {}, setValues: () => {}, optionState: {},
})

function storageKey(dashboardId: string) { return `aether_dashvars_${dashboardId}` }

function parseSearchValue(v: DashboardVariable, search: URLSearchParams): unknown {
  const all = search.getAll(v.name)
  if (!all.length) return undefined
  switch (v.type) {
    case 'multi_select': return all
    case 'number': { const n = Number(all[0]); return Number.isFinite(n) ? n : undefined }
    case 'boolean': return all[0] === 'true' ? true : all[0] === 'false' ? false : undefined
    case 'date_range': return all[0].includes(',') ? all[0].split(',', 2) : undefined
    default: return all[0]
  }
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

function serializeValue(v: DashboardVariable, value: unknown): string[] {
  if (value === undefined || value === null) return []
  switch (v.type) {
    case 'multi_select': return Array.isArray(value) ? value.map(String) : []
    case 'date_range': return Array.isArray(value) && value.length === 2 ? [`${value[0]},${value[1]}`] : []
    case 'boolean': return [String(value)]
    case 'number': return [String(value)]
    default: return [String(value)]
  }
}

export function DashboardVariablesProvider({ dashboardId, variables, children, endpointBase, storageId }: {
  dashboardId: string
  variables: DashboardVariable[]
  children: React.ReactNode
  /** Overrides the options endpoint base, e.g. `/api/v1/public/{token}` for public dashboards. */
  endpointBase?: string
  /** Overrides the localStorage namespace, e.g. a public token so visitors don't leak values across dashboards. */
  storageId?: string
}) {
  const storageNamespace = storageId ?? dashboardId
  const optionsBase = endpointBase ?? `/api/v1/dashboards/${dashboardId}`
  const [searchParams, setSearchParams] = useSearchParams()
  const [values, setValuesState] = useState<Record<string, unknown>>(() => loadInitial(storageNamespace, variables, searchParams))
  const [optionState, setOptionState] = useState<Record<string, VariableOptionsState>>({})
  const valuesRef = useRef(values)
  valuesRef.current = values

  const persist = useCallback((next: Record<string, unknown>) => {
    try { localStorage.setItem(storageKey(storageNamespace), JSON.stringify(next)) } catch { /* ignore quota */ }
    const params = new URLSearchParams()
    for (const v of variables) {
      const serialized = serializeValue(v, next[v.name])
      const isDefault = JSON.stringify(next[v.name]) === JSON.stringify(v.default)
      if (!isDefault) serialized.forEach(s => params.append(v.name, s))
    }
    setSearchParams(params, { replace: true })
  }, [storageNamespace, variables, setSearchParams])

  const setValue = useCallback((name: string, value: unknown) => {
    setValuesState(prev => {
      const next = { ...prev, [name]: value }
      persist(next)
      return next
    })
  }, [persist])

  const setValues = useCallback((next: Record<string, unknown>) => {
    setValuesState(next); persist(next)
  }, [persist])

  const loadOptions = useCallback(async (name: string) => {
    setOptionState(prev => ({ ...prev, [name]: { ...(prev[name] ?? { options: [] }), loading: true, error: null } }))
    try {
      const resp = await api.post<{ options: DashboardVariableOption[] }>(
        `${optionsBase}/variables/${name}/options`,
        { variables: valuesRef.current },
      )
      setOptionState(prev => ({ ...prev, [name]: { options: resp.options, loading: false, error: null, reload: () => { void loadOptions(name) } } }))
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to load options'
      setOptionState(prev => ({ ...prev, [name]: { options: [], loading: false, error: message, reload: () => { void loadOptions(name) } } }))
    }
  }, [optionsBase])

  // Reload query-backed options on mount and whenever a dependency changes.
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
  }, [depSignature])

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
    () => ({ variables, values, setValue, setValues, optionState: optionStateWithStatic }),
    [variables, values, setValue, setValues, optionStateWithStatic],
  )

  return (
    <Ctx.Provider value={value}>
      {children}
    </Ctx.Provider>
  )
}

export function useDashboardVariables() { return useContext(Ctx) }
