/**
 * Pure helpers for routing Hocuspocus documents to the Go internal API and
 * for the dashboard live-update fan-in. This module deliberately has no
 * server/redis imports so the logic can be unit-tested without a relay
 * (see dashboardDocs.test.ts).
 */

/** Document-name prefix for live dashboard documents. */
export const DASHBOARD_DOC_PREFIX = 'dashboard:'

/** Redis pub/sub pattern carrying backend-originated Yjs updates. */
export const DASHBOARD_DOC_UPDATE_PATTERN = 'aether:dashboard-doc:*'

/**
 * Redis pub/sub pattern carrying dashboard-document invalidation notices.
 *
 * The two patterns are disjoint: the update pattern requires ':' right after
 * 'doc', while the invalidate channel has '-invalidate' at that position, so
 * Redis never delivers an invalidate message to the update pattern (verified
 * against Redis 7.4 with PSUBSCRIBE/PUBLISH).
 */
export const DASHBOARD_DOC_INVALIDATE_PATTERN = 'aether:dashboard-doc-invalidate:*'

const DASHBOARD_DOC_UPDATE_CHANNEL_PREFIX = 'aether:dashboard-doc:'
const DASHBOARD_DOC_INVALIDATE_CHANNEL_PREFIX = 'aether:dashboard-doc-invalidate:'

/** Re-authorization cadence for live dashboard connections. */
export const DASHBOARD_REVALIDATE_INTERVAL_MS = 60_000

export type DocumentKind = 'dashboard' | 'notebook'

/** Internal API load/store URLs for one relay document. */
export interface DocumentRoute {
  kind: DocumentKind
  /** Dashboard UUID for dashboards; the raw document name for notebooks. */
  id: string
  loadUrl: string
  storeUrl: string
}

/** Raised when POST /internal/collab/authorize does not grant access. */
export class AuthorizeError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'AuthorizeError'
    this.status = status
  }
}

/** Successful authorization: the dashboard and whether the caller may edit. */
export interface AuthorizeResult {
  documentId: string
  canEdit: boolean
}

/** Whether a document name addresses a live dashboard document. */
export function isDashboardDocument(documentName: string): boolean {
  return documentName.startsWith(DASHBOARD_DOC_PREFIX)
}

/**
 * Resolves the Go internal endpoint for a relay document name:
 * `dashboard:<uuid>` documents go to /internal/dashboard-yjs/<uuid>,
 * everything else (notebook UUIDs) keeps /internal/yjs/<name>.
 */
export function resolveDocumentRoute(documentName: string, apiUrl: string): DocumentRoute {
  const base = apiUrl.replace(/\/+$/, '')
  if (isDashboardDocument(documentName)) {
    const id = documentName.slice(DASHBOARD_DOC_PREFIX.length)
    const url = `${base}/internal/dashboard-yjs/${encodeURIComponent(id)}`
    return { kind: 'dashboard', id, loadUrl: url, storeUrl: url }
  }
  const url = `${base}/internal/yjs/${encodeURIComponent(documentName)}`
  return { kind: 'notebook', id: documentName, loadUrl: url, storeUrl: url }
}

/**
 * Maps a POST /internal/collab/authorize response to an AuthorizeResult.
 *
 * Every non-200 status (401/403/404/5xx) throws: a 403 means no access at
 * all and must reject the connection, never degrade it to read-only. A 200
 * body must carry a string document_id and a boolean can_edit; anything else
 * is a malformed response and also rejects (fail closed).
 */
export function mapAuthorizeResponse(status: number, body: unknown): AuthorizeResult {
  if (status !== 200) {
    throw new AuthorizeError(status, `dashboard authorization failed with status ${status}`)
  }
  if (typeof body !== 'object' || body === null) {
    throw new AuthorizeError(status, 'dashboard authorization returned a malformed body')
  }
  const record = body as Record<string, unknown>
  if (typeof record.document_id !== 'string' || typeof record.can_edit !== 'boolean') {
    throw new AuthorizeError(status, 'dashboard authorization returned a malformed body')
  }
  return { documentId: record.document_id, canEdit: record.can_edit }
}

/** How onStoreDocument must react to the internal store endpoint's status. */
export type StoreResponseDisposition = 'ok' | 'terminal' | 'retry'

/**
 * Classifies a store response for onStoreDocument.
 *
 * - 2xx: the state was persisted; resolve normally.
 * - 404: the notebook/dashboard row is gone or trashed, so no retry can ever
 *   succeed; the store is abandoned (terminal) and invalidation/unload drops
 *   the document separately.
 * - anything else (401, 5xx, ...): uncertain or transient. The hook must
 *   throw so Hocuspocus keeps the document in memory instead of unloading it
 *   ("Document stays in memory to avoid data loss" in storeDocumentHooks).
 *   Throwing is deliberately not used for 404: the pinned server treats every
 *   non-SkipFurtherHooksError throw identically (keep in memory), which would
 *   turn a terminal 404 into an endless resident document.
 */
export function storeResponseDisposition(status: number): StoreResponseDisposition {
  if (status >= 200 && status < 300) return 'ok'
  if (status === 404) return 'terminal'
  return 'retry'
}

/** Extracts a non-empty bearer token from a connection context object. */
export function extractToken(value: unknown): string | null {
  if (value && typeof value === 'object') {
    const token = (value as { token?: unknown }).token
    if (typeof token === 'string' && token.length > 0) return token
  }
  return null
}

/**
 * Token used for a store call: the context of the change itself wins, then
 * the per-document cache. Cross-replica and post-disconnect stores carry no
 * token in their context, so the cache is what keeps them authenticated.
 */
export function pickStoreToken(lastContext: unknown, cachedToken: string | undefined): string | null {
  return extractToken(lastContext) ?? (cachedToken && cachedToken.length > 0 ? cachedToken : null)
}

/** Returns the dashboard id of an update channel, or null for anything else. */
export function parseUpdateChannel(channel: string): string | null {
  if (!channel.startsWith(DASHBOARD_DOC_UPDATE_CHANNEL_PREFIX)) return null
  const id = channel.slice(DASHBOARD_DOC_UPDATE_CHANNEL_PREFIX.length)
  return id.length > 0 ? id : null
}

/** Returns the dashboard id of an invalidation channel, or null. */
export function parseInvalidateChannel(channel: string): string | null {
  if (!channel.startsWith(DASHBOARD_DOC_INVALIDATE_CHANNEL_PREFIX)) return null
  const id = channel.slice(DASHBOARD_DOC_INVALIDATE_CHANNEL_PREFIX.length)
  return id.length > 0 ? id : null
}

/**
 * Key for the per-connection revalidation timers. The onDisconnect payload
 * carries no connection object, only socketId, so timers are keyed by
 * socketId + document name.
 */
export function revalidationKey(socketId: string, documentName: string): string {
  return `${socketId}\u0000${documentName}`
}
