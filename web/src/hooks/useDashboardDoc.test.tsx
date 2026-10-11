import { describe, test, expect, beforeEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import * as Y from 'yjs'
import { useDashboardDoc } from './useDashboardDoc'
import type { UseDashboardDocResult } from './useDashboardDoc'
import type { Widget } from '../types'

// A fake Hocuspocus provider: records its configuration, tracks listeners and
// the awareness `user` field, and never opens a socket. The hook's lazy
// `import('../components/dashboardCollabRuntime')` therefore exercises the real
// runtime (ref-counting, projection, mutators) with no network.
const fake = vi.hoisted(() => {
  type Listener = (payload?: unknown) => void

  interface FakeConfiguration {
    name?: string
    token?: string
    url?: string
    document?: unknown
  }

  class FakeHocuspocusProvider {
    static instances: FakeHocuspocusProvider[] = []

    readonly configuration: FakeConfiguration
    readonly doc: unknown
    destroyed = false
    readonly localStateFields: Record<string, unknown> = {}
    readonly awareness: { setLocalStateField: (field: string, value: unknown) => void }
    private readonly listeners = new Map<string, Set<Listener>>()

    constructor(configuration: FakeConfiguration) {
      this.configuration = configuration
      this.doc = configuration.document
      this.awareness = {
        setLocalStateField: (field, value) => { this.localStateFields[field] = value },
      }
      FakeHocuspocusProvider.instances.push(this)
    }

    on(event: string, listener: Listener): void {
      let set = this.listeners.get(event)
      if (!set) {
        set = new Set()
        this.listeners.set(event, set)
      }
      set.add(listener)
    }

    off(event: string, listener: Listener): void {
      this.listeners.get(event)?.delete(listener)
    }

    emit(event: string, payload?: unknown): void {
      this.listeners.get(event)?.forEach((listener) => listener(payload))
    }

    listenerCount(event: string): number {
      return this.listeners.get(event)?.size ?? 0
    }

    destroy(): void {
      this.destroyed = true
    }
  }

  return { FakeHocuspocusProvider }
})

vi.mock('@hocuspocus/provider', () => ({ HocuspocusProvider: fake.FakeHocuspocusProvider }))

const DASHBOARD_ID = 'dash-1'

type FakeProvider = InstanceType<typeof fake.FakeHocuspocusProvider>

function latestProvider(): FakeProvider {
  const provider = fake.FakeHocuspocusProvider.instances.at(-1)
  if (!provider) throw new Error('no fake provider created')
  return provider
}

function sharedDoc(): Y.Doc {
  return latestProvider().doc as Y.Doc
}

/**
 * Renders the hook and waits until it has acquired the shared document AND
 * subscribed to the provider (the runtime registers one `synced` listener at
 * creation; the hook's external-store subscription adds the second).
 */
async function renderDashboardDoc(
  dashboardId = DASHBOARD_ID,
  options?: { enabled?: boolean },
): Promise<ReturnType<typeof renderHook<UseDashboardDocResult, unknown>>> {
  const rendered = renderHook(() => useDashboardDoc(dashboardId, options))
  await waitFor(() => expect(fake.FakeHocuspocusProvider.instances).toHaveLength(1))
  const provider = latestProvider()
  await waitFor(() => expect(provider.listenerCount('synced')).toBeGreaterThanOrEqual(2))
  return rendered
}

function seedWidget(
  doc: Y.Doc,
  id: string,
  options: { query?: string; config?: string; layout?: Partial<Widget['layout']> } = {},
): void {
  doc.transact(() => {
    const wm = new Y.Map<unknown>()
    wm.set('type', 'chart')
    wm.set('language', 'sql')
    wm.set('connector_id', 'conn-1')
    wm.set('notebook_id', '')
    wm.set('cell_id', '')
    wm.set('config', options.config ?? '{}')
    const layout = new Y.Map<unknown>()
    layout.set('row', options.layout?.row ?? 0)
    layout.set('col', options.layout?.col ?? 0)
    layout.set('width', options.layout?.width ?? 6)
    layout.set('height', options.layout?.height ?? 6)
    wm.set('layout', layout)
    wm.set('query', new Y.Text(options.query ?? 'SELECT 1'))
    doc.getMap('widgets').set(id, wm)
  })
}

beforeEach(() => {
  fake.FakeHocuspocusProvider.instances.length = 0
  localStorage.clear()
  localStorage.setItem('aether_token', 'test-token')
  localStorage.setItem('aether_user_name', 'Nova')
  localStorage.setItem('aether_user_email', 'nova@example.com')
})

describe('useDashboardDoc', () => {
  test('projects the document and re-renders on local transactions', async () => {
    const { result } = await renderDashboardDoc()
    const doc = sharedDoc()

    act(() => {
      doc.transact(() => {
        doc.getMap('meta').set('title', 'Ops dashboard')
        const settings = new Y.Map<unknown>()
        settings.set('grid_cols', 12)
        settings.set('public_live', false)
        doc.getMap('meta').set('settings', settings)
        const variables = new Y.Array<unknown>()
        const variable = new Y.Map<unknown>()
        variable.set('name', 'region')
        variable.set('type', 'text')
        variables.push([variable])
        doc.getMap('meta').set('variables', variables)
      })
      seedWidget(doc, 'w-1', { query: 'SELECT 1' })
    })

    await waitFor(() => expect(result.current.title).toBe('Ops dashboard'))
    expect(result.current.settings).toEqual({ grid_cols: 12, public_live: false })
    expect(result.current.variables).toEqual([{ name: 'region', type: 'text' }])
    expect(result.current.widgets).toHaveLength(1)
    expect(result.current.widgets[0]).toMatchObject({
      id: 'w-1',
      dashboard_id: DASHBOARD_ID,
      type: 'chart',
      language: 'sql',
      connector_id: 'conn-1',
      notebook_id: null,
      cell_id: null,
      query: 'SELECT 1',
      layout: { row: 0, col: 0, width: 6, height: 6 },
      config: {},
      created_at: '',
    })

    // A subsequent local transaction re-renders with the new projection.
    act(() => { doc.getMap('meta').set('title', 'Renamed') })
    await waitFor(() => expect(result.current.title).toBe('Renamed'))
  })

  test('applies a remote update and re-renders', async () => {
    const { result } = await renderDashboardDoc()
    const doc = sharedDoc()

    const remote = new Y.Doc()
    remote.transact(() => {
      remote.getMap('meta').set('title', 'From a collaborator')
    })
    const update = Y.encodeStateAsUpdate(remote)

    act(() => { Y.applyUpdate(doc, update) })
    await waitFor(() => expect(result.current.title).toBe('From a collaborator'))
  })

  test('mutators write layout fields, query text, config string, and a debounced title', async () => {
    const { result } = await renderDashboardDoc(DASHBOARD_ID, { enabled: true })
    const doc = sharedDoc()
    act(() => { seedWidget(doc, 'w-1', { query: 'SELECT 1' }) })
    await waitFor(() => expect(result.current.widgets).toHaveLength(1))

    act(() => { result.current.updateLayout('w-1', { row: 2, col: 3, width: 4, height: 5 }) })
    const wm = doc.getMap('widgets').get('w-1') as Y.Map<unknown>
    const layout = wm.get('layout') as Y.Map<unknown>
    expect(layout.get('row')).toBe(2)
    expect(layout.get('col')).toBe(3)
    expect(layout.get('width')).toBe(4)
    expect(layout.get('height')).toBe(5)

    act(() => { result.current.setQuery('w-1', 'SELECT 2') })
    expect((wm.get('query') as Y.Text).toString()).toBe('SELECT 2')
    await waitFor(() => expect(result.current.widgets[0].query).toBe('SELECT 2'))

    act(() => { result.current.setConfig('w-1', { chart: 'bar' }) })
    expect(wm.get('config')).toBe('{"chart":"bar"}')

    act(() => { result.current.setTitle('New title') })
    // Debounced: the write is not applied synchronously.
    expect(doc.getMap('meta').get('title')).toBeUndefined()
    await waitFor(() => expect(doc.getMap('meta').get('title')).toBe('New title'))
    await waitFor(() => expect(result.current.title).toBe('New title'))
  })

  test('mutators are no-ops unless editing is enabled', async () => {
    const { result } = await renderDashboardDoc()
    const doc = sharedDoc()
    act(() => { seedWidget(doc, 'w-1', { query: 'SELECT 1' }) })
    await waitFor(() => expect(result.current.widgets).toHaveLength(1))

    act(() => {
      result.current.updateLayout('w-1', { row: 9, col: 9, width: 1, height: 1 })
      result.current.setQuery('w-1', 'SELECT secret')
      result.current.setConfig('w-1', { chart: 'pie' })
      result.current.setTitle('Nope')
    })

    const wm = doc.getMap('widgets').get('w-1') as Y.Map<unknown>
    expect((wm.get('layout') as Y.Map<unknown>).get('row')).toBe(0)
    expect((wm.get('query') as Y.Text).toString()).toBe('SELECT 1')
    expect(wm.get('config')).toBe('{}')
    expect(doc.getMap('meta').get('title')).toBeUndefined()
  })

  test('tracks synced and connected from provider events', async () => {
    const { result } = await renderDashboardDoc()
    const provider = latestProvider()

    expect(result.current.synced).toBe(false)
    expect(result.current.connected).toBe(false)

    act(() => { provider.emit('synced', { state: true }) })
    await waitFor(() => expect(result.current.synced).toBe(true))

    act(() => { provider.emit('status', { status: 'connected' }) })
    await waitFor(() => expect(result.current.connected).toBe(true))

    act(() => { provider.emit('status', { status: 'disconnected' }) })
    await waitFor(() => expect(result.current.connected).toBe(false))
    // synced latches across reconnects.
    expect(result.current.synced).toBe(true)
  })

  test('shares one provider per dashboard, sets the awareness user, and releases on the last unmount', async () => {
    const first = await renderDashboardDoc()
    const provider = latestProvider()

    expect(provider.configuration.name).toBe(`dashboard:${DASHBOARD_ID}`)
    expect(provider.configuration.token).toBe('test-token')
    const user = provider.localStateFields.user as { name: string; email: string; color: string }
    expect(user.name).toBe('Nova')
    expect(user.email).toBe('nova@example.com')
    expect(user.color).toMatch(/^hsl\(\d+, 70%, 55%\)$/)
    expect(first.result.current.awareness).toBe(provider.awareness)

    const second = renderHook(() => useDashboardDoc(DASHBOARD_ID))
    const doc = sharedDoc()
    // Observe a doc write through the second hook to prove it acquired the
    // same entry (rather than creating a second provider).
    act(() => { doc.getMap('meta').set('title', 'Shared') })
    await waitFor(() => expect(second.result.current.title).toBe('Shared'))
    expect(fake.FakeHocuspocusProvider.instances).toHaveLength(1)

    first.unmount()
    expect(provider.destroyed).toBe(false)
    second.unmount()
    expect(provider.destroyed).toBe(true)
  })
})
