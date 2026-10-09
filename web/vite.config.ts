/// <reference types="vitest/config" />
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { storybookTest } from '@storybook/addon-vitest/vitest-plugin';
import { playwright } from '@vitest/browser-playwright';
const dirname = typeof __dirname !== 'undefined' ? __dirname : path.dirname(fileURLToPath(import.meta.url));
// The dev proxy follows API_URL so a worktree-local API (or any non-default
// target) can be used; the default stays the docker dev API on :8088.
const apiTarget = process.env.API_URL ?? 'http://localhost:8088';

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          // Cell normalizes chart config on every render; keep that pure
          // helper in its own tiny chunk so the heavy ECharts chart UI
          // (lazy-loaded by OutputRenderer) is not pulled into every
          // notebook page.
          if (id.includes('/charts/normalizeChartConfig')) return 'chart-config-normalize'
          return undefined
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': { target: apiTarget, changeOrigin: true, ws: true },
      '/internal': { target: apiTarget, changeOrigin: true },
      '/docs': { target: apiTarget, changeOrigin: true },
      '/swagger.json': { target: apiTarget, changeOrigin: true },
    }
  },
  optimizeDeps: {
    include: ['@dnd-kit/core', '@dnd-kit/sortable', '@dnd-kit/utilities', 'rehype-highlight']
  },
  test: {
    projects: [{
      extends: true,
      test: {
        name: 'default',
        environment: 'jsdom',
        globals: true,
        setupFiles: ['./src/test/setup.ts']
      }
    }, {
      extends: true,
      plugins: [
      // The plugin will run tests for the stories defined in your Storybook config
      // See options at: https://storybook.js.org/docs/next/writing-tests/integrations/vitest-addon#storybooktest
      storybookTest({
        configDir: path.join(dirname, '.storybook')
      })],
      test: {
        name: 'storybook',
        browser: {
          enabled: true,
          headless: true,
          provider: playwright({}),
          instances: [{
            browser: 'chromium'
          }]
        }
      }
    }]
  }
});