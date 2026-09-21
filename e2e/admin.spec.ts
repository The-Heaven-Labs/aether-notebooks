import { test, expect } from '@playwright/test'
import { loginAsPlatformAdmin, registerAndOnboard } from './helpers'

test.describe('Platform admin panel', () => {
  // Register a user and try to access /admin — regular users should not see it
  test('non-admin cannot access /admin page content', async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    await page.goto('/admin')
    // The page shell is public, but the admin tabs only render for platform admins.
    await expect(page.getByRole('tab', { name: 'Orgs' })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: 'Users' })).toHaveCount(0)
  })

  test('visual: /admin as non-admin (should be empty or redirect)', async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    await page.goto('/admin')
    await expect(page.locator('h1:has-text("Platform Admin")')).toBeVisible()
    // The top bar shows the unique org name; tolerate its glyph-width drift.
    await expect(page).toHaveScreenshot('admin-non-admin.png', { maxDiffPixelRatio: 0.002 })
  })

  // Platform admin credentials are seeded by the dev stack
  // (AETHER_PLATFORM_ADMIN_EMAIL=admin@heaven-labs.com / admin123).
  test.describe('with platform admin credentials', () => {
    test.skip(({ browserName }) => browserName !== 'chromium', 'admin tests run only on chromium')

    test('platform admin can view orgs table', async ({ page }) => {
      await loginAsPlatformAdmin(page)
      await page.goto('/admin')
      await expect(page.locator('h1:has-text("Platform Admin")')).toBeVisible()
      await expect(page.getByRole('tab', { name: 'Orgs' })).toBeVisible()
      await expect(page.locator('table')).toBeVisible()
    })

    test('platform admin can switch to users tab', async ({ page }) => {
      await loginAsPlatformAdmin(page)
      await page.goto('/admin')
      await page.getByRole('tab', { name: 'Users' }).click()
      await expect(page.locator('th:has-text("Email")')).toBeVisible()
    })

    test('visual: platform admin panel', async ({ page }) => {
      await loginAsPlatformAdmin(page)
      await page.goto('/admin')
      await expect(page.locator('table tbody tr').first()).toBeVisible()
      // Org names/counts/dates and the pagination totals are live data; mask
      // them so the snapshot covers the panel chrome, not the dev DB contents.
      await expect(page).toHaveScreenshot('platform-admin-panel.png', {
        mask: [page.locator('table'), page.getByText(/Showing .* entries/)],
      })
    })
  })
})
