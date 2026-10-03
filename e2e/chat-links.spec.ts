import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { registerAndOnboard, login } from './helpers'
import {
  BASE_URL,
  PASSWORD,
  joinViaInvite,
  listMembers,
  provisionSession,
  putSessionShares,
  registerViaApi,
  sendWsMessage,
  startFakeLlm,
  tokenFrom,
  uniqueSuffix,
  type FakeLlm,
} from './agent-session-helpers'

// End-to-end coverage for /chats/:id deep links: ACL-gated shareable links for
// standalone and notebook-attached agent chats. Owners get the full-page
// interactive chat; recipients get the read-only live viewer.

let fakeLlm: FakeLlm

test.beforeAll(async () => {
  fakeLlm = await startFakeLlm()
})

test.afterAll(async () => {
  await fakeLlm.close()
})

async function ownerWithSession(
  request: APIRequestContext,
  page: Page,
  opts: { withNotebook?: boolean } = {},
) {
  const suffix = uniqueSuffix()
  await registerAndOnboard(page, suffix)
  const token = await tokenFrom(page)
  const headers = { Authorization: `Bearer ${token}` }
  const fixture = await provisionSession(request, headers, { withNotebook: opts.withNotebook ?? false })
  return { token, headers, ...fixture }
}

async function shareWithNewRecipient(
  request: APIRequestContext,
  ownerHeaders: Record<string, string>,
  sessionId: string,
) {
  const email = `e2e-recipient-${uniqueSuffix()}@example.com`
  const onboarding = await registerViaApi(request, email, 'E2E Recipient')
  await joinViaInvite(request, ownerHeaders, onboarding)
  const members = await listMembers(request, ownerHeaders)
  const recipient = members.find((m) => m.email === email)
  expect(recipient).toBeTruthy()
  await putSessionShares(request, ownerHeaders, sessionId, [
    { subject_type: 'user', subject_id: recipient!.user_id, actions: ['view'] },
  ])
  return { email, userId: recipient!.user_id }
}

// loginKeepingRedirect logs in without the shared helper's waitForURL('/'):
// ProtectedRoute stores the deep link and LoginPage returns to it after auth.
async function loginKeepingRedirect(page: Page, email: string, password: string, returnPath: string) {
  await page.goto('/login')
  await page.fill('input[type="email"]', email)
  await page.locator('input[type="email"]').press('Enter')
  await page.fill('input[type="password"]', password)
  await page.locator('input[type="password"]').press('Enter')
  await page.waitForURL((url) => url.pathname === returnPath, { timeout: 20_000 })
}

test.describe('chat links', () => {
  test('owner opens their standalone chat link in an interactive full-page chat', async ({ page, request }) => {
    const { sessionId } = await ownerWithSession(request, page)

    await page.goto(`/chats/${sessionId}`)
    const composer = page.getByPlaceholder(/Message agent/)
    await expect(composer).toBeVisible({ timeout: 20_000 })
    await composer.fill('hello from the link')
    await composer.press('Enter')
    await expect(page.getByText('Echo: hello from the link')).toBeVisible({ timeout: 20_000 })
  })

  test('a shared recipient opens the link read-only and sees live updates', async ({ browser, request }) => {
    const ownerPage = await browser.newPage()
    const { token, headers, sessionId } = await ownerWithSession(request, ownerPage)
    await sendWsMessage(sessionId, token, 'first message')
    const recipient = await shareWithNewRecipient(request, headers, sessionId)

    const recipientPage = await browser.newPage()
    await login(recipientPage, recipient.email, PASSWORD)
    await recipientPage.goto(`/chats/${sessionId}`)

    await expect(recipientPage.getByText('Shared · Read-only')).toBeVisible({ timeout: 20_000 })
    await expect(recipientPage.getByText('first message', { exact: true })).toBeVisible()
    await expect(recipientPage.getByPlaceholder(/Message agent/)).toHaveCount(0)

    await sendWsMessage(sessionId, token, 'live update')
    await expect(recipientPage.getByText('Echo: live update')).toBeVisible({ timeout: 20_000 })

    await ownerPage.close()
    await recipientPage.close()
  })

  test('a logged-out visitor returns to the chat link after login', async ({ browser, page, request }) => {
    const ownerPage = await browser.newPage()
    const { headers, sessionId } = await ownerWithSession(request, ownerPage)
    const recipient = await shareWithNewRecipient(request, headers, sessionId)

    await page.goto(`/chats/${sessionId}`)
    await expect(page).toHaveURL(/\/login/)
    await loginKeepingRedirect(page, recipient.email, PASSWORD, `/chats/${sessionId}`)

    await expect(page.getByText('Shared · Read-only')).toBeVisible({ timeout: 20_000 })
    await ownerPage.close()
  })

  test('an unauthorized user sees no-access, and a bad id shows not-found', async ({ browser, page, request }) => {
    const ownerPage = await browser.newPage()
    const { sessionId } = await ownerWithSession(request, ownerPage)

    await registerAndOnboard(page, uniqueSuffix()) // a separate user in their own org
    await page.goto(`/chats/${sessionId}`)
    await expect(page.getByText(/don't have access to this chat/i)).toBeVisible({ timeout: 20_000 })

    await page.goto('/chats/00000000-0000-0000-0000-000000000000')
    await expect(page.getByText(/Chat not found or has been deleted/i)).toBeVisible({ timeout: 20_000 })
    await ownerPage.close()
  })

  test('the permissions panel exposes the exact chat link', async ({ page, request }) => {
    const { sessionId } = await ownerWithSession(request, page)

    await page.goto(`/chats/${sessionId}`)
    await page.getByRole('button', { name: /Share this session/ }).click()
    const dialog = page.getByRole('dialog', { name: /permissions/i })
    await expect(dialog.getByLabel('Chat link', { exact: true })).toHaveValue(`${BASE_URL}/chats/${sessionId}`)
  })

  test('a notebook-attached session link resolves the same way', async ({ page, request }) => {
    const { sessionId } = await ownerWithSession(request, page, { withNotebook: true })

    await page.goto(`/chats/${sessionId}`)
    await expect(page.getByPlaceholder(/Message agent/)).toBeVisible({ timeout: 20_000 })
  })
})
