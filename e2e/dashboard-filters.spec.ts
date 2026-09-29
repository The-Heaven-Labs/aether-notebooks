import { test, expect } from '@playwright/test'
import type { APIRequestContext, Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

async function authHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

/** Creates a dev-Postgres connector, a dashboard with a `region` text variable and a live query widget. */
async function createLiveDashboard(
  page: Page,
  request: APIRequestContext,
  opts: { publicLive: boolean; suffix: string },
): Promise<{ dashboardId: string; headers: { Authorization: string } }> {
  const headers = await authHeaders(page)
  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `Filters DB ${opts.suffix}`,
      type: 'postgres',
      config: {
        host: 'aether-postgres',
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
    data: {
      title: `Filters ${opts.suffix}`,
      settings: {
        public_live: opts.publicLive,
        variables: [{ name: 'region', label: 'Region', type: 'text', default: 'EMEA' }],
      },
    },
  })
  expect(dashResp.ok()).toBeTruthy()
  const dashboard = await dashResp.json()

  const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
    headers,
    data: {
      connector_id: connector.id,
      query: 'SELECT {{region}} AS region',
      language: 'sql',
      type: 'table',
      layout: { row: 0, col: 0, width: 6, height: 6 },
    },
  })
  expect(widgetResp.ok()).toBeTruthy()

  return { dashboardId: dashboard.id, headers }
}

test.describe('Dashboard variables and live queries', () => {
  test('filters a live query widget and honors URL values', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const { dashboardId } = await createLiveDashboard(page, request, { publicLive: false, suffix: ts })

    await page.goto(`/dashboards/${dashboardId}/view`)
    const filter = page.locator('#dash-var-region')
    await expect(filter).toBeVisible()
    await expect(page.locator('td[data-row="0"][data-col="0"]')).toHaveText('EMEA')

    await filter.fill('AMER')
    await filter.press('Enter')
    await expect(page.locator('td[data-row="0"][data-col="0"]')).toHaveText('AMER')

    await page.goto(`/dashboards/${dashboardId}/view?region=APAC`)
    await expect(page.locator('td[data-row="0"][data-col="0"]')).toHaveText('APAC')
  })

  test('public visitors run live queries only when opted in', async ({ page, request, browser }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const { dashboardId, headers } = await createLiveDashboard(page, request, { publicLive: true, suffix: ts })

    const shareResp = await request.post(`/api/v1/dashboards/${dashboardId}/share`, { headers })
    expect(shareResp.ok()).toBeTruthy()
    const { token } = await shareResp.json()

    const ctx = await browser.newContext()
    const pub = await ctx.newPage()
    await pub.goto(`/public/dashboards/${token}`)
    await expect(pub.locator('#dash-var-region')).toBeVisible()
    await expect(pub.locator('td[data-row="0"][data-col="0"]')).toHaveText('EMEA')

    await pub.locator('#dash-var-region').fill('APAC')
    await pub.locator('#dash-var-region').press('Enter')
    await expect(pub.locator('td[data-row="0"][data-col="0"]')).toHaveText('APAC')
    await ctx.close()
  })

  test('public pages hide query widgets when live mode is off', async ({ page, request, browser }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const { dashboardId, headers } = await createLiveDashboard(page, request, { publicLive: false, suffix: ts })

    const shareResp = await request.post(`/api/v1/dashboards/${dashboardId}/share`, { headers })
    expect(shareResp.ok()).toBeTruthy()
    const { token } = await shareResp.json()

    const ctx = await browser.newContext()
    const pub = await ctx.newPage()
    await pub.goto(`/public/dashboards/${token}`)
    await expect(pub.getByText('No widgets in this dashboard')).toBeVisible()
    await expect(pub.locator('#dash-var-region')).toHaveCount(0)
    await ctx.close()
  })
})
