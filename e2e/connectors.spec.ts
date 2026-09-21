import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

test.describe('Connectors', () => {
  test.beforeEach(async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
  })

  test('create postgres connector with database field', async ({ page }) => {
    await page.goto('/connectors')
    await page.getByRole('button', { name: '+ New Connector' }).click()
    await page.getByLabel('Type').selectOption('postgres')
    await page.getByLabel('Name').fill('Test PG')
    await page.getByLabel('Host').fill('localhost')
    await page.getByLabel('Port').fill('5432')
    await page.getByLabel('Database').fill('aether')
    await page.getByLabel('User').fill('aether')
    await page.getByLabel('Password').fill('aether_dev')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Test PG')).toBeVisible()
  })

  test('create ClickHouse connector without database field', async ({ page }) => {
    await page.goto('/connectors')
    await page.getByRole('button', { name: '+ New Connector' }).click()
    await page.getByLabel('Type').selectOption('clickhouse')
    await page.getByLabel('Name').fill('Test CH')
    await page.getByLabel('Host').fill('localhost')
    await page.getByLabel('User').fill('dev')
    await page.getByLabel('Password').fill('dev')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Test CH')).toBeVisible()
  })

  test('schema browser lists tables for a ClickHouse connector without a default DB', async ({ page, request }) => {
    const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('aether_token'))}` }

    const connResp = await request.post('/api/v1/connectors', {
      headers,
      data: {
        name: 'Test CH Schema',
        type: 'clickhouse',
        config: { host: 'localhost', port: 9000, user: 'dev', password: 'dev', database: '' },
      },
    })
    expect(connResp.ok()).toBeTruthy()

    const nbResp = await request.post('/api/v1/notebooks', { headers, data: { title: 'Schema Browser E2E' } })
    expect(nbResp.ok()).toBeTruthy()
    const notebook = await nbResp.json()

    await page.goto(`/notebooks/${notebook.id}`)
    await page.getByLabel('Select a connector').selectOption({ label: 'Test CH Schema' })
    await page.getByRole('button', { name: /View/ }).click()
    await page.getByRole('button', { name: 'Schema', exact: true }).click()

    // Tables come from the live dev ClickHouse seed (dev/clickhouse-seed.sql).
    await expect(page.getByText('Schema Browser', { exact: true })).toBeVisible()
    await expect(page.getByText('events', { exact: true })).toBeVisible({ timeout: 20_000 })
  })
})
