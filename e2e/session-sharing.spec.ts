import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { registerAndOnboard, login } from './helpers'
import {
  BASE_URL,
  PASSWORD,
  startFakeLlm,
  uniqueSuffix,
  authHeaders,
  tokenFrom,
  listMembers,
  registerViaApi,
  joinViaInvite,
  provisionSession,
  sendWsMessage,
  putSessionShares,
  type FakeLlm,
} from './agent-session-helpers'

// End-to-end coverage for agent session sharing. Agent sessions are ACL
// resources (V124): owners see a Share panel and the notebook inheritance
// toggle, recipients get a read-only viewer, and the notebook Chats drawer is
// the inherited discovery surface. The suite drives two browser contexts
// (owner + recipient) and a fake OpenAI-compatible model endpoint so turns
// complete deterministically without live LLM tokens.
//
// Requirements: the target API server must have register/login rate limits
// raised (each test registers at least one account): docker-compose.dev.yml
// sets AETHER_RATE_LIMIT_REGISTER/LOGIN=500/min, while the server defaults are
// 5/min register and 10/min login. Target any stack via E2E_BASE_URL
// (default http://localhost:5173).

let fakeLlm: FakeLlm

async function expectSessionFlag(
  request: APIRequestContext,
  headers: Record<string, string>,
  sessionId: string,
  expected: boolean,
): Promise<void> {
  await expect.poll(async () => {
    const resp = await request.get(`/api/v1/sessions/${sessionId}`, { headers })
    if (!resp.ok()) return null
    return ((await resp.json()) as { share_with_notebook_viewers: boolean }).share_with_notebook_viewers
  }, { timeout: 15_000 }).toBe(expected)
}

async function openChatsDrawer(page: Page): Promise<void> {
  const viewButton = page.getByRole('button', { name: /^View/ })
  await expect(viewButton).toBeVisible({ timeout: 20_000 })
  await viewButton.click()
  await page.getByRole('button', { name: 'Chats', exact: true }).click()
  await expect(page.getByText('Chats', { exact: true }).first()).toBeVisible()
}

function viewerDialog(page: Page) {
  return page.getByRole('dialog', { name: 'Shared agent session' })
}

function permissionsDialog(page: Page) {
  // The dialog's accessible name is "Permissions" (capital P); regex name
  // matching is case-sensitive, so the /i flag is required.
  return page.getByRole('dialog', { name: /permissions/i })
}

// openAgentHistory selects the agent in the global agent panel and switches to
// the chat-history view, where "My sessions" and "Shared with me" live.
async function openAgentHistory(page: Page, agentName: string): Promise<void> {
  await page.getByTitle('Open AI Agent (Ctrl+K)').click()
  const select = page.locator('select.agent-select').first()
  await expect(select.locator('option', { hasText: agentName })).toHaveCount(1, { timeout: 15_000 })
  await select.selectOption({ label: agentName })
  await page.getByTitle('View chat history').click()
  await expect(page.getByText('Chat history')).toBeVisible({ timeout: 15_000 })
}

test.describe('Agent session sharing', () => {
  test.beforeAll(async () => {
    fakeLlm = await startFakeLlm()
  })

  test.afterAll(async () => {
    await fakeLlm?.close()
  })

  test.beforeEach(() => {
    test.setTimeout(180_000)
  })

  test('owner sees the session in the notebook chats drawer and opens it with sharing controls', async ({ page, request }) => {
    await registerAndOnboard(page, uniqueSuffix())
    const ownerHeaders = authHeaders(await tokenFrom(page))
    const fx = await provisionSession(request, ownerHeaders, { withNotebook: true })
    await sendWsMessage(fx.sessionId, await tokenFrom(page), 'hello')

    await page.goto(`/notebooks/${fx.notebookId}`)
    await expect(page.getByText(/E2E Notebook/).first()).toBeVisible({ timeout: 20_000 })
    await openChatsDrawer(page)

    const row = page.getByRole('button', { name: /hello/ })
    await expect(row).toBeVisible({ timeout: 15_000 })
    // Own rows never render the Shared badge.
    await expect(page.getByText('Shared', { exact: true })).toHaveCount(0)

    await row.click()
    const viewer = viewerDialog(page)
    await expect(viewer).toBeVisible()
    // The settled transcript has exactly one copy of each message; opening a
    // viewer can transiently append the replayed `done` before reconnect_sync
    // replaces the transcript, so assert the stable count rather than the
    // first matching node.
    await expect(viewer.getByText('Echo: hello')).toHaveCount(1, { timeout: 15_000 })
    await expect(viewer.getByText('hello', { exact: true })).toHaveCount(1)

    // Owners get editing affordances: Share button, no read-only banner.
    await expect(viewer.getByRole('button', { name: 'Share' })).toBeVisible()
    await expect(viewer.getByText('Shared · Read-only')).toHaveCount(0)
  })

  test('owner shares with a member and enables notebook inheritance from the panel', async ({ page, request }) => {
    await registerAndOnboard(page, uniqueSuffix())
    const ownerToken = await tokenFrom(page)
    const ownerHeaders = authHeaders(ownerToken)
    const fx = await provisionSession(request, ownerHeaders, { withNotebook: true })
    await sendWsMessage(fx.sessionId, ownerToken, 'hello')

    const recipientEmail = `recipient-${uniqueSuffix()}@example.com`
    const recipientName = `E2E Recipient ${uniqueSuffix()}`
    const onboardingToken = await registerViaApi(request, recipientEmail, recipientName)
    await joinViaInvite(request, ownerHeaders, onboardingToken)
    const recipient = (await listMembers(request, ownerHeaders)).find((m) => m.email === recipientEmail)
    expect(recipient).toBeTruthy()

    await page.goto(`/notebooks/${fx.notebookId}`)
    await openChatsDrawer(page)
    await page.getByRole('button', { name: /hello/ }).click()
    const viewer = viewerDialog(page)
    await expect(viewer).toBeVisible()
    await viewer.getByRole('button', { name: 'Share' }).click()

    const panel = permissionsDialog(page)
    await expect(panel).toBeVisible()

    // Add the member with view-only access. The panel's composer is an inline
    // picker: trigger → search combobox → option → Add. Picking a subject
    // pre-selects the view capability chip, so assert it instead of clicking
    // (clicking would toggle it off and keep Add disabled).
    const composer = panel.getByRole('group', { name: 'Add access' })
    await composer.getByRole('button', { name: /Add people or groups/ }).click()
    await composer.getByRole('combobox', { name: 'Search people and groups' }).fill(recipientEmail)
    await composer.getByRole('option', { name: recipientName }).click()
    await expect(composer.getByRole('button', { name: 'view', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await composer.getByRole('button', { name: 'Add', exact: true }).click()
    await panel.getByRole('button', { name: 'Save' }).click()
    // Draft actions disappear only after the PUT resolves; without this the
    // ACL read below can race the in-flight save.
    await expect(panel.getByRole('button', { name: 'Save' })).toHaveCount(0, { timeout: 15_000 })
    await expect(panel.getByText(recipientName)).toBeVisible({ timeout: 15_000 })

    // The share is persisted as a view-only agent_session entry.
    const aclResp = await request.get(`/api/v1/acl/agent_session/${fx.sessionId}`, { headers: ownerHeaders })
    expect(aclResp.ok()).toBeTruthy()
    const acl = (await aclResp.json()) as Array<{ subject_type: string; subject_id: string; actions: string[] }>
    const entry = acl.find((e) => e.subject_type === 'user' && e.subject_id === recipient!.user_id)
    expect(entry?.actions).toEqual(['view'])

    // Enable notebook inheritance and confirm the PATCH round-trip.
    // The checkbox is controlled and only flips once the PATCH resolves, so
    // click and poll the state instead of check()/uncheck()'s post-click assert.
    const inherit = panel.getByRole('checkbox', { name: /Anyone who can view this notebook/ })
    await expect(inherit).not.toBeChecked()
    await inherit.click()
    await expect(inherit).toBeChecked({ timeout: 15_000 })
    await expect(panel.getByText('Notebook viewers can read this session live, view only.')).toBeVisible({ timeout: 15_000 })
    await expectSessionFlag(request, ownerHeaders, fx.sessionId, true)
  })

  test('recipient discovers the session in history and reads it live without controls', async ({ page, browser, request }) => {
    const ownerAccount = await registerAndOnboard(page, uniqueSuffix())
    const ownerToken = await tokenFrom(page)
    const ownerHeaders = authHeaders(ownerToken)
    const fx = await provisionSession(request, ownerHeaders, { withNotebook: true })
    await sendWsMessage(fx.sessionId, ownerToken, 'hello')

    const recipientEmail = `recipient-${uniqueSuffix()}@example.com`
    const recipientName = `E2E Recipient ${uniqueSuffix()}`
    const onboardingToken = await registerViaApi(request, recipientEmail, recipientName)
    await joinViaInvite(request, ownerHeaders, onboardingToken)
    const members = await listMembers(request, ownerHeaders)
    const owner = members.find((m) => m.email === ownerAccount.email)
    const recipient = members.find((m) => m.email === recipientEmail)
    expect(owner).toBeTruthy()
    expect(recipient).toBeTruthy()

    // Direct session share plus agent visibility: "Shared with me" is scoped to
    // the selected agent, so the recipient needs the agent in their panel.
    await putSessionShares(request, ownerHeaders, fx.sessionId, [
      { subject_type: 'user', subject_id: recipient!.user_id, actions: ['view'] },
    ])
    const agentAclResp = await request.put(`/api/v1/acl/agent/${fx.agentId}`, {
      headers: ownerHeaders,
      data: {
        entries: [
          { subject_type: 'user', subject_id: recipient!.user_id, actions: ['view'] },
          { subject_type: 'user', subject_id: owner!.user_id, actions: ['view', 'edit', 'delete'] },
        ],
      },
    })
    expect(agentAclResp.ok()).toBeTruthy()

    const recipientContext = await browser.newContext({ baseURL: BASE_URL })
    const recipientPage = await recipientContext.newPage()
    try {
      await login(recipientPage, recipientEmail, PASSWORD)
      await openAgentHistory(recipientPage, fx.agentName)

      await expect(recipientPage.getByText('Shared with me')).toBeVisible()
      const sharedRow = recipientPage.getByRole('button', { name: /hello/ })
      await expect(sharedRow).toBeVisible({ timeout: 15_000 })
      await expect(sharedRow.getByText('Shared', { exact: true })).toBeVisible()
      await expect(sharedRow.getByText(ownerAccount.email)).toBeVisible()

      await sharedRow.click()
      const viewer = viewerDialog(recipientPage)
      await expect(viewer).toBeVisible()
      await expect(viewer.getByText('Shared · Read-only')).toBeVisible()
      await expect(viewer.getByText('hello', { exact: true })).toHaveCount(1)
      await expect(viewer.getByText('Echo: hello')).toHaveCount(1)

      // Read-only surface: no composer, no Share button.
      await expect(viewer.locator('textarea')).toHaveCount(0)
      await expect(viewer.getByRole('button', { name: 'Share' })).toHaveCount(0)

      // Live fan-out: a second owner turn streams into the recipient viewer.
      await expect(viewer.getByText('Live')).toBeVisible({ timeout: 15_000 })
      await sendWsMessage(fx.sessionId, ownerToken, 'live ping')
      await expect(viewer.getByText('Echo: live ping')).toHaveCount(1, { timeout: 30_000 })
    } finally {
      await recipientContext.close()
    }
  })

  test('notebook inheritance drives recipient chats visibility and revocation denies stale opens', async ({ page, browser, request }) => {
    const ownerAccount = await registerAndOnboard(page, uniqueSuffix())
    const ownerToken = await tokenFrom(page)
    const ownerHeaders = authHeaders(ownerToken)
    const fx = await provisionSession(request, ownerHeaders, { withNotebook: true, shareWithNotebookViewers: true })
    await sendWsMessage(fx.sessionId, ownerToken, 'hello')

    const recipientEmail = `recipient-${uniqueSuffix()}@example.com`
    const recipientName = `E2E Recipient ${uniqueSuffix()}`
    const onboardingToken = await registerViaApi(request, recipientEmail, recipientName)
    await joinViaInvite(request, ownerHeaders, onboardingToken)
    const members = await listMembers(request, ownerHeaders)
    const owner = members.find((m) => m.email === ownerAccount.email)
    const recipient = members.find((m) => m.email === recipientEmail)
    expect(owner).toBeTruthy()
    expect(recipient).toBeTruthy()

    // Share the notebook but NOT the session: visibility must come purely from
    // the inheritance flag.
    const nbAclResp = await request.put(`/api/v1/acl/notebook/${fx.notebookId}`, {
      headers: ownerHeaders,
      data: {
        entries: [
          { subject_type: 'user', subject_id: owner!.user_id, actions: ['view', 'run', 'edit', 'share', 'delete', 'create'] },
          { subject_type: 'user', subject_id: recipient!.user_id, actions: ['view', 'run'] },
        ],
      },
    })
    expect(nbAclResp.ok()).toBeTruthy()

    const recipientContext = await browser.newContext({ baseURL: BASE_URL })
    const recipientPage = await recipientContext.newPage()
    try {
      await login(recipientPage, recipientEmail, PASSWORD)
      await recipientPage.goto(`/notebooks/${fx.notebookId}`)
      await openChatsDrawer(recipientPage)
      const row = recipientPage.getByRole('button', { name: /hello/ })
      await expect(row).toBeVisible({ timeout: 15_000 })
      await expect(row.getByText('Shared', { exact: true })).toBeVisible()
      await expect(row.getByText(owner!.email)).toBeVisible()

      // The inherited session opens read-only for the notebook viewer.
      await row.click()
      await expect(viewerDialog(recipientPage).getByText('Shared · Read-only')).toBeVisible()
      await viewerDialog(recipientPage).getByRole('button', { name: 'Close viewer' }).click()
      await expect(viewerDialog(recipientPage)).toHaveCount(0)

      // Owner turns inheritance off through the Share panel checkbox.
      await page.goto(`/notebooks/${fx.notebookId}`)
      await openChatsDrawer(page)
      await page.getByRole('button', { name: /hello/ }).click()
      const ownerViewer = viewerDialog(page)
      await ownerViewer.getByRole('button', { name: 'Share' }).click()
      const panel = permissionsDialog(page)
      const inherit = panel.getByRole('checkbox', { name: /Anyone who can view this notebook/ })
      await expect(inherit).toBeChecked()
      await inherit.click()
      await expect(inherit).not.toBeChecked({ timeout: 15_000 })
      await expect(panel.getByText('Only people explicitly shared below can read this session.')).toBeVisible({ timeout: 15_000 })
      await expectSessionFlag(request, ownerHeaders, fx.sessionId, false)
      await panel.getByRole('button', { name: 'Close permissions dialog' }).click()
      await ownerViewer.getByRole('button', { name: 'Close viewer' }).click()

      // Recipient reload: the inherited session is gone from the Chats drawer.
      await recipientPage.reload()
      await openChatsDrawer(recipientPage)
      await expect(recipientPage.getByText('No chats in this notebook yet')).toBeVisible({ timeout: 15_000 })
      await recipientPage.getByTitle('Close chats').click()

      // Direct share restores access; revoking it while the drawer is open
      // leaves a stale row whose open must show the access-denied state.
      await putSessionShares(request, ownerHeaders, fx.sessionId, [
        { subject_type: 'user', subject_id: recipient!.user_id, actions: ['view'] },
      ])
      await recipientPage.reload()
      await openChatsDrawer(recipientPage)
      await expect(recipientPage.getByRole('button', { name: /hello/ })).toBeVisible({ timeout: 15_000 })

      await putSessionShares(request, ownerHeaders, fx.sessionId, [])
      await recipientPage.getByRole('button', { name: /hello/ }).click()
      const denied = viewerDialog(recipientPage)
      await expect(denied.getByText('You do not have access to this session (HTTP 403)')).toBeVisible({ timeout: 15_000 })
      await expect(denied.locator('textarea')).toHaveCount(0)
      await expect(denied.getByRole('button', { name: 'Share' })).toHaveCount(0)

      // And a reload drops the row entirely.
      await recipientPage.getByRole('button', { name: 'Close viewer' }).click()
      await recipientPage.reload()
      await openChatsDrawer(recipientPage)
      await expect(recipientPage.getByText('No chats in this notebook yet')).toBeVisible({ timeout: 15_000 })
    } finally {
      await recipientContext.close()
    }
  })

  test('a session without a notebook rejects inheritance and stays out of notebook chats', async ({ page, request }) => {
    await registerAndOnboard(page, uniqueSuffix())
    const ownerHeaders = authHeaders(await tokenFrom(page))
    const fx = await provisionSession(request, ownerHeaders, { withNotebook: false })
    await sendWsMessage(fx.sessionId, await tokenFrom(page), 'hello')

    // The owner can read the notebook-less session from the agent history.
    await openAgentHistory(page, fx.agentName)
    await expect(page.getByText('My sessions')).toBeVisible()
    const ownRow = page.getByRole('button', { name: /hello/ })
    await expect(ownRow).toBeVisible({ timeout: 15_000 })
    await ownRow.click()
    await expect(page.getByText('hello', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /Resume/ })).toBeVisible()
    // Own history rows resume inline, not in the read-only viewer.
    await expect(viewerDialog(page)).toHaveCount(0)

    // The API carries no notebook link and rejects the inheritance toggle.
    const sessionResp = await request.get(`/api/v1/sessions/${fx.sessionId}`, { headers: ownerHeaders })
    expect(sessionResp.ok()).toBeTruthy()
    const session = (await sessionResp.json()) as { notebook_id: string; share_with_notebook_viewers: boolean }
    expect(session.notebook_id).toBe('')
    expect(session.share_with_notebook_viewers).toBe(false)

    const patchResp = await request.patch(`/api/v1/sessions/${fx.sessionId}`, {
      headers: ownerHeaders,
      data: { share_with_notebook_viewers: true },
    })
    expect(patchResp.status()).toBe(400)

    // Notebook Chats is notebook-scoped, so the session never appears there.
    const nbResp = await request.post('/api/v1/notebooks', {
      headers: ownerHeaders,
      data: { title: `E2E Empty Notebook ${uniqueSuffix()}` },
    })
    expect(nbResp.ok()).toBeTruthy()
    const notebookId = ((await nbResp.json()) as { id: string }).id
    await page.goto(`/notebooks/${notebookId}`)
    await openChatsDrawer(page)
    await expect(page.getByText('No chats in this notebook yet')).toBeVisible({ timeout: 15_000 })

    // NOTE: there is currently no owner-facing entry point to the read-only
    // SessionViewer for a session without a notebook (the Chats drawer is
    // notebook-scoped and the agent history resumes own rows inline), so the
    // "no inheritance checkbox" rendering is covered by the SessionViewer
    // component test instead of this browser suite.
  })
})
