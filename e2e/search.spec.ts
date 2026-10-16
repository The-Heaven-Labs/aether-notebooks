import { test, expect } from '@playwright/test'
import { registerAndOnboard, seedNotebooks, tokenFromPage } from './helpers'

/**
 * Search surface end to end: the Ctrl/Cmd+K palette (open, find a seeded
 * notebook, Enter opens it), the Ctrl/Cmd+J agent chord it displaced, and the
 * `/search` results page (deep link, groups, favorites chip URL sync).
 *
 * The Vite dev server does not inject `window.__AETHER_CONFIG__`, so the relay
 * URL is injected before the app boots (same workaround as
 * e2e/dashboard-live.spec.ts) — the notebook page connects the collab stack.
 */

const RELAY_URL = process.env.E2E_RELAY_URL ?? 'ws://localhost:3001'

test.describe('Search', () => {
  test('Ctrl+K palette finds a notebook, Ctrl+J opens the agent, /search deep links', async ({
    page,
    context,
    request,
  }) => {
    test.setTimeout(60_000)

    await context.addInitScript(
      (cfg) => {
        ;(window as Window & { __AETHER_CONFIG__?: { relayUrl?: string } }).__AETHER_CONFIG__ = {
          relayUrl: cfg.relayUrl,
        }
      },
      { relayUrl: RELAY_URL },
    )

    const suffix = Date.now().toString(36)
    await registerAndOnboard(page, suffix)
    const token = await tokenFromPage(page)
    const title = `Searchable Revenue Ledger ${suffix}`
    const [notebookId] = await seedNotebooks(request, token, null, 1, title)

    // Seed a cell whose SQL carries a term absent from the title, so the
    // search resolves to a content hit. The matched cell id must ride the
    // summary so opening the result deep-links to `#cell-{id}`.
    const contentTerm = `zebrafish${suffix}`
    const cellResp = await request.post(`/api/v1/notebooks/${notebookId}/cells`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { type: 'code', language: 'sql', source: `SELECT ${contentTerm} FROM revenue_entries` },
    })
    expect(cellResp.ok()).toBeTruthy()

    // Ctrl+K opens the palette. Wait for the shell's search field first: the
    // keydown listener attaches with the shell, and an early chord is lost.
    await page.goto('/')
    await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeVisible()
    await page.keyboard.press('Control+k')
    const palette = page.getByRole('dialog', { name: 'Search palette' })
    await expect(palette).toBeVisible()

    // Typing debounces (~150ms) into the search request. Wait for its response
    // first: while the query is still un-debounced the palette renders the
    // Recents empty state, where a matching row would make Enter flush the
    // pending keystroke instead of opening the result.
    const searchResponse = page.waitForResponse((resp) => {
      if (!resp.url().includes('/api/v1/search?')) return false
      return new URL(resp.url()).searchParams.get('q') === contentTerm
    })
    await palette.getByRole('combobox').fill(contentTerm)
    await searchResponse
    const option = palette
      .getByRole('group', { name: 'Notebooks' })
      .getByRole('option', { name: new RegExp(title) })
    await expect(option).toBeVisible()
    await palette.getByRole('combobox').press('Enter')
    // A content hit deep-links to the matched cell; stay order-independent of
    // the exact cell id.
    await expect(page).toHaveURL(new RegExp(`/notebooks/${notebookId}#cell-`))
    await expect(page.getByText(title).first()).toBeVisible({ timeout: 15_000 })

    // Ctrl+J opens the agent panel (the chord moved off Ctrl+K). The chord's
    // listener attaches with the freshly mounted shell, so a press can race
    // the mount and be lost. The FAB disappears synchronously when a press
    // lands (state before the lazy panel chunk renders), so re-press only
    // while it is still there.
    const closeAgent = page.getByTitle('Close agent panel')
    await expect.poll(async () => {
      if ((await page.getByTitle('Open AI Agent (Ctrl+J)').count()) > 0) {
        await page.keyboard.press('Control+j')
      }
      return closeAgent.count()
    }, { timeout: 10_000 }).toBeGreaterThan(0)
    await expect(closeAgent).toBeVisible()
    await closeAgent.click()

    // The /search deep link renders the notebook group with its result row,
    // and the content hit links straight to the matched cell.
    await page.goto(`/search?q=${encodeURIComponent(contentTerm)}`)
    await expect(page.getByRole('heading', { name: /Notebooks · 1/ })).toBeVisible({ timeout: 10_000 })
    const resultLink = page.getByRole('link', { name: new RegExp(title) })
    await expect(resultLink).toBeVisible()
    await expect(resultLink).toHaveAttribute('href', new RegExp(`/notebooks/${notebookId}#cell-`))
    await resultLink.click()
    await expect(page).toHaveURL(new RegExp(`/notebooks/${notebookId}#cell-`))

    // The favorites chip toggles the URL-synced `?favorites=1` filter.
    const favoritesChip = page.getByRole('button', { name: 'Favorites', exact: true })
    await expect(favoritesChip).toHaveAttribute('aria-pressed', 'false')
    await favoritesChip.click()
    await expect(favoritesChip).toHaveAttribute('aria-pressed', 'true')
    await expect(page).toHaveURL(/favorites=1/)
  })
})
