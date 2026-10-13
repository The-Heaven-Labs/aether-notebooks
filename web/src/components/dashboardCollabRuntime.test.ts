import { describe, test, expect, vi } from 'vitest'
import * as Y from 'yjs'
import { projectDashboardDoc, updateWidgetLayout, setWidgetTab } from './dashboardCollabRuntime'
import type { DashboardCollab } from './dashboardCollabRuntime'

// The runtime imports the Hocuspocus provider at module load; these tests
// drive the shared Y.Doc directly, so the socket stack is stubbed out.
vi.mock('@hocuspocus/provider', () => ({ HocuspocusProvider: class {} }))

function fakeCollab(doc: Y.Doc): DashboardCollab {
  return {
    doc,
    provider: {} as DashboardCollab['provider'],
    refCount: 1,
    synced: true,
    connected: true,
  }
}

/** Seeds one table widget; `layout` entries override the default grid fields. */
function seedWidget(doc: Y.Doc, layout: Record<string, unknown> = {}): void {
  const wm = new Y.Map<unknown>()
  wm.set('type', 'table')
  wm.set('language', 'sql')
  wm.set('config', '{}')
  const lm = new Y.Map<unknown>()
  lm.set('row', 0)
  lm.set('col', 0)
  lm.set('width', 6)
  lm.set('height', 4)
  for (const [key, value] of Object.entries(layout)) lm.set(key, value)
  wm.set('layout', lm)
  doc.getMap('widgets').set('w1', wm)
}

describe('projectLayout', () => {
  test('projects layout.tab when present', () => {
    const doc = new Y.Doc()
    seedWidget(doc, { tab: 'overview' })

    const proj = projectDashboardDoc(doc, 'd1')
    expect(proj.widgets).toHaveLength(1)
    expect(proj.widgets[0].layout).toEqual({ row: 0, col: 0, width: 6, height: 4, tab: 'overview' })
  })

  test('omits layout.tab when absent or not a string', () => {
    const doc = new Y.Doc()
    seedWidget(doc, { tab: 7 })

    const layout = projectDashboardDoc(doc, 'd1').widgets[0].layout
    expect(layout).toEqual({ row: 0, col: 0, width: 6, height: 4 })
    expect('tab' in layout).toBe(false)
  })
})

describe('updateWidgetLayout', () => {
  test('writes tab alongside the grid fields', () => {
    const doc = new Y.Doc()
    seedWidget(doc)

    updateWidgetLayout(fakeCollab(doc), 'w1', { row: 1, col: 2, width: 3, height: 4, tab: 'deep-dive' })

    expect(projectDashboardDoc(doc, 'd1').widgets[0].layout).toEqual(
      { row: 1, col: 2, width: 3, height: 4, tab: 'deep-dive' },
    )
  })

  test('an empty tab removes the key', () => {
    const doc = new Y.Doc()
    seedWidget(doc, { tab: 'overview' })

    updateWidgetLayout(fakeCollab(doc), 'w1', { row: 0, col: 0, width: 6, height: 4 })

    const wm = doc.getMap('widgets').get('w1') as Y.Map<unknown>
    const lm = wm.get('layout') as Y.Map<unknown>
    expect(lm.has('tab')).toBe(false)
    expect('tab' in projectDashboardDoc(doc, 'd1').widgets[0].layout).toBe(false)
  })
})

describe('setWidgetTab', () => {
  test('sets the tab and preserves the current grid fields', () => {
    const doc = new Y.Doc()
    seedWidget(doc, { row: 2, col: 3, width: 8, height: 5 })

    setWidgetTab(fakeCollab(doc), 'w1', 'deep-dive')

    expect(projectDashboardDoc(doc, 'd1').widgets[0].layout).toEqual(
      { row: 2, col: 3, width: 8, height: 5, tab: 'deep-dive' },
    )
  })

  test('replaces an existing tab', () => {
    const doc = new Y.Doc()
    seedWidget(doc, { tab: 'overview' })

    setWidgetTab(fakeCollab(doc), 'w1', 'details')

    expect(projectDashboardDoc(doc, 'd1').widgets[0].layout.tab).toBe('details')
  })

  test('an empty tab clears the placement', () => {
    const doc = new Y.Doc()
    seedWidget(doc, { tab: 'overview' })

    setWidgetTab(fakeCollab(doc), 'w1', '')

    const wm = doc.getMap('widgets').get('w1') as Y.Map<unknown>
    expect((wm.get('layout') as Y.Map<unknown>).has('tab')).toBe(false)
  })

  test('a missing widget is ignored', () => {
    const doc = new Y.Doc()
    seedWidget(doc)

    expect(() => setWidgetTab(fakeCollab(doc), 'nope', 'overview')).not.toThrow()
    expect(projectDashboardDoc(doc, 'd1').widgets[0].layout.tab).toBeUndefined()
  })
})

describe('projectDashboardDoc widget types', () => {
  test('projects an html panel (the editor must never drop the wave-3 type)', () => {
    const doc = new Y.Doc()
    const wm = new Y.Map<unknown>()
    wm.set('type', 'html')
    wm.set('language', 'sql')
    wm.set('config', JSON.stringify({ html: '<p>hi</p>', css: 'p{}' }))
    wm.set('notebook_id', '')
    wm.set('cell_id', '')
    wm.set('connector_id', '')
    const lm = new Y.Map<unknown>()
    lm.set('row', 0)
    lm.set('col', 0)
    lm.set('width', 4)
    lm.set('height', 4)
    wm.set('layout', lm)
    doc.getMap('widgets').set('w-html', wm)

    const proj = projectDashboardDoc(doc, 'd1')
    expect(proj.widgets).toHaveLength(1)
    expect(proj.widgets[0].type).toBe('html')
    expect(proj.widgets[0].config).toEqual({ html: '<p>hi</p>', css: 'p{}' })
    expect(proj.widgets[0].notebook_id).toBeNull()
    expect(proj.widgets[0].connector_id).toBeNull()
  })

  test('projects a custom component (the editor must never drop the wave-4 type)', () => {
    const doc = new Y.Doc()
    const wm = new Y.Map<unknown>()
    wm.set('type', 'custom')
    wm.set('language', 'sql')
    wm.set('config', JSON.stringify({ html: '<p>x</p>', css: 'p{}', js: '1', bindings: [] }))
    wm.set('notebook_id', '')
    wm.set('cell_id', '')
    wm.set('connector_id', '')
    const lm = new Y.Map<unknown>()
    lm.set('row', 0)
    lm.set('col', 0)
    lm.set('width', 4)
    lm.set('height', 4)
    wm.set('layout', lm)
    doc.getMap('widgets').set('w-custom', wm)

    const proj = projectDashboardDoc(doc, 'd1')
    expect(proj.widgets).toHaveLength(1)
    expect(proj.widgets[0].type).toBe('custom')
    expect(proj.widgets[0].config).toEqual({ html: '<p>x</p>', css: 'p{}', js: '1', bindings: [] })
    expect(proj.widgets[0].notebook_id).toBeNull()
    expect(proj.widgets[0].connector_id).toBeNull()
  })
})
