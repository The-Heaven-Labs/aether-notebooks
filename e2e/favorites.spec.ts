import { test, expect } from '@playwright/test'
import { createFolderViaApi, registerAndOnboard, seedNotebooks, tokenFromPage } from './helpers'

/**
 * Favorites end to end: bookmark a notebook from its folder row, flip the
 * Files favorites chip (`?fav=1`, flat cross-folder list with folder paths),
 * reload for server persistence, see it in the palette's empty-query
 * Favorites section, then un-bookmark from the favorites view and land on the
 * empty state.
 */

test.describe('Favorites', () => {
  test('favorites toggle, filter, and persistence', async ({ page, request }) => {
    const suffix = Date.now().toString(36)
    await registerAndOnboard(page, suffix)
    const token = await tokenFromPage(page)
    const folderName = `Bookmarks ${suffix}`
    const folderId = await createFolderViaApi(request, token, folderName)
    const title = `Bookmark Me ${suffix}`
    await seedNotebooks(request, token, folderId, 1, title)

    // Bookmark the notebook from its Files row. The toggle is hover-revealed
    // (opacity-only), so hover the row first, then click.
    await page.goto(`/?folder=${folderId}`)
    const row = page.locator('.file-list-item', { hasText: title })
    await expect(row).toBeVisible()
    await row.hover()
    const addButton = row.getByRole('button', { name: 'Add to favorites' })
    const putDone = page.waitForResponse(
      (resp) => resp.url().includes('/api/v1/favorites/notebook/') && resp.request().method() === 'PUT',
    )
    await addButton.click()
    expect((await putDone).status()).toBe(204)
    await expect(row.getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true')

    // The Favorites chip flips to the flat cross-folder view, URL-synced.
    await page.getByRole('button', { name: 'Favorites', exact: true }).click()
    await expect(page).toHaveURL(/[?&]fav=1/)
    const favoriteRow = page.locator('.favorite-host', { hasText: title })
    await expect(favoriteRow).toBeVisible()
    await expect(favoriteRow).toContainText(folderName)

    // Reload: the favorite is server-persisted, not local state.
    await page.reload()
    await expect(page.locator('.favorite-host', { hasText: title })).toBeVisible({ timeout: 10_000 })

    // The palette's empty-query state lists it under Favorites.
    await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeVisible()
    await page.keyboard.press('Control+k')
    const palette = page.getByRole('dialog', { name: 'Search palette' })
    await expect(palette).toBeVisible()
    await expect(
      palette.getByRole('group', { name: 'Favorites' }).getByRole('option', { name: new RegExp(title) }),
    ).toBeVisible({ timeout: 10_000 })
    await page.keyboard.press('Escape')
    await expect(palette).toBeHidden()

    // Un-bookmark from the favorites view: the row disappears and the empty
    // state takes over.
    await favoriteRow.hover()
    const deleteDone = page.waitForResponse(
      (resp) => resp.url().includes('/api/v1/favorites/notebook/') && resp.request().method() === 'DELETE',
    )
    await favoriteRow.getByRole('button', { name: 'Remove from favorites' }).click()
    expect((await deleteDone).status()).toBe(204)
    await expect(page.locator('.favorite-host', { hasText: title })).toHaveCount(0)
    await expect(page.getByText('No favorites yet')).toBeVisible()
  })
})
