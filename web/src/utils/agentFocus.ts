export function isAgentOrigin(userEmail?: string | null): boolean {
  return userEmail === 'agent@aether'
}

export interface FlashQueue {
  push(cellId: string): void
  flush(): void
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
    flush() {
      if (timer) clearTimeout(timer)
      run()
    },
  }
}
