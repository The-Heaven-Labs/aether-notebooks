import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

// Helper: register + onboard to get a session
async function loginAsNewUser(page: import('@playwright/test').Page) {
  const ts = Date.now().toString()
  await registerAndOnboard(page, ts)
}

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsNewUser(page)
  })

  test('sidebar contrast is sufficient in light theme', async ({ page }) => {
    // Use light theme
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/')
    await loginAsNewUser(page)

    // Expand sidebar to see labels
    await page.getByTitle('Expand sidebar').click()

    // Get all non-active nav items (should be muted)
    const navItems = page.locator('nav a')
    const count = await navItems.count()

    for (let i = 0; i < count; i++) {
      const item = navItems.nth(i)
      const isActive = await item.getAttribute('aria-current')
      if (isActive) continue // Skip active item (uses accent color)

      const color = await item.evaluate((el) => {
        return window.getComputedStyle(el).color
      })
      const bgColor = await item.evaluate((el) => {
        return window.getComputedStyle(el).backgroundColor
      })

      // Parse RGB values
      const parseRgb = (rgb: string) => {
        const match = rgb.match(/rgb\((\d+),\s*(\d+),\s*(\d+)\)/)
        if (!match) return [0, 0, 0]
        return [parseInt(match[1]), parseInt(match[2]), parseInt(match[3])]
      }

      const textRgb = parseRgb(color)
      const bgRgb = parseRgb(bgColor)

      // Calculate relative luminance
      const luminance = (rgb: number[]) => {
        const [r, g, b] = rgb.map((v) => {
          const normalized = v / 255
          return normalized <= 0.03928 ? normalized / 12.92 : Math.pow((normalized + 0.055) / 1.055, 2.4)
        })
        return 0.2126 * r + 0.7152 * g + 0.0722 * b
      }

      const textLum = luminance(textRgb)
      const bgLum = luminance(bgRgb)

      const contrastRatio =
        textLum > bgLum
          ? (bgLum + 0.05) / (textLum + 0.05)
          : (textLum + 0.05) / (bgLum + 0.05)

      // WCAG AA requires 4.5:1 for normal text
      expect(contrastRatio).toBeGreaterThan(4.5)
    }
  })

  test('visual: sidebar renders and collapses', async ({ page }) => {
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
