interface AetherRuntimeConfig {
  apiUrl?: string
  relayUrl?: string
}

declare global {
  interface Window {
    __AETHER_CONFIG__?: AetherRuntimeConfig
  }
}

export function getRuntimeConfig(): AetherRuntimeConfig {
  return window.__AETHER_CONFIG__ ?? {}
}

export function getApiUrl(): string {
  return getRuntimeConfig().apiUrl ?? ''
}

export function getWsUrl(): string {
  const base = getApiUrl() || window.location.origin
  return base.replace(/^http/, 'ws')
}

export function getRelayUrl(): string {
  return getRuntimeConfig().relayUrl ||
    `${window.location.origin.replace(/^http/, 'ws')}/relay`
}
