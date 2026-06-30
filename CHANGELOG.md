# Changelog

## Unreleased

## v0.1.0-alpha — 2026-06-29

### Features

- Subdomain-based multi-tenancy org resolution
- Public sharing for notebooks and dashboards
- AI agent with tool call permissions and reasoning effort
- SQL command tool with multi-executor support
- Global agent panel with docking/undocking
- Notebook export as static HTML
- Trash/soft-delete for resources (default 7-day retention)

### Improvements

- Permission system refactored to ACL-only model
- Org admin mode toggle (bypass ACLs for troubleshooting)
- Improved scroll management in agent panels
- Cell reordering with dnd-kit
- Searchable user/group selectors in permission panels
- Chart type additions: stacked area, sankey, map geo, timeline

### Fixes

- Dark theme caret visibility in CodeMirror
- SSO provider persistence and discovery
- Agent cell updates appearing without refresh
- Registration validation and org mismatch checks
- Dashboard widget sizing and layout

### Infrastructure

- Go 1.25, Node 22, Vite 6
- Docker Compose dev stack with Keycloak SSO
- CI via GitHub Actions (Go tests, TypeScript, builds)
- Dependabot for automated dependency updates
