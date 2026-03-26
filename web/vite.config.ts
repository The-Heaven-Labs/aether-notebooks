import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// When running inside Docker the Vite container reaches the API via the
// internal service name.  Outside Docker it falls back to localhost.
const apiTarget = process.env.API_URL ?? 'http://localhost:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    proxy: {
      '/api': apiTarget,
      '/internal': apiTarget,
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
})
