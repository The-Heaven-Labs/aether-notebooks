export function isAgentOrigin(userEmail?: string | null): boolean {
  return userEmail === 'agent@aether'
}

export type FlashAction = 'queue' | 'flash' | 'none'

export function resolveAgentAwareFlash(opts: { userEmail?: string | null; followsUser: boolean }): FlashAction {
  if (isAgentOrigin(opts.userEmail)) return 'queue'
  return opts.followsUser ? 'flash' : 'none'
}

export interface FlashQueue {
  push(cellId: string): void
  cancel(): void
}

export function createFlashQueue(flash: (cellId: string) => void, delayMs = 250): FlashQueue {
  let pending: string | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  const run = () => {
    timer = null
    const id = pending
    pending = null
    if (id) flash(id)
  }
  return {
    push(cellId: string) {
      pending = cellId
      if (timer) clearTimeout(timer)
      timer = setTimeout(run, delayMs)
    },
    cancel() {
      if (timer) clearTimeout(timer)
      timer = null
      pending = null
    },
  }
}
