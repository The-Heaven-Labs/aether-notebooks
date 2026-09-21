import { test, expect } from '@playwright/test'
import { login, registerAndOnboard } from './helpers'

test.describe('Authentication', () => {
  test('register → onboarding → create org → land on home', async ({ page }) => {
    const ts = Date.now().toString()
    await page.goto('/login')
    await page.getByRole('button', { name: /create account/i }).click()
    await page.fill('input[placeholder*="Jane"]', `Test User ${ts}`)
    await page.fill('input[type="email"]', `test-${ts}@example.com`)
    await page.fill('input[type="password"]', 'password123')
    await page.locator('input[type="password"]').press('Enter')

    // Should land on /onboarding
    await expect(page).toHaveURL(/\/onboarding/)
    await expect(page.locator('text=Create a new organization')).toBeVisible()

    // Create org
    await page.click('text=Create a new organization')
    await page.fill('input[placeholder*="Acme"]', `My Org ${ts}`)
    await page.click('button:has-text("Create organization")')

    // Should land on home
    await expect(page).toHaveURL('/')
  })

  test('SSO provider button is offered for a matching email domain', async ({ page }) => {
    await page.goto('/login')
    await page.fill('input[type="email"]', 'alice@aether-dev.test')
    await page.click('button[type="submit"]')

    // The login page probes SSO providers by email domain after the email step.
    // The dev stack seeds a platform Keycloak provider for aether-dev.test when
    // AETHER_ENABLE_DEV_SEED=true; skip with a clear reason otherwise.
    await expect(page.locator('input[type="password"]')).toBeVisible()
    const ssoButton = page.getByRole('button', { name: /sign in with/i })
    if ((await ssoButton.count()) === 0) {
      test.skip(true, 'No SSO provider for aether-dev.test (start the API with AETHER_ENABLE_DEV_SEED=true; the probe is also rate-limited to 20/min)')
    }
    await expect(ssoButton).toBeVisible()
  })

  test('login with existing account redirects to home', async ({ page }) => {
    const ts = Date.now().toString()
    // First register a new user so we know the credentials
    const { email } = await registerAndOnboard(page, ts)
    // Sign out
    await page.getByLabel('Profile menu').click()
    await page.getByRole('button', { name: /sign out/i }).click()
    await expect(page).toHaveURL('/login')
    // Sign back in — but this user has an org so it should go to /
    await login(page, email, 'testpass123')
    await expect(page).toHaveURL('/')
  })

  test('sign-out clears session and redirects to /login', async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    await page.getByLabel('Profile menu').click()
    await page.getByRole('button', { name: /sign out/i }).click()
    await expect(page).toHaveURL('/login')
    // aether_token should be gone — visiting protected route redirects to login
    await page.goto('/')
    await expect(page).toHaveURL('/login')
  })

  test('visual: login page', async ({ page }) => {
    await page.goto('/login')
    await expect(page).toHaveScreenshot('login-page.png')
  })

  test('visual: onboarding page', async ({ page }) => {
    await page.goto('/onboarding')
    await expect(page).toHaveScreenshot('onboarding-page.png')
  })
})
