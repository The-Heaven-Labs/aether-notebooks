# Aether Frontend — React + Vite + TypeScript

This is the web frontend for Aether, a collaborative SQL/data notebook platform.

## Development

```bash
# Install dependencies
cd web && npm install

# Start dev server (port 5173, proxies /api to :8080)
task dev:web

# Or directly:
npm run dev
```

## Tech Stack

- React 18 + TypeScript
- Vite (build tool)
- @tanstack/react-query (data fetching)
- CodeMirror 6 (SQL editor)
- ECharts (charts)
- react-grid-layout (dashboards)
- Lucide React (icons)
- Storybook (component development)

## Testing

```bash
npx vitest           # Run tests
npx vitest --ui      # UI mode
npx vitest --coverage # With coverage
```

## Storybook

```bash
npm run storybook
```

## Environment

- `VITE_RELAY_URL` — WebSocket URL for the Hocuspocus relay (default: `ws://localhost:3001`)

## Project Structure

```
src/
  components/    — Reusable UI components (Cell, OutputRenderer, Sidebar, etc.)
  pages/         — Page-level components (HomePage, NotebookPage, etc.)
  charts/        — ECharts chart type modules
  hooks/         — Custom React hooks
  lib/           — Utility functions
  styles/        — CSS and theme files
  types/         — TypeScript type definitions
  test/          — Test setup and utilities
```

See `FRONTEND.md` for detailed visual component documentation.
