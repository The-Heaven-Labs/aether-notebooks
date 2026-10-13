import { test, expect } from '@playwright/test'
import type { APIRequestContext, Browser, BrowserContext, Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'
import { joinViaInvite, listMembers, registerViaApi } from './agent-session-helpers'

/**
 * Wave-2 publish surface: the draft → publish split, App chapters (tabs),
 * dashboard theming, and the editor's draft preview.
 *
 * Requires the dev stack (API + relay + web) served by the embedded build on
 * :8088 via E2E_BASE_URL; the API-side Postgres connector host defaults to
 * `localhost` (native API run) and is overridden with E2E_PG_CONNECTOR_HOST
 * when the API runs in Docker (e.g. `aether-postgres`).
 *
 * Run:
 *   E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres \
 *     npx playwright test dashboard-publish.spec.ts
 */
const PG_CONNECTOR_HOST = process.env.E2E_PG_CONNECTOR_HOST ?? 'localhost'

// Pin timezone + locale and freeze the page clock in the visual test below:
// the themed snapshot masks the live footer tail, and the masthead's visible
// "Updated …" line must not drift between runs.
test.use({ timezoneId: 'UTC', locale: 'en-US' })

// Registration, editor document sync, publish round-trips, and the versions
// poll all run against the live stack; the 30s config default is too tight.
test.beforeEach(() => {
  test.setTimeout(120_000)
})

// Viewer contexts opened by the publish-flow test; closed per test so a
// lingering WebSocket cannot leak into later tests.
const openContexts: BrowserContext[] = []
test.afterEach(async () => {
  await Promise.all(openContexts.splice(0).map((ctx) => ctx.close()))
})

async function authHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

interface QueryWidgetSpec {
  query: string
  type?: 'table' | 'metric'
  /** Stored as `config.title`, so the editor's "Edit <title>" controls are unambiguous. */
  title?: string
  config?: Record<string, unknown>
}

interface PublishFixture {
  dashboardId: string
  connectorId: string
  /** Widget ids in creation order. */
  widgetIds: string[]
  headers: { Authorization: string }
}

/** Creates a dev-Postgres connector and a dashboard with one query widget per spec. */
async function createPublishDashboard(
  page: Page,
  request: APIRequestContext,
  opts: { suffix: string; widgets: QueryWidgetSpec[] },
): Promise<PublishFixture> {
  const headers = await authHeaders(page)
  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `Publish DB ${opts.suffix}`,
      type: 'postgres',
      config: {
        host: PG_CONNECTOR_HOST,
        port: 5432,
        user: 'aether',
        password: 'aether_dev',
        database: 'aether',
      },
    },
  })
  expect(connResp.ok()).toBeTruthy()
  const connector = await connResp.json()

  const dashResp = await request.post('/api/v1/dashboards', {
    headers,
    data: { title: `Publish ${opts.suffix}` },
  })
  expect(dashResp.ok()).toBeTruthy()
  const dashboard = await dashResp.json()

  const widgetIds: string[] = []
  for (const [index, widget] of opts.widgets.entries()) {
    const widgetResp = await request.post(`/api/v1/dashboards/${dashboard.id}/widgets`, {
      headers,
      data: {
        connector_id: connector.id,
        query: widget.query,
        language: 'sql',
        type: widget.type ?? 'table',
        layout: { row: index * 6, col: 0, width: 6, height: 6 },
        config: {
          ...(widget.title ? { title: widget.title } : {}),
          ...(widget.config ?? {}),
        },
      },
    })
    expect(widgetResp.ok()).toBeTruthy()
    widgetIds.push(((await widgetResp.json()) as { id: string }).id)
  }

  return { dashboardId: dashboard.id, connectorId: connector.id, widgetIds, headers }
}

/** Opens the editor and waits until the shared document has armed editing. */
async function openEditor(page: Page, dashboardId: string): Promise<void> {
  await page.goto(`/dashboards/${dashboardId}`)
  await expect(page.getByRole('button', { name: '+ Add Widget' })).toBeEnabled({ timeout: 20_000 })
}

/**
 * Renames the dashboard through the editor's title control (a live-document
 * write) and waits until the stored draft reflects it. The relay debounces
 * document stores, and publishing must snapshot the rename — not the title
 * from before it.
 */
async function renameDraft(
  page: Page,
  request: APIRequestContext,
  headers: { Authorization: string },
  dashboardId: string,
  next: string,
): Promise<void> {
  await page.locator('span[title="Click to rename"]').click()
  const input = page.locator('header input')
  await input.fill(next)
  await input.press('Enter')
  await expect(page.locator('span[title="Click to rename"]')).toHaveText(next)
  await expect.poll(async () => {
    const resp = await request.get(`/api/v1/dashboards/${dashboardId}`, { headers })
    if (!resp.ok()) return null
    return ((await resp.json()) as { title: string }).title
  }, { timeout: 15_000 }).toBe(next)
}

/** Publishes from the editor and waits for the status chip to acknowledge the new version. */
async function publishViaEditor(page: Page, version: number): Promise<void> {
  await page.getByRole('button', { name: 'Publish', exact: true }).click()
  await expect(page.getByTestId('publish-status'))
    .toHaveAccessibleName(new RegExp(`^v${version} `), { timeout: 15_000 })
}

/**
 * Registers a second org member, grants them view access to the dashboard and
 * connector, and returns their session. PUT replaces direct ACL entries, so
 * the owner's entries ride along in the same payload.
 */
async function addViewer(
  request: APIRequestContext,
  ownerHeaders: { Authorization: string },
  suffix: string,
  dashboardId: string,
  connectorId: string,
): Promise<{ token: string; email: string }> {
  const email = `viewer-${suffix}@example.com`
  const onboardingToken = await registerViaApi(request, email, `E2E Viewer ${suffix}`)
  const token = await joinViaInvite(request, ownerHeaders, onboardingToken)

  const members = await listMembers(request, ownerHeaders)
  const viewer = members.find((m) => m.email === email)
  const owner = members.find((m) => m.email !== email)
  if (!viewer || !owner) throw new Error('viewer or owner missing from org members')

  const dashAcl = await request.put(`/api/v1/acl/dashboard/${dashboardId}`, {
    headers: ownerHeaders,
    data: {
      entries: [
        { subject_type: 'user', subject_id: owner.user_id, actions: ['view', 'view_with_data', 'edit', 'delete', 'share'] },
        { subject_type: 'user', subject_id: viewer.user_id, actions: ['view', 'view_with_data'] },
      ],
    },
  })
  expect(dashAcl.ok()).toBeTruthy()
  const connAcl = await request.put(`/api/v1/acl/connector/${connectorId}`, {
    headers: ownerHeaders,
    data: {
      entries: [
        { subject_type: 'user', subject_id: owner.user_id, actions: ['view', 'use'] },
        { subject_type: 'user', subject_id: viewer.user_id, actions: ['view', 'use'] },
      ],
    },
  })
  expect(connAcl.ok()).toBeTruthy()
  return { token, email }
}

/** Opens a second browser context signed in as the viewer. */
async function openViewer(browser: Browser, viewer: { token: string; email: string }): Promise<Page> {
  const context = await browser.newContext()
  openContexts.push(context)
  await context.addInitScript((cfg) => {
    localStorage.setItem('aether_token', cfg.token)
    localStorage.setItem('aether_user_email', cfg.email)
  }, viewer)
  return context.newPage()
}

/** Assigns a widget to a chapter through its config drawer's Placement select. */
async function assignWidgetTab(page: Page, widgetTitle: string, tabTitle: string): Promise<void> {
  await page.getByRole('button', { name: `Edit ${widgetTitle}` }).click()
  const drawer = page.getByRole('dialog', { name: 'Widget configuration' })
  await expect(drawer).toBeVisible()
  await drawer.getByLabel('Placement').selectOption({ label: tabTitle })
  await drawer.getByRole('button', { name: 'Close widget configuration' }).click()
  await expect(drawer).not.toBeVisible()
}

test.describe('Dashboard publish, tabs, theming, and draft preview', () => {
  test('draft edits stay hidden from viewers until published; restore reverts the draft', async ({ page, request, browser }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const fx = await createPublishDashboard(page, request, {
      suffix: ts,
      widgets: [{ query: "SELECT 'live-value' AS marker" }],
    })
    const publishedTitle = `Publish ${ts}`
    const renamedTitle = `Renamed ${ts}`

    // The editor publishes the initial draft as v1.
    await openEditor(page, fx.dashboardId)
    await publishViaEditor(page, 1)

    // A true viewer (no edit ACL) sees the published app.
    const viewer = await addViewer(request, fx.headers, ts, fx.dashboardId, fx.connectorId)
    const viewerPage = await openViewer(browser, viewer)
    const viewerCell = () => viewerPage.locator('td[data-row="0"][data-col="0"]')
    await viewerPage.goto(`/dashboards/${fx.dashboardId}/view`)
    await expect(viewerPage.getByRole('heading', { name: publishedTitle })).toBeVisible()
    await expect(viewerCell()).toHaveText('live-value')

    // Draft edits: the title through the editor (a document write) and the
    // widget's SQL over REST (the same write path the agent tools use).
    await renameDraft(page, request, fx.headers, fx.dashboardId, renamedTitle)
    const widgetEdit = await request.put(`/api/v1/dashboards/${fx.dashboardId}/widgets/${fx.widgetIds[0]}`, {
      headers: fx.headers,
      data: { query: "SELECT 'draft-value' AS marker" },
    })
    expect(widgetEdit.ok()).toBeTruthy()

    // The published app is unchanged — for the editor's own /view ...
    await page.goto(`/dashboards/${fx.dashboardId}/view`)
    await expect(page.getByRole('heading', { name: publishedTitle })).toBeVisible()
    await expect(page.getByText(renamedTitle)).toHaveCount(0)
    await expect(page.locator('td[data-row="0"][data-col="0"]')).toHaveText('live-value')
    await expect(page.getByText('draft-value')).toHaveCount(0)
    // ... and for the viewer.
    await viewerPage.reload()
    await expect(viewerPage.getByRole('heading', { name: publishedTitle })).toBeVisible()
    await expect(viewerCell()).toHaveText('live-value')

    // Publishing from the editor makes both edits live.
    await openEditor(page, fx.dashboardId)
    await publishViaEditor(page, 2)
    await page.goto(`/dashboards/${fx.dashboardId}/view`)
    await expect(page.getByRole('heading', { name: renamedTitle })).toBeVisible()
    await expect(page.locator('td[data-row="0"][data-col="0"]')).toHaveText('draft-value')
    await viewerPage.reload()
    await expect(viewerPage.getByRole('heading', { name: renamedTitle })).toBeVisible()
    await expect(viewerCell()).toHaveText('draft-value')

    // Another draft edit flips the editor's status chip to Unpublished changes.
    await openEditor(page, fx.dashboardId)
    await renameDraft(page, request, fx.headers, fx.dashboardId, `Draft ${ts}`)
    await expect(page.getByTestId('publish-status-dot')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByTestId('publish-status'))
      .toHaveAccessibleName(/Unpublished changes/, { timeout: 20_000 })

    // Restore v1: the draft reverts, the live app keeps serving v2.
    await page.getByTestId('publish-status').click()
    const history = page.getByRole('dialog', { name: 'Version history' })
    await expect(history).toBeVisible()
    await expect(history.getByTestId('version-row')).toHaveCount(2)
    await history.getByRole('button', { name: 'Restore v1' }).click()
    await page.getByRole('dialog', { name: 'Restore version v1?' }).getByRole('button', { name: 'Restore' }).click()
    await expect(history.getByText('Restored v1 into the draft.')).toBeVisible()
    await history.getByRole('button', { name: 'Close version history' }).click()

    // The editor's draft is back to v1 (a reload reads the restored document) ...
    await openEditor(page, fx.dashboardId)
    await expect(page.locator('span[title="Click to rename"]')).toHaveText(publishedTitle)
    // ... including the widget's SQL: restore copies the whole snapshot, not
    // just the title.
    await expect.poll(async () => {
      const resp = await request.get(`/api/v1/dashboards/${fx.dashboardId}`, { headers: fx.headers })
      if (!resp.ok()) return null
      const body = (await resp.json()) as { widgets: Array<{ id: string; query?: string }> }
      return body.widgets.find((w) => w.id === fx.widgetIds[0])?.query ?? null
    }, { timeout: 15_000 }).toBe("SELECT 'live-value' AS marker")
    // ... while /view and the viewer still render the last published version (v2).
    await page.goto(`/dashboards/${fx.dashboardId}/view`)
    await expect(page.getByRole('heading', { name: renamedTitle })).toBeVisible()
    await expect(page.locator('td[data-row="0"][data-col="0"]')).toHaveText('draft-value')
    await viewerPage.reload()
    await expect(viewerPage.getByRole('heading', { name: renamedTitle })).toBeVisible()
    await expect(viewerCell()).toHaveText('draft-value')
  })

  test('App chapters render on /view and sync ?tab=', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const fx = await createPublishDashboard(page, request, {
      suffix: ts,
      widgets: [
        // Metric widgets: their reading renders inside the hidden chapter too
        // (a virtualized table body would not), so "mounted but hidden" is a
        // real DOM state the assertions can pin.
        { title: 'Alpha', type: 'metric', query: 'SELECT 41 AS marker', config: { metric: { value_column: 'marker', aggregation: 'none' } } },
        { title: 'Beta', type: 'metric', query: 'SELECT 42 AS marker', config: { metric: { value_column: 'marker', aggregation: 'none' } } },
      ],
    })

    // Add two chapters through the App settings panel.
    await openEditor(page, fx.dashboardId)
    await page.getByRole('button', { name: 'App', exact: true }).click()
    const appSettings = page.getByRole('dialog', { name: 'App settings' })
    await expect(appSettings).toBeVisible()
    await appSettings.getByRole('button', { name: 'Add tab' }).click()
    await appSettings.getByRole('button', { name: 'Add tab' }).click()
    await appSettings.getByLabel('Tab 1 title').fill('Overview')
    await appSettings.getByLabel('Tab 2 title').fill('Orders')
    await appSettings.getByRole('button', { name: 'Save app settings' }).click()
    await expect(appSettings.getByText('App settings saved')).toBeVisible()
    await appSettings.getByRole('button', { name: 'Close app settings' }).click()

    // The editor strip reflects the saved chapters once the document round-trips.
    const strip = page.getByRole('group', { name: 'Dashboard chapters' })
    await expect(strip.getByRole('button', { name: 'Overview' })).toBeVisible({ timeout: 15_000 })
    await expect(strip.getByRole('button', { name: 'Orders' })).toBeVisible()

    // Place each widget into a chapter via its drawer's Placement select.
    await assignWidgetTab(page, 'Alpha', 'Overview')
    await assignWidgetTab(page, 'Beta', 'Orders')

    // Placements are live-document writes; wait until the stored draft has
    // them before publishing (the relay store is debounced). "Tab 1"/"Tab 2"
    // slug to the stable ids tab-1/tab-2.
    await expect.poll(async () => {
      const resp = await request.get(`/api/v1/dashboards/${fx.dashboardId}`, { headers: fx.headers })
      if (!resp.ok()) return null
      const body = (await resp.json()) as { widgets: Array<{ id: string; layout?: { tab?: string } }> }
      return body.widgets.map((w) => `${w.id}:${w.layout?.tab ?? ''}`).sort()
    }, { timeout: 15_000 }).toEqual([`${fx.widgetIds[0]}:tab-1`, `${fx.widgetIds[1]}:tab-2`].sort())

    await publishViaEditor(page, 1)

    // /view renders the chapter bar; switching chapters swaps the visible
    // reading and writes ?tab= (replacing, so it stays out of the back stack).
    // Both chapters stay mounted — the inactive reading is in the DOM but
    // hidden, not absent — so pin the count before the visibility assertion:
    // `not.toBeVisible` alone would also pass if the widget were missing. The
    // locators are plain CSS (not getByRole): role selectors exclude
    // `display:none` subtrees from the accessibility tree, which is exactly
    // the state being pinned.
    const alphaReading = page.locator('[role="group"][aria-label="41"]')
    const betaReading = page.locator('[role="group"][aria-label="42"]')
    await page.goto(`/dashboards/${fx.dashboardId}/view`)
    await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
    await expect(alphaReading).toHaveCount(1)
    await expect(alphaReading).toBeVisible()
    await expect(betaReading).toHaveCount(1)
    await expect(betaReading).not.toBeVisible()

    await page.getByRole('tab', { name: 'Orders' }).click()
    await expect(page.getByRole('tab', { name: 'Orders' })).toHaveAttribute('aria-selected', 'true')
    await expect(betaReading).toHaveCount(1)
    await expect(betaReading).toBeVisible()
    await expect(alphaReading).toHaveCount(1)
    await expect(alphaReading).not.toBeVisible()
    await expect(page).toHaveURL(new RegExp(`/dashboards/${fx.dashboardId}/view\\?tab=tab-2$`))

    // A reload lands on the same chapter (the URL is the source of truth).
    await page.reload()
    await expect(page.getByRole('tab', { name: 'Orders' })).toHaveAttribute('aria-selected', 'true')
    await expect(betaReading).toHaveCount(1)
    await expect(betaReading).toBeVisible()
  })

  test('the app surface takes the dashboard theme (visual)', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const fx = await createPublishDashboard(page, request, {
      suffix: ts,
      widgets: [{
        type: 'metric',
        // Eight rows so the configured sparkline renders (≥2 numeric rows);
        // the sparkline stroke consumes --accent, so the accent override is
        // visible in the snapshot instead of only existing as a token.
        query: 'SELECT i AS step, 40 + i AS revenue, 40 AS prev FROM generate_series(1, 8) AS i',
        config: {
          metric: {
            value_column: 'revenue',
            aggregation: 'none',
            comparison: { column: 'prev', mode: 'percent' },
            sparkline: { x_column: 'step', y_column: 'revenue' },
          },
        },
      }],
    })

    // A light scheme (the global default is dark, so this visibly flips the
    // surface), canvas/card overrides, an accent the sparkline paints, and a
    // masthead title — all through the App settings panel. The accent is a
    // dark indigo so it clears the app-theme contrast guardrail against the
    // light canvas.
    await openEditor(page, fx.dashboardId)
    await page.getByRole('button', { name: 'App', exact: true }).click()
    const appSettings = page.getByRole('dialog', { name: 'App settings' })
    await expect(appSettings).toBeVisible()
    await appSettings.getByLabel('App title').fill('Themed Brief')
    await appSettings.getByLabel('Accent', { exact: true }).fill('#4338ca')
    await appSettings.getByLabel('Background', { exact: true }).fill('#eef2ff')
    await appSettings.getByLabel('Surface', { exact: true }).fill('#faf5ff')
    await appSettings.getByLabel('Color scheme').selectOption('light')
    await appSettings.getByRole('button', { name: 'Save app settings' }).click()
    await expect(appSettings.getByText('App settings saved')).toBeVisible()
    await appSettings.getByRole('button', { name: 'Close app settings' }).click()

    await publishViaEditor(page, 1)

    // Freeze the clock before the app surface loads: the masthead's visible
    // "Updated …" line must not drift between runs (the footer row is masked).
    await page.clock.setFixedTime(new Date('2026-06-01T12:00:00Z'))
    await page.goto(`/dashboards/${fx.dashboardId}/view`)

    // The scoped tokens prove the theme resolved (not just the attribute):
    // the container's canvas is the background override, and the accent is
    // what the sparkline stroke consumes.
    const themed = page.locator('.app-theme')
    await expect(themed).toHaveAttribute('data-app-scheme', 'light')
    await expect(themed).toHaveCSS('background-color', 'rgb(238, 242, 255)')
    await expect(themed).toHaveCSS('--bg-card', '#faf5ff')
    await expect(themed).toHaveCSS('--accent', '#4338ca')
    await expect(page.getByRole('heading', { name: 'Themed Brief' })).toBeVisible()
    await expect(page.getByRole('group', { name: '41, +2.5%' })).toBeVisible()

    // Guard the mask locator: if the footer text ever changes, this fails
    // loudly here instead of silently dropping the mask and letting the pixel
    // comparison swallow a live-timestamp/duration diff.
    await expect(page.getByText(/^Executed at /)).toBeVisible()

    await expect(page).toHaveScreenshot('dashboard-app-themed.png', {
      // Mask the footer's parent row: it is fixed-size, while the "Executed
      // at …" span's width drifts with the live `cached`/`Nms` tail, which
      // would move the mask box. Exact comparison — the clock freeze keeps
      // the remaining pixels stable.
      mask: [page.getByText(/^Executed at /).locator('..')],
      maxDiffPixelRatio: 0,
    })
  })

  test('?preview=draft shows the editor the draft; the plain path shows published', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const fx = await createPublishDashboard(page, request, {
      suffix: ts,
      widgets: [{ query: "SELECT 'live-value' AS marker" }],
    })
    const publishedTitle = `Publish ${ts}`
    const draftTitle = `Draft ${ts}`

    await openEditor(page, fx.dashboardId)
    await publishViaEditor(page, 1)
    await renameDraft(page, request, fx.headers, fx.dashboardId, draftTitle)

    // Plain /view: the published snapshot, with the way into the draft preview.
    await page.goto(`/dashboards/${fx.dashboardId}/view`)
    await expect(page.getByRole('heading', { name: publishedTitle })).toBeVisible()
    await expect(page.getByText(draftTitle)).toHaveCount(0)
    await expect(page.getByText('Draft preview')).toHaveCount(0)
    await expect(page.getByRole('link', { name: /Preview draft/ }))
      .toHaveAttribute('href', `/dashboards/${fx.dashboardId}/view?preview=draft`)

    // Draft preview: the badge, the draft title, and the way back.
    await page.goto(`/dashboards/${fx.dashboardId}/view?preview=draft`)
    await expect(page.getByText('Draft preview')).toBeVisible()
    await expect(page.getByRole('heading', { name: draftTitle })).toBeVisible()
    await expect(page.getByRole('link', { name: /View published/ }))
      .toHaveAttribute('href', `/dashboards/${fx.dashboardId}/view`)

    // The draft-preview action set (LIVE + DRAFT PREVIEW + View published on
    // top of the usual controls) is wider than a 1280px masthead row, so the
    // header must wrap instead of crushing the title block to an ellipsis
    // (regression: the h1 measured ~10px wide before the wrap fix).
    const draftTitleBox = await page.getByRole('heading', { name: draftTitle }).boundingBox()
    expect(draftTitleBox?.width ?? 0).toBeGreaterThan(180)
  })
})
