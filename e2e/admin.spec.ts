import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

test.describe('Platform admin panel', () => {
  // Register a user and try to access /admin — regular users should not see it
  test('non-admin cannot access /admin page content', async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    await page.goto('/admin')
    // Regular user should see either a redirect or no admin content
    // The AdminPage renders only for platform admins; regular users see nothing useful
    await expect(page.locator('h1:has-text("Platform Administration")')).not.toBeVisible()
  })

  test('visual: /admin as non-admin (should be empty or redirect)', async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    await page.goto('/admin')
    await expect(page).toHaveScreenshot('admin-non-admin.png')
  })

  // Note: platform admin tests require a seeded platform admin user.
  // Run `task db:seed-admin` or manually set is_platform_admin=true in the DB.
  test.describe('with platform admin credentials', () => {
    test.skip(({ browserName }) => browserName !== 'chromium', 'admin tests run only on chromium')

    test('platform admin can view orgs table', async ({ page }) => {
      await page.goto('/login')
      await page.fill('input[type="email"]', 'platform-admin@example.com')
      await page.fill('input[type="password"]', 'password')
      await page.click('button[type="submit"]')
      // If login succeeds (user exists), check admin page
      if (page.url().includes('/onboarding')) {
        test.skip() // user not seeded
      }
      await expect(page).toHaveURL('/')
      await page.goto('/admin')
      await expect(page.locator('h1:has-text("Platform Administration")')).toBeVisible()
      await expect(page.locator('table')).toBeVisible()
    })

    test('platform admin can switch to users tab', async ({ page }) => {
      await page.goto('/login')
      await page.fill('input[type="email"]', 'platform-admin@example.com')
      await page.fill('input[type="password"]', 'password')
      await page.click('button[type="submit"]')
      if (page.url().includes('/onboarding')) {
        test.skip()
      }
      await page.goto('/admin')
      await page.click('button:has-text("Users")')
      await expect(page.locator('th:has-text("Email")')).toBeVisible()
    })

    test('visual: platform admin panel', async ({ page }) => {
      await page.goto('/login')
      await page.fill('input[type="email"]', 'platform-admin@example.com')
      await page.fill('input[type="password"]', 'password')
      await page.click('button[type="submit"]')
      if (page.url().includes('/onboarding')) {
        test.skip()
      }
      await page.goto('/admin')
      await expect(page).toHaveScreenshot('platform-admin-panel.png')
    })
  })
})
