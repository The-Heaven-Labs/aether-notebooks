import { test, expect } from '@playwright/test'

// Helper: register + login to get a session
async function loginAsNewUser(page: import('@playwright/test').Page) {
  const ts = Date.now()
  await page.goto('/login')
  await page.getByRole('link', { name: /register/i }).click()
  await page.getByPlaceholder(/email/i).fill(`nav-test-${ts}@example.com`)
  await page.getByPlaceholder(/password/i).fill('testpass123')
  await page.getByPlaceholder(/name/i).fill('Nav Tester')
  await page.getByPlaceholder(/org/i).fill(`NavOrg-${ts}`)
  await page.getByRole('button', { name: /register/i }).click()
  await page.waitForURL('/')
}

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsNewUser(page)
  })

  test('sidebar renders and collapses', async ({ page }) => {
    await expect(page.getByTitle('Notebooks')).toBeVisible()
    await expect(page.getByTitle('Dashboards')).toBeVisible()
    await expect(page.getByTitle('Connectors')).toBeVisible()
    await expect(page.getByTitle('Members')).toBeVisible()
    await expect(page.getByTitle('Audit')).toBeVisible()

    // Expand
    await page.getByTitle('Expand sidebar').click()
    await expect(page.getByTitle('Collapse sidebar')).toBeVisible()

    // Visual snapshot — expanded
    await expect(page).toHaveScreenshot('sidebar-expanded.png', { maxDiffPixelRatio: 0.01 })

    // Collapse back
    await page.getByTitle('Collapse sidebar').click()
    await expect(page).toHaveScreenshot('sidebar-collapsed.png', { maxDiffPixelRatio: 0.01 })
  })

  test('navigates to Dashboards page', async ({ page }) => {
    await page.getByTitle('Dashboards').click()
    await expect(page).toHaveURL('/dashboards')
  })

  test('profile dropdown shows name and sign-out', async ({ page }) => {
    await page.getByLabel('Profile menu').click()
    await expect(page.getByText('Nav Tester')).toBeVisible()
    await expect(page.getByRole('button', { name: /sign out/i })).toBeVisible()
  })

  test('sign out redirects to login', async ({ page }) => {
    await page.getByLabel('Profile menu').click()
    await page.getByRole('button', { name: /sign out/i }).click()
    await expect(page).toHaveURL('/login')
  })
})
