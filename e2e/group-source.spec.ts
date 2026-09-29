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

  test('renaming an SSO group requires typing the synced name', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `SSO Rename ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok(), await resp.text()).toBeTruthy()
    const group = await resp.json()

    try {
      setGroupSource(group.id, 'sso')

      // API fails closed without confirm_name.
      const blocked = await request.put(`/api/v1/groups/${group.id}`, {
        headers, data: { name: `${name} API` },
      })
      expect(blocked.status()).toBe(409)

      await page.goto('/groups')
      const row = page.getByText(name).locator('..').locator('..')
      await row.getByTitle('Group actions').click()
      await page.getByText('Rename', { exact: true }).click()
      const nameInput = page.getByLabel('Group name')
      await nameInput.fill(`${name} v2`)
      await nameInput.press('Enter')

      await expect(page.getByText('Rename SSO-managed group?')).toBeVisible()
      const confirm = page.getByRole('button', { name: 'Rename group' })
      await expect(confirm).toBeDisabled()
      await page.getByLabel('Confirm group name').fill('wrong-name')
      await expect(confirm).toBeDisabled()
      await page.getByLabel('Confirm group name').fill(name)
      await expect(confirm).toBeEnabled()
      await confirm.click()

      await expect(page.getByText(`${name} v2`)).toBeVisible()
    } finally {
      await request.delete(`/api/v1/groups/${group.id}?force=true`, { headers }).catch(() => {})
    }
  })

  test('a failed rename closes the confirmation and shows the error', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const name = `SSO Error ${Date.now()}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok(), await resp.text()).toBeTruthy()
    const group = await resp.json()

    try {
      setGroupSource(group.id, 'sso')

      // Force the PUT to fail, deterministically, without touching the backend.
      await page.route(`**/api/v1/groups/${group.id}`, async (route) => {
        if (route.request().method() === 'PUT') {
          await route.fulfill({
            status: 409,
            contentType: 'application/json',
            body: JSON.stringify({ error: 'group is managed by SSO; renaming it disconnects sync' }),
          })
        } else {
          await route.continue()
        }
      })

      await page.goto('/groups')
      const row = page.getByText(name).locator('..').locator('..')
      await row.getByTitle('Group actions').click()
      await page.getByText('Rename', { exact: true }).click()
      const nameInput = page.getByLabel('Group name')
      await nameInput.fill(`${name} v2`)
      await nameInput.press('Enter')
      await page.getByLabel('Confirm group name').fill(name)
      await page.getByRole('button', { name: 'Rename group' }).click()

      await expect(page.getByText('Rename SSO-managed group?')).toHaveCount(0)
      await expect(page.getByText(/managed by SSO; renaming it disconnects sync/)).toBeVisible()
    } finally {
      await page.unroute(`**/api/v1/groups/${group.id}`)
      await request.delete(`/api/v1/groups/${group.id}?force=true`, { headers }).catch(() => {})
    }
  })

  test('renaming an SSO group to an existing name surfaces the conflict', async ({ page, request }) => {
    await loginAsAdmin(page)
    const headers = await adminHeaders(page)
    const stamp = Date.now()
    const name = `SSO Duplicate ${stamp}`
    const takenName = `Taken Name ${stamp}`
    const resp = await request.post('/api/v1/groups', { headers, data: { name } })
    expect(resp.ok(), await resp.text()).toBeTruthy()
    const group = await resp.json()
    const takenResp = await request.post('/api/v1/groups', { headers, data: { name: takenName } })
    expect(takenResp.ok(), await takenResp.text()).toBeTruthy()
    const taken = await takenResp.json()

    try {
      setGroupSource(group.id, 'sso')

      await page.goto('/groups')
      const row = page.getByText(name).locator('..').locator('..')
      await row.getByTitle('Group actions').click()
      await page.getByText('Rename', { exact: true }).click()
      const nameInput = page.getByLabel('Group name')
      await nameInput.fill(takenName)
      await nameInput.press('Enter')
      await page.getByLabel('Confirm group name').fill(name)
      await page.getByRole('button', { name: 'Rename group' }).click()

      await expect(page.getByText('Rename SSO-managed group?')).toHaveCount(0)
      await expect(page.getByText(/already exists/i)).toBeVisible()
    } finally {
      await request.delete(`/api/v1/groups/${group.id}?force=true`, { headers }).catch(() => {})
      await request.delete(`/api/v1/groups/${taken.id}`, { headers }).catch(() => {})
    }
  })
})
