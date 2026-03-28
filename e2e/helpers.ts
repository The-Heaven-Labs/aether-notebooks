import type { Page } from '@playwright/test'

export async function loginAsAdmin(page: Page) {
  await page.goto('/login')
  await page.fill('input[name="email"]', 'admin@example.com')
  await page.fill('input[name="password"]', 'password')
  await page.click('button[type="submit"]')
  await page.waitForURL('/')
}

export async function loginAsPlatformAdmin(page: Page) {
  await page.goto('/login')
  await page.fill('input[name="email"]', 'platform-admin@example.com')
  await page.fill('input[name="password"]', 'password')
  await page.click('button[type="submit"]')
  await page.waitForURL('/')
}

export async function registerAndOnboard(page: Page, suffix: string): Promise<void> {
  const ts = suffix ?? Date.now().toString()
  await page.goto('/login')
  // Switch to register tab
  await page.getByRole('button', { name: /create account/i }).click()
  await page.fill('input[placeholder*="Jane"]', `Test User ${ts}`)
  await page.fill('input[type="email"]', `test-${ts}@example.com`)
  await page.fill('input[type="password"]', 'testpass123')
  await page.click('button[type="submit"]')
  // After register without org_name, should go to /onboarding
  await page.waitForURL('/onboarding')
  await page.click('text=Create a new organization')
  await page.fill('input[placeholder*="Acme"]', `Test Org ${ts}`)
  await page.click('button:has-text("Create organization")')
  await page.waitForURL('/')
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
