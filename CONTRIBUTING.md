# Contributing to Aether Notebooks

Thanks for your interest in contributing! We welcome contributions of all
kinds — bug fixes, features, documentation, and feedback.

## Getting Started

### Prerequisites

- Go 1.25+
- Node.js 22+
- Docker (for Postgres, Redis, ClickHouse)

### Development Setup

```bash
# Clone the repo
git clone https://github.com/The-Heaven-Labs/aether-notebooks.git
cd aether-notebooks

# Start infrastructure (Postgres, Redis, ClickHouse)
docker compose -f docker-compose.dev.yml up -d postgres redis clickhouse

# Start the Go API (with hot reload)
task dev

# In another terminal, start the frontend
task dev:web

# In another terminal, start the relay
task dev:relay
```

See `AGENTS.md` for full development environment setup including
SSO/OIDC and multi-tenancy testing.

## Coding Conventions

### Go

- Follow [Go standard project layout](https://github.com/golang-standards/project-layout)
- No framework — use `net/http` ServeMux
- Tests hit a real database (no mocks)
- Run `task check` before committing (fmt + vet + tidy)

### TypeScript / React

- React 18 with hooks, no class components
- `@tanstack/react-query` for data fetching
- CSS variables for theming (`var(--accent)`, `var(--bg-primary)`)
- No hardcoded colors or fonts
- Run `npx tsc --noEmit` in `web/` before committing

### Commit Messages

We use conventional commits:

```
feat: add notebook export to HTML
fix: resolve cell reordering on mobile
docs: update README with screenshots
chore: bump dependencies
refactor: extract permission checking into middleware
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes
4. Run tests: `task test`
5. Push and open a Pull Request
6. Maintainers will review within a few days

### Before Submitting

- [ ] Code compiles (`go build ./...` / `cd web && npm run build`)
- [ ] Linter passes (`go vet ./...`)
- [ ] Tests pass (`task test`)
- [ ] No new TypeScript errors (`cd web && npx tsc --noEmit`)
- [ ] Follows existing code style

## Code of Conduct

Please read and follow our [Code of Conduct](CODE_OF_CONDUCT.md).
