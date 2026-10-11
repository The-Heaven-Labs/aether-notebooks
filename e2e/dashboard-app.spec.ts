import { test, expect } from '@playwright/test'
import type { APIRequestContext, Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * The App surface (`/dashboards/:id/view`): the full-bleed, chrome-free
 * rendering of a dashboard configured with `settings.app`.
 *
 * Requires a running stack and points at it via E2E_BASE_URL (default :5173;
 * the embedded build on :8088 serves this working copy). The API-side Postgres
 * connector host defaults to `localhost` (native API run); override with
 * E2E_PG_CONNECTOR_HOST when the API runs in Docker (e.g. `aether-postgres`).
 */
const PG_CONNECTOR_HOST = process.env.E2E_PG_CONNECTOR_HOST ?? 'localhost'

// Pin timezone + locale and freeze the page clock below: the footer mask
// hides the live timestamps, and the freeze keeps the masthead's visible
// "Updated …" line from drifting between runs.
test.use({ timezoneId: 'UTC', locale: 'en-US' })

async function authHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

/**
 * Creates a dev-Postgres connector, a dashboard, and a metric widget that
 * reads `42` against a previous `40` (+5.0%).
 */
async function createAppDashboard(
  page: Page,
  request: APIRequestContext,
  suffix: string,
): Promise<{ dashboardId: string }> {
  const headers = await authHeaders(page)
  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `App DB ${suffix}`,
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
  const connector = await connResp.json()

  const dashResp = await request.post('/api/v1/dashboards', {
    headers,
    data: { title: `App ${suffix}` },
  })
  expect(dashResp.ok()).toBeTruthy()
  const dashboard = await dashResp.json()

  const metricResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
    headers,
    data: {
      connector_id: connector.id,
      query: 'SELECT 42 AS revenue, 40 AS prev',
      language: 'sql',
      type: 'metric',
      layout: { row: 0, col: 0, width: 6, height: 6 },
      config: {
        metric: {
          value_column: 'revenue',
          aggregation: 'none',
          comparison: { column: 'prev', mode: 'percent' },
        },
      },
    },
  })
  expect(metricResp.ok()).toBeTruthy()

  return { dashboardId: dashboard.id }
}

test.describe('Dashboard App surface', () => {
  test('masthead, metric, and text panel render on the bare app shell', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const { dashboardId } = await createAppDashboard(page, request, ts)

    // Add the text panel through the picker (source "None"), the way an
    // author would — the metric widget above came from the API helper.
    await page.goto(`/dashboards/${dashboardId}`)
    await page.getByRole('button', { name: '+ Add Widget' }).click()
    await page.getByLabel('Widget source').selectOption('none')
    await page.getByLabel('Text panel markdown').fill('## What changed\n\nRevenue is up.')
    await page.getByRole('button', { name: 'Add Widget', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'What changed' })).toBeVisible()

    // App settings panel: masthead title + subtitle.
    await page.getByTitle('App settings').click()
    await page.getByLabel('App title').fill('Executive Brief')
    await page.getByLabel('Subtitle').fill('Sales ops')
    await page.getByRole('button', { name: 'Save app settings' }).click()
    await expect(page.getByText('App settings saved')).toBeVisible()

    // Freeze the clock before the app surface loads: the masthead's visible
    // "Updated …" line must not drift between runs (the footer row is masked).
    await page.clock.setFixedTime(new Date('2026-06-01T12:00:00Z'))
    await page.goto(`/dashboards/${dashboardId}/view`)

    // The bare shell: no sidebar/nav chrome, just the main landmark.
    await expect(page.locator('nav')).toHaveCount(0)
    await expect(page.locator('#main-content')).toBeVisible()

    // Masthead from settings.app.
    await expect(page.getByRole('heading', { name: 'Executive Brief' })).toBeVisible()
    await expect(page.getByText('Sales ops')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Refresh data', exact: true })).toBeVisible()

    // Metric reading and comparison (42 vs 40 = +5.0%).
    await expect(page.getByRole('group', { name: '42, +5.0%' })).toBeVisible()
    // Text panel markdown.
    await expect(page.getByRole('heading', { name: 'What changed' })).toBeVisible()

    // Guard the mask locator: if the footer text ever changes, this fails
    // loudly here instead of silently dropping the mask and letting the
    // pixel comparison swallow a live-timestamp diff.
    await expect(page.getByText(/^Executed at /)).toBeVisible()

    await expect(page).toHaveScreenshot('dashboard-app.png', {
      // Mask the footer's parent row: it is fixed-size, while the "Executed
      // at …" span's width drifts with the live `cached`/`Nms` tail, which
      // would move the mask box. Exact comparison — the clock freeze keeps
      // the remaining pixels stable.
      mask: [page.getByText(/^Executed at /).locator('..')],
      maxDiffPixelRatio: 0,
    })
  })
})
