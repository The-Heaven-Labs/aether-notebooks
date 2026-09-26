import { test, expect, type APIRequestContext } from '@playwright/test'
import { registerAndOnboard } from './helpers'

/**
 * End-to-end coverage for the provisioner connector execution block:
 * the block is on by default, the notebook picker offers the provisioner only
 * as a disabled target, the connectors page badges it, the warehouse settings
 * toggle persists, and enabling the override turns the provisioner into a
 * normal managed service (per-user ClickHouse identity, never the stored
 * credential). Because per-user identities are provisioned only for subjects
 * with at least one table grant, the test grants a table before running.
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

async function chReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const resp = await request.post(CH_HTTP_URL, {
      headers: { Authorization: chAuth },
      params: { query: 'SELECT 1' },
      timeout: 10_000,
    })
    return resp.ok()
  } catch {
    return false
  }
}

async function chScalar(request: APIRequestContext, query: string): Promise<string | null> {
  try {
    const resp = await request.post(CH_HTTP_URL, {
      headers: { Authorization: chAuth },
      params: { query },
      timeout: 10_000,
    })
    if (!resp.ok()) return null
    return (await resp.text()).trim()
  } catch {
    return null
  }
}

test.describe('Provisioner connector execution block', () => {
  test('blocked by default, picker disabled, override enables managed execution', async ({ page, request }) => {
    test.setTimeout(240_000)
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('aether_token'))}` }

    const configResp = await request.get('/api/v1/auth/config')
    const config = await configResp.json()
    if (!config.warehouse_table_permissions_enabled) {
      test.skip(true, 'AETHER_CH_TABLE_PERMISSIONS is not enabled on the API')
    }
    if (!(await chReachable(request))) {
      test.skip(true, `ClickHouse is not reachable at ${CH_HTTP_URL}`)
    }

    const connName = `Prov Block CH ${ts}`
    const whName = `Prov Block WH ${ts}`
    const grantTable = `e2e_prov_${ts}`
    let connectorId = ''
    let warehouseId = ''
    let notebookId = ''

    try {
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

      const whResp = await request.post('/api/v1/warehouses', {
        headers,
        data: { name: whName, provisioner_connector_id: connectorId },
      })
      expect(whResp.ok()).toBeTruthy()
      warehouseId = (await whResp.json()).id

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

      const nbResp = await request.post('/api/v1/notebooks', { headers, data: { title: `Prov Block ${ts}` } })
      expect(nbResp.ok()).toBeTruthy()
      notebookId = (await nbResp.json()).id

      // With the override off, the notebook connector picker offers the
      // provisioner only as a disabled, clearly-labelled target.
      await page.goto(`/notebooks/${notebookId}`)
      const blockedOption = page.getByLabel('Select a connector').locator(`option[value="${connectorId}"]`)
      await expect(blockedOption).toContainText('(provisioner — no access)')
      await expect(blockedOption).toBeDisabled()

      // The connectors page badges it as a provisioner.
      await page.goto('/connectors')
      await expect(
        page.locator('tr', { hasText: connName }).getByText('Provisioner', { exact: true }),
      ).toBeVisible()

      // Executing a cell wired to the provisioner fails closed.
      const cellResp = await request.post(`/api/v1/notebooks/${notebookId}/cells`, {
        headers,
        data: {
          type: 'code',
          language: 'sql',
          source: 'SELECT currentUser() AS ch_user',
          connector_id: connectorId,
        },
      })
      expect(cellResp.ok()).toBeTruthy()
      const cell = await cellResp.json()
      const blockedRun = await request.post(`/api/v1/notebooks/${notebookId}/cells/${cell.id}/execute`, {
        headers,
        data: {},
      })
      expect(blockedRun.status()).toBe(403)
      expect((await blockedRun.json()).error).toContain('provisioner')

      // Per-user identities are provisioned only for subjects with at least
      // one table grant, so grant a real table and wait for reconcile to
      // create the derived ClickHouse user. Without this the managed run
      // would authenticate as a user that was never provisioned.
      expect(
        await chScalar(
          request,
          `CREATE TABLE IF NOT EXISTS analytics.${grantTable} (id UInt8) ENGINE = Memory`,
        ),
      ).not.toBeNull()
      const grantResp = await request.post(`/api/v1/warehouses/${warehouseId}/grants`, {
        headers,
        data: { subject_type: 'everyone', subject_id: 'everyone', database: 'analytics', table: grantTable },
      })
      expect(grantResp.ok()).toBeTruthy()
      const effectiveResp = await request.get(`/api/v1/warehouses/${warehouseId}/effective-access`, { headers })
      expect(effectiveResp.ok()).toBeTruthy()
      const chUser = (await effectiveResp.json()).ch_user as string
      expect(chUser).toMatch(/^aether_/)

      const identityProvisioned = async () => {
        const count = await chScalar(request, `SELECT count() FROM system.users WHERE name = '${chUser}'`)
        const resp = await request.get(`/api/v1/warehouses/${warehouseId}`, { headers })
        const body = await resp.json()
        return `${body.sync_status}:${count}`
      }
      await expect
        .poll(identityProvisioned, {
          timeout: 120_000,
          intervals: [500, 1_000, 2_000],
          message: 'provisioned ClickHouse identity did not appear',
        })
        .toBe('ready:1')

      // Enable the override through the warehouse settings UI; it persists
      // across a reload.
      await page.goto('/warehouses')
      await page.getByTitle('Expand').click()
      const allowProvisioner = page.getByLabel('Allow queries through the provisioner')
      await expect(allowProvisioner).not.toBeChecked()
      await allowProvisioner.check()
      await expect
        .poll(
          async () => {
            const resp = await request.get(`/api/v1/warehouses/${warehouseId}`, { headers })
            return (await resp.json()).allow_provisioner_execution
          },
          { timeout: 20_000, intervals: [500, 1_000, 2_000], message: 'override did not persist' },
        )
        .toBe(true)
      await page.reload()
      await page.getByTitle('Expand').click()
      await expect(page.getByLabel('Allow queries through the provisioner')).toBeChecked()

      // The picker now offers the provisioner as a normal managed service.
      await page.goto(`/notebooks/${notebookId}`)
      const allowedOption = page.getByLabel('Select a connector').locator(`option[value="${connectorId}"]`)
      await expect(allowedOption).toHaveText(/\(provisioner\)$/)
      await expect(allowedOption).not.toBeDisabled()

      // Managed execution runs as the derived per-user identity, never the
      // provisioner's stored credential.
      const run = await request.post(`/api/v1/notebooks/${notebookId}/cells/${cell.id}/execute`, {
        headers,
        data: {},
      })
      expect(run.ok()).toBeTruthy()
      const result = (await run.json()) as { routing?: { ch_user?: string }; outputs?: unknown }
      expect(result.routing?.ch_user).toBe(chUser)
      expect(JSON.stringify(result.outputs)).toContain(result.routing!.ch_user!)
    } finally {
      // Best-effort self-cleanup; unique names keep a failed run harmless.
      if (notebookId) await request.delete(`/api/v1/notebooks/${notebookId}`, { headers }).catch(() => {})
      if (warehouseId) await request.delete(`/api/v1/warehouses/${warehouseId}?force=true`, { headers }).catch(() => {})
      if (connectorId) await request.delete(`/api/v1/connectors/${connectorId}`, { headers }).catch(() => {})
      await chScalar(request, `DROP TABLE IF EXISTS analytics.${grantTable}`)
    }
  })
})
