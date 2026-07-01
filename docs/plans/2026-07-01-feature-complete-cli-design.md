# Feature-Complete CLI Design

**Date:** 2026-07-01
**Status:** Approved

## Goal

Extend the `aether` CLI (`cmd/aether`, `internal/cli/`) to cover **all** API endpoints, achieving feature parity between the CLI and the web UI.

## Architecture

Keep the existing pattern:
- **Cobra** for command parsing
- One file per resource domain in `internal/cli/`
- `Client` struct wraps `http.DefaultClient` with JWT auth
- Typed `Client` methods per resource (e.g., `ListFolders`, `CreateDashboard`)
- Commands are thin wrappers calling `Client` methods

## Command Tree

New commands in bold.

```
aether
├── login
├── logout
├── notebooks
│   ├── list
│   ├── create
│   ├── get
│   ├── update
│   ├── delete
│   ├── clone
│   ├── export
│   ├── import
│   ├── share (on|off|status)
│   ├── permissions
│   ├── snapshots (list|create|restore|diff)
│   └── schedules (list|create|get|update|delete)
├── cells
│   ├── execute
│   ├── list
│   ├── get
│   ├── update
│   ├── delete
│   ├── duplicate
│   └── versions (list|restore)
├── connectors
│   ├── list
│   ├── create
│   ├── get
│   ├── update
│   ├── delete
│   ├── set-default
│   ├── test
│   ├── schema
│   └── databases
├── dashboards
│   ├── list|create|get|update|delete
│   ├── widgets (add|update|delete)
│   ├── share (on|off|status)
│   └── permissions
├── folders
│   ├── list|create|get|update|delete
│   └── ancestors
├── groups
│   ├── list|create|update|delete
│   └── members (list|add|remove)
├── acl
│   ├── get
│   └── set
├── members
│   ├── list
│   ├── invite|invite-link
│   └── update-role|remove
├── org
│   ├── sharing (get|update)
│   ├── invitations (get|update)
│   └── registration (get|update)
├── tokens
│   ├── list|create
│   └── delete
├── templates
│   ├── list
│   ├── create
│   └── delete
├── trash
│   ├── list
│   └── restore
├── home
│   └── list|ensure
├── recent
├── attachments
│   ├── list|upload
│   └── get|delete
├── audit
├── motd
│   ├── list
│   └── admin (list|create|update|delete)
├── agents
│   ├── list|create|get|update|delete
│   ├── sessions (list|create|get)
│   └── messages
├── model-configs
│   ├── list|create|get|update|delete
│   └── test
├── skills
│   ├── list|create|get|update|delete
├── tools
│   ├── list|create|get|update|delete
│   └── test
├── mcp-servers
│   ├── list|create|get|update|delete
│   └── test
├── sso
│   ├── providers (list|create|update|delete)
│   ├── platform-providers (list|enable|disable)
│   └── settings (get|update)
├── admin
│   ├── orgs (list|create)
│   ├── users (list|update)
│   └── sso (list|create|update|delete)
└── seed
```

## Client Pattern

Each resource file has typed `Client` methods:

```go
func (c *Client) ListFolders(parentID string) ([]Folder, error)
func (c *Client) CreateFolder(name, parentID string) (*Folder, error)
func (c *Client) GetFolder(id string) (*Folder, error)
func (c *Client) UpdateFolder(id, name string) (*Folder, error)
func (c *Client) DeleteFolder(id string) error
```

Auth, error handling, and JSON marshalling reuse the existing `Do`/`GetJSON`/`PostJSON`/`DeleteJSON` methods. Admin-only endpoints use the same client — the backend enforces authorization.

## File Organization

New files (additions to `internal/cli/`):

| File | Contents |
|---|---|
| `dashboards.go` | dashboards + widgets + share |
| `folders.go` | folders + home + recent |
| `groups.go` | groups + members |
| `acl.go` | ACL get/set |
| `members.go` | member management |
| `org.go` | org settings, SSO |
| `tokens.go` | personal access tokens |
| `templates.go` | templates |
| `trash.go` | trash |
| `attachments.go` | attachments |
| `audit.go` | audit logs |
| `motd.go` | MOTD |
| `agents.go` | agents + sessions + messages |
| `model_configs.go` | model configs |
| `skills.go` | skills |
| `tools.go` | tools |
| `mcp.go` | MCP servers |
| `admin.go` | platform admin |
| `types.go` | shared response structs |

Existing files **extended** (subcommands added):
- `notebooks.go` — update, clone, export, import, share, snapshots, schedules
- `cells.go` — list, get, update, delete, duplicate, versions
- `connectors.go` — create, get, update, test, set-default, schema, databases

Existing files **untouched**:
- `auth.go`
- `client.go`
- `seed.go`
