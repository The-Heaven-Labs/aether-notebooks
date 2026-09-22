import { test, expect, type APIRequestContext } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * End-to-end coverage for the ClickHouse warehouse permissions UI:
 * warehouse creation, connector linking, provisioner sync to `ready`, the
 * table-grant matrix (including the no-service-access warning), the
 * new-tables inbox, validation warnings, the managed badge on ConnectorsPage,
 * and the per-user routing preference on the profile page.
 *
 * Requires a dev stack whose API runs with AETHER_CH_TABLE_PERMISSIONS=true
 * and a reachable ClickHouse. The API-side connector host defaults to
 * `localhost` (native API run); override with E2E_CH_CONNECTOR_HOST when the
 * API runs in Docker (e.g. `aether-clickhouse`). The test-side HTTP endpoint
 * defaults to http://localhost:8123; override with E2E_CH_HTTP_URL.
 */
const CH_HTTP_URL = process.env.E2E_CH_HTTP_URL ?? 'http://localhost:8123'
const CH_CONNECTOR_HOST = process.env.E2E_CH_CONNECTOR_HOST ?? 'localhost'
const CH_USER = process.env.E2E_CH_USER ?? 'dev'
const CH_PASSWORD = process.env.E2E_CH_PASSWORD ?? 'dev'

const chAuth = `Basic ${Buffer.from(`${CH_USER}:${CH_PASSWORD}`).toString('base64')}`

async function chExec(request: APIRequestContext, query: string): Promise<boolean> {
  try {
    const resp = await request.post(CH_HTTP_URL, {
      headers: { Authorization: chAuth },
      params: { query },
      timeout: 10_000,
    })
    return resp.ok()
  } catch {
    return false
  }
}

async function findWarehouse(
  request: APIRequestContext,
  headers: Record<string, string>,
  name: string,
): Promise<{ id: string; sync_status: string; sync_error: string | null } | null> {
  const resp = await request.get('/api/v1/warehouses', { headers })
  expect(resp.ok()).toBeTruthy()
  const warehouses = await resp.json()
  return warehouses.find((w: { name: string }) => w.name === name) ?? null
}

test.describe('ClickHouse warehouse permissions', () => {
  test('provision, grant, inbox, validation, managed badge, routing preference', async ({ page, request }) => {
    test.setTimeout(240_000)
    const ts = Date.now().toString()
    const { name: adminName } = await registerAndOnboard(page, ts)
    const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('aether_token'))}` }

    const configResp = await request.get('/api/v1/auth/config')
    const config = await configResp.json()
    if (!config.warehouse_table_permissions_enabled) {
      test.skip(true, 'AETHER_CH_TABLE_PERMISSIONS is not enabled on the API')
    }
    if (!(await chExec(request, 'SELECT 1'))) {
      test.skip(true, `ClickHouse is not reachable at ${CH_HTTP_URL}`)
    }

    const tableGranted = `e2e_wh_${ts}_granted`
    const tableNew = `e2e_wh_${ts}_new`
    const connName = `WH E2E CH ${ts}`
    const whName = `WH E2E ${ts}`
    const groupName = `WH E2E Group ${ts}`
    let connectorId = ''
    let warehouseId = ''

    try {
      // A unique table that will receive a grant, plus one left ungranted so
      // the new-tables inbox has something to show.
      expect(await chExec(request, `CREATE TABLE IF NOT EXISTS analytics.${tableGranted} (id UInt8) ENGINE = Memory`)).toBeTruthy()
      expect(await chExec(request, `CREATE TABLE IF NOT EXISTS analytics.${tableNew} (id UInt8) ENGINE = Memory`)).toBeTruthy()

      // A group with no service access, so granting it a table surfaces the
      // no_service_access warning. Created before the warehouse UI loads so it
      // is present in the grant matrix's subject picker.
      const groupResp = await request.post('/api/v1/groups', { headers, data: { name: groupName } })
      expect(groupResp.ok()).toBeTruthy()

      const connResp = await request.post('/api/v1/connectors', {
        headers,
        data: {
          name: connName,
          type: 'clickhouse',
          config: { host: CH_CONNECTOR_HOST, port: 9000, user: CH_USER, password: CH_PASSWORD, database: '' },
        },
      })
      expect(connResp.ok()).toBeTruthy()
      connectorId = (await connResp.json()).id

      // Before linking, the connector is a shared-credential service.
      await page.goto('/connectors')
      await expect(
        page.locator('tr', { hasText: connName }).getByText('Shared credential — not table-scoped'),
      ).toBeVisible()

      // Create a warehouse from the UI, then link the connector and assign it
      // as provisioner through the expanded card.
      await page.goto('/warehouses')
      await page.getByRole('button', { name: '+ New Warehouse' }).click()
      await page.getByPlaceholder('Analytics Warehouse').fill(whName)
      await page.getByRole('button', { name: 'Create', exact: true }).click()

      await page.getByLabel('Add connector').selectOption({ label: connName })
      await page.getByRole('button', { name: 'Add', exact: true }).click()
      const provisionerSelect = page.getByLabel('Provisioner connector')
      await expect(provisionerSelect.locator('option', { hasText: connName })).toHaveCount(1)
      await provisionerSelect.selectOption({ label: connName })
      await expect(page.getByText('Provisioner', { exact: true })).toBeVisible()

      // Provisioning runs real ClickHouse DDL through the provisioner.
      const warehouse = await findWarehouse(request, headers, whName)
      expect(warehouse).toBeTruthy()
      warehouseId = warehouse!.id
      await expect
        .poll(
          async () => {
            const resp = await request.get(`/api/v1/warehouses/${warehouseId}`, { headers })
            const body = await resp.json()
            return body.sync_status === 'error' ? `error: ${body.sync_error}` : body.sync_status
          },
          { timeout: 120_000, intervals: [1_000, 2_000, 4_000], message: 'warehouse sync did not reach ready' },
        )
        .toBe('ready')

      // The schema read records the catalog so the inbox can list both tables.
      const schemaResp = await request.get(`/api/v1/connectors/${connectorId}/schema`, { headers })
      expect(schemaResp.ok()).toBeTruthy()

      await page.reload()
      await expect(page.getByText('Ready', { exact: true })).toBeVisible()
      await page.getByTitle('Expand').click()
      await expect(page.getByText(`analytics.${tableNew}`)).toBeVisible({ timeout: 20_000 })

      // Grant one table to the group through the grant matrix.
      await page.getByLabel('Subject', { exact: true }).selectOption({ label: groupName })
      await page.getByLabel('Database', { exact: true }).selectOption('analytics')
      await page.getByLabel(tableGranted, { exact: true }).check()
      await page.getByRole('button', { name: 'Add grant', exact: true }).click()

      // The granted table is now checked + disabled in the checklist.
      await expect(page.getByText('already granted')).toBeVisible()

      // Advisory warning: the grant is saved but the group cannot run queries.
      await expect(page.getByText(/No service access: /)).toBeVisible()
      await expect(page.getByText(`analytics.${tableGranted}`).first()).toBeVisible()
      await expect(page.getByText('No service access', { exact: true })).toBeVisible()

      // Granting advances the review cutoff, so the inbox is now empty, and
      // the validation banner reports both half-configured subjects: the
      // group (grants, no service) and the admin (service, no grants).
      await expect(page.getByText('No new tables since your last review.')).toBeVisible({ timeout: 20_000 })
      const inboxWarnings = page.getByRole('status').filter({ hasText: 'holds no table grants' })
      await expect(inboxWarnings).toBeVisible()
      await expect(inboxWarnings.getByText(groupName)).toBeVisible()
      await expect(inboxWarnings.getByText(adminName)).toBeVisible()
      await expect(inboxWarnings.getByText(/cannot use any warehouse service/)).toBeVisible()

      // ConnectorsPage now reports the connector as managed.
      await page.goto('/connectors')
      await expect(
        page.locator('tr', { hasText: connName }).getByText('Managed — table grants enforced'),
      ).toBeVisible()

      // Profile shows a routing preference for the warehouse; picking the
      // service persists it across a reload.
      await page.goto('/profile')
      const routing = page.getByLabel(`Preferred service for ${whName}`)
      await expect(routing).toBeVisible()
      await routing.selectOption({ label: connName })
      await page.reload()
      await expect(page.getByLabel(`Preferred service for ${whName}`)).toHaveValue(connectorId)

      // Deleting the warehouse returns the connector to shared-credential mode.
      await page.goto('/warehouses')
      await page.getByRole('button', { name: 'Delete', exact: true }).click()
      await expect(page.getByText('Delete warehouse')).toBeVisible()
      await page.getByRole('button', { name: 'Delete', exact: true }).last().click()
      // The warehouse has a linked connector, so deletion requires the
      // force-unlink confirmation.
      await page.getByRole('button', { name: 'Delete and unlink' }).click()
      await expect(page.getByText(whName)).toHaveCount(0)

      await page.goto('/connectors')
      await expect(
        page.locator('tr', { hasText: connName }).getByText('Shared credential — not table-scoped'),
      ).toBeVisible()
    } finally {
      // Best-effort self-cleanup: the unique names keep a failed run harmless.
      if (warehouseId) await request.delete(`/api/v1/warehouses/${warehouseId}?force=true`, { headers }).catch(() => {})
      if (connectorId) await request.delete(`/api/v1/connectors/${connectorId}`, { headers }).catch(() => {})
      await chExec(request, `DROP TABLE IF EXISTS analytics.${tableGranted}`)
      await chExec(request, `DROP TABLE IF EXISTS analytics.${tableNew}`)
    }
  })
})
