import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createFlashQueue, isAgentOrigin } from './agentFocus'

describe('isAgentOrigin', () => {
  it('recognizes the agent user', () => {
    expect(isAgentOrigin('agent@aether')).toBe(true)
    expect(isAgentOrigin('nova@heaven-labs.com')).toBe(false)
    expect(isAgentOrigin(undefined)).toBe(false)
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

  it('flush runs the pending flash immediately and clears it', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.flush()
    expect(flash).toHaveBeenCalledWith('a')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
  })
})
