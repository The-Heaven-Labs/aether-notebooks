# Heaven's Notebooks (hnb) — Design Document

## Overview

Heaven's Notebooks is a web-based collaborative notebook platform for data analysts. It provides SQL-first notebook editing with real-time collaboration, built-in visualizations, standalone dashboards, and scheduling — all backed by a Go API with a CLI for automation.

**Core philosophy:** Notebooks are a lens for looking at data, not a place to store it. They analyze, visualize, and share — they don't create or persist new data.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Web UI (React)                      │
│              Pastel theme, minimal design                 │
├──────────────┬──────────────────────┬───────────────────┤
│  REST/gRPC   │     WebSocket        │    WebSocket       │
│              │   (doc editing)      │  (live outputs)    │
▼              ▼                      ▼                    │
┌──────────────────┐  ┌──────────────────┐                │
│   Go API Server  │◄─┤  Yjs Relay (Node) │                │
│                  │  └──────────────────┘                │
│  - Auth (OIDC +  │                                      │
│    local)        │         ┌───────────────┐            │
│  - Notebooks API │────────►│   Executors    │            │
│  - Dashboards    │         │  ┌───────────┐ │            │
│  - Scheduler     │         │  │ SQL (Go)  │ │            │
│  - Audit logging │         │  │ JS (Deno) │ │            │
│  - Org/tenancy   │         │  └───────────┘ │            │
│  - CLI gateway   │         └───────────────┘            │
└────────┬─────────┘                                      │
         │                                                 │
         ▼                                                 │
┌──────────────────┐  ┌──────────────────┐                │
│    PostgreSQL    │  │      Redis       │                │
│  (state, meta,   │  │ (sessions, pub/  │                │
│   audit logs)    │  │  sub, cache)     │                │
└──────────────────┘  └──────────────────┘                │
```

**Approach:** Go monolith + Yjs WebSocket relay. Go handles everything (API, auth, execution, scheduling, audit). A lightweight Node/Hocuspocus server handles only CRDT-based real-time editing sync and persists resolved document state back to the Go API.

**Key flows:**
- **Editing:** Browser ↔ Yjs Relay (CRDT sync) → periodic persistence to Go API → Postgres
- **Execution:** Browser → Go API → Executor (SQL/JS) → results streamed back via WebSocket
- **Scheduling:** Go scheduler triggers parameterized notebook runs, overwrites current outputs
- **CLI:** Thin client that talks to the same Go API (REST/gRPC)

## Data Model

### Core Entities

```
Org
├── Members (users with roles: admin, editor, viewer)
├── Connectors (database connections: ClickHouse, Postgres, etc.)
├── Notebooks
│   ├── Cells (ordered list)
│   │   ├── CodeCell (language, source, connector_id, outputs)
│   │   └── TextCell (markdown/JSX content)
│   ├── Parameters (name, type, default value)
│   └── Schedules (cron expression, parameter overrides)
└── Dashboards
    └── Widgets (layout position, source: notebook_id + cell_id, display config)
```

### Notebook File Format (`.hnb.json`)

```json
{
  "version": 1,
  "id": "uuid",
  "title": "Revenue Analysis",
  "parameters": [
    { "name": "date_range", "type": "daterange", "default": "last_7d" }
  ],
  "cells": [
    {
      "id": "uuid",
      "type": "code",
      "language": "sql",
      "connector_id": "uuid",
      "source": "SELECT date, sum(revenue) FROM orders WHERE {{date_range}} GROUP BY 1",
      "outputs": [
        { "type": "table", "data": { "columns": ["..."], "rows": ["..."] } },
        { "type": "chart", "config": { "type": "line", "x": "date", "y": "revenue" } }
      ]
    },
    {
      "id": "uuid",
      "type": "text",
      "source": "## Revenue is trending up\nWe see a **12% increase** week-over-week."
    }
  ]
}
```

### Key Decisions

- **Connectors are org-level** — shared across notebooks, with permission controls
- **Outputs live inside cells** — scheduled runs overwrite the `outputs` array
- **Parameters use template syntax** (`{{param_name}}`) in SQL — simple substitution
- **Chart config is declarative** — stored alongside table output, the UI renders it

### Multi-tenancy

All tables have `org_id`. Row-level security in Postgres plus application-level enforcement. Connector credentials encrypted at rest per org (AES-256-GCM), encryption keys managed via a master key (env var or KMS integration later).

## Real-time Collaboration

Each notebook is a **Yjs document**. The document structure mirrors the notebook: an ordered array of cells, each cell containing its source text as a `Y.Text` (for character-level co-editing).

The **Yjs Relay** (Hocuspocus) manages document rooms. When a user opens a notebook, they join the room. Edits sync peer-to-peer through the relay via WebSocket.

**Persistence:** The relay periodically (and on last-user-disconnect) serializes the Yjs document state and sends it to the Go API, which writes it to Postgres. On room open, it hydrates from the last saved state.

**Awareness protocol:** Built into Yjs — shows cursors, selections, and who's online. No custom work needed.

### What syncs via CRDT (Yjs)

- Cell source text (character-level)
- Cell order (reordering, adding, deleting cells)
- Cell metadata (language, connector selection)
- Text cell content

### What does NOT sync via CRDT (goes through Go API)

- Cell execution and outputs (triggered via API, results broadcast via separate WebSocket)
- Chart configuration changes
- Dashboard layout
- Connector settings, parameters, schedules

### Conflict Scenarios

- Two people edit the same cell text → CRDT merges automatically
- One person deletes a cell while another edits it → delete wins (standard Yjs behavior), toast notification shown
- Two people run the same cell → both executions happen, last result wins

## Execution Engine

Executors run as separate processes spawned by the Go API. Each execution is isolated.

### SQL Executor (Go)

- Receives: SQL source, parameter values, connector config
- Connects to target database via appropriate driver (ClickHouse, Postgres, etc.)
- Streams results — first N rows immediately for preview, full result set capped at configurable limit (10k rows default)
- Returns structured output: column names, types, and row data

**Connectors are pluggable:**

```go
type Connector interface {
    ID() string
    Name() string
    Execute(ctx context.Context, query string, params map[string]any) (*ResultSet, error)
    TestConnection(ctx context.Context) error
    Schema(ctx context.Context) (*SchemaInfo, error)  // for autocomplete
}
```

### JS Executor (Deno)

- For custom visualizations when built-in chart types aren't enough
- Runs in a **sandboxed Deno subprocess** — no network access, no filesystem, time-limited
- Receives: cell's JS source + data from a preceding SQL cell's output
- Returns: rendered SVG/HTML
- Libraries available in sandbox: Observable Plot, D3, Vega-Lite (bundled)

### Execution Flow

1. User hits "Run" (or scheduler triggers)
2. Go API resolves parameters, picks the right executor
3. Executor runs, streams results back
4. Go API stores outputs in the cell, broadcasts to all connected clients
5. If a chart config exists, frontend re-renders the chart with new data

### Safeguards

- Query timeout per connector (configurable)
- Row limit per query
- Concurrent execution limit per org
- JS sandbox has CPU/memory limits via Deno permissions

## Dashboards

Dashboards are standalone entities within an org. They pull live data from notebook cell outputs.

### Structure

- **Grid layout** (CSS Grid) — widgets positioned by row/column/span
- **Widget types:**
  - **Chart widget** — renders cell's chart config with current output data
  - **Table widget** — shows cell's tabular output (with optional column filtering/sorting)
  - **Text widget** — renders a text cell's markdown content
  - **Metric widget** — pulls a single value from a cell output (e.g. "Total Revenue: $1.2M")

### Live Updates

When a notebook cell's output changes (manual run or scheduled), dashboards referencing that cell update automatically via WebSocket push. No polling.

### Dashboard Features

- **Auto-refresh interval** — optionally re-execute all source notebook cells on a cadence
- **Parameter overrides** — dashboards can set parameter values for referenced notebooks
- **View-only sharing** — shareable via public link (with optional token auth) for stakeholders without notebook access

### What Dashboards Are NOT

- Not a notebook view mode — they're their own entity
- Not a data store — always reflect current state of source cells
- Not editable by viewers — only editors/admins modify layout and config

## Auth, Tenancy & Audit

### Authentication

- **Local accounts:** email + password, bcrypt hashed. Email verification on signup.
- **OIDC/SSO:** Generic OIDC provider only. Org admins configure: issuer URL, client ID, client secret, scopes. Standard OIDC discovery (`.well-known/openid-configuration`).
- **Provider interface is modular:**

```go
type OIDCProvider interface {
    Name() string
    AuthURL(state string) string
    Exchange(ctx context.Context, code string) (*OIDCClaims, error)
    Validate(ctx context.Context, token string) (*OIDCClaims, error)
}
```

`GenericOIDCProvider` implements this interface. Future named providers (Google, Okta) would implement the same interface with provider-specific defaults.

- **Sessions:** JWT access tokens (short-lived, 15min) + refresh tokens (stored in Redis).

### Roles (per org)

| Role       | Notebooks         | Dashboards  | Connectors | Members |
|------------|-------------------|-------------|------------|---------|
| **Admin**  | Full access       | Full access | Manage     | Manage  |
| **Editor** | Create, edit, run | Create, edit| Use        | View    |
| **Viewer** | View, run         | View        | —          | —       |

### Audit Logging

- Every mutating action logged: who, what, when, org, resource type/id
- Actions: notebook CRUD, cell execution, connector changes, member changes, login/logout, dashboard changes, schedule changes
- Stored in dedicated `audit_logs` Postgres table, append-only
- Queryable via API (admins only), with filtering by user/resource/action/time range
- Retention policy configurable per org

### CLI Authentication

- `hnb login` — opens browser for OIDC or prompts for email/password
- Stores token in `~/.hnb/credentials.json`

## Frontend & Visual Design

**Stack:** React + TypeScript, Vite for bundling.

### Design Language

- **Pastel color palette** — soft backgrounds (warm white, light lavender, pale mint), muted accents
- **Simple outlines** — 1px borders, no shadows or gradients
- **Minimal chrome** — no unnecessary toolbars or sidebars. Content-first.
- **Typography-driven** — hierarchy through font size/weight, not color or decoration
- **No animations** — instant transitions, simple spinners for loading states

### Key Views

- **Notebook editor** — vertical cell list, thin toolbar (run all, add cell, params, share). Cells have subtle left border by type. Collaborator cursors as colored carets with name labels.
- **Dashboard editor** — grid canvas, drag-to-place widgets, resize handles. Property panel on right.
- **Dashboard viewer** — full-screen grid, no editing chrome. Optional auto-refresh indicator.
- **Home/org view** — list of notebooks and dashboards, search/filter. Clean table/list, no cards.

### Libraries

- **Code editor:** CodeMirror 6 (SQL support, Yjs binding via `y-codemirror.next`)
- **Charting:** Apache ECharts (built-in chart types)
- **Markdown:** `react-markdown` with GFM support

## CLI

```
hnb login                              # Auth (browser OIDC or email/password)
hnb logout

hnb notebooks list                     # List notebooks in current org
hnb notebooks create "Title"           # Create new notebook
hnb notebooks run <id> [--param k=v]   # Run all cells with optional param overrides
hnb notebooks export <id> --format pdf # Export notebook

hnb cells list <notebook_id>           # List cells in a notebook
hnb cells run <notebook_id> <cell_id>  # Run a single cell
hnb cells output <notebook_id> <cell_id> # Get cell output (JSON)

hnb dashboards list
hnb dashboards export <id> --format pdf

hnb connectors list
hnb connectors test <id>               # Test connection

hnb schedules list <notebook_id>
hnb schedules create <notebook_id> --cron "0 9 * * *" [--param k=v]
hnb schedules delete <id>

hnb orgs list
hnb orgs switch <id>

hnb config set <key> <value>           # Local config (default org, API URL, etc.)
```

**Principles:**
- Thin client — all logic in the API, CLI is HTTP calls + output formatting
- JSON output by default, `--pretty` for human-readable tables
- Exit codes: 0 success, 1 error
- Pipe-friendly: `hnb cells output <nb> <cell> | jq '.rows'`
