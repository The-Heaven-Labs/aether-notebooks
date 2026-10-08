import { describe, expect, test } from 'vitest'
import {
  DEFAULT_IDLE_TIMEOUT_MINUTES,
  cloudStateView,
  connectorIdleTimeoutMinutes,
  formatRelativeAgo,
  inferIdleState,
  isClickHouseCloudHost,
  parseIdleTimeoutMinutes,
} from './cloudState'
import type { Connector } from '../types'

const connector = (config: Connector['config'], lastSuccessAt?: string | null): Connector => ({
  id: 'c-1', name: 'CH', type: 'clickhouse', created_at: '2026-01-01T00:00:00Z',
  config, last_success_at: lastSuccessAt,
})

describe('cloudStateView', () => {
  test.each([
    ['running', 'success', 'Running'],
    ['idle', 'neutral', 'Idle — wakes on next query'],
    ['awaking', 'neutral', 'Awaking…'],
    ['stopped', 'neutral', 'Stopped'],
    ['degraded', 'error', 'Degraded'],
    ['failed', 'error', 'Failed'],
  ])('maps %s', (state, status, label) => {
    expect(cloudStateView(state)).toEqual({ status, label })
  })

  test('falls back for unknown and missing states', () => {
    expect(cloudStateView('paused')).toEqual({ status: 'neutral', label: 'paused' })
    expect(cloudStateView(undefined)).toEqual({ status: 'neutral', label: 'Unknown' })
  })
})

describe('isClickHouseCloudHost', () => {
  test('matches ClickHouse Cloud hosts only', () => {
    expect(isClickHouseCloudHost('abc.clickhouse.cloud')).toBe(true)
    expect(isClickHouseCloudHost('ABC.ClickHouse.Cloud')).toBe(true)
    expect(isClickHouseCloudHost('evilclickhouse.cloud')).toBe(false)
    expect(isClickHouseCloudHost('localhost')).toBe(false)
    expect(isClickHouseCloudHost(undefined)).toBe(false)
  })
})

describe('parseIdleTimeoutMinutes', () => {
  test('defaults to 15 for empty, invalid, and non-positive values', () => {
    expect(parseIdleTimeoutMinutes('')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('abc')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('0')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('-5')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('30')).toBe(30)
  })
})

describe('connectorIdleTimeoutMinutes', () => {
  test('prefers stored config and falls back to 15', () => {
    expect(connectorIdleTimeoutMinutes(connector({ idle_timeout_minutes: 45 }))).toBe(45)
    expect(connectorIdleTimeoutMinutes(connector({}))).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
  })
})

describe('formatRelativeAgo', () => {
  test('formats sub-minute, minutes, hours, and days', () => {
    expect(formatRelativeAgo(30_000)).toBe('just now')
    expect(formatRelativeAgo(5 * 60_000)).toBe('5m ago')
    expect(formatRelativeAgo(125 * 60_000)).toBe('2h 5m ago')
    expect(formatRelativeAgo(3 * 24 * 60 * 60_000)).toBe('3d ago')
  })
})

describe('inferIdleState', () => {
  const now = new Date('2026-10-08T12:00:00Z')

  test('returns null for non-Cloud hosts', () => {
    expect(inferIdleState({ host: 'localhost', lastSuccessAt: '2026-10-08T11:00:00Z', idleTimeoutMinutes: 15, now })).toBeNull()
    expect(inferIdleState({ host: undefined, lastSuccessAt: null, idleTimeoutMinutes: 15, now })).toBeNull()
  })

  test('reports likely idle past the timeout', () => {
    const result = inferIdleState({
      host: 'abc.clickhouse.cloud',
      lastSuccessAt: '2026-10-08T11:20:00Z', // 40m ago
      idleTimeoutMinutes: 15,
      now,
    })
    expect(result).toEqual({ kind: 'likely_idle', idleTimeoutMinutes: 15, lastActivityAgeMs: 40 * 60_000 })
  })

  test('reports active within the timeout', () => {
    expect(inferIdleState({
      host: 'abc.clickhouse.cloud',
      lastSuccessAt: '2026-10-08T11:55:00Z',
      idleTimeoutMinutes: 15,
      now,
    })).toEqual({ kind: 'active', idleTimeoutMinutes: 15, lastActivityAgeMs: 5 * 60_000 })
  })

  test('reports unknown with no recorded activity', () => {
    expect(inferIdleState({ host: 'abc.clickhouse.cloud', lastSuccessAt: null, idleTimeoutMinutes: 15, now }))
      .toEqual({ kind: 'unknown', idleTimeoutMinutes: 15 })
  })

  test('falls back to the default timeout for bad values', () => {
    const result = inferIdleState({
      host: 'abc.clickhouse.cloud',
      lastSuccessAt: '2026-10-08T11:00:00Z',
      idleTimeoutMinutes: 0,
      now,
    })
    expect(result?.idleTimeoutMinutes).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
  })
})
