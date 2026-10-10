import { test } from 'node:test'
import * as assert from 'node:assert/strict'
import {
  AuthorizeError,
  DASHBOARD_DOC_PREFIX,
  DASHBOARD_REVALIDATE_INTERVAL_MS,
  applyRevalidationDisposition,
  extractToken,
  isDashboardDocument,
  mapAuthorizeResponse,
  parseInvalidateChannel,
  parseUpdateChannel,
  pickStoreToken,
  resolveDocumentRoute,
  revalidationDisposition,
  revalidationKey,
  storeResponseDisposition,
} from './dashboardDocs.ts'

test('resolveDocumentRoute routes dashboard docs to the dashboard endpoint', () => {
  const route = resolveDocumentRoute(`${DASHBOARD_DOC_PREFIX}3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f`, 'http://api:8080')
  assert.equal(route.kind, 'dashboard')
  assert.equal(route.id, '3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f')
  assert.equal(route.loadUrl, 'http://api:8080/internal/dashboard-yjs/3f1c2a6e-0d2f-4a8a-9c5b-7e6d1b2c3a4f')
  assert.equal(route.storeUrl, route.loadUrl)
})

test('resolveDocumentRoute keeps notebook docs on the yjs endpoint', () => {
  const notebookId = '9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d'
  const route = resolveDocumentRoute(notebookId, 'http://api:8080')
  assert.equal(route.kind, 'notebook')
  assert.equal(route.id, notebookId)
  assert.equal(route.loadUrl, `http://api:8080/internal/yjs/${notebookId}`)
  assert.equal(route.storeUrl, route.loadUrl)
})

test('resolveDocumentRoute normalizes a trailing slash on the API URL', () => {
  const route = resolveDocumentRoute('nb-1', 'http://localhost:8088/')
  assert.equal(route.loadUrl, 'http://localhost:8088/internal/yjs/nb-1')
})

test('isDashboardDocument only matches the dashboard prefix', () => {
  assert.equal(isDashboardDocument(`${DASHBOARD_DOC_PREFIX}abc`), true)
  assert.equal(isDashboardDocument('abc'), false)
  assert.equal(isDashboardDocument('aether:dashboard-doc:abc'), false)
})

test('authorize 200 with can_edit true is an editor', () => {
  const result = mapAuthorizeResponse(200, { document_id: 'dash-1', can_edit: true })
  assert.deepEqual(result, { documentId: 'dash-1', canEdit: true })
})

test('authorize 200 with can_edit false is a read-only viewer', () => {
  const result = mapAuthorizeResponse(200, { document_id: 'dash-1', can_edit: false })
  assert.equal(result.canEdit, false)
  // The relay maps !canEdit to connectionConfig.readOnly = true; a viewer
  // keeps the connection (200) unlike the rejection cases below.
})

test('authorize 403 rejects the connection instead of mapping to read-only', () => {
  assert.throws(
    () => mapAuthorizeResponse(403, { error: 'access denied' }),
    (err: unknown) => err instanceof AuthorizeError && err.status === 403,
  )
})

test('authorize 401 rejects the connection', () => {
  assert.throws(
    () => mapAuthorizeResponse(401, { error: 'invalid token' }),
    (err: unknown) => err instanceof AuthorizeError && err.status === 401,
  )
})

test('authorize 500 rejects the connection', () => {
  assert.throws(
    () => mapAuthorizeResponse(500, { error: 'boom' }),
    (err: unknown) => err instanceof AuthorizeError && err.status === 500,
  )
})

test('authorize 200 with a malformed body rejects (fail closed)', () => {
  assert.throws(() => mapAuthorizeResponse(200, null), AuthorizeError)
  assert.throws(() => mapAuthorizeResponse(200, {}), AuthorizeError)
  assert.throws(() => mapAuthorizeResponse(200, { document_id: 'd', can_edit: 'yes' }), AuthorizeError)
})

test('store 200/204 responses resolve normally', () => {
  assert.equal(storeResponseDisposition(200), 'ok')
  assert.equal(storeResponseDisposition(201), 'ok')
  assert.equal(storeResponseDisposition(204), 'ok')
})

test('store 404 is terminal (no retry for a trashed or deleted document)', () => {
  assert.equal(storeResponseDisposition(404), 'terminal')
})

test('store 400/403 are terminal (rejected stores must not retry forever)', () => {
  assert.equal(storeResponseDisposition(400), 'terminal')
  assert.equal(storeResponseDisposition(403), 'terminal')
})

test('store 401/500/502 failures must throw so the doc stays in memory', () => {
  assert.equal(storeResponseDisposition(401), 'retry')
  assert.equal(storeResponseDisposition(500), 'retry')
  assert.equal(storeResponseDisposition(502), 'retry')
  assert.equal(storeResponseDisposition(503), 'retry')
})

test('parseInvalidateChannel extracts the dashboard id', () => {
  assert.equal(parseInvalidateChannel('aether:dashboard-doc-invalidate:abc-123'), 'abc-123')
})

test('parseInvalidateChannel returns null for non-matching channels', () => {
  assert.equal(parseInvalidateChannel('aether:dashboard-doc:abc-123'), null)
  assert.equal(parseInvalidateChannel('aether:something-else'), null)
  assert.equal(parseInvalidateChannel('aether:dashboard-doc-invalidate:'), null)
})

test('parseUpdateChannel extracts the dashboard id', () => {
  assert.equal(parseUpdateChannel('aether:dashboard-doc:abc-123'), 'abc-123')
})

test('parseUpdateChannel returns null for the invalidate channel and empties', () => {
  // The colon after "doc" in the update prefix does not match the hyphen in
  // "doc-invalidate", which is what keeps the two subscriptions disjoint.
  assert.equal(parseUpdateChannel('aether:dashboard-doc-invalidate:abc-123'), null)
  assert.equal(parseUpdateChannel('aether:dashboard-doc:'), null)
  assert.equal(parseUpdateChannel('aether:other:abc'), null)
})

test('extractToken only accepts non-empty string tokens', () => {
  assert.equal(extractToken({ token: 'jwt' }), 'jwt')
  assert.equal(extractToken({ token: '' }), null)
  assert.equal(extractToken({ token: 42 }), null)
  assert.equal(extractToken(undefined), null)
})

test('pickStoreToken prefers the change context over the document cache', () => {
  assert.equal(pickStoreToken({ token: 'from-context' }, 'from-cache'), 'from-context')
})

test('pickStoreToken falls back to the document cache after disconnects', () => {
  assert.equal(pickStoreToken({}, 'from-cache'), 'from-cache')
  assert.equal(pickStoreToken(undefined, 'from-cache'), 'from-cache')
  assert.equal(pickStoreToken({ token: '' }, 'from-cache'), 'from-cache')
})

test('pickStoreToken returns null when neither source has a token', () => {
  assert.equal(pickStoreToken({}, undefined), null)
  assert.equal(pickStoreToken(undefined, ''), null)
})

test('revalidationKey distinguishes socket and document combinations', () => {
  assert.notEqual(revalidationKey('socket-a', 'dashboard:1'), revalidationKey('socket-b', 'dashboard:1'))
  assert.notEqual(revalidationKey('socket-a', 'dashboard:1'), revalidationKey('socket-a', 'dashboard:2'))
  assert.equal(revalidationKey('socket-a', 'dashboard:1'), revalidationKey('socket-a', 'dashboard:1'))
})

test('revalidation interval is one minute', () => {
  assert.equal(DASHBOARD_REVALIDATE_INTERVAL_MS, 60_000)
})

test('revalidation 200 with can_edit true keeps the connection', () => {
  assert.equal(revalidationDisposition(200, true), 'keep')
})

test('revalidation 200 with can_edit false downgrades to read-only', () => {
  assert.equal(revalidationDisposition(200, false), 'readonly')
})

test('revalidation 200 with an unreadable body is inconclusive and retried', () => {
  assert.equal(revalidationDisposition(200), 'retry')
  assert.equal(revalidationDisposition(200, null), 'retry')
})

test('revalidation 401/403/404 are definitive and close the connection', () => {
  assert.equal(revalidationDisposition(401, null), 'close')
  assert.equal(revalidationDisposition(403, null), 'close')
  assert.equal(revalidationDisposition(404, null), 'close')
})

test('revalidation 5xx and other statuses are transient and retried', () => {
  assert.equal(revalidationDisposition(500), 'retry')
  assert.equal(revalidationDisposition(502), 'retry')
  assert.equal(revalidationDisposition(503), 'retry')
  assert.equal(revalidationDisposition(429), 'retry')
})

test('revalidation network errors (no status) are transient and retried', () => {
  assert.equal(revalidationDisposition(null), 'retry')
  assert.equal(revalidationDisposition(null, null), 'retry')
})

test('applyRevalidationDisposition keeps and retries without touching the connection', () => {
  for (const disposition of ['keep', 'retry'] as const) {
    const connection = { readOnly: false }
    let closed = false
    const effect = applyRevalidationDisposition(disposition, connection, () => {
      closed = true
    })
    assert.equal(effect, disposition === 'keep' ? 'kept' : 'retried')
    assert.equal(connection.readOnly, false)
    assert.equal(closed, false)
  }
})

test('applyRevalidationDisposition downgrades the live connection on readonly', () => {
  const connection = { readOnly: false }
  let closed = false
  const effect = applyRevalidationDisposition('readonly', connection, () => {
    closed = true
  })
  assert.equal(effect, 'downgraded')
  assert.equal(connection.readOnly, true)
  assert.equal(closed, false)
})

test('applyRevalidationDisposition is idempotent for an already read-only connection', () => {
  const connection = { readOnly: true }
  let closed = false
  const effect = applyRevalidationDisposition('readonly', connection, () => {
    closed = true
  })
  assert.equal(effect, 'kept', 'a viewer must not re-report the downgrade every tick')
  assert.equal(connection.readOnly, true)
  assert.equal(closed, false)
})

test('applyRevalidationDisposition closes with the revocation reason on close', () => {
  const connection = { readOnly: false }
  const reasons: string[] = []
  const effect = applyRevalidationDisposition('close', connection, (reason) => {
    reasons.push(reason)
  })
  assert.equal(effect, 'closed')
  assert.deepEqual(reasons, ['authorization revoked'])
  assert.equal(connection.readOnly, false)
})
