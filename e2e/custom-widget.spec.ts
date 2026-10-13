import { test, expect } from '@playwright/test'
import type { APIRequestContext, Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * Custom components (data apps wave 4): the allow-scripts opaque-origin
 * sandbox, the validated postMessage data bridge, the org kill switch, the
 * full custom page, and public gating.
 *
 * Requires a running stack and points at it via E2E_BASE_URL (default :5173;
 * the embedded build on :8088 serves this working copy and injects the relay
 * config the editor connects with). The API-side Postgres connector host
 * defaults to `localhost` (native API run); override with
 * E2E_PG_CONNECTOR_HOST when the API runs in Docker (e.g. `aether-postgres`).
 */
test.use({ timezoneId: 'UTC', locale: 'en-US' })

const PG_CONNECTOR_HOST = process.env.E2E_PG_CONNECTOR_HOST ?? 'localhost'

async function authHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

async function setCustomCode(request: APIRequestContext, headers: { Authorization: string }, enabled: boolean): Promise<void> {
  const resp = await request.put('/api/v1/org/settings', { headers, data: { custom_code_enabled: enabled } })
  expect(resp.ok()).toBeTruthy()
}

async function createPostgresConnector(
  request: APIRequestContext,
  headers: { Authorization: string },
  suffix: string,
): Promise<string> {
  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `Custom DB ${suffix}`,
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
  return ((await connResp.json()) as { id: string }).id
}

async function createDashboard(
  request: APIRequestContext,
  headers: { Authorization: string },
  title: string,
): Promise<string> {
  const dashResp = await request.post('/api/v1/dashboards', { headers, data: { title } })
  expect(dashResp.ok()).toBeTruthy()
  return ((await dashResp.json()) as { id: string }).id
}

async function createQueryWidget(
  request: APIRequestContext,
  headers: { Authorization: string },
  dashboardId: string,
  connectorId: string,
  query: string,
): Promise<string> {
  const resp = await request.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
    headers,
    data: {
      connector_id: connectorId,
      query,
      language: 'sql',
      type: 'table',
      layout: { row: 0, col: 0, width: 6, height: 6 },
    },
  })
  expect(resp.ok()).toBeTruthy()
  return ((await resp.json()) as { id: string }).id
}

/**
 * The bridge client: requests `sales` and retries on error frames (the host
 * answers only requests — data that is not resolved yet replies with an
 * error, and the component re-requests). It also probes the network.
 */
const BOUND_HTML = '<div data-sales="waiting">waiting</div>'
const BOUND_JS = [
  'function req(){ aether.requestData("sales") }',
  'window.addEventListener("aether:data", function(ev){ document.querySelector("[data-sales]").textContent = "data:" + JSON.stringify(ev.detail.result).slice(0, 200) })',
  'window.addEventListener("aether:error", function(ev){ document.querySelector("[data-sales]").textContent = "error:" + ev.detail.message; setTimeout(req, 300) })',
  'fetch("https://example.com/leak").then(function(){ document.body.setAttribute("data-fetch", "ok") }).catch(function(){ document.body.setAttribute("data-fetch", "blocked") })',
  'req()',
].join(';')

async function createBoundCustomWidget(
  request: APIRequestContext,
  headers: { Authorization: string },
  dashboardId: string,
  targetWidgetId: string,
): Promise<string> {
  const resp = await request.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
    headers,
    data: {
      type: 'custom',
      layout: { row: 6, col: 0, width: 6, height: 6 },
      config: {
        html: BOUND_HTML,
        js: BOUND_JS,
        bindings: [{ key: 'sales', widget_id: targetWidgetId }],
      },
    },
  })
  expect(resp.ok()).toBeTruthy()
  return ((await resp.json()) as { id: string }).id
}

async function createStaticCustomWidget(
  request: APIRequestContext,
  headers: { Authorization: string },
  dashboardId: string,
  html: string,
): Promise<void> {
  const resp = await request.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
    headers,
    data: {
      type: 'custom',
      layout: { row: 6, col: 0, width: 6, height: 6 },
      config: { html },
    },
  })
  expect(resp.ok()).toBeTruthy()
}

test.describe('Custom components', () => {
  test('delivers bound query data into the opaque-origin sandbox and blocks the network', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = await authHeaders(page)
    await setCustomCode(request, headers, true)
    const connectorId = await createPostgresConnector(request, headers, ts)
    const dashboardId = await createDashboard(request, headers, `Custom Bridge ${ts}`)
    const queryWidgetId = await createQueryWidget(request, headers, dashboardId, connectorId, 'SELECT 42 AS sales')
    await createBoundCustomWidget(request, headers, dashboardId, queryWidgetId)

    // Every request made from a non-main frame is a sandbox leak: the
    // component is inline-only and must make none.
    const frameRequests: string[] = []
    await page.route('**/*', (route) => {
      if (route.request().frame() !== page.mainFrame()) frameRequests.push(route.request().url())
      return route.continue()
    })

    await page.goto(`/dashboards/${dashboardId}/view`)
    const frame = page.frameLocator('iframe[title="Custom component"]')
    // The query ran through the execute pipeline as the viewer and the host
    // answered the bridge with the result.
    await expect(frame.locator('[data-sales]')).toContainText('data:', { timeout: 15_000 })
    await expect(frame.locator('[data-sales]')).toContainText('42')

    // Opaque origin: reading cookies either throws (Chromium sandboxed frame)
    // or comes back empty — nothing the host set is ever visible inside.
    const cookieRead = await frame.locator('body').evaluate(() => {
      try {
        return document.cookie === '' ? 'empty' : document.cookie
      } catch {
        return 'blocked'
      }
    })
    expect(['blocked', 'empty']).toContain(cookieRead)
    expect(
      await frame.locator('body').evaluate(() => {
        try {
          window.localStorage.setItem('x', '1')
          return 'reachable'
        } catch {
          return 'blocked'
        }
      }),
    ).toBe('blocked')
    // The CSP `connect-src 'none'` blocked the probe fetch before it left.
    await expect(frame.locator('body')).toHaveAttribute('data-fetch', 'blocked')
    expect(frameRequests).toEqual([])
  })

  test('the org kill switch replaces components and refuses creation', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = await authHeaders(page)
    await setCustomCode(request, headers, true)
    const dashboardId = await createDashboard(request, headers, `Custom Switch ${ts}`)
    await createStaticCustomWidget(request, headers, dashboardId, '<h1>Kill switch component</h1>')

    const frame = page.frameLocator('iframe[title="Custom component"]')
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(frame.getByRole('heading', { name: 'Kill switch component' })).toBeVisible()

    // Disabling replaces the rendered component with a quiet placeholder…
    await setCustomCode(request, headers, false)
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(page.getByText('Custom code is disabled for this organization.')).toBeVisible()
    await expect(page.locator('iframe[title="Custom component"]')).toHaveCount(0)

    // …and refuses new ones at the API.
    const refused = await request.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
      headers,
      data: { type: 'custom', layout: { row: 12, col: 0, width: 4, height: 4 } },
    })
    expect(refused.status()).toBe(400)
    expect(await refused.text()).toContain('disabled')

    // Re-enabling restores the retained component; a second disable/enable
    // round-trip keeps its config.
    await setCustomCode(request, headers, true)
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(frame.getByRole('heading', { name: 'Kill switch component' })).toBeVisible()

    await setCustomCode(request, headers, false)
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(page.getByText('Custom code is disabled for this organization.')).toBeVisible()
    await setCustomCode(request, headers, true)
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(frame.getByRole('heading', { name: 'Kill switch component' })).toBeVisible()
  })

  test('the full custom page renders only the component and badges data sources', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = await authHeaders(page)
    await setCustomCode(request, headers, true)
    const connectorId = await createPostgresConnector(request, headers, ts)
    const dashboardId = await createDashboard(request, headers, `Custom Page ${ts}`)
    const marker = `query-marker-${ts}`
    await createQueryWidget(request, headers, dashboardId, connectorId, `SELECT '${marker}' AS marker`)
    await createStaticCustomWidget(request, headers, dashboardId, '<h1>Full page component</h1>')

    const layout = await request.put(`/api/v1/dashboards/${dashboardId}`, {
      headers,
      data: { settings: { app: { layout: 'custom' } } },
    })
    expect(layout.ok()).toBeTruthy()

    // The app surface renders the component full-bleed; the query widget is a
    // hidden data source (its marker never reaches the page).
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(
      page.frameLocator('iframe[title="Custom component"]').getByRole('heading', { name: 'Full page component' }),
    ).toBeVisible()
    await expect(page.getByText(marker)).toHaveCount(0)

    // The editor keeps every widget visible and badges the non-component ones.
    await page.goto(`/dashboards/${dashboardId}`)
    await expect(page.getByText('Data source')).toBeVisible()
  })

  test('public pages render the component and gate data on public_live', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = await authHeaders(page)
    await setCustomCode(request, headers, true)
    const connectorId = await createPostgresConnector(request, headers, ts)
    const dashboardId = await createDashboard(request, headers, `Custom Public ${ts}`)
    const queryWidgetId = await createQueryWidget(request, headers, dashboardId, connectorId, 'SELECT 42 AS sales')
    await createBoundCustomWidget(request, headers, dashboardId, queryWidgetId)

    const shareResp = await request.post(`/api/v1/dashboards/${dashboardId}/share`, { headers })
    expect(shareResp.ok()).toBeTruthy()
    const { token } = (await shareResp.json()) as { token: string }

    // Not live yet: the frame renders (the org switch is on) but every data
    // request is answered with the not-enabled error.
    await page.goto(`/public/dashboards/${token}`)
    const frame = page.frameLocator('iframe[title="Custom component"]')
    await expect(frame.locator('[data-sales]')).toContainText('not enabled', { timeout: 15_000 })

    // Turn on live data, then publish: the snapshot governs the public page.
    const live = await request.put(`/api/v1/dashboards/${dashboardId}`, {
      headers,
      data: { settings: { public_live: true } },
    })
    expect(live.ok()).toBeTruthy()
    const publish = await request.post(`/api/v1/dashboards/${dashboardId}/publish`, { headers })
    expect(publish.ok()).toBeTruthy()

    await page.goto(`/public/dashboards/${token}`)
    await expect(frame.locator('[data-sales]')).toContainText('data:', { timeout: 15_000 })
    await expect(frame.locator('[data-sales]')).toContainText('42')
  })
})
