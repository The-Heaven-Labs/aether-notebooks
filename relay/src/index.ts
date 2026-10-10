import { Connection, Server } from '@hocuspocus/server'
import { Redis } from '@hocuspocus/extension-redis'
import IORedis from 'ioredis'
import * as Y from 'yjs'
import {
  AuthorizeError,
  DASHBOARD_DOC_INVALIDATE_PATTERN,
  DASHBOARD_DOC_PREFIX,
  DASHBOARD_DOC_UPDATE_PATTERN,
  DASHBOARD_REVALIDATE_INTERVAL_MS,
  applyRevalidationDisposition,
  extractToken,
  loadResponseDisposition,
  mapAuthorizeResponse,
  parseInvalidateChannel,
  parseUpdateChannel,
  pickStoreToken,
  resolveDocumentRoute,
  revalidationDisposition,
  revalidationKey,
  storeResponseDisposition,
  type AuthorizeResult,
} from './dashboardDocs'

const API_URL = process.env.AETHER_API_URL || 'http://localhost:8088'
const PORT = parseInt(process.env.AETHER_RELAY_PORT || '3001')
const REDIS_HOST = process.env.REDIS_HOST || 'localhost'
const REDIS_PORT = parseInt(process.env.REDIS_PORT || '6379')

/** Bound every internal API call so a hung API cannot stall document hooks. */
const API_FETCH_TIMEOUT_MS = 10_000

/**
 * Connection context stashed by onAuthenticate and carried by every later
 * hook (including onStoreDocument's lastContext).
 */
interface RelayContext {
  /** Session JWT used to authenticate internal API load/store calls. */
  token?: string
  /** Dashboard ACL edit flag (dashboard documents only). */
  canEdit?: boolean
  /** Dashboard id echoed by the authorize endpoint (dashboard documents only). */
  documentId?: string
}

/** Per-document session token, set on authenticate and cleared on unload. */
const documentTokens = new Map<string, string>()

/** Per-connection revalidation intervals, keyed by socketId + document name. */
const revalidationTimers = new Map<string, ReturnType<typeof setInterval>>()

function clearRevalidationTimer(socketId: string, documentName: string): void {
  const key = revalidationKey(socketId, documentName)
  const timer = revalidationTimers.get(key)
  if (timer) {
    clearInterval(timer)
    revalidationTimers.delete(key)
  }
}

function closeConnection(connection: Connection<RelayContext>, reason: string): void {
  try {
    // Protocol close first: removes the connection server-side and tells the
    // client why. The provider treats this message as informational (it does
    // not reconnect on it), so the socket is closed right after to make the
    // provider reconnect and re-run onAuthenticate with the current ACL.
    connection.close({ code: 4401, reason })
  } catch (err) {
    console.warn(`[relay] failed to close connection ${connection.socketId}:`, err)
  }
  try {
    connection.webSocket.close(4401, reason)
  } catch (err) {
    console.warn(`[relay] failed to close socket for connection ${connection.socketId}:`, err)
  }
}

/** POSTs /internal/collab/authorize and maps the response; throws on denial. */
async function authorizeDashboard(documentName: string, token: string): Promise<AuthorizeResult> {
  const res = await fetch(`${API_URL}/internal/collab/authorize`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ document_name: documentName }),
    signal: AbortSignal.timeout(API_FETCH_TIMEOUT_MS),
  })
  let body: unknown = null
  try {
    body = await res.json()
  } catch {
    body = null
  }
  // Non-200 statuses (401/403/404/5xx) always throw: a 403 means no access at
  // all, never a read-only viewer. Only 200 {can_edit:false} is read-only.
  return mapAuthorizeResponse(res.status, body)
}

/**
 * Re-runs dashboard authorization for a live connection and applies the
 * outcome:
 * - still an editor → keep the connection as-is;
 * - demoted to viewer → downgrade the live connection to read-only;
 * - definitive denial/removal (401/403/404) → disconnect;
 * - transient failure (network error, 5xx, ...) → keep the connection and
 *   retry on the next tick, so an API blip cannot evict every live viewer.
 * A 200 with an unreadable body is inconclusive and retried, never kept.
 */
async function revalidateDashboardConnection(
  documentName: string,
  connection: Connection<RelayContext>,
  context: RelayContext,
): Promise<void> {
  const token = extractToken(context) ?? documentTokens.get(documentName) ?? null
  if (!token) {
    closeConnection(connection, 'authorization revoked')
    return
  }

  let status: number | null = null
  let canEdit: boolean | null = null
  let failure: string | null = null
  try {
    const result = await authorizeDashboard(documentName, token)
    status = 200
    canEdit = result.canEdit
  } catch (err) {
    // AuthorizeError carries the HTTP status; anything else is a network or
    // abort failure with no status at all (retried, never a disconnect).
    status = err instanceof AuthorizeError ? err.status : null
    failure = err instanceof Error ? err.message : String(err)
  }

  const disposition = revalidationDisposition(status, canEdit)
  const effect = applyRevalidationDisposition(disposition, connection, (reason) =>
    closeConnection(connection, reason),
  )
  if (effect === 'downgraded') {
    console.warn(
      `[relay] dashboard "${documentName}" (socket ${connection.socketId}) is no longer editable; downgraded to read-only`,
    )
  } else if (effect === 'closed') {
    console.warn(
      `[relay] re-authorization failed for "${documentName}" (socket ${connection.socketId}), disconnecting:`,
      failure ?? `status ${status}`,
    )
  } else if (effect === 'retried') {
    console.warn(
      `[relay] re-authorization for "${documentName}" (socket ${connection.socketId}) was inconclusive (status ${status ?? 'network error'}); keeping the connection and retrying:`,
      failure,
    )
  }
}

const server = new Server<RelayContext>({
  port: PORT,
  extensions: [
    new Redis({
      host: REDIS_HOST,
      port: REDIS_PORT,
    }),
  ],

  async onLoadDocument({ documentName, context }) {
    const route = resolveDocumentRoute(documentName, API_URL)
    const token = extractToken(context) ?? documentTokens.get(documentName) ?? null
    if (!token) {
      console.warn(`[relay] no session token to load document "${documentName}"`)
      return null
    }
    const res = await fetch(route.loadUrl, {
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(API_FETCH_TIMEOUT_MS),
    })
    const disposition = loadResponseDisposition(route.kind, res.status)
    if (disposition === 'empty') {
      console.warn(`[relay] load ${route.kind} document "${route.id}" failed with status ${res.status}; serving an empty document`)
      return null
    }
    if (disposition === 'fail') {
      // Dashboard GETs always seed server-side, so a non-2xx is a real
      // failure. Throwing aborts the load and the provider retries; serving
      // an empty document here would show a blank dashboard and could be
      // edited into a state nobody ever intended.
      throw new Error(`load ${route.kind} document "${route.id}" failed with status ${res.status}`)
    }
    const buf = await res.arrayBuffer()
    if (buf.byteLength === 0) return null
    return new Uint8Array(buf)
  },

  async onStoreDocument({ documentName, document, lastContext }) {
    const route = resolveDocumentRoute(documentName, API_URL)
    const token = pickStoreToken(lastContext, documentTokens.get(documentName))
    if (!token) {
      console.warn(`[relay] skipping store for "${documentName}": no session token`)
      return
    }
    const state = Y.encodeStateAsUpdate(document)
    const res = await fetch(route.storeUrl, {
      method: 'PUT',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/octet-stream',
      },
      body: Buffer.from(state),
      signal: AbortSignal.timeout(API_FETCH_TIMEOUT_MS),
    })
    const disposition = storeResponseDisposition(res.status)
    if (disposition === 'ok') return
    if (disposition === 'terminal') {
      // Terminal rejection: the notebook/dashboard is trashed or gone (404),
      // or the store was rejected on its merits (400 malformed body, 403 the
      // store actor lacks the required rights). No retry can succeed, and
      // throwing would keep the document resident forever because the pinned
      // server treats every non-SkipFurtherHooksError throw as "stay in
      // memory". Dashboard invalidation unloads the document separately.
      console.warn(`[relay] store ${route.kind} document "${route.id}" rejected (status ${res.status}); abandoning store`)
      return
    }
    // Throw so Hocuspocus keeps the document in memory and retries on the
    // next change instead of unloading it ("Document stays in memory to
    // avoid data loss" in storeDocumentHooks).
    throw new Error(`store ${route.kind} document "${route.id}" failed with status ${res.status}`)
  },

  async onAuthenticate({ token, documentName, context, connectionConfig }) {
    if (!token) throw new Error('Unauthorized')
    const route = resolveDocumentRoute(documentName, API_URL)

    if (route.kind === 'dashboard') {
      const result = await authorizeDashboard(documentName, token)
      context.canEdit = result.canEdit
      context.documentId = result.documentId
      if (!result.canEdit) {
        // Read-only viewer: the connection is accepted, syncs down and sees
        // presence, but MessageReceiver rejects its sync/update messages.
        connectionConfig.readOnly = true
      }
    } else {
      // Notebooks keep the existing session validation (no doc-level ACL).
      const res = await fetch(`${API_URL}/internal/auth/validate`, {
        headers: { Authorization: `Bearer ${token}` },
        signal: AbortSignal.timeout(API_FETCH_TIMEOUT_MS),
      })
      if (!res.ok) throw new Error('Unauthorized')
    }

    context.token = token
    documentTokens.set(documentName, token)
  },

  async connected({ documentName, connection, context }) {
    const route = resolveDocumentRoute(documentName, API_URL)
    if (route.kind !== 'dashboard') return

    if (context.canEdit === false) {
      // Defense-in-depth: onAuthenticate already set connectionConfig.readOnly
      // before queued messages replay; re-assert it on the live Connection.
      connection.readOnly = true
    }

    clearRevalidationTimer(connection.socketId, documentName)
    const timer = setInterval(() => {
      void revalidateDashboardConnection(documentName, connection, context)
    }, DASHBOARD_REVALIDATE_INTERVAL_MS)
    timer.unref()
    revalidationTimers.set(revalidationKey(connection.socketId, documentName), timer)
  },

  async onDisconnect({ documentName, socketId, clientsCount, instance }) {
    clearRevalidationTimer(socketId, documentName)
    if (clientsCount > 0) return
    // Last connection left. Keep the cached token when a store is still
    // pending or running: that store may carry no token in lastContext (e.g.
    // an update fanned in from another replica). The unload path clears it.
    const storeId = `onStoreDocument-${documentName}`
    if (instance.debouncer.isDebounced(storeId) || instance.debouncer.isCurrentlyExecuting(storeId)) return
    documentTokens.delete(documentName)
  },

  async afterUnloadDocument({ documentName }) {
    // A document unloads once its last connection is gone and stores have
    // settled; the cached token must not outlive the in-memory document.
    documentTokens.delete(documentName)
  },
})

/**
 * Applies a backend-originated dashboard update to a locally loaded document.
 * The publisher has already persisted and materialized the change, so a
 * replica without the document loaded skips it (Postgres is current).
 */
async function handleDashboardDocUpdate(dashboardId: string, message: Buffer): Promise<void> {
  if (message.byteLength === 0) return
  const documentName = DASHBOARD_DOC_PREFIX + dashboardId
  if (!server.hocuspocus.documents.has(documentName)) return

  const token = documentTokens.get(documentName) ?? null
  if (!token) {
    console.warn(`[relay] skipping dashboard update for "${documentName}": no session token`)
    return
  }

  const connection = await server.hocuspocus.openDirectConnection(documentName, { token })
  try {
    await connection.transact((document) => {
      Y.applyUpdate(document, new Uint8Array(message))
    })
  } catch (err) {
    console.warn(`[relay] failed to apply dashboard update for "${documentName}":`, err)
  } finally {
    try {
      // Default unloadImmediately stores now and unloads when no client is
      // connected, so fan-in never leaves a direct connection behind.
      await connection.disconnect()
    } catch (err) {
      console.warn(`[relay] failed to disconnect fan-in connection for "${documentName}":`, err)
    }
  }
}

/**
 * Handles a dashboard invalidation (trash/purge): disconnect every viewer and
 * drop the in-memory document so a stale copy cannot outlive the row.
 */
async function handleDashboardDocInvalidate(dashboardId: string, message: Buffer): Promise<void> {
  const documentName = DASHBOARD_DOC_PREFIX + dashboardId
  let reason = 'invalidated'
  try {
    const payload = JSON.parse(message.toString()) as { reason?: unknown }
    if (typeof payload.reason === 'string' && payload.reason.length > 0) reason = payload.reason
  } catch {
    // The reason is diagnostic only; a malformed payload still invalidates.
  }
  console.log(`[relay] invalidating dashboard document ${dashboardId} (${reason})`)
  // The dashboard is gone or trashed; a stale token must not be reused.
  documentTokens.delete(documentName)
  server.hocuspocus.closeConnections(documentName)
  const document = server.hocuspocus.documents.get(documentName)
  if (document) {
    // unloadDocument is a no-op while a debounced store is still running; the
    // store path unloads the document itself once it settles.
    await server.hocuspocus.unloadDocument(document)
  }
}

async function handleRedisMessage(channel: string, message: Buffer): Promise<void> {
  const invalidateId = parseInvalidateChannel(channel)
  if (invalidateId) {
    await handleDashboardDocInvalidate(invalidateId, message)
    return
  }
  const updateId = parseUpdateChannel(channel)
  if (updateId) {
    await handleDashboardDocUpdate(updateId, message)
  }
}

/**
 * Subscribes to the dashboard fan-in channels. Redis being absent or down
 * degrades live cross-replica updates only: ioredis keeps retrying in the
 * background (and re-subscribes on reconnect) while live sessions and the
 * durable load/store path keep working.
 */
function startDashboardDocSubscriber(): void {
  const subscriber = new IORedis({
    host: REDIS_HOST,
    port: REDIS_PORT,
    maxRetriesPerRequest: null,
    retryStrategy: (times: number) => Math.min(times * 500, 5_000),
  })

  subscriber.on('error', (err: Error) => {
    console.warn(`[relay] redis subscriber unavailable (dashboard fan-in degraded): ${err.message}`)
  })
  subscriber.on('ready', () => {
    console.log('[relay] redis dashboard fan-in subscriber connected')
  })
  subscriber.on('pmessageBuffer', (_pattern: string, channel: Buffer, message: Buffer) => {
    handleRedisMessage(channel.toString(), message).catch((err: unknown) => {
      console.warn('[relay] dashboard doc redis message failed:', err)
    })
  })

  subscriber
    .psubscribe(DASHBOARD_DOC_UPDATE_PATTERN, DASHBOARD_DOC_INVALIDATE_PATTERN)
    .catch((err: Error) => {
      console.warn('[relay] redis psubscribe failed:', err.message)
    })
}

server.listen().then(() => {
  console.log(`Hocuspocus relay listening on port ${PORT}`)
  startDashboardDocSubscriber()
})
