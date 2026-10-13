import { test, expect } from '@playwright/test'
import type { APIRequestContext, Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * HTML panels (data apps wave 3): the sanitized, sandboxed renderer, the
 * drawer authoring surface, and the org kill switch.
 *
 * Requires a running stack and points at it via E2E_BASE_URL (default :5173;
 * the embedded build on :8088 serves this working copy and injects the relay
 * config the editor connects with).
 */
test.use({ timezoneId: 'UTC', locale: 'en-US' })

async function authHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

/** Markup with a heading, a script, and an off-origin image: only the heading survives. */
const PANEL_HTML = [
  '<h2 class="stat">Sanitized Panel</h2>',
  '<script>window.__pwned = true</script>',
  '<img src="https://evil.example/x.png" alt="evil">',
  '<p id="body-copy">Plain text survives.</p>',
].join('')
const PANEL_CSS = '.stat { color: rgb(200, 10, 20); }'

/** Creates a dashboard with one html widget and returns its id. */
async function createPanelDashboard(
  page: Page,
  request: APIRequestContext,
  suffix: string,
): Promise<string> {
  const headers = await authHeaders(page)
  const dashResp = await request.post('/api/v1/dashboards', {
    headers,
    data: { title: `Panel ${suffix}` },
  })
  expect(dashResp.ok()).toBeTruthy()
  const dashboard = await dashResp.json()

  const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
    headers,
    data: {
      type: 'html',
      layout: { row: 0, col: 0, width: 6, height: 6 },
      config: { html: PANEL_HTML, css: PANEL_CSS },
    },
  })
  expect(widgetResp.ok()).toBeTruthy()
  return dashboard.id as string
}

test.describe('HTML panels', () => {
  test('renders sanitized markup inside the sandboxed frame', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const dashboardId = await createPanelDashboard(page, request, ts)

    await page.goto(`/dashboards/${dashboardId}/view`)

    const frame = page.frameLocator('iframe[title="HTML panel"]')
    await expect(frame.getByRole('heading', { name: 'Sanitized Panel' })).toBeVisible()
    // The stylesheet applies inside the frame.
    expect(await frame.locator('h2.stat').evaluate((el) => getComputedStyle(el).color)).toBe(
      'rgb(200, 10, 20)',
    )
    // Scripts and off-origin image sources are stripped before the frame
    // composes. DOMPurify removes the disallowed `src` attribute, so the
    // surviving <img> element must carry no source at all.
    await expect(frame.locator('script')).toHaveCount(0)
    await expect(frame.locator('img[src]')).toHaveCount(0)
    expect(
      await frame.locator('body').evaluate(
        () => (window as unknown as { __pwned?: boolean }).__pwned,
      ),
    ).toBeUndefined()

    // The public page renders the same static panel through the share token
    // (no relay, no session).
    const shareResp = await request.post(`/api/v1/dashboards/${dashboardId}/share`, { headers: await authHeaders(page) })
    expect(shareResp.ok()).toBeTruthy()
    const share = (await shareResp.json()) as { token: string }
    await page.goto(`/public/dashboards/${share.token}`)
    await expect(
      page.frameLocator('iframe[title="HTML panel"]').getByRole('heading', { name: 'Sanitized Panel' }),
    ).toBeVisible()
  })

  test('authors panel markup and stylesheet in the widget drawer', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const dashboardId = await createPanelDashboard(page, request, ts)

    await page.goto(`/dashboards/${dashboardId}`)
    await page.getByRole('button', { name: 'Edit widget' }).first().click()

    const drawer = page.getByRole('dialog', { name: 'Widget configuration' })
    // Role textbox: `getByLabel` would also match the HelpTip trigger button
    // (`aria-label="Help: Panel HTML"`).
    const htmlEditor = drawer.getByRole('textbox', { name: 'Panel HTML' })
    const cssEditor = drawer.getByRole('textbox', { name: 'Panel CSS' })
    await expect(htmlEditor).toHaveValue(PANEL_HTML)
    await expect(cssEditor).toHaveValue(PANEL_CSS)
    const preview = drawer.frameLocator('iframe[title="HTML panel"]')
    await expect(preview.getByRole('heading', { name: 'Sanitized Panel' })).toBeVisible()

    // Typing updates the preview once the debounced save lands.
    await htmlEditor.fill('<h2>Live Preview</h2>')
    await expect(preview.getByRole('heading', { name: 'Live Preview' })).toBeVisible()
  })

  test('the org kill switch replaces panels and refuses new ones', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const dashboardId = await createPanelDashboard(page, request, ts)
    const headers = await authHeaders(page)

    const disabled = await request.put('/api/v1/org/settings', {
      headers,
      data: { html_panels_enabled: false },
    })
    expect(disabled.ok()).toBeTruthy()

    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(page.getByText('HTML panels are disabled for this organization.')).toBeVisible()
    await expect(page.locator('iframe[title="HTML panel"]')).toHaveCount(0)

    // Creation fails closed while the switch is off.
    const refused = await request.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
      headers,
      data: { type: 'html', layout: { row: 6, col: 0, width: 4, height: 4 } },
    })
    expect(refused.status()).toBe(400)

    // Re-enabling restores the retained panel.
    const reenabled = await request.put('/api/v1/org/settings', {
      headers,
      data: { html_panels_enabled: true },
    })
    expect(reenabled.ok()).toBeTruthy()
    await page.goto(`/dashboards/${dashboardId}/view`)
    await expect(
      page.frameLocator('iframe[title="HTML panel"]').getByRole('heading', { name: 'Sanitized Panel' }),
    ).toBeVisible()
  })
})
