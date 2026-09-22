import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * End-to-end coverage for the result-table UX added in the upstream UX fixes:
 * rectangular cell selection, TSV copy from the data model, and per-column
 * resize via the header handle.
 *
 * Requires a dev stack with a reachable Postgres for the connector (native API
 * run: localhost:5432, same assumption as output-limits.spec.ts).
 */

interface Fixture {
  token: string
  notebookId: string
  cellId: string
}

const SQL = `SELECT 1 AS id, 'Alice' AS name, NULL::jsonb AS meta
UNION ALL SELECT 2, 'Bob', '{"env":"prod","v":3}'::jsonb
UNION ALL SELECT 3, 'Carol', '{"plain":true}'::jsonb
UNION ALL SELECT 4, 'Dave', NULL::jsonb
ORDER BY id`

async function apiHeaders(page: Page): Promise<Record<string, string>> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  return { Authorization: `Bearer ${token}` }
}

async function seedTable(page: Page, request: APIRequestContext): Promise<Fixture> {
  const headers = await apiHeaders(page)

  const connResp = await request.post('/api/v1/connectors', {
    headers,
    data: {
      name: `Table UX DB ${Date.now()}`,
      type: 'postgres',
      config: { host: 'localhost', port: 5432, user: 'aether', password: 'aether_dev', database: 'aether' },
    },
  })
  expect(connResp.ok()).toBeTruthy()
  const connector = await connResp.json()

  const nbResp = await request.post('/api/v1/notebooks', {
    headers,
    data: { title: 'Table UX E2E' },
  })
  expect(nbResp.ok()).toBeTruthy()
  const notebook = await nbResp.json()

  const cellResp = await request.post(`/api/v1/notebooks/${notebook.id}/cells`, {
    headers,
    data: { type: 'code', language: 'sql', source: SQL, connector_id: connector.id },
  })
  expect(cellResp.ok()).toBeTruthy()
  const cell = await cellResp.json()

  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  const execResp = await request.post(
    `/api/v1/notebooks/${notebook.id}/cells/${cell.id}/execute`,
    { headers: { Authorization: `Bearer ${token}` }, data: {} },
  )
  expect(execResp.ok()).toBeTruthy()

  return { token: token!, notebookId: notebook.id, cellId: cell.id }
}

function cell(page: Page, row: number, col: number) {
  return page.locator(`td[data-row="${row}"][data-col="${col}"]`)
}

async function dragSelection(page: Page, from: { row: number; col: number }, to: { row: number; col: number }) {
  const a = await cell(page, from.row, from.col).boundingBox()
  const b = await cell(page, to.row, to.col).boundingBox()
  expect(a && b).toBeTruthy()
  await page.mouse.move(a!.x + 5, a!.y + 5)
  await page.mouse.down()
  await page.mouse.move((a!.x + b!.x) / 2, (a!.y + b!.y) / 2)
  await page.mouse.move(b!.x + 5, b!.y + 5)
  await page.mouse.up()
}

test.describe('Result table selection, copy, and resize', () => {
  test('drag-selects a rectangle, copies TSV, and resizes a column', async ({ page, request }) => {
    await registerAndOnboard(page, Date.now().toString())
    const fixture = await seedTable(page, request)

    await page.goto(`/notebooks/${fixture.notebookId}`)
    await expect(cell(page, 0, 0)).toBeVisible()
    await expect(cell(page, 3, 2)).toBeVisible()

    // ── Rectangular selection ────────────────────────────────────────────────
    await dragSelection(page, { row: 0, col: 0 }, { row: 1, col: 2 })

    const transparent = 'rgba(0, 0, 0, 0)'
    await expect(cell(page, 0, 0)).not.toHaveCSS('background-color', transparent)
    await expect(cell(page, 1, 2)).not.toHaveCSS('background-color', transparent)
    await expect(cell(page, 2, 0)).toHaveCSS('background-color', transparent)

    // ── Ctrl+C copies the rectangle as TSV from the data model ───────────────
    await page.evaluate(() => {
      const w = window as unknown as { __copied: string | null }
      w.__copied = null
      document.addEventListener('copy', (e) => {
        const ev = e as ClipboardEvent
        w.__copied = ev.clipboardData?.getData('text/plain') ?? null
      })
    })
    await page.keyboard.press('Control+c')
    const copied = await page.evaluate(() => (window as unknown as { __copied: string | null }).__copied)
    // jsonb normalizes key order, so the object serializes as v,env.
    expect(copied).toBe('1\tAlice\t\n2\tBob\t{"v":3,"env":"prod"}')

    // ── Column resize via the header handle ─────────────────────────────────
    // The handle must win hit testing against the neighbouring header cell;
    // a handle that extends past the th's edge is swallowed by the next th.
    const handle = page.getByLabel('Resize column id')
    const hb = await handle.boundingBox()
    expect(hb).toBeTruthy()
    await page.mouse.move(hb!.x + hb!.width / 2, hb!.y + hb!.height / 2)
    await page.mouse.down()
    await page.mouse.move(hb!.x + hb!.width / 2 + 40, hb!.y + hb!.height / 2)
    await page.mouse.move(hb!.x + hb!.width / 2 + 80, hb!.y + hb!.height / 2)
    await page.mouse.up()

    await expect
      .poll(async () => page.locator('thead th').nth(1).evaluate((el) => (el as HTMLElement).style.width))
      .toBe('220px')
    const bodyWidth = await cell(page, 0, 0).evaluate((el) => (el as HTMLElement).style.width)
    expect(bodyWidth).toBe('220px')

    // Resizing must not have toggled the column sort.
    await expect(cell(page, 0, 1)).toHaveText('Alice')

    // ── Plain click still opens the detail panel ────────────────────────────
    const clicked = await cell(page, 2, 1).boundingBox()
    await page.mouse.click(clicked!.x + 5, clicked!.y + 5)
    await expect(page.getByLabel('Copy value')).toBeVisible()
  })
})
