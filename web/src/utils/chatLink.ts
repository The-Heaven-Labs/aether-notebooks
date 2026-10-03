/** Absolute URL that opens a chat: `${origin}/chats/{sessionId}`. The origin
 * defaults to the current window so subdomain-based orgs resolve correctly. */
export function chatLinkUrl(sessionId: string, origin: string = window.location.origin): string {
  return `${origin}/chats/${sessionId}`
}
