import { test, expect, type Page } from '@playwright/test'
import { registerAndOnboard } from './helpers'

// End-to-end coverage for pre-provisioned group members: an admin stages a
// membership by email before the person has an account, the membership is
// materialized when they first join the org, and the group's ACL grants access
// to a shared notebook immediately.

async function adminToken(page: Page): Promise<string> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  expect(token).toBeTruthy()
  return token!
}

test.describe('Pre-provisioned group members', () => {
  test('staged member materializes on first login and inherits group ACL access', async ({ page, browser, request }) => {
    test.setTimeout(120_000)
    await registerAndOnboard(page, Date.now().toString())
    const token = await adminToken(page)
    const headers = { Authorization: `Bearer ${token}` }

    const ts = Date.now()
    const groupName = `PreProvision ${ts}`
    const newEmail = `preprovision-${ts}@example.com`

    // Admin sets up a group, a notebook, and a group ACL on it.
    const groupResp = await request.post('/api/v1/groups', { headers, data: { name: groupName } })
    expect(groupResp.ok()).toBeTruthy()
    const group = await groupResp.json()

    const nbResp = await request.post('/api/v1/notebooks', { headers, data: { title: 'Shared With Group' } })
    expect(nbResp.ok()).toBeTruthy()
    const notebook = await nbResp.json()

    const aclResp = await request.put(`/api/v1/acl/notebook/${notebook.id}`, {
      headers,
      data: { entries: [{ subject_type: 'group', subject_id: group.id, actions: ['view', 'run'] }] },
    })
    expect(aclResp.ok()).toBeTruthy()

    // Stage the membership before the person exists.
    const pendingResp = await request.post(`/api/v1/groups/${group.id}/pending-members`, {
      headers,
      data: { emails: [newEmail] },
    })
    expect(pendingResp.ok()).toBeTruthy()
    expect((await pendingResp.json()).added).toBe(1)

    // Admin UI shows the staged row with the pending badge.
    await page.goto('/groups')
    await page.getByText(groupName).click()
    await expect(page.getByText(newEmail)).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText('Pending — awaiting first login')).toBeVisible()

    // The newcomer registers and joins via a non-admin invite link, so access
    // is granted by the group ACL rather than org-admin bypass.
    const regResp = await request.post('/api/v1/auth/register', {
      data: { email: newEmail, password: 'testpass123', name: 'Pre Provisioned' },
    })
    expect(regResp.ok()).toBeTruthy()
    const onboardingToken = (await regResp.json()).onboarding_token

    const linkResp = await request.post('/api/v1/members/invite-link', {
      headers,
      data: { role: 'non-admin' },
    })
    expect(linkResp.ok()).toBeTruthy()
    const link = await linkResp.json()

    const joinResp = await request.post('/api/v1/auth/org/join', {
      headers: { Authorization: `Bearer ${onboardingToken}` },
      data: { invite_link_token: link.token },
    })
    expect(joinResp.ok()).toBeTruthy()
    const joined = await joinResp.json()

    // The group ACL now grants the newcomer access to the notebook.
    const nbGet = await request.get(`/api/v1/notebooks/${notebook.id}`, {
      headers: { Authorization: `Bearer ${joined.token}` },
    })
    expect(nbGet.ok()).toBeTruthy()

    // The newcomer can open the shared notebook in the browser.
    const userContext = await browser.newContext()
    const userPage = await userContext.newPage()
    await userPage.goto('/login')
    await userPage.fill('input[type="email"]', newEmail)
    await userPage.click('button[type="submit"]')
    await userPage.fill('input[type="password"]', 'testpass123', { timeout: 15_000 })
    await userPage.click('button[type="submit"]')
    await userPage.waitForURL('/')
    await userPage.goto(`/notebooks/${notebook.id}`)
    await expect(userPage).toHaveTitle(/Shared With Group/, { timeout: 20_000 })
    await userContext.close()

    // Back in the admin UI the pending row is consumed and the person is a
    // regular member.
    await page.goto('/groups')
    await page.getByText(groupName).click()
    await expect(page.getByText(newEmail)).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText('Pending — awaiting first login')).toHaveCount(0)
  })
})
