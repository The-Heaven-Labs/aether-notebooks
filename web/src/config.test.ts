import { describe, it, expect, beforeEach } from 'vitest'

describe('getRuntimeConfig', () => {
  beforeEach(() => {
    delete (window as any).__AETHER_CONFIG__
  })

  it('returns empty object when no config is set', async () => {
    const { getRuntimeConfig, getApiUrl, getRelayUrl } = await import('./config')
    expect(getRuntimeConfig()).toEqual({})
    expect(getApiUrl()).toBe('')
    expect(getRelayUrl()).toBe(`${window.location.origin.replace(/^http/, 'ws')}/relay`)
  })

  it('returns apiUrl from window.__AETHER_CONFIG__', async () => {
    (window as any).__AETHER_CONFIG__ = { apiUrl: 'https://api.example.com' }
    const { getRuntimeConfig, getApiUrl } = await import('./config')
    expect(getRuntimeConfig()).toEqual({ apiUrl: 'https://api.example.com' })
    expect(getApiUrl()).toBe('https://api.example.com')
  })

  it('returns relayUrl from window.__AETHER_CONFIG__', async () => {
    (window as any).__AETHER_CONFIG__ = { relayUrl: 'wss://relay.example.com' }
    const { getRelayUrl } = await import('./config')
    expect(getRelayUrl()).toBe('wss://relay.example.com')
  })

  it('derives relayUrl from origin when not configured', async () => {
    const { getRelayUrl } = await import('./config')
    expect(getRelayUrl()).toBe(`${window.location.origin.replace(/^http/, 'ws')}/relay`)
  })

  it('derives wsUrl from apiUrl', async () => {
    (window as any).__AETHER_CONFIG__ = { apiUrl: 'https://api.example.com' }
    const { getWsUrl } = await import('./config')
    expect(getWsUrl()).toBe('wss://api.example.com')
  })

  it('derives wsUrl from page origin when apiUrl is empty', async () => {
    const { getWsUrl } = await import('./config')
    expect(getWsUrl()).toBe(window.location.origin.replace(/^http/, 'ws'))
  })
})
