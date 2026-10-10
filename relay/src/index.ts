import { Connection, Server } from '@hocuspocus/server'
import { Redis } from '@hocuspocus/extension-redis'
import IORedis from 'ioredis'
import * as Y from 'yjs'
import {
  DASHBOARD_DOC_INVALIDATE_PATTERN,
  DASHBOARD_DOC_PREFIX,
  DASHBOARD_DOC_UPDATE_PATTERN,
  DASHBOARD_REVALIDATE_INTERVAL_MS,
  extractToken,
  mapAuthorizeResponse,
  parseInvalidateChannel,
  parseUpdateChannel,
  pickStoreToken,
  resolveDocumentRoute,
  revalidationKey,
  type AuthorizeResult,
} from './dashboardDocs'

const API_URL = process.env.AETHER_API_URL || 'http://localhost:8088'
const PORT = parseInt(process.env.AETHER_RELAY_PORT || '3001')
const REDIS_HOST = process.env.REDIS_HOST || 'localhost'
const REDIS_PORT = parseInt(process.env.REDIS_PORT || '6379')

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
 * Re-runs dashboard authorization for a live connection. Any failure
 * (non-200 or network) disconnects it; a 200 is left alone so role changes
 * are applied by the next connection's authenticate, never by silently
 * downgrading (or upgrading) a live connection.
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
  try {
    await authorizeDashboard(documentName, token)
  } catch (err) {
    console.warn(
      `[relay] re-authorization failed for "${documentName}" (socket ${connection.socketId}), disconnecting:`,
      err instanceof Error ? err.message : err,
    )
    closeConnection(connection, 'authorization revoked')
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
    })
    if (!res.ok) {
      console.warn(`[relay] load ${route.kind} document "${route.id}" failed with status ${res.status}`)
      return null
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
    })
    if (!res.ok) {
      console.warn(`[relay] store ${route.kind} document "${route.id}" failed with status ${res.status}`)
    }
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
