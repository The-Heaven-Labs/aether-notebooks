import { expect, type APIRequestContext, type Page } from '@playwright/test'
import http from 'node:http'
import type { AddressInfo } from 'node:net'

export const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost:5173'
export const PASSWORD = 'testpass123'

export interface FakeLlm {
  baseUrl: string
  close: () => Promise<void>
}

// The most recently started fake LLM. provisionSession points new model
// configs at it, so startFakeLlm must assign it before provisioning; specs
// keep their own local handle from the return value for afterAll teardown.
let fakeLlm: FakeLlm

// startFakeLlm serves the subset of /chat/completions the agent engine uses:
// any POST gets an assistant reply echoing the last user message. The title
// generator's prompt is answered with a stable title.
export async function startFakeLlm(): Promise<FakeLlm> {
  const server = http.createServer((req, res) => {
    let body = ''
    req.on('data', (chunk: Buffer) => { body += chunk.toString() })
    req.on('end', () => {
      let content = 'ok'
      try {
        const parsed = JSON.parse(body) as { messages?: Array<{ role: string; content?: unknown }> }
        const user = [...(parsed.messages ?? [])].reverse().find((m) => m.role === 'user')
        const text = typeof user?.content === 'string' ? user.content : ''
        content = text.startsWith('Generate a concise title') ? 'E2E Chat' : `Echo: ${text}`
      } catch {
        // Non-JSON body: keep the default reply.
      }
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({
        id: 'chatcmpl-e2e',
        object: 'chat.completion',
        model: 'e2e-model',
        choices: [{ index: 0, message: { role: 'assistant', content }, finish_reason: 'stop' }],
        usage: { prompt_tokens: 12, completion_tokens: 6, total_tokens: 18 },
      }))
    })
  })
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  const { port } = server.address() as AddressInfo
  fakeLlm = {
    baseUrl: `http://127.0.0.1:${port}/v1`,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  }
  return fakeLlm
}

export let counter = 0
export function uniqueSuffix(): string {
  counter += 1
  return `${Date.now().toString(36)}${counter}${Math.random().toString(36).slice(2, 6)}`
}

export function authHeaders(token: string): Record<string, string> {
  return { Authorization: `Bearer ${token}` }
}

export async function tokenFrom(page: Page): Promise<string> {
  const token = await page.evaluate(() => localStorage.getItem('aether_token'))
  expect(token).toBeTruthy()
  return token!
}

export interface Member {
  user_id: string
  name: string
  email: string
  role: string
}

export async function listMembers(request: APIRequestContext, headers: Record<string, string>): Promise<Member[]> {
  const resp = await request.get('/api/v1/members', { headers })
  expect(resp.ok()).toBeTruthy()
  return (await resp.json()) as Member[]
}

// registerViaApi creates an account without an org and returns the onboarding
// token used to redeem an invite link.
export async function registerViaApi(request: APIRequestContext, email: string, name: string): Promise<string> {
  const resp = await request.post('/api/v1/auth/register', {
    data: { email, password: PASSWORD, name },
  })
  expect(resp.ok()).toBeTruthy()
  return ((await resp.json()) as { onboarding_token: string }).onboarding_token
}

// joinViaInvite redeems a non-admin invite link for the onboarding user.
export async function joinViaInvite(
  request: APIRequestContext,
  ownerHeaders: Record<string, string>,
  onboardingToken: string,
): Promise<string> {
  const linkResp = await request.post('/api/v1/members/invite-link', {
    headers: ownerHeaders,
    data: { role: 'non-admin' },
  })
  expect(linkResp.ok()).toBeTruthy()
  const link = (await linkResp.json()) as { token: string }
  const joinResp = await request.post('/api/v1/auth/org/join', {
    headers: authHeaders(onboardingToken),
    data: { invite_link_token: link.token },
  })
  expect(joinResp.ok()).toBeTruthy()
  return ((await joinResp.json()) as { token: string }).token
}

export interface SessionFixture {
  agentId: string
  agentName: string
  sessionId: string
  notebookId: string | null
}

// provisionSession creates a model config (pointed at the fake LLM), an agent,
// optionally a notebook, and a session attached to it.
export async function provisionSession(
  request: APIRequestContext,
  headers: Record<string, string>,
  opts: { withNotebook: boolean; shareWithNotebookViewers?: boolean },
): Promise<SessionFixture> {
  if (!fakeLlm) throw new Error('startFakeLlm() must be called before provisionSession()')
  const suffix = uniqueSuffix()

  const cfgResp = await request.post('/api/v1/model-configs', {
    headers,
    data: {
      name: `E2E Model ${suffix}`,
      provider: 'openai',
      base_url: fakeLlm.baseUrl,
      model: 'e2e-model',
      api_key: 'e2e-key',
      context_window: 128000,
    },
  })
  expect(cfgResp.ok()).toBeTruthy()
  const modelConfigId = ((await cfgResp.json()) as { id: string }).id

  const agentName = `E2E Agent ${suffix}`
  const agentResp = await request.post('/api/v1/agents', {
    headers,
    data: { name: agentName, model_config_id: modelConfigId },
  })
  expect(agentResp.ok()).toBeTruthy()
  const agentId = ((await agentResp.json()) as { id: string }).id

  let notebookId: string | null = null
  if (opts.withNotebook) {
    const nbResp = await request.post('/api/v1/notebooks', {
      headers,
      data: { title: `E2E Notebook ${suffix}` },
    })
    expect(nbResp.ok()).toBeTruthy()
    notebookId = ((await nbResp.json()) as { id: string }).id
  }

  const sessionBody: Record<string, unknown> = { max_turns: 5 }
  if (notebookId) sessionBody.notebook_id = notebookId
  if (opts.shareWithNotebookViewers) sessionBody.share_with_notebook_viewers = true

  const sessResp = await request.post(`/api/v1/agents/${agentId}/session`, {
    headers,
    data: sessionBody,
  })
  expect(sessResp.ok()).toBeTruthy()
  const sessionId = ((await sessResp.json()) as { session_id: string }).session_id

  return { agentId, agentName, sessionId, notebookId }
}

// sendWsMessage drives one owner turn over the agent WebSocket. It resolves on
// done (fake LLM success) and rejects on error frames or socket failures, so a
// failed turn surfaces instead of silently passing on a persisted-but-
// unanswered user message. All call sites expect the turn to complete.
export async function sendWsMessage(sessionId: string, token: string, content: string): Promise<void> {
  const url = `${BASE_URL.replace(/^http/, 'ws')}/api/v1/ws/agents/${sessionId}?token=${encodeURIComponent(token)}`
  await new Promise<void>((resolve, reject) => {
    const ws = new WebSocket(url)
    const timer = setTimeout(() => {
      try { ws.close() } catch { /* already closed */ }
      reject(new Error(`timed out waiting for the agent turn on ${sessionId}`))
    }, 30_000)
    const settle = (err?: Error) => {
      clearTimeout(timer)
      try { ws.close() } catch { /* already closed */ }
      if (err) reject(err)
      else resolve()
    }
    ws.onopen = () => ws.send(JSON.stringify({ type: 'message', content }))
    ws.onmessage = (event) => {
      const msg = JSON.parse(typeof event.data === 'string' ? event.data : String(event.data)) as { type?: string; message?: string }
      if (msg.type === 'done') settle()
      else if (msg.type === 'error') settle(new Error(`agent turn failed on ${sessionId}: ${msg.message ?? 'unknown error'}`))
    }
    ws.onerror = () => settle(new Error(`agent websocket failed on ${sessionId}`))
  })
}

export async function putSessionShares(
  request: APIRequestContext,
  headers: Record<string, string>,
  sessionId: string,
  entries: Array<{ subject_type: string; subject_id: string; actions: string[] }>,
): Promise<void> {
  const resp = await request.put(`/api/v1/acl/agent_session/${sessionId}`, { headers, data: { entries } })
  expect(resp.ok()).toBeTruthy()
}
