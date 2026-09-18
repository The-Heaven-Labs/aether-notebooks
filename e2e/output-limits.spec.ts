import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

// End-to-end coverage for bounded cell outputs: the executor byte cap marks
// oversized results as truncated, the notebook view surfaces a truncation badge,
// and the full payload streams from the download endpoint. Also covers the
// org-level settings section.

interface Fixture {
  token: string
  notebookId: string
  cellId: string
}

async function apiHeaders(page: Page): Promise<Record<string, string>> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  expect(token).toBeTruthy()
  return { Authorization: `Bearer ${token}` }
}

// createTruncatedCell provisions a connector, notebook, and cell, sets a tiny
// per-cell output cap, and executes a query whose result exceeds it.
async function createTruncatedCell(page: Page, request: APIRequestContext): Promise<Fixture> {
  const headers = await apiHeaders(page)

  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `Output Limits DB ${Date.now()}`,
      type: 'postgres',
      config: { host: 'localhost', port: 5432, user: 'aether', password: 'aether_dev', database: 'aether' },
    },
  })
  expect(connResp.ok()).toBeTruthy()
  const connector = await connResp.json()

  const nbResp = await request.post('/api/v1/notebooks', { headers, data: { title: 'Output Limits E2E' } })
  expect(nbResp.ok()).toBeTruthy()
  const notebook = await nbResp.json()

  const limitResp = await request.put('/api/v1/org/output-limits', {
    headers,
    data: { cell_output_max_bytes: 2500 },
  })
  expect(limitResp.ok()).toBeTruthy()

  const cellResp = await request.post(`/api/v1/notebooks/${notebook.id}/cells`, {
    headers,
    data: {
      type: 'code',
      language: 'sql',
      source: `SELECT i, repeat('x', 1000) AS payload FROM generate_series(1, 10) AS i`,
      connector_id: connector.id,
    },
  })
  expect(cellResp.ok()).toBeTruthy()
  const cell = await cellResp.json()

  const execResp = await request.post(`/api/v1/notebooks/${notebook.id}/cells/${cell.id}/execute`, {
    headers,
    data: {},
  })
  expect(execResp.ok()).toBeTruthy()

  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { token: token!, notebookId: notebook.id, cellId: cell.id }
}

test.describe('Bounded cell outputs', () => {
  test.beforeEach(async ({ page }) => {
    await registerAndOnboard(page, Date.now().toString())
  })

  test('shows the truncation badge and streams the full result', async ({ page, request }) => {
    const fixture = await createTruncatedCell(page, request)

    await page.goto(`/notebooks/${fixture.notebookId}`)
    await expect(page.getByText(/Truncated —/)).toBeVisible({ timeout: 20_000 })

    const downloadPromise = page.waitForEvent('download')
    await page.getByLabel('Download full result').first().click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe(`cell-${fixture.cellId}.json`)
  })

  test('org settings expose and save the output limits', async ({ page }) => {
    await page.goto('/settings')
    const section = page.locator('section', { hasText: 'Cell Output Limits' })
    await expect(section).toBeVisible()

    const cellInput = section.locator('label', { hasText: 'Max bytes per cell output' }).locator('input')
    await cellInput.fill('5242880')
    await section.locator('button', { hasText: /^Save$/ }).click()
    await expect(section.getByText('Saved')).toBeVisible({ timeout: 10_000 })
    await expect(cellInput).toHaveValue('5242880')
  })
})
