import { describe, expect, it } from 'vitest'
import { formatRelativeTime } from './formatRelativeTime'

const NOW = Date.parse('2026-10-08T12:00:00Z')
const at = (secondsAgo: number) => new Date(NOW - secondsAgo * 1000).toISOString()

describe('formatRelativeTime', () => {
  it('returns an empty string for missing or invalid timestamps', () => {
    expect(formatRelativeTime(null, NOW)).toBe('')
    expect(formatRelativeTime(undefined, NOW)).toBe('')
    expect(formatRelativeTime('not-a-date', NOW)).toBe('')
  })

  it('renders recent instants as just now', () => {
    expect(formatRelativeTime(at(5), NOW)).toBe('just now')
    expect(formatRelativeTime(at(59), NOW)).toBe('just now')
  })

  it('renders minutes, hours, and days', () => {
    expect(formatRelativeTime(at(60), NOW)).toBe('1m ago')
    expect(formatRelativeTime(at(12 * 60), NOW)).toBe('12m ago')
    expect(formatRelativeTime(at(32 * 60), NOW)).toBe('32m ago')
    expect(formatRelativeTime(at(60 * 60), NOW)).toBe('1h ago')
    expect(formatRelativeTime(at(25 * 60 * 60), NOW)).toBe('1d ago')
  })

  it('clamps future timestamps to just now', () => {
    expect(formatRelativeTime(new Date(NOW + 5000).toISOString(), NOW)).toBe('just now')
  })
})
