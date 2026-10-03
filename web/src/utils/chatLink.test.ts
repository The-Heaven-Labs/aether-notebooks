import { describe, it, expect } from 'vitest'
import { chatLinkUrl } from './chatLink'

describe('chatLinkUrl', () => {
  it('builds an absolute /chats/:id URL from the given origin', () => {
    expect(chatLinkUrl('s-1', 'https://org1.aether.test')).toBe('https://org1.aether.test/chats/s-1')
  })

  it('defaults to the current origin so subdomain orgs keep working', () => {
    expect(chatLinkUrl('s-1')).toBe(`${window.location.origin}/chats/s-1`)
  })
})
