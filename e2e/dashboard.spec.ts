import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

test.describe('Dashboard variables and widgets', () => {
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

  test('date variable filter accepts input', async ({ page, request }) => {
    const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('aether_token'))}` }
    const dashResp = await request.post('/api/v1/dashboards', {
      headers,
      data: {
        title: `Date Filter ${Date.now()}`,
        settings: { variables: [{ name: 'start_date', label: 'Start date', type: 'date' }] },
      },
    })
    expect(dashResp.ok()).toBeTruthy()
    const dashboard = await dashResp.json()

    await page.goto(`/dashboards/${dashboard.id}/view`)
    const datePicker = page.getByLabel('Start date')
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
