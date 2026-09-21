import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

test.describe('Dashboard input widgets', () => {
  test.beforeEach(async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
  })

  test('dashboards list page renders', async ({ page }) => {
    await page.goto('/dashboards')
    await expect(page).toHaveURL('/dashboards')
    // Page title or empty state
    await expect(page.locator('h1, h2').first()).toBeVisible()
  })

  test('creating a dashboard from the list adds it to the list', async ({ page }) => {
    await page.goto('/dashboards')
    await page.getByRole('button', { name: '+ New Dashboard' }).click()
    const title = `Widget Test ${Date.now()}`
    await page.getByPlaceholder('Dashboard title').fill(title)
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText(title)).toBeVisible()
  })

  test('date picker widget accepts input', async ({ page, request }) => {
    const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('aether_token'))}` }
    const dashResp = await request.post('/api/v1/dashboards', {
      headers,
      data: { title: `Date Picker ${Date.now()}`, settings: {} },
    })
    expect(dashResp.ok()).toBeTruthy()
    const dashboard = await dashResp.json()

    const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
      headers,
      data: {
        type: 'date_picker',
        layout: { row: 0, col: 0, width: 4, height: 3 },
        config: { paramName: 'start_date', label: 'Start date' },
      },
    })
    expect(widgetResp.ok()).toBeTruthy()

    await page.goto(`/dashboards/${dashboard.id}/view`)
    const datePicker = page.locator('input[type="date"]').first()
    await expect(datePicker).toBeVisible()
    await datePicker.fill('2024-01-15')
    await expect(datePicker).toHaveValue('2024-01-15')
  })

  test('visual: dashboards page', async ({ page }) => {
    await page.goto('/dashboards')
    await expect(page.getByText('No dashboards yet')).toBeVisible()
    // The top bar shows the unique org name; tolerate its glyph-width drift.
    await expect(page).toHaveScreenshot('dashboards-page.png', { maxDiffPixelRatio: 0.002 })
  })
})
