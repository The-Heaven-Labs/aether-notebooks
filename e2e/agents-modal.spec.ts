import { test, expect } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// Regression guard for the v0.65.0 agent modal: the two-pane panel used a grid
// row that sized to its content, so on desktop the body was clipped by the
// panel's overflow instead of scrolling — the MCP Servers section and the
// rail's Create/Cancel buttons sat below the visible edge and were unreachable.
test('new agent modal scrolls to MCP Servers and keeps rail actions visible', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/agents')

  await page.getByRole('button', { name: '+ New Agent' }).first().click()
  const panel = page.locator('.agent-modal-panel')
  const body = page.locator('.agent-modal-body')
  await expect(panel).toBeVisible()

  // The body itself must be the scroll container (not the clipped panel).
  const { clientHeight, scrollHeight } = await body.evaluate((el) => ({
    clientHeight: el.clientHeight,
    scrollHeight: el.scrollHeight,
  }))
  expect(scrollHeight).toBeGreaterThan(clientHeight)

  await body.evaluate((el) => {
    el.scrollTop = el.scrollHeight
  })

  // Scrolling reaches the MCP Servers section...
  await expect(page.getByPlaceholder('Search MCP servers...')).toBeInViewport()
  // ...and the rail actions stay pinned inside the panel.
  await expect(page.locator('.agent-modal-foot').getByRole('button', { name: 'Create' })).toBeInViewport()
  await expect(page.locator('.agent-modal-foot').getByRole('button', { name: 'Cancel' })).toBeInViewport()
})
