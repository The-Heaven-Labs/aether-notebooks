<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="logo.svg">
    <img src="logo.svg" alt="Aether Notebooks" width="200">
  </picture>
</p>

<h1 align="center">Aether Notebooks</h1>

<p align="center">
  A collaborative SQL/data notebook platform — think Jupyter for analytics,
  with real-time collaboration, AI agents, and beautiful dashboards.
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <a href="https://github.com/The-Heaven-Labs/aether-notebooks/actions/workflows/ci.yml"><img src="https://github.com/The-Heaven-Labs/aether-notebooks/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="CODE_OF_CONDUCT.md"><img src="https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg" alt="Code of Conduct"></a>
  <a href="#"><img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go" alt="Go 1.25"></a>
  <a href="#"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs Welcome"></a>
</p>

---

## Features

- **SQL Notebooks** — Write and execute SQL against Postgres, ClickHouse, or JavaScript, all within the same notebook
- **Real-time Collaboration** — Multiple users edit simultaneously via Yjs CRDT, just like Google Docs for data
- **AI Agent** — Built-in agent that can write queries, create charts, and explore your data conversationally
- **Rich Charts** — Bar, line, area, pie, scatter, timeline, sankey, map, big number, and more — all powered by ECharts
- **Dashboards** — Arrange cell outputs into live, shareable dashboards with drag-and-drop layout
- **Fine-grained Permissions** — ACL-based permission system with folder hierarchy inheritance
- **Multi-tenancy** — Subdomain-based org isolation for teams and enterprises
- **SSO / OIDC** — Single sign-on with any OpenID Connect provider, including automatic group provisioning
- **Audit Logging** — Full query and action audit trail stored in PostgreSQL

## Screenshots

<table>
  <tr>
    <td><img src="readme-screenshots/notebook-sql.png" alt="SQL notebook with query results" width="100%"></td>
    <td><img src="readme-screenshots/dashboard.png" alt="Dashboard with chart and data table" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>SQL notebook — write queries, see results, switch to charts</em></td>
    <td align="center"><em>Dashboard — combine charts, tables, and markdown into shared views</em></td>
  </tr>
  <tr>
    <td><img src="readme-screenshots/home-light.png" alt="File browser (light theme)" width="100%"></td>
    <td><img src="readme-screenshots/agent-chat.png" alt="AI agent with rendered markdown" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>File browser — folders, connectors, dashboards</em></td>
    <td align="center"><em>AI agent — chat with markdown, code blocks, and tables</em></td>
  </tr>
</table>

## Quick Start


```bash
git clone https://github.com/The-Heaven-Labs/aether-notebooks.git
cd aether-notebooks
docker compose -f docker-compose.dev.yml up -d
```

Then visit **[http://localhost:8080](http://localhost:8080)**.

```bash
# Or download a pre-built release and run directly (requires Postgres + Redis)
curl -LO https://github.com/The-Heaven-Labs/aether-notebooks/releases/latest/download/aether_latest_linux_amd64.tar.gz
tar xzf aether_latest_linux_amd64.tar.gz
AETHER_DATABASE_URL="postgres://..." AETHER_JWT_SECRET="..." AETHER_MASTER_KEY="..." ./aether-server
```

### Default Users

| Email | Password | Role |
|---|---|---|
| `nova@heaven-labs.com` | `nova123` | Primary test user |
| `sol@heaven-labs.com` | `sol123` | Secondary test user |
| `admin@heaven-labs.com` | — | Platform admin (auto-promoted) |

### Development with SSO

The dev stack includes Keycloak for SSO testing. See [AGENTS.md](AGENTS.md) for full setup including OIDC providers and group provisioning.

## Architecture

```
cmd/aether-server        → Go API server (port 8080)
cmd/aether               → CLI client
web/                     → React + TypeScript frontend (built with Vite)
relay/                   → Hocuspocus WebSocket relay (port 3001)
internal/
  api/                   → HTTP handlers + router (net/http ServeMux)
  auth/                  → JWT issuer + OIDC providers
  agent/                 → AI agent engine with tool calling
  config/                → Environment-based configuration
  database/              → pgx connection pool + SQL migrations
  executor/              → SQL executors (Postgres, ClickHouse, JavaScript)
  models/                → Shared model structs
  audit/                 → ClickHouse audit logger
migrations/              → SQL migration files (applied on startup)
```

## Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go (net/http, pgx, ygo/crdt) |
| Frontend | React 18, TypeScript, Vite, CodeMirror 6 |
| Charts | ECharts (10+ chart types) |
| Real-time | Yjs, Hocuspocus WebSocket relay |
| Databases | Postgres (primary), ClickHouse (optional connector target) |
| Cache | Redis |
| Auth | JWT, OIDC / Keycloak |
| AI | Multi-provider LLM agent (OpenAI, Anthropic, DeepSeek, etc.) |

## Development

**Recommended**: start the entire stack with a single command using Docker Compose:

```bash
docker compose -f docker-compose.dev.yml up -d
```

This runs all services (API, frontend, relay, Postgres, Redis, ClickHouse, OpenSearch, Keycloak) with hot-reload enabled. Visit **[http://localhost:8080](http://localhost:8080)**.

For finer control — or to run services natively — the [Taskfile](Taskfile.yml) provides individual commands:

```bash
task infra:up            # Start Postgres and Redis
task dev                 # Start Go API server (API + frontend on :8080, Air hot-reload)
task dev:relay           # Start Hocuspocus relay (port 3001)

# For frontend development outside Docker:
cd web && npm run build:watch  # Auto-rebuild on frontend changes
```

### Testing

```bash
task test                # All Go tests (starts infra automatically)
task test:v              # Verbose output
task test:api            # Only API tests
```

### Building

```bash
task build               # Go binaries
task build:web           # Frontend bundle
task build:all           # Everything
```

### Code quality

```bash
task fmt                 # gofmt
task vet                 # go vet
task check               # fmt + vet + tidy + test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development setup and coding conventions.

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `AETHER_MASTER_KEY` | Yes | — | AES-256 key for encrypting connector credentials |
| `AETHER_JWT_SECRET` | Yes | — | JWT signing secret |
| `AETHER_DATABASE_URL` | No | `postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable` | Postgres connection string |
| `AETHER_REDIS_URL` | No | `redis://localhost:6379` | Redis connection string |
| `AETHER_PORT` | No | `8080` | API server listen port |
| `AETHER_FRONTEND_URL` | No | `http://localhost:5173` | Frontend URL for CORS and OIDC redirect |
| `AETHER_PUBLIC_URL` | No | `http://localhost:8080` | Public-facing URL |
| `AETHER_PLATFORM_ADMIN_EMAIL` | No | — | Email to auto-promote to platform admin |
| `AETHER_DISABLE_REGISTRATION` | No | `false` | Disable new user registration |
| `AETHER_STORAGE_BACKEND` | No | `local` | Attachment storage (`local` or `s3`) |
| `AETHER_S3_BUCKET` | No | — | S3 bucket name (if using S3 storage) |
| `AETHER_MAX_ATTACHMENT_BYTES` | No | `10485760` | Max attachment upload size |

## Project Structure

```
.
├── cmd/
│   ├── aether-server/      # Go API server
│   └── aether/              # CLI client
├── internal/
│   ├── api/                 # HTTP handlers + middleware
│   ├── auth/                # JWT + OIDC authentication
│   ├── agent/               # AI agent engine
│   ├── config/              # Configuration loading
│   ├── crypto/              # Encryption utilities
│   ├── database/            # Database layer + migrations
│   ├── executor/            # SQL execution engines
│   ├── models/              # Data models
│   ├── audit/               # Audit logging
│   └── scheduler/           # Notebook scheduling
├── web/                     # React frontend
├── relay/                   # Hocuspocus collaboration relay
├── migrations/              # SQL migration files
├── dev/                     # Development seed data
└── scripts/                 # Utility scripts
```

## License

[MIT](LICENSE) — Copyright (c) 2026 The Heaven Labs

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md)
and [Code of Conduct](CODE_OF_CONDUCT.md) to get started.
