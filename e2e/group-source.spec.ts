import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { loginAsAdmin } from './helpers'

const repoRoot = path.resolve(__dirname, '..')

function setGroupSource(groupId: string, source: 'sso'): void {
  execFileSync(
    'docker',
    [
      'compose', '-f', 'docker-compose.dev.yml',
      'exec', '-T', 'aether-postgres',
      'psql', '-U', 'aether', '-d', 'aether', '-c',
      `UPDATE groups SET source='${source}' WHERE id='${groupId}'`,
    ],
    { cwd: repoRoot, stdio: 'pipe', timeout: 15_000 },
  )
}

async function adminHeaders(page: Page): Promise<{ Authorization: string }> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  expect(token).toBeTruthy()
  return { Authorization: `Bearer ${token}` }
}

test.describe('Group provenance and SSO-managed delete guard', () => {
  test('manual group has no badge and deletes with the regular dialog', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `Manual Group ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok(), await resp.text()).toBeTruthy()
    const group = await resp.json()

    try {
      await page.goto('/groups')
      const row = page.getByText(name).locator('..').locator('..')
      await expect(row.getByTitle('Group actions')).toBeVisible()
      await expect(row.getByText('SSO')).toHaveCount(0)

      await row.getByTitle('Group actions').click()
      await page.getByText('Delete', { exact: true }).click()
      await expect(page.getByText(/This cannot be undone/)).toBeVisible()
      await page.getByRole('button', { name: 'Delete', exact: true }).click()
      await expect(page.getByText(name)).toHaveCount(0)
    } finally {
      if (group?.id) {
        await request.delete(`/api/v1/groups/${group.id}`, { headers }).catch(() => {})
      }
    }
  })

  test('SSO group shows a badge, blocks unforced API delete, and force-deletes from the UI', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `SSO Group ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok(), await resp.text()).toBeTruthy()
    const group = await resp.json()

    try {
      setGroupSource(group.id, 'sso')

      await page.goto('/groups')
      const row = page.getByText(name).locator('..').locator('..')
      await expect(row.getByText('SSO', { exact: true })).toBeVisible()

      const blocked = await request.delete(`/api/v1/groups/${group.id}`, { headers })
      expect(blocked.status()).toBe(400)

      await row.getByTitle('Group actions').click()
      await page.getByText('Delete', { exact: true }).click()
      await expect(page.getByText(/managed by SSO/i)).toBeVisible()
      await page.getByRole('button', { name: 'Delete anyway' }).click()
      await expect(page.getByText(name)).toHaveCount(0)
    } finally {
      if (group?.id) {
        await request.delete(`/api/v1/groups/${group.id}?force=true`, { headers }).catch(() => {})
      }
    }
  })

  test('Everyone exposes no actions menu', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/groups')
    const row = page.getByText('Everyone', { exact: true }).first().locator('..').locator('..')
    await expect(row.getByText('System')).toBeVisible()
    await expect(row.getByTitle('Group actions')).toHaveCount(0)
  })
})
