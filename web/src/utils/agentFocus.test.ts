import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createFlashQueue, isAgentOrigin, resolveAgentAwareFlash } from './agentFocus'

describe('isAgentOrigin', () => {
  it('recognizes the agent user', () => {
    expect(isAgentOrigin('agent@aether')).toBe(true)
    expect(isAgentOrigin('nova@heaven-labs.com')).toBe(false)
    expect(isAgentOrigin(undefined)).toBe(false)
  })
})

describe('resolveAgentAwareFlash', () => {
  it('queues agent flashes even when the user is not followed', () => {
    expect(resolveAgentAwareFlash({ userEmail: 'agent@aether', followsUser: false })).toBe('queue')
    expect(resolveAgentAwareFlash({ userEmail: 'agent@aether', followsUser: true })).toBe('queue')
  })

  it('flashes for a followed human', () => {
    expect(resolveAgentAwareFlash({ userEmail: 'nova@heaven-labs.com', followsUser: true })).toBe('flash')
  })

  it('does not flash for an unfollowed human', () => {
    expect(resolveAgentAwareFlash({ userEmail: 'nova@heaven-labs.com', followsUser: false })).toBe('none')
  })

  it('does not flash when the origin is unknown', () => {
    expect(resolveAgentAwareFlash({ userEmail: undefined, followsUser: false })).toBe('none')
    expect(resolveAgentAwareFlash({ userEmail: null, followsUser: false })).toBe('none')
  })
})

describe('createFlashQueue', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('flashes only the last cell in a burst', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.push('b')
    q.push('c')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
    expect(flash).toHaveBeenCalledWith('c')
  })

  it('flashes on the trailing edge after the final push', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    vi.advanceTimersByTime(200)
    q.push('b')
    vi.advanceTimersByTime(200)
    expect(flash).not.toHaveBeenCalled()
    vi.advanceTimersByTime(50)
    expect(flash).toHaveBeenCalledTimes(1)
    expect(flash).toHaveBeenCalledWith('b')
  })

  it('rapidly re-pushing the same cell still flashes once', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    vi.advanceTimersByTime(100)
    q.push('a')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
    expect(flash).toHaveBeenCalledWith('a')
  })

  it('flush runs the pending flash immediately and clears it', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.flush()
    expect(flash).toHaveBeenCalledWith('a')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
  })

  it('flush with nothing pending does nothing', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.flush()
    vi.advanceTimersByTime(250)
    expect(flash).not.toHaveBeenCalled()
  })

  it('flush fires the pending flash only once', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.flush()
    q.flush()
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
    expect(flash).toHaveBeenCalledWith('a')
  })

  it('cancel clears the pending flash without firing', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.cancel()
    vi.advanceTimersByTime(250)
    expect(flash).not.toHaveBeenCalled()
  })

  it('push after cancel schedules a fresh flash', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.cancel()
    q.push('b')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
    expect(flash).toHaveBeenCalledWith('b')
  })
})
