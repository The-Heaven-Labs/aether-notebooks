import { describe, test, expect, vi, afterEach } from 'vitest'
import {
  AUTO_RERUN_DEBOUNCE_MS,
  createPerWidgetDebouncer,
  diffVariableDefinitions,
  diffWidgetRuns,
  referencedVariables,
  variableDefinitionKey,
  variableTokens,
  widgetRunSignature,
  widgetsReferencingTokens,
} from './dashboardRunSignature'
import type { RunSignatureWidget } from './dashboardRunSignature'
import type { DashboardVariable } from '../types'

function widget(overrides: Partial<RunSignatureWidget> = {}): RunSignatureWidget {
  return {
    id: 'w1',
    connector_id: 'c1',
    language: 'sql',
    query: 'SELECT 1',
    notebook_id: null,
    cell_id: null,
    ...overrides,
  }
}

function variable(overrides: Partial<DashboardVariable> = {}): DashboardVariable {
  return { name: 'region', type: 'text', default: 'us', ...overrides }
}

describe('widgetRunSignature', () => {
  test('combines connector, language, and query', () => {
    expect(widgetRunSignature(widget())).toBe('c1|sql|SELECT 1')
  })

  test('unset fields collapse to empty parts', () => {
    expect(widgetRunSignature(widget({ connector_id: null, language: null, query: null }))).toBe('||')
  })

  test('only run-relevant fields participate', () => {
    const a = widget({ query: 'SELECT 1' })
    const b = { ...a, id: 'w2', layout: { row: 4, col: 4, width: 2, height: 2 } }
    expect(widgetRunSignature(a)).toBe(widgetRunSignature(b as RunSignatureWidget))
  })
})

describe('referencedVariables', () => {
  test('extracts tokens and tolerates whitespace', () => {
    expect(referencedVariables('SELECT * FROM t WHERE a = {{region}} AND b = {{ env }}')).toEqual(['region', 'env'])
  })

  test('deduplicates repeated references', () => {
    expect(referencedVariables('SELECT {{x}}, {{x}} FROM t WHERE y = {{x}}')).toEqual(['x'])
  })

  test('supports the server name grammar (letters, digits, underscore, dash)', () => {
    expect(referencedVariables('SELECT {{a_b-9}}')).toEqual(['a_b-9'])
  })

  test('ignores non-token braces and empty names', () => {
    expect(referencedVariables("SELECT '{a}', {{}}, {{a b}}, {x} FROM t")).toEqual([])
  })

  test('handles null and empty SQL', () => {
    expect(referencedVariables(null)).toEqual([])
    expect(referencedVariables(undefined)).toEqual([])
    expect(referencedVariables('')).toEqual([])
  })
})

describe('diffWidgetRuns', () => {
  test('the first sync only seeds baselines — the takeover never re-runs', () => {
    const diff = diffWidgetRuns(null, [widget()])
    expect(diff.rerun).toEqual([])
    expect(diff.cellRefChanged).toEqual([])
    expect(diff.removed).toEqual([])
    expect(diff.next.w1).toEqual({ signature: 'c1|sql|SELECT 1', notebookId: null, cellId: null })
  })

  test('unchanged widgets (new array identity) produce no work', () => {
    const first = diffWidgetRuns(null, [widget()])
    const second = diffWidgetRuns(first.next, [widget()])
    expect(second.rerun).toEqual([])
    expect(second.cellRefChanged).toEqual([])
    expect(second.removed).toEqual([])
  })

  test('a layout/config-only change produces no work', () => {
    const first = diffWidgetRuns(null, [widget()])
    const moved = { ...widget(), layout: { row: 9, col: 9, width: 1, height: 1 }, config: { title: 'New' } }
    const second = diffWidgetRuns(first.next, [moved as RunSignatureWidget])
    expect(second.rerun).toEqual([])
    expect(second.cellRefChanged).toEqual([])
  })

  test('a query change re-runs exactly that widget', () => {
    const first = diffWidgetRuns(null, [widget({ id: 'w1' }), widget({ id: 'w2', query: 'SELECT 2' })])
    const second = diffWidgetRuns(first.next, [widget({ id: 'w1', query: 'SELECT 1 -- edited' }), widget({ id: 'w2', query: 'SELECT 2' })])
    expect(second.rerun).toEqual(['w1'])
  })

  test('connector and language changes re-run the widget', () => {
    const first = diffWidgetRuns(null, [widget()])
    expect(diffWidgetRuns(first.next, [widget({ connector_id: 'c2' })]).rerun).toEqual(['w1'])
    expect(diffWidgetRuns(first.next, [widget({ language: 'javascript' })]).rerun).toEqual(['w1'])
  })

  test('a cell reference change invalidates and never re-runs', () => {
    const first = diffWidgetRuns(null, [widget({ query: null, connector_id: null, notebook_id: 'nb-1', cell_id: 'cell-1' })])
    const second = diffWidgetRuns(first.next, [widget({ query: null, connector_id: null, notebook_id: 'nb-2', cell_id: 'cell-2' })])
    expect(second.rerun).toEqual([])
    expect(second.cellRefChanged).toEqual([{ widgetId: 'w1', notebookId: 'nb-2' }])
  })

  test('a removed widget is reported for pending-run cleanup', () => {
    const first = diffWidgetRuns(null, [widget({ id: 'w1' }), widget({ id: 'w2' })])
    const second = diffWidgetRuns(first.next, [widget({ id: 'w2' })])
    expect(second.removed).toEqual(['w1'])
    expect(second.next.w1).toBeUndefined()
  })

  test('a newly added widget is seeded without a run (mount fetches it)', () => {
    const first = diffWidgetRuns(null, [widget({ id: 'w1' })])
    const second = diffWidgetRuns(first.next, [widget({ id: 'w1' }), widget({ id: 'w2', query: 'SELECT 2' })])
    expect(second.rerun).toEqual([])
    expect(second.next.w2?.signature).toBe('c1|sql|SELECT 2')
  })

  test('a cell-to-query conversion reports both the re-run and the ref change', () => {
    const first = diffWidgetRuns(null, [widget({ connector_id: null, query: null, notebook_id: 'nb-1', cell_id: 'cell-1' })])
    const second = diffWidgetRuns(first.next, [widget({ connector_id: 'c1', query: 'SELECT 1' })])
    expect(second.rerun).toEqual(['w1'])
    expect(second.cellRefChanged).toEqual([{ widgetId: 'w1', notebookId: null }])
  })
})

describe('variable definitions', () => {
  test('variableTokens expands date ranges to their _start/_end placeholders', () => {
    expect(variableTokens({ name: 'region', type: 'text' })).toEqual(['region'])
    expect(variableTokens({ name: 'range', type: 'date_range' })).toEqual(['range_start', 'range_end'])
  })

  test('the definition key ignores label edits', () => {
    const a = variable()
    const b = variable({ label: 'Sales region' })
    expect(variableDefinitionKey(a)).toBe(variableDefinitionKey(b))
  })

  test('the definition key tracks execution-relevant fields', () => {
    expect(variableDefinitionKey(variable())).not.toBe(variableDefinitionKey(variable({ default: 'eu' })))
    expect(variableDefinitionKey(variable())).not.toBe(variableDefinitionKey(variable({ type: 'number' })))
    expect(variableDefinitionKey(variable())).not.toBe(variableDefinitionKey(variable({ required: true })))
  })

  test('the first call seeds without reporting tokens', () => {
    const diff = diffVariableDefinitions(null, [variable()])
    expect(diff.tokens).toEqual([])
    expect(diff.next.region?.key).toBe(variableDefinitionKey(variable()))
  })

  test('an unchanged definition reports nothing', () => {
    const first = diffVariableDefinitions(null, [variable()])
    const second = diffVariableDefinitions(first.next, [variable()])
    expect(second.tokens).toEqual([])
  })

  test('a label-only edit reports nothing', () => {
    const first = diffVariableDefinitions(null, [variable()])
    const second = diffVariableDefinitions(first.next, [variable({ label: 'Region!' })])
    expect(second.tokens).toEqual([])
  })

  test('a default change reports the changed token', () => {
    const first = diffVariableDefinitions(null, [variable()])
    const second = diffVariableDefinitions(first.next, [variable({ default: 'eu' })])
    expect(second.tokens).toEqual(['region'])
  })

  test('a date_range change reports both placeholders', () => {
    const range = variable({ name: 'range', type: 'date_range', default: ['2026-01-01', '2026-01-31'] })
    const first = diffVariableDefinitions(null, [range])
    const second = diffVariableDefinitions(first.next, [variable({ ...range, default: ['2026-02-01', '2026-02-28'] })])
    expect(second.tokens.sort()).toEqual(['range_end', 'range_start'])
  })

  test('a type change reports old and new tokens', () => {
    const first = diffVariableDefinitions(null, [variable({ name: 'range', type: 'text', default: 'x' })])
    const second = diffVariableDefinitions(first.next, [variable({ name: 'range', type: 'date_range', default: ['a', 'b'] })])
    expect(second.tokens.sort()).toEqual(['range', 'range_end', 'range_start'])
  })

  test('an added variable reports its token; a removed one reports the old token', () => {
    const first = diffVariableDefinitions(null, [variable({ name: 'region' })])
    const added = diffVariableDefinitions(first.next, [variable({ name: 'region' }), variable({ name: 'env' })])
    expect(added.tokens).toEqual(['env'])
    const removed = diffVariableDefinitions(added.next, [variable({ name: 'region' })])
    expect(removed.tokens).toEqual(['env'])
  })
})

describe('widgetsReferencingTokens', () => {
  test('returns only widgets whose query references a changed token', () => {
    const widgets = [
      widget({ id: 'w1', query: 'SELECT * FROM t WHERE r = {{region}}' }),
      widget({ id: 'w2', query: 'SELECT * FROM t WHERE e = {{env}}' }),
      widget({ id: 'w3', query: 'SELECT 1' }),
    ]
    expect(widgetsReferencingTokens(widgets, ['region'])).toEqual(['w1'])
  })

  test('matches date_range placeholders', () => {
    const widgets = [widget({ id: 'w1', query: 'WHERE ts >= {{range_start}} AND ts < {{range_end}}' })]
    expect(widgetsReferencingTokens(widgets, ['range_start', 'range_end'])).toEqual(['w1'])
    expect(widgetsReferencingTokens(widgets, ['range'])).toEqual([])
  })

  test('no tokens means no widgets', () => {
    expect(widgetsReferencingTokens([widget()], [])).toEqual([])
  })
})

describe('createPerWidgetDebouncer', () => {
  afterEach(() => vi.useRealTimers())

  test('coalesces a burst into a single run after the delay', () => {
    vi.useFakeTimers()
    const run = vi.fn()
    const debouncer = createPerWidgetDebouncer(run, AUTO_RERUN_DEBOUNCE_MS)

    debouncer.schedule('w1')
    vi.advanceTimersByTime(1000)
    debouncer.schedule('w1')
    vi.advanceTimersByTime(1000)
    debouncer.schedule('w1')

    expect(run).not.toHaveBeenCalled()
    expect(debouncer.isPending('w1')).toBe(true)

    vi.advanceTimersByTime(AUTO_RERUN_DEBOUNCE_MS)
    expect(run).toHaveBeenCalledTimes(1)
    expect(run).toHaveBeenCalledWith('w1')
    expect(debouncer.isPending('w1')).toBe(false)
  })

  test('tracks widgets independently', () => {
    vi.useFakeTimers()
    const run = vi.fn()
    const debouncer = createPerWidgetDebouncer(run, 100)

    debouncer.schedule('w1')
    vi.advanceTimersByTime(50)
    debouncer.schedule('w2')
    vi.advanceTimersByTime(50)
    expect(run).toHaveBeenCalledTimes(1)
    expect(run).toHaveBeenCalledWith('w1')

    vi.advanceTimersByTime(50)
    expect(run).toHaveBeenCalledTimes(2)
    expect(run).toHaveBeenLastCalledWith('w2')
  })

  test('cancel drops a pending run', () => {
    vi.useFakeTimers()
    const run = vi.fn()
    const debouncer = createPerWidgetDebouncer(run, 100)

    debouncer.schedule('w1')
    debouncer.cancel('w1')
    expect(debouncer.isPending('w1')).toBe(false)
    vi.advanceTimersByTime(500)
    expect(run).not.toHaveBeenCalled()
  })

  test('cancelAll drops every pending run', () => {
    vi.useFakeTimers()
    const run = vi.fn()
    const debouncer = createPerWidgetDebouncer(run, 100)

    debouncer.schedule('w1')
    debouncer.schedule('w2')
    debouncer.cancelAll()
    vi.advanceTimersByTime(500)
    expect(run).not.toHaveBeenCalled()
  })

  test('re-arms after a run has fired', () => {
    vi.useFakeTimers()
    const run = vi.fn()
    const debouncer = createPerWidgetDebouncer(run, 100)

    debouncer.schedule('w1')
    vi.advanceTimersByTime(100)
    debouncer.schedule('w1')
    vi.advanceTimersByTime(100)
    expect(run).toHaveBeenCalledTimes(2)
  })
})
