# Wave 3: Test Connection Buttons (Items 34, 42)

## Item 34: MCP config test button

### Backend
- **File:** `internal/api/mcp_server_handlers.go`
  - Added `handleTestMCPServer` method on `mcpServerHandlers`
  - For HTTP servers: makes GET request to the server's command URL with 10s timeout
  - For stdio servers: returns `success: false` with "testing not supported for stdio MCP servers"
  - Returns `{ success, status_code?, error? }`
  - Validates server belongs to caller's org

- **Route:** `POST /api/v1/mcp-servers/{id}/test` (authenticated, any role)

### Frontend
- **File:** `web/src/pages/MCPPage.tsx`
  - Added "Test" button in each server row (before Edit/Delete)
  - Shows Loader2 spinner while testing
  - Shows green ✓ with status code on success
  - Shows red ✗ with error message on failure
  - Uses local state `testResults` and `testingIds` per server

## Item 42: OIDC provider form test/validate button

### Backend
- **File:** `internal/api/sso_org_handlers.go`
  - Added `handleOrgTestSSOProvider` method on `Server`
  - Accepts `{ discovery_url, client_id, client_secret }`
  - Fetches the discovery URL with 10s timeout
  - Validates response is valid OIDC discovery document (checks for `issuer` field)
  - Returns `{ success, issuer?, provider_info?, error? }`

- **Route:** `POST /api/v1/sso/providers/test` (authenticated, admin role required)

### Frontend
- **File:** `web/src/pages/OrgSettingsPage.tsx`
  - Added "Test Connection" button in `ProviderForm` component (between Save and Cancel)
  - Disabled when discovery_url is empty
  - Shows Loader2 spinner while testing
  - Shows green banner with issuer name on success
  - Shows red banner with error message on failure
  - Test state is local to each form instance (resets on unmount)

## Validation
- `go build ./...` — clean
- `npx tsc --noEmit` — clean

## Files Changed
- `internal/api/mcp_server_handlers.go` — added handleTestMCPServer
- `internal/api/sso_org_handlers.go` — added handleOrgTestSSOProvider
- `internal/api/router.go` — added 2 routes
- `web/src/pages/MCPPage.tsx` — added test button + state
- `web/src/pages/OrgSettingsPage.tsx` — added test connection button in form
