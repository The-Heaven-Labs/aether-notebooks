import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import fs from 'node:fs/promises'
import { registerAndOnboard } from './helpers'

// End-to-end coverage for bounded cell outputs: the executor byte cap marks
// oversized results as truncated, the notebook view surfaces a truncation badge
// (or a read-path stub when the inline budget is exhausted), the full payload
// streams from the download endpoint, and the org-level settings round-trip.

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

// createCell provisions a connector, notebook, and cell but does not execute it.
async function createCell(page: Page, request: APIRequestContext, source: string): Promise<Fixture> {
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

  const cellResp = await request.post(`/api/v1/notebooks/${notebook.id}/cells`, {
    headers,
    data: { type: 'code', language: 'sql', source, connector_id: connector.id },
  })
  expect(cellResp.ok()).toBeTruthy()
  const cell = await cellResp.json()

  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { token: token!, notebookId: notebook.id, cellId: cell.id }
}

async function executeCell(request: APIRequestContext, fixture: Fixture): Promise<void> {
  const resp = await request.post(`/api/v1/notebooks/${fixture.notebookId}/cells/${fixture.cellId}/execute`, {
    headers: { Authorization: `Bearer ${fixture.token}` },
    data: {},
  })
  expect(resp.ok()).toBeTruthy()
}

async function setOutputLimits(page: Page, request: APIRequestContext, body: Record<string, number>): Promise<void> {
  const headers = await apiHeaders(page)
  const resp = await request.put('/api/v1/org/output-limits', { headers, data: body })
  expect(resp.ok()).toBeTruthy()
}

test.describe('Bounded cell outputs', () => {
  test.beforeEach(async ({ page }) => {
    await registerAndOnboard(page, Date.now().toString())
  })

  test('shows the truncation badge and streams the full result', async ({ page, request }) => {
    const fixture = await createCell(page, request, `SELECT i, repeat('x', 1000) AS payload FROM generate_series(1, 10) AS i`)
    await setOutputLimits(page, request, { cell_output_max_bytes: 2500 })
    await executeCell(request, fixture)

    await page.goto(`/notebooks/${fixture.notebookId}`)
    await expect(page.getByText(/Truncated —/)).toBeVisible({ timeout: 20_000 })

    const downloadPromise = page.waitForEvent('download')
    await page.getByLabel('Download full result').first().click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe(`cell-${fixture.cellId}.json`)
  })

  test('renders the read-path stub and downloads the full stored payload', async ({ page, request }) => {
    // Execute with the default cap so the full result is stored, then shrink
    // the notebook inline budget so the next GET stubs the cell. Random hex
    // keeps the jsonb value incompressible so pg_column_size exceeds the budget.
    const fixture = await createCell(page, request, `SELECT i, md5(random()::text) || md5(random()::text) AS payload FROM generate_series(1, 100) AS i`)
    await executeCell(request, fixture)
    await setOutputLimits(page, request, { notebook_inline_outputs_max_bytes: 1024 })

    await page.goto(`/notebooks/${fixture.notebookId}`)
    await expect(page.getByText(/Output truncated — .* not inlined/)).toBeVisible({ timeout: 20_000 })

    const downloadPromise = page.waitForEvent('download')
    await page.getByLabel('Download full result').first().click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe(`cell-${fixture.cellId}.json`)

    // The download must be the real stored payload, not the stub.
    const path = await download.path()
    expect(path).toBeTruthy()
    const content = await fs.readFile(path!, 'utf8')
    expect(content).toContain('"payload"')
    // The stored payload (~7.5KB) is far larger than the 1KB inline budget,
    // proving the endpoint streamed the real column rather than the stub.
    expect(content.length).toBeGreaterThan(5_000)
  })

  test('JSON export link downloads the raw outputs', async ({ page, request }) => {
    const fixture = await createCell(page, request, `SELECT 42 AS answer`)
    await executeCell(request, fixture)

    await page.goto(`/notebooks/${fixture.notebookId}`)
    // The export controls only render once the table output has loaded.
    await expect(page.getByLabel('Download as JSON')).toBeVisible({ timeout: 20_000 })

    const downloadPromise = page.waitForEvent('download')
    await page.getByLabel('Download as JSON').click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe(`cell-${fixture.cellId}.json`)

    const path = await download.path()
    const content = await fs.readFile(path!, 'utf8')
    expect(content).toContain('"rows"')
  })

  test('hides export and download controls when data export is disabled', async ({ page, request }) => {
    const fixture = await createCell(page, request, `SELECT 7 AS n`)
    await executeCell(request, fixture)

    const headers = await apiHeaders(page)
    const disableResp = await request.put('/api/v1/org/data-export', { headers, data: { data_export_enabled: false } })
    expect(disableResp.ok()).toBeTruthy()

    await page.goto(`/notebooks/${fixture.notebookId}`)
    await expect(page.getByText(/1 row · 1 columns/)).toBeVisible({ timeout: 20_000 })
    await expect(page.getByLabel('Download as JSON')).toHaveCount(0)
    await expect(page.getByLabel('Download as CSV')).toHaveCount(0)
    await expect(page.getByLabel('Download full result')).toHaveCount(0)
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
