import { test, expect } from '@playwright/test'
import type { APIRequestContext, Browser, BrowserContext, Page } from '@playwright/test'

/**
 * Two-context live dashboard collaboration (plan Task 24).
 *
 * Proves that edits made by one browser are visible live in another:
 *  1. a SQL edit in the widget drawer re-runs the widget in the viewer,
 *  2. a widget drag moves the widget in the viewer without re-executing it,
 *  3. a widget added over REST appears in the viewer,
 *  4. a view-only user gets disabled mutation controls.
 *
 * Requires the dev stack (API :8088, relay :3001, Vite :5173). The Vite dev
 * server does not inject `window.__AETHER_CONFIG__`, so every context injects
 * the relay URL before the app loads (test-only workaround for the dev gap).
 */

const RELAY_URL = process.env.E2E_RELAY_URL ?? 'ws://localhost:3001'
/** The API runs natively in dev, so `localhost` reaches the dockerized Postgres. */
const PG_CONNECTOR_HOST = process.env.E2E_PG_CONNECTOR_HOST ?? 'localhost'

const NOVA = { email: 'nova@heaven-labs.com', password: 'nova123' }
const SOL = { email: 'sol@heaven-labs.com', password: 'sol123' }

interface Session {
  token: string
  email: string
  name: string
  id: string
}

interface LoginResponse {
  token: string
  user: { id: string; email: string; name: string }
}

let fixtureSeq = 0
const openContexts: BrowserContext[] = []

test.afterEach(async () => {
  await Promise.all(openContexts.splice(0).map((ctx) => ctx.close()))
})

async function apiLogin(request: APIRequestContext, creds: { email: string; password: string }): Promise<Session> {
  const resp = await request.post('/api/v1/auth/login', { data: creds })
  expect(resp.ok()).toBeTruthy()
  const body = (await resp.json()) as LoginResponse
  return { token: body.token, email: body.user.email, name: body.user.name, id: body.user.id }
}

/**
 * Login sessions are cached across the file: `/auth/login` is rate-limited to
 * 10 requests per minute per client IP in dev, so re-authenticating in every
 * test would burn that budget over a normal run.
 */
let sessions: { nova: Session; sol: Session } | null = null

async function loginSessions(request: APIRequestContext): Promise<{ nova: Session; sol: Session }> {
  if (!sessions) {
    sessions = { nova: await apiLogin(request, NOVA), sol: await apiLogin(request, SOL) }
  }
  return sessions
}

/**
 * Opens a browser context with the session seeded into localStorage and the
 * relay URL injected before the app boots (the Vite dev server omits it).
 */
async function openSession(browser: Browser, session: Session): Promise<{ page: Page }> {
  const context = await browser.newContext()
  openContexts.push(context)
  await context.addInitScript(
    (cfg) => {
      localStorage.setItem('aether_token', cfg.token)
      localStorage.setItem('aether_user_email', cfg.email)
      localStorage.setItem('aether_user_name', cfg.name)
      ;(window as Window & { __AETHER_CONFIG__?: { relayUrl?: string } }).__AETHER_CONFIG__ = {
        relayUrl: cfg.relayUrl,
      }
    },
    { token: session.token, email: session.email, name: session.name, relayUrl: RELAY_URL },
  )
  return { page: await context.newPage() }
}

/**
 * Creates a fresh Postgres connector + dashboard + query widget as nova and
 * grants sol `view`+`view_with_data` on the dashboard and `use` on the
 * connector. PUT replaces the direct ACL entries, so nova's entries are
 * re-stated in the same payload.
 */
async function createLiveFixture(
  request: APIRequestContext,
  nova: Session,
  sol: Session,
  query: string,
): Promise<{ dashboardId: string; widgetId: string; connectorId: string }> {
  const headers = { Authorization: `Bearer ${nova.token}` }
  const suffix = `${Date.now()}-${++fixtureSeq}`

  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `Live Collab PG ${suffix}`,
      type: 'postgres',
      config: {
        host: PG_CONNECTOR_HOST,
        port: 5432,
        user: 'aether',
        password: 'aether_dev',
        database: 'aether',
      },
    },
  })
  expect(connResp.ok()).toBeTruthy()
  const connector = (await connResp.json()) as { id: string }

  const dashResp = await request.post('/api/v1/dashboards', {
    headers,
    data: { title: `Live Collab ${suffix}`, settings: {} },
  })
  expect(dashResp.ok()).toBeTruthy()
  const dashboard = (await dashResp.json()) as { id: string }

  const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
    headers,
    data: {
      connector_id: connector.id,
      query,
      language: 'sql',
      type: 'table',
      layout: { row: 0, col: 0, width: 6, height: 8 },
    },
  })
  expect(widgetResp.ok()).toBeTruthy()
  const widget = (await widgetResp.json()) as { id: string }

  const dashAcl = await request.put(`/api/v1/acl/dashboard/${dashboard.id}`, {
    headers,
    data: {
      entries: [
        { subject_type: 'user', subject_id: nova.id, actions: ['view', 'view_with_data', 'edit', 'delete', 'share'] },
        { subject_type: 'user', subject_id: sol.id, actions: ['view', 'view_with_data'] },
      ],
    },
  })
  expect(dashAcl.ok()).toBeTruthy()

  const connAcl = await request.put(`/api/v1/acl/connector/${connector.id}`, {
    headers,
    data: {
      entries: [
        { subject_type: 'user', subject_id: nova.id, actions: ['view', 'use'] },
        { subject_type: 'user', subject_id: sol.id, actions: ['view', 'use'] },
      ],
    },
  })
  expect(connAcl.ok()).toBeTruthy()

  return { dashboardId: dashboard.id, widgetId: widget.id, connectorId: connector.id }
}

/** Counts the POSTs a page makes to a dashboard execute endpoint. */
function trackExecutes(page: Page): () => number {
  let count = 0
  page.on('request', (req) => {
    if (req.method() === 'POST' && req.url().includes('/execute')) count += 1
  })
  return () => count
}

/** Table cell of a rendered result set (see OutputRenderer's data-row/data-col). */
const cell = (page: Page, row: number, col: number) => page.locator(`td[data-row="${row}"][data-col="${col}"]`)

/** Drags the widget by its `.dash-widget-drag` handle with trusted mouse input. */
async function dragWidget(page: Page, dx: number, dy: number): Promise<void> {
  const handle = page.locator('.dash-widget-drag').first()
  await expect(handle).toBeVisible()
  const box = await handle.boundingBox()
  if (!box) throw new Error('drag handle has no bounding box')
  const cx = box.x + box.width / 2
  const cy = box.y + box.height / 2
  await page.mouse.move(cx, cy)
  await page.mouse.down()
  // Cross react-grid-layout's 3px drag threshold first, then travel to the target.
  await page.mouse.move(cx + 12, cy, { steps: 3 })
  await page.mouse.move(cx + dx, cy + dy, { steps: 15 })
  await page.mouse.up()
}

test.describe('live dashboard collaboration', () => {
  test.setTimeout(60_000)

  test('editor SQL change re-runs the widget in the viewer', async ({ browser, request }) => {
    const { nova, sol } = await loginSessions(request)
    const fx = await createLiveFixture(
      request,
      nova,
      sol,
      'SELECT i AS number, i * 3 AS doubled FROM generate_series(1,5) AS i',
    )

    const editor = await openSession(browser, nova)
    const viewer = await openSession(browser, sol)
    const executes = trackExecutes(viewer.page)

    await editor.page.goto(`/dashboards/${fx.dashboardId}`)
    await viewer.page.goto(`/dashboards/${fx.dashboardId}/view`)

    // Both ends synced: the editor's controls arm only after the document
    // syncs, and the viewer sees nova's presence through awareness.
    await expect(editor.page.getByRole('button', { name: '+ Add Widget' })).toBeEnabled({ timeout: 15_000 })
    await expect(viewer.page.getByTitle('Nova')).toBeVisible({ timeout: 15_000 })
    await expect(cell(viewer.page, 4, 1)).toHaveText('15')
    const initialExecutes = executes()
    expect(initialExecutes).toBeGreaterThanOrEqual(1)

    // Edit the SQL in the drawer: `* 3` → `* 2`. The editor may still be
    // attaching to the shared document (which would revert an early
    // keystroke), so verify the text stuck and retry until it does.
    const target = 'SELECT i AS number, i * 2 AS doubled FROM generate_series(1,5) AS i'
    await editor.page.getByRole('button', { name: 'Edit widget' }).click()
    const drawer = editor.page.getByRole('dialog', { name: 'Widget configuration' })
    await expect(drawer).toBeVisible()
    const sqlEditor = drawer.locator('.cm-content')
    await expect(sqlEditor).toBeVisible()
    let typed = false
    for (let attempt = 0; attempt < 6 && !typed; attempt++) {
      await sqlEditor.click()
      await editor.page.keyboard.press('Control+a')
      await editor.page.keyboard.type(target)
      // Settle past the shared-doc attach: a late attach reverts to the shared
      // text, which the next check detects and retries.
      await editor.page.waitForTimeout(400)
      typed = (await sqlEditor.textContent())?.includes('i * 2') ?? false
    }

    // The viewer re-runs the widget itself (debounced ~2s) and shows the new data.
    await expect(cell(viewer.page, 4, 1)).toHaveText('10', { timeout: 15_000 })
    expect(executes()).toBeGreaterThan(initialExecutes)
  })

  test('editor drag moves the widget in the viewer without re-executing', async ({ browser, request }) => {
    const { nova, sol } = await loginSessions(request)
    const fx = await createLiveFixture(
      request,
      nova,
      sol,
      'SELECT i AS number, i * 2 AS doubled FROM generate_series(1,5) AS i',
    )

    const editor = await openSession(browser, nova)
    const viewer = await openSession(browser, sol)
    const executes = trackExecutes(viewer.page)

    await editor.page.goto(`/dashboards/${fx.dashboardId}`)
    await viewer.page.goto(`/dashboards/${fx.dashboardId}/view`)

    await expect(editor.page.getByRole('button', { name: '+ Add Widget' })).toBeEnabled({ timeout: 15_000 })
    await expect(viewer.page.getByTitle('Nova')).toBeVisible({ timeout: 15_000 })
    await expect(cell(viewer.page, 0, 0)).toHaveText('1')
    const initialExecutes = executes()

    const item = viewer.page.locator('.react-grid-item')
    await expect(item).toBeVisible()
    const before = await item.boundingBox()
    if (!before) throw new Error('viewer widget has no bounding box')

    await dragWidget(editor.page, 300, 44)

    // The viewer's widget moves live from the shared document.
    await expect
      .poll(async () => {
        const box = await item.boundingBox()
        return box ? Math.round(box.x) : 0
      }, { timeout: 10_000, message: 'viewer widget should move live after the editor drag' })
      .toBeGreaterThan(Math.round(before.x) + 50)

    // A pure layout move must not schedule a re-run: wait past the ~2s
    // per-widget debounce window before checking the request count.
    await viewer.page.waitForTimeout(2500)
    expect(executes()).toBe(initialExecutes)
  })

  test('a widget added over REST appears in the viewer', async ({ browser, request }) => {
    const { nova, sol } = await loginSessions(request)
    const fx = await createLiveFixture(request, nova, sol, 'SELECT 1 AS one')

    const editor = await openSession(browser, nova)
    const viewer = await openSession(browser, sol)

    await editor.page.goto(`/dashboards/${fx.dashboardId}`)
    await viewer.page.goto(`/dashboards/${fx.dashboardId}/view`)

    await expect(editor.page.getByRole('button', { name: '+ Add Widget' })).toBeEnabled({ timeout: 15_000 })
    await expect(viewer.page.getByTitle('Nova')).toBeVisible({ timeout: 15_000 })
    await expect(cell(viewer.page, 0, 0)).toHaveText('1')

    const addResp = await request.post(`/api/v1/dashboards/${fx.dashboardId}/widgets`, {
      headers: { Authorization: `Bearer ${nova.token}` },
      data: {
        connector_id: fx.connectorId,
        query: "SELECT 'widget-two' AS marker",
        language: 'sql',
        type: 'table',
        layout: { row: 8, col: 0, width: 6, height: 6 },
      },
    })
    expect(addResp.ok()).toBeTruthy()

    // The viewer renders the new widget live and runs its query itself.
    await expect(viewer.page.getByText('widget-two')).toBeVisible({ timeout: 10_000 })
  })

  test('a view-only user gets disabled mutation controls', async ({ browser, request }) => {
    const { nova, sol } = await loginSessions(request)
    const fx = await createLiveFixture(
      request,
      nova,
      sol,
      'SELECT i AS number, i * 2 AS doubled FROM generate_series(1,5) AS i',
    )

    const viewer = await openSession(browser, sol)
    await viewer.page.goto(`/dashboards/${fx.dashboardId}`)
    await expect(viewer.page.locator('.dash-widget-card')).toBeVisible({ timeout: 15_000 })

    // Every mutating control on the editor route is disabled for sol.
    await expect(viewer.page.getByRole('button', { name: '+ Add Widget' })).toBeDisabled()
    await expect(viewer.page.getByRole('button', { name: 'Variables' })).toBeDisabled()
    await expect(viewer.page.getByRole('button', { name: '12 columns' })).toBeDisabled()
    await expect(viewer.page.getByRole('button', { name: 'Remove widget' })).toBeDisabled()

    // The drawer opens read-only: the SQL editor is not editable and the
    // widget type select is disabled.
    await viewer.page.getByRole('button', { name: 'Edit widget' }).click()
    const drawer = viewer.page.getByRole('dialog', { name: 'Widget configuration' })
    await expect(drawer).toBeVisible()
    await expect(drawer.locator('.cm-content')).toHaveAttribute('contenteditable', 'false')
    await expect(drawer.locator('#widget-type')).toBeDisabled()
    await drawer.getByRole('button', { name: 'Close widget configuration' }).click()
    await expect(drawer).not.toBeVisible()

    // A drag attempt on the handle does not move (or persist) the widget.
    const item = viewer.page.locator('.react-grid-item')
    await expect(item).toBeVisible()
    const before = await item.boundingBox()
    if (!before) throw new Error('viewer widget has no bounding box')
    await dragWidget(viewer.page, 300, 44)
    await viewer.page.waitForTimeout(1000)
    const after = await item.boundingBox()
    if (!after) throw new Error('viewer widget has no bounding box')
    expect(Math.abs(after.x - before.x)).toBeLessThan(5)
    expect(Math.abs(after.y - before.y)).toBeLessThan(5)
  })
})
