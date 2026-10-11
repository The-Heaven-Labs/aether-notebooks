import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  // The suite drives a single Vite dev server, Go API, relay, and dev database.
  // Uncapped parallelism (one worker per file) starves app boot and WS fan-out,
  // which flakes timing-sensitive specs; four workers keeps runs deterministic.
  workers: 4,
  retries: 0,
  timeout: 30_000,
  use: {
    // Point the suite at another stack (e.g. a worktree-local embedded build)
    // with E2E_BASE_URL; the default stays the local Vite dev server.
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  snapshotDir: './snapshots',
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
