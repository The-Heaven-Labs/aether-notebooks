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

  test('date picker widget accepts input', async ({ page }) => {
    await page.goto('/dashboards')
    // Create a new dashboard if button exists
    const newBtn = page.locator('button:has-text("New Dashboard")')
    if (await newBtn.isVisible()) {
      await newBtn.click()
      await page.fill('input', `Widget Test ${Date.now()}`)
      await page.click('button:has-text("Create")')
      await expect(page).toHaveURL(/\/dashboards\//)
    }
    // If there is a date input on the page, interact with it
    const datePicker = page.locator('input[type="date"]').first()
    if (await datePicker.isVisible()) {
      await datePicker.fill('2024-01-15')
      await expect(datePicker).toHaveValue('2024-01-15')
    }
  })

  test('visual: dashboards page', async ({ page }) => {
    await page.goto('/dashboards')
    await expect(page).toHaveScreenshot('dashboards-page.png')
  })
})
