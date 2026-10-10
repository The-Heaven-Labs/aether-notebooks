import { test } from 'node:test'
import * as assert from 'node:assert/strict'
import {
  AuthorizeError,
  DASHBOARD_DOC_PREFIX,
  DASHBOARD_REVALIDATE_INTERVAL_MS,
  extractToken,
  isDashboardDocument,
  mapAuthorizeResponse,
  parseInvalidateChannel,
  parseUpdateChannel,
  pickStoreToken,
  resolveDocumentRoute,
  revalidationKey,
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
