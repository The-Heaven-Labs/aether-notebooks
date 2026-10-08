import type { Connector } from '../types'

export const DEFAULT_IDLE_TIMEOUT_MINUTES = 15
export const CLICKHOUSE_CLOUD_HOST_SUFFIX = '.clickhouse.cloud'

export type CloudStateStatus = 'success' | 'error' | 'neutral'

export interface CloudStateView {
  status: CloudStateStatus
  label: string
}

/** Maps a ClickHouse Cloud service state onto chip copy and styling. */
export function cloudStateView(state: string | undefined): CloudStateView {
  switch (state?.trim().toLowerCase()) {
    case 'running':
      return { status: 'success', label: 'Running' }
    case 'idle':
      return { status: 'neutral', label: 'Idle — wakes on next query' }
    case 'awaking':
      return { status: 'neutral', label: 'Awaking…' }
    case 'starting':
      return { status: 'neutral', label: 'Starting…' }
    case 'provisioning':
      return { status: 'neutral', label: 'Provisioning…' }
    case 'stopped':
      return { status: 'neutral', label: 'Stopped' }
    case 'degraded':
      return { status: 'error', label: 'Degraded' }
    case 'failed':
      return { status: 'error', label: 'Failed' }
    default:
      return { status: 'neutral', label: state?.trim() ? state.trim() : 'Unknown' }
  }
}

/** True for hosts managed by ClickHouse Cloud, where idle inference applies. */
export function isClickHouseCloudHost(host: string | undefined): boolean {
  if (!host) return false
  return host.trim().toLowerCase().endsWith(CLICKHOUSE_CLOUD_HOST_SUFFIX)
}

/** Parses the manual idle-timeout form value; empty/invalid falls back to 15. */
export function parseIdleTimeoutMinutes(value: string): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : DEFAULT_IDLE_TIMEOUT_MINUTES
}

/** Reads the stored idle timeout, defaulting to 15 when absent/invalid. */
export function connectorIdleTimeoutMinutes(connector: Connector): number {
  const stored = connector.config?.idle_timeout_minutes
  return typeof stored === 'number' && stored > 0 ? stored : DEFAULT_IDLE_TIMEOUT_MINUTES
}

/** Compact "ago" copy for durations in ms: "just now", "5m ago", "2h 5m ago", "3d ago". */
export function formatRelativeAgo(ms: number): string {
  if (!Number.isFinite(ms) || ms < 60_000) return 'just now'
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  const restMinutes = minutes % 60
  if (hours < 24) return restMinutes > 0 ? `${hours}h ${restMinutes}m ago` : `${hours}h ago`
  const days = Math.floor(hours / 24)
  const restHours = hours % 24
  return restHours > 0 ? `${days}d ${restHours}h ago` : `${days}d ago`
}

export type IdleInferenceKind = 'likely_idle' | 'active' | 'unknown'

export interface IdleInference {
  kind: IdleInferenceKind
  idleTimeoutMinutes: number
  /** Age of the last recorded activity at evaluation time, when known. */
  lastActivityAgeMs?: number
}

/**
 * Probabilistic idle inference for ClickHouse Cloud hosts without Cloud API
 * credentials. Returns null for non-Cloud hosts (nothing to infer) so callers
 * render no line at all. Wording stays probabilistic on purpose: activity is
 * only what Aether has observed, not the service's real state.
 */
export function inferIdleState(opts: {
  host: string | undefined
  lastSuccessAt: string | null | undefined
  idleTimeoutMinutes: number
  now?: Date
}): IdleInference | null {
  if (!isClickHouseCloudHost(opts.host)) return null
  const timeout = opts.idleTimeoutMinutes > 0 ? opts.idleTimeoutMinutes : DEFAULT_IDLE_TIMEOUT_MINUTES
  if (!opts.lastSuccessAt) return { kind: 'unknown', idleTimeoutMinutes: timeout }
  const last = new Date(opts.lastSuccessAt)
  if (Number.isNaN(last.getTime())) return { kind: 'unknown', idleTimeoutMinutes: timeout }
  const ageMs = (opts.now ?? new Date()).getTime() - last.getTime()
  if (ageMs > timeout * 60_000) {
    return { kind: 'likely_idle', idleTimeoutMinutes: timeout, lastActivityAgeMs: ageMs }
  }
  return { kind: 'active', idleTimeoutMinutes: timeout, lastActivityAgeMs: ageMs }
}
