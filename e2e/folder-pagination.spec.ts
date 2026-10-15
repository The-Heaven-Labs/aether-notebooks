import { test, expect } from '@playwright/test'
import { createFolderViaApi, registerAndOnboard, seedNotebooks, tokenFromPage } from './helpers'

/**
 * Folder section pagination: a folder with more notebooks than one page (the
 * API default is 50) reports the exact visible total, hands out pages through
 * Load more, and never shows a duplicate row.
 */

const SEED_COUNT = 120

test.describe('Folder pagination', () => {
  test('folder section load more', async ({ page, request }) => {
    test.setTimeout(120_000)

    const suffix = Date.now().toString(36)
    await registerAndOnboard(page, suffix)
    const token = await tokenFromPage(page)
    const folderId = await createFolderViaApi(request, token, `Paged folder ${suffix}`)
    await seedNotebooks(request, token, folderId, SEED_COUNT, `Paged NB ${suffix}`)

    await page.goto(`/?folder=${folderId}`)

    const notebooksSection = page
      .locator('section')
      .filter({ has: page.getByText('Notebooks', { exact: true }) })
    const rows = notebooksSection.locator('a[href^="/notebooks/"]')

    // First page: 50 rows, counter names the exact server-side total.
    await expect(notebooksSection.getByText('50 of 120', { exact: true })).toBeVisible({ timeout: 15_000 })
    await expect(rows).toHaveCount(50)

    await notebooksSection.getByRole('button', { name: 'Load more notebooks' }).click()
    await expect(notebooksSection.getByText('100 of 120', { exact: true })).toBeVisible()
    await expect(rows).toHaveCount(100)

    await notebooksSection.getByRole('button', { name: 'Load more notebooks' }).click()
    await expect(rows).toHaveCount(SEED_COUNT)
    await expect(notebooksSection.getByRole('button', { name: 'Load more notebooks' })).toHaveCount(0)

    // Every seeded notebook appears exactly once.
    const hrefs = await rows.evaluateAll((els) => els.map((el) => el.getAttribute('href')))
    expect(hrefs).toHaveLength(SEED_COUNT)
    expect(new Set(hrefs).size).toBe(SEED_COUNT)
  })
})
