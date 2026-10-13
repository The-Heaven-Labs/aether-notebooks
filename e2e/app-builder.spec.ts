import { test, expect } from '@playwright/test'
import type { APIRequestContext, Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * App builder surfaces: chapter-scoped filters, the App health panel, and the
 * builder guidance (picker descriptors, field help, App settings hints).
 *
 * Requires a running stack and points at it via E2E_BASE_URL (default :5173;
 * the embedded build on :8088 serves this working copy).
 */
test.use({ timezoneId: 'UTC', locale: 'en-US' })

async function authHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

/**
 * A two-chapter dashboard with one filter per chapter, chapter-scoped:
 * overview uses `overview_region`, orders uses `orders_status`.
 */
async function createScopedDashboard(
  page: Page,
  request: APIRequestContext,
  suffix: string,
): Promise<{ dashboardId: string }> {
  const headers = await authHeaders(page)
  const dashResp = await request.post('/api/v1/dashboards', {
    headers,
    data: { title: `Scoped ${suffix}` },
  })
  expect(dashResp.ok()).toBeTruthy()
  const dashboard = await dashResp.json()

  const settingsResp = await request.put(`/api/v1/dashboards/${dashboard.id}`, {
    headers,
    data: {
      settings: {
        app: { title: `Scoped ${suffix}`, filter_scope: 'tab' },
        tabs: [
          { id: 'overview', title: 'Overview' },
          { id: 'orders', title: 'Orders' },
        ],
        variables: [
          { name: 'overview_region', label: 'Overview region', type: 'text', default: 'us' },
          { name: 'orders_status', label: 'Orders status', type: 'text', default: 'open' },
        ],
      },
    },
  })
  expect(settingsResp.ok()).toBeTruthy()

  for (const [tab, markdown] of [
    ['overview', '## Overview\n\nRegion: {{overview_region}}'],
    ['orders', '## Orders\n\nStatus: {{orders_status}}'],
  ] as const) {
    const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
      headers,
      data: {
        type: 'text',
        layout: { row: 0, col: 0, width: 6, height: 6, tab },
        config: { text: { markdown } },
      },
    })
    expect(widgetResp.ok()).toBeTruthy()
  }

  return { dashboardId: dashboard.id }
}

test.describe('Chapter-scoped filters', () => {
  test('the strip follows the active chapter and the scope toggle', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const { dashboardId } = await createScopedDashboard(page, request, ts)

    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible()

    // Chapter A shows only its own filter.
    await expect(page.getByLabel('Overview region')).toBeVisible()
    await expect(page.getByLabel('Orders status')).toHaveCount(0)

    // Switching chapters re-scopes the strip.
    await page.getByRole('tab', { name: 'Orders' }).click()
    await expect(page.getByLabel('Orders status')).toBeVisible()
    await expect(page.getByLabel('Overview region')).toHaveCount(0)

    // A hidden filter pinned in the URL raises the muted hint.
    await page.goto(`/dashboards/${dashboardId}/view?tab=orders&overview_region=eu`)
    await expect(page.getByRole('tab', { name: 'Orders' })).toBeVisible()
    await expect(page.getByText('1 filter set in other chapters')).toBeVisible()

    // The escape hatch: switch the scope to "all" and every filter returns.
    await page.goto(`/dashboards/${dashboardId}`)
    await page.getByTitle('App settings').click()
    await page.getByLabel('All filters everywhere').check()
    await page.getByRole('button', { name: 'Save app settings' }).click()
    await expect(page.getByText('App settings saved')).toBeVisible()

    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible()
    await expect(page.getByLabel('Overview region')).toBeVisible()
    await expect(page.getByLabel('Orders status')).toBeVisible()
  })
})

test.describe('App health panel', () => {
  test('an empty chapter is reported, and adding a widget clears it', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = await authHeaders(page)

    // Two chapters; only overview has a widget.
    const dashResp = await request.post('/api/v1/dashboards', {
      headers,
      data: { title: `Health ${ts}` },
    })
    const dashboard = await dashResp.json()
    const settingsResp = await request.put(`/api/v1/dashboards/${dashboard.id}`, {
      headers,
      data: {
        settings: {
          app: { title: `Health ${ts}` },
          tabs: [
            { id: 'overview', title: 'Overview' },
            { id: 'orders', title: 'Orders' },
          ],
        },
      },
    })
    expect(settingsResp.ok()).toBeTruthy()
    const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
      headers,
      data: {
        type: 'text',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'overview' },
        config: { text: { markdown: '## Overview\n\nFine.' } },
      },
    })
    expect(widgetResp.ok()).toBeTruthy()

    await page.goto(`/dashboards/${dashboard.id}`)
    await page.getByRole('button', { name: 'Review' }).click()
    const panel = page.getByRole('dialog', { name: 'App health' })
    await expect(panel).toBeVisible()
    await expect(panel.getByText('chapter "orders" has no widgets')).toBeVisible()

    // Fix it the way the finding says: give the chapter a widget, then reopen
    // the panel (each open runs a fresh check).
    const fixed = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
      headers,
      data: {
        type: 'text',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'orders' },
        config: { text: { markdown: '## Orders\n\nFine.' } },
      },
    })
    expect(fixed.ok()).toBeTruthy()

    await panel.getByRole('button', { name: 'Close app health' }).click()
    // Prove the panel actually closed before reopening: the reopen must run a
    // fresh check, not keep the first panel's data.
    await expect(page.getByRole('dialog', { name: 'App health' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Review' }).click()
    await expect(page.getByRole('dialog', { name: 'App health' })).toBeVisible()
    await expect(page.getByText('chapter "orders" has no widgets')).toHaveCount(0)
  })
})

test.describe('Builder guidance', () => {
  test('picker descriptors and field help are visible and keyboard reachable', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const { dashboardId } = await createScopedDashboard(page, request, ts)

    await page.goto(`/dashboards/${dashboardId}`)
    await page.getByRole('button', { name: '+ Add Widget' }).click()
    await expect(page.getByText('Reuses a notebook cell’s stored output.')).toBeVisible()
    // Metric is a query-source type; switch the source so the option enables.
    await page.getByLabel('Widget source').selectOption('query')
    await page.getByLabel('Widget type').selectOption('metric')
    await expect(page.getByText('Metric — one number from the query.')).toBeVisible()
    // Close the picker before opening the drawer.
    await page.getByRole('button', { name: 'Close add widget' }).click()

    // Field help opens by keyboard and closes on Escape.
    await page.getByRole('button', { name: 'Edit widget' }).first().click()
    const help = page.getByRole('button', { name: 'Help: Markdown' })
    await help.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('tooltip')).toContainText('values interpolate as plain text')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('tooltip')).toHaveCount(0)

    await page.keyboard.press('Escape')
    await page.getByTitle('App settings').click()
    await expect(page.getByText(/filters its widgets use/)).toBeVisible()
    await page.getByRole('button', { name: 'Help: Accent' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('tooltip')).toContainText('must clear 3:1 contrast')

    // Snapshot the App settings panel with the guidance visible (desktop).
    // The profile avatar's color derives from the fresh-per-run test email.
    await expect(page).toHaveScreenshot('app-builder-app-settings.png', {
      animations: 'disabled',
      maxDiffPixelRatio: 0,
      mask: [page.getByRole('button', { name: 'Profile menu' })],
    })
  })
})
