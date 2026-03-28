import type { Page } from '@playwright/test'

export async function loginAsAdmin(page: Page) {
  await page.goto('/login')
  await page.fill('input[name="email"]', 'admin@example.com')
  await page.fill('input[name="password"]', 'password')
  await page.click('button[type="submit"]')
  await page.waitForURL('/')
}

export async function createNotebook(page: Page, title: string): Promise<string> {
  await page.goto('/notebooks')
  await page.click('button:has-text("New Notebook")')
  await page.fill('input[placeholder*="title"]', title)
  await page.click('button:has-text("Create")')
  const url = page.url()
  return url.split('/notebooks/')[1]
}
