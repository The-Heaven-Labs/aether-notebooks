import type { APIRequestContext, Page } from '@playwright/test'

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

// ── API seeding ──────────────────────────────────────────────────────────────
// Specs authenticate once through the UI (register/login), then seed larger
// fixtures through the API with the session token the app stored. Request
// paths are relative to the Playwright baseURL, so the Vite dev proxy still
// fronts them (same origin as the app).

/** Reads the bearer token the app stored for the logged-in user. */
export async function tokenFromPage(page: Page): Promise<string> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  if (!token) throw new Error('no aether_token in localStorage — is the user logged in?')
  return token
}

/** Creates a folder via the API and returns its id. */
export async function createFolderViaApi(
  request: APIRequestContext,
  token: string,
  name: string,
  parentId?: string,
): Promise<string> {
  const resp = await request.post('/api/v1/folders', {
    headers: { Authorization: `Bearer ${token}` },
    data: { name, ...(parentId ? { parent_id: parentId } : {}) },
  })
  if (!resp.ok()) throw new Error(`create folder failed: ${resp.status()} ${await resp.text()}`)
  return ((await resp.json()) as { id: string }).id
}

/**
 * Seeds `n` notebooks via the API (`folderId` null = root) and returns their
 * ids. Bounded concurrency: one request at a time would drag out 100+-row
 * pagination fixtures; unbounded would flood the dev DB.
 */
export async function seedNotebooks(
  request: APIRequestContext,
  token: string,
  folderId: string | null,
  n: number,
  titlePrefix = 'Seed Notebook',
): Promise<string[]> {
  const ids: string[] = []
  const headers = { Authorization: `Bearer ${token}` }
  const batchSize = 10
  for (let start = 0; start < n; start += batchSize) {
    const stop = Math.min(start + batchSize, n)
    const batch: Array<Promise<string>> = []
    for (let i = start; i < stop; i++) {
      batch.push(
        request
          .post('/api/v1/notebooks', {
            headers,
            data: {
              title: `${titlePrefix} ${String(i + 1).padStart(3, '0')}`,
              ...(folderId ? { folder_id: folderId } : {}),
            },
          })
          .then(async (resp) => {
            if (!resp.ok()) throw new Error(`seed notebook failed: ${resp.status()} ${await resp.text()}`)
            return ((await resp.json()) as { id: string }).id
          }),
      )
    }
    ids.push(...(await Promise.all(batch)))
  }
  return ids
}
