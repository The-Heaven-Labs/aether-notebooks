# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Any technical — or not so technical — team or user that is data-driven and needs SQL to talk to data, or eventually to display data. Confirmed in the init interview (2026-10-04): the audience is deliberately broad, spanning analysts, engineers, and less-technical data consumers; the product must serve both heavy SQL users and people whose goal is only to see or present data.

## Product Purpose

Aether Notebooks is a collaborative SQL/data notebook platform — "think Jupyter for analytics" — where teams write and execute SQL against Postgres, ClickHouse, OpenSearch, or Databricks in shared notebooks, turn results into charts and dashboards, and work alongside a built-in AI agent. Success means teams get from data to shared, displayed insight without leaving their own infrastructure.

## Positioning

Self-hostable open-source platform: the full analytics workspace — notebooks, dashboards, fine-grained ACLs, SSO/OIDC, audit logging, multi-tenancy, and an MCP server/tool catalog for external AI harnesses — runs inside the user's own infrastructure under the MIT license. Single-user notebooks and hosted-only platforms cannot truthfully claim the same combination of scope and self-hosting.

## Operating Context

- Self-hosted operation is the primary delivery model (confirmed in the init interview): full stack via `docker compose -f docker-compose.dev.yml`, or pre-built `aether-server` release binaries with Postgres + Redis.
- Primary interface is the web app (React + Vite); companion surfaces are the Go API server, the `aether` CLI, the Hocuspocus collaboration relay, and MCP for external AI harnesses (OpenCode, Claude Code, etc.).
- Multi-tenant by subdomain (`org1.aether.test` → org), with org-level and platform-level admin tiers; SSO via any OIDC provider (Keycloak in the dev stack), including group provisioning.
- Data lives in connected databases/warehouses (Postgres, ClickHouse, OpenSearch, Databricks); ClickHouse warehouses can be managed with per-user/per-group table permissions provisioned by Aether.
- Repo-level development rituals: Taskfile (`task check`, `task test`), Go tests against a real database, Playwright E2E + visual-regression tests, Storybook, and mandatory real-browser validation for UI changes.

## Capabilities and Constraints

Capabilities (confirmed by the repository):
- SQL notebooks with multiple executors, CodeMirror 6 cells, markdown cells, cell titles, parameters, reordering, and bounded/truncated outputs.
- Real-time collaboration via Yjs CRDT over the Hocuspocus relay.
- Built-in AI agent (multi-provider, tool calling, sessions and sharing, subagents, skills) that can write queries, create charts, and explore data conversationally.
- 10+ chart types via ECharts; dashboards with drag-and-drop layout, variables, and query widgets.
- ACL-based permissions with folder-hierarchy inheritance (user / group / Everyone subjects), org admins, and platform admins.
- Multi-tenancy (subdomain isolation), SSO/OIDC with group provisioning, audit logging.
- MCP server exposing the tool catalog with OAuth 2.1 browser consent or personal access tokens.
- Public sharing of dashboards, with optional live queries (rate-limited per token + IP).
- Notebook scheduling (cron-based) is present, but scheduler execution is explicitly not wired yet.

Constraints and undecided facts:
- License: MIT (Copyright (c) 2026 The Heaven Labs).
- No published pricing, hosted-service, or support commitments are documented; do not invent them.
- Whether or when a hosted offering is planned is undecided.

Terminology in use: notebooks, cells, connections/connectors, dashboards, agents and agent sessions, skills, MCP servers, orgs, warehouses, folders.

## Brand Commitments

- Name: Aether Notebooks (The Heaven Labs).
- Existing identity assets: `logo.svg` / `logo.png`, documented in `logo-philosophy.md` as "Cellular Order" — celestial geometry, deep indigo / electric-blue palette, monospace + serif typography, craft over cleverness.
- README positions the product with "beautiful dashboards"; the visual identity is committed and on hand.
- No formal voice/tone guide was found; treat verbal identity as unformalized rather than inventing one.

## Evidence on Hand

- Real screenshots in `readme-screenshots/` (SQL notebook, dashboard, file browser, agent chat).
- Logo assets (`logo.svg`, `logo.png`) and `logo-philosophy.md`.
- README, AGENTS.md, FRONTEND.md (design system + component visual documentation), CONTRIBUTING.md, `docs/` design docs for shipped features, CHANGELOG.md.
- Dev seed users and a Keycloak test realm (`dev/keycloak-realm.json`).
- Playwright E2E/visual-regression suite and Storybook stories.
- Absent (must not be fabricated): customer logos, testimonials, case studies, benchmarks, press coverage, pricing.

## Product Principles

1. Self-host first: everything must remain completable inside the user's own infrastructure under MIT — no required cloud dependency.
2. One place for the data conversation: query, collaborate, visualize, and present in the same workspace instead of exporting between tools.
3. The agent is a first-class collaborator: it works on the same notebooks, permissions, and audit trail as human users.
4. Permissions and audit are platform features, not add-ons: folder-inherited ACLs, admin tiers, and full audit logging come standard.
5. Approachable for the SQL-capable and the SQL-adjacent: people whose job is to understand or display data should get value without becoming warehouse experts.

## Accessibility & Inclusion

The project maintains frontend accessibility practices documented in FRONTEND.md (keyboard navigation, visible focus states, ARIA labels, color contrast). No product-specific accessibility mandate (e.g., a WCAG level) was confirmed; do not claim one.
