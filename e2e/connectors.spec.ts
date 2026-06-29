import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

test.describe('Connectors', () => {
  test.beforeEach(async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
  })

  test('create postgres connector with database field', async ({ page }) => {
    await page.goto('/connectors')
    await page.click('button:has-text("New Connector")')
    await page.selectOption('select[name="type"]', 'postgres')
    await page.fill('input[name="name"]', 'Test PG')
    await page.fill('input[name="host"]', 'localhost')
    await page.fill('input[name="port"]', '5432')
    await page.fill('input[name="database"]', 'aether')
    await page.fill('input[name="user"]', 'aether')
    await page.fill('input[name="password"]', 'aether_dev')
    await page.click('button:has-text("Save")')
    await expect(page.locator('text=Test PG')).toBeVisible()
  })

  test('create ClickHouse connector without database field', async ({ page }) => {
    await page.goto('/connectors')
    await page.click('button:has-text("New Connector")')
    await page.selectOption('select[name="type"]', 'clickhouse')
    await page.fill('input[name="name"]', 'Test CH')
    await page.fill('input[name="host"]', 'localhost')
    await page.fill('input[name="port"]', '9000')
    await page.fill('input[name="user"]', 'default')
    await page.click('button:has-text("Save")')
    await expect(page.locator('text=Test CH')).toBeVisible()
  })

  test('schema browser shows database picker for connector without default DB', async ({ page }) => {
    await page.goto('/notebooks')
    await page.click('text=New Notebook')
    await page.locator('select').first().selectOption({ label: /Test CH/ })
    await page.click('button:has-text("Schema")')
    await expect(page.locator('text=select database')).toBeVisible()
  })
})
