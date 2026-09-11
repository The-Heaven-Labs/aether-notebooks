import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useNotebookWs, shouldFlashExecutingCell } from './useNotebookWs'

vi.mock('../api/client', () => ({ getToken: () => 'test-token' }))

class MockWebSocket {
  static instances: MockWebSocket[] = []
  url: string
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }
  send() {}
  close() {
    this.onclose?.()
  }
}

function lastSocket(): MockWebSocket {
  const all = MockWebSocket.instances
  if (all.length === 0) throw new Error('no WebSocket created')
  return all[all.length - 1]
}

function emit(sock: MockWebSocket, msg: Record<string, unknown>) {
  act(() => {
    sock.onmessage?.({ data: JSON.stringify(msg) })
  })
}

const RealWebSocket = globalThis.WebSocket

beforeEach(() => {
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
})

afterEach(() => {
  vi.stubGlobal('WebSocket', RealWebSocket)
})

async function renderExecutingHook(onCellExecuting: (cellId: string, startedAt?: string, userEmail?: string) => void) {
  const hook = renderHook(() =>
    useNotebookWs('nb-1', undefined, undefined, undefined, undefined, undefined, undefined, onCellExecuting),
  )
  // connect() is deferred via setTimeout(0) in the hook
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10))
  })
  return hook
}

describe('useNotebookWs cell_executing', () => {
  it('threads user_email through to onCellExecuting', async () => {
    const spy = vi.fn()
    const { unmount } = await renderExecutingHook(spy)
    emit(lastSocket(), { type: 'cell_executing', cell_id: 'c1', started_at: '2026-09-11T00:00:00Z', user_email: 'agent@aether' })
    expect(spy).toHaveBeenCalledWith('c1', '2026-09-11T00:00:00Z', 'agent@aether')
    unmount()
  })

  it('passes human user_email through unchanged (no forced scroll downstream)', async () => {
    const spy = vi.fn()
    const { unmount } = await renderExecutingHook(spy)
    emit(lastSocket(), { type: 'cell_executing', cell_id: 'c2', started_at: '2026-09-11T00:00:00Z', user_email: 'human@example.com' })
    expect(spy).toHaveBeenCalledWith('c2', '2026-09-11T00:00:00Z', 'human@example.com')
    unmount()
  })

  it('tolerates cell_executing without user_email (legacy user path)', async () => {
    const spy = vi.fn()
    const { unmount } = await renderExecutingHook(spy)
    emit(lastSocket(), { type: 'cell_executing', cell_id: 'c3' })
    expect(spy).toHaveBeenCalledWith('c3', undefined, undefined)
    unmount()
  })
})

describe('shouldFlashExecutingCell', () => {
  it('scrolls for agent runs', () => {
    expect(shouldFlashExecutingCell('agent@aether', new Set(), 'c1')).toBe(true)
  })

  it('does not scroll for human runs', () => {
    expect(shouldFlashExecutingCell('human@example.com', new Set(), 'c1')).toBe(false)
  })

  it('does not scroll without a user_email', () => {
    expect(shouldFlashExecutingCell(undefined, new Set(), 'c1')).toBe(false)
  })

  it('does not scroll for locally initiated runs', () => {
    expect(shouldFlashExecutingCell('agent@aether', new Set(['c1']), 'c1')).toBe(false)
  })
})
