import type { Page } from '@playwright/test'

/**
 * Two-step login: submit the email first, then the password. The login page
 * probes SSO providers for the email domain before showing the password form,
 * so the password field must not be filled before the email step completes.
 */
export async function login(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.fill('input[type="email"]', email)
  await page.locator('input[type="email"]').press('Enter')
  await page.fill('input[type="password"]', password)
  await page.locator('input[type="password"]').press('Enter')
  await page.waitForURL('/')
}

export async function loginAsAdmin(page: Page) {
  await login(page, 'admin@heaven-labs.com', 'admin123')
}

export async function loginAsPlatformAdmin(page: Page) {
  await login(page, 'admin@heaven-labs.com', 'admin123')
}

export async function registerAndOnboard(
  page: Page,
  suffix: string,
  name = `Test User ${suffix}`,
): Promise<{ email: string; name: string; orgName: string }> {
  const ts = suffix ?? Date.now().toString()
  const email = `test-${ts}@example.com`
  const orgName = `Test Org ${ts}`
  await page.goto('/login')
  // Switch to register tab
  await page.getByRole('button', { name: /create account/i }).click()
  await page.fill('input[placeholder*="Jane"]', name)
  await page.fill('input[type="email"]', email)
  await page.fill('input[type="password"]', 'testpass123')
  // Submit via Enter: the submit button re-renders while the request is in
  // flight, and Playwright's click retry can then submit the form twice.
  await page.locator('input[type="password"]').press('Enter')
  // After register without org_name, should go to /onboarding
  await page.waitForURL('/onboarding')
  await page.click('text=Create a new organization')
  await page.fill('input[placeholder*="Acme"]', orgName)
  await page.click('button:has-text("Create organization")')
  await page.waitForURL('/')
  return { email, name, orgName }
}

export async function createNotebook(page: Page, title: string): Promise<string> {
  await page.goto('/')
  await page.click('button:has-text("New Notebook")')
  await page.fill('input[placeholder*="title"]', title)
  await page.click('button:has-text("Create")')
  const url = page.url()
  return url.split('/notebooks/')[1]
}

export async function createDashboard(page: Page, title: string): Promise<string> {
  await page.goto('/dashboards')
  await page.click('button:has-text("New Dashboard")')
  await page.fill('input[placeholder*="title"]', title)
  await page.click('button:has-text("Create")')
  const url = page.url()
  return url.split('/dashboards/')[1]
}
