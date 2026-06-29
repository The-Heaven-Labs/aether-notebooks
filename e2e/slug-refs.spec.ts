import { test, expect } from '@playwright/test'
import { registerAndOnboard, createNotebook } from './helpers'

test.describe('Slug references', () => {
  test.beforeEach(async ({ page }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
  })

  test('cell with slug can be referenced by another cell', async ({ page }) => {
    const nbId = await createNotebook(page, 'Slug Test Notebook')
    await page.goto(`/notebooks/${nbId}`)

    const firstCell = page.locator('.cell').first()
    await firstCell.locator('input[placeholder*="slug"]').fill('base_query')
    await firstCell.locator('input[placeholder*="slug"]').press('Tab')

    await page.click('button:has-text("Add Cell")')
    const secondCell = page.locator('.cell').nth(1)
    await secondCell.locator('.cm-editor').click()
    await page.keyboard.type('SELECT * FROM ({{base_query}}) t LIMIT 10')

    await expect(secondCell.locator('.cm-editor')).toContainText('{{base_query}}')
  })
})
