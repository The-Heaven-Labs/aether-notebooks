import { test, expect } from '@playwright/test'
import { registerAndOnboard } from './helpers'

test.describe('Slug references', () => {
  test('a cell slug can be referenced by another cell at execution time', async ({ page, request }) => {
    const ts = Date.now().toString()
    await registerAndOnboard(page, ts)
    const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('aether_token'))}` }

    const connResp = await request.post('/api/v1/connectors', {
      headers,
      data: {
        name: `Slug Test DB ${ts}`,
        type: 'postgres',
        config: { host: 'localhost', port: 5432, user: 'aether', password: 'aether_dev', database: 'aether' },
      },
    })
    expect(connResp.ok()).toBeTruthy()
    const connector = await connResp.json()

    const nbResp = await request.post('/api/v1/notebooks', { headers, data: { title: `Slug Test ${ts}` } })
    expect(nbResp.ok()).toBeTruthy()
    const notebook = await nbResp.json()

    const baseResp = await request.post(`/api/v1/notebooks/${notebook.id}/cells`, {
      headers,
      data: { type: 'code', language: 'sql', source: 'SELECT 42 AS answer', connector_id: connector.id },
    })
    expect(baseResp.ok()).toBeTruthy()
    const baseCell = await baseResp.json()

    const slugResp = await request.put(`/api/v1/notebooks/${notebook.id}/cells/${baseCell.id}`, {
      headers,
      data: { slug: 'base_query' },
    })
    expect(slugResp.ok()).toBeTruthy()

    const refResp = await request.post(`/api/v1/notebooks/${notebook.id}/cells`, {
      headers,
      data: {
        type: 'code',
        language: 'sql',
        source: 'SELECT * FROM ({{base_query}}) AS t',
        connector_id: connector.id,
      },
    })
    expect(refResp.ok()).toBeTruthy()
    const refCell = await refResp.json()

    // Executing the referencing cell resolves {{base_query}} to the sibling
    // cell's SQL; without resolution this is invalid SQL and execution fails.
    const execResp = await request.post(
      `/api/v1/notebooks/${notebook.id}/cells/${refCell.id}/execute`,
      { headers, data: {} },
    )
    expect(execResp.ok()).toBeTruthy()
    const result = await execResp.json()
    const serialized = JSON.stringify(result.outputs)
    expect(serialized).toContain('answer')
    expect(serialized).toContain('42')

    // The stored output renders in the notebook view.
    await page.goto(`/notebooks/${notebook.id}`)
    await expect(page.getByText('answer').first()).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('42').first()).toBeVisible()
  })
})
