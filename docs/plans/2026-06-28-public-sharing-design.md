# Public Sharing Design

Share notebooks and dashboards publicly via unauthenticated read-only links, with an org-level toggle to disable the feature.

## 1. Public Tokens Table

Replace the ad-hoc `public_token` column on `dashboards` with a dedicated table:

```sql
CREATE TABLE public_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('notebook', 'dashboard')),
    resource_id   UUID NOT NULL,
    token         TEXT NOT NULL UNIQUE DEFAULT encode(gen_random_bytes(16), 'hex'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_public_tokens_org ON public_tokens (org_id);
CREATE INDEX idx_public_tokens_resource ON public_tokens (resource_type, resource_id);
```

Migration: migrate existing `dashboards.public_token` values into the new table, then drop the column.

Benefits over per-resource columns:
- Single table for all shareable types
- Easy to add more types later
- Built-in revocation (delete the row)
- Track who created the token

## 2. Org-Level Toggle

```sql
ALTER TABLE orgs ADD COLUMN public_sharing_enabled BOOLEAN NOT NULL DEFAULT true;
```

- Defaults to enabled
- Org admins toggle via Org Settings
- When disabled: `POST .../share` returns 403, public endpoints return 404

## 3. Share / Revoke Endpoints

### `POST /api/v1/notebooks/{id}/share`
- Requires `share` permission on the notebook
- Checks `orgs.public_sharing_enabled`
- Inserts into `public_tokens` (or returns existing token)
- Returns `{ "public_url": "/public/{token}" }`

### `DELETE /api/v1/notebooks/{id}/share`
- Requires `share` permission
- Deletes the token row
- Returns 204

Same pattern for dashboards (replacing the current `POST /api/v1/dashboards/{id}/share`).

## 4. Public Endpoints (No Auth)

### `GET /api/v1/public/{token}`
- Looks up `public_tokens` by token
- Checks `orgs.public_sharing_enabled`
- Routes based on `resource_type`

**Notebook response:**
```json
{
    "type": "notebook",
    "notebook": { /* title, description, parameters */ },
    "cells": [
        {
            "type": "code",
            "language": "sql",
            "source": "SELECT * FROM table",
            "outputs": [ /* full output objects */ ]
        }
    ]
}
```

Includes: source, outputs, charts (outputs with `chart_config`), markdown rendered as HTML. Excludes: cell metadata, ACL info, edit/slide configs, version history.

**Dashboard response:**
Same as current `GET /api/v1/public/dashboards/{token}` but via the new table.

## 5. Frontend

### Public Notebook Page (`/public/:token`)
- No auth wrapper
- Fetches from `GET /api/v1/public/{token}`
- Renders notebook in read-only mode:
  - Code cells: source + outputs (tables, charts rendered fully)
  - Markdown cells: rendered
  - Cell collapse state respected
  - No edit buttons, no add cell bars, no run button
- Branded header with "Read-only" badge (matching the existing `PublicDashboardPage` pattern)

### Sharing UI
- "Share" button in notebook toolbar
- Modal shows:
  - Public URL (read-only input, copy button)
  - "Revoke" button (confirms, then removes token)
  - Disabled state when org-level toggle is off (with explanation text)
- Dashboard share page updated to use the same pattern

### Org Settings
- Toggle: "Allow public sharing of notebooks and dashboards"
- Stored in `orgs.public_sharing_enabled`

## 6. Migration

```sql
-- New table
CREATE TABLE public_tokens (...);

-- Migrate existing dashboard tokens
INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_at, created_by)
SELECT d.org_id, 'dashboard', d.id, d.public_token, NOW(), '00000000-0000-0000-0000-000000000000'
FROM dashboards d WHERE d.public_token IS NOT NULL;

-- Drop old column
ALTER TABLE dashboards DROP COLUMN public_token;

-- Org toggle
ALTER TABLE orgs ADD COLUMN public_sharing_enabled BOOLEAN NOT NULL DEFAULT true;
```

## 7. Files Changed

| File | Change |
|---|---|
| `internal/database/migrations/074_public_tokens.sql` | New migration |
| `internal/models/` | Add public_token model |
| `internal/sso/sso.go` | No change |
| `internal/api/notebook_handlers.go` | Add share/revoke handlers |
| `internal/api/dashboard_handlers.go` | Refactor share to use new table |
| `internal/api/public_handlers.go` | New file: public access handlers |
| `internal/api/router.go` | New routes |
| `internal/api/org_handlers.go` | Add sharing toggle |
| `web/src/pages/PublicNotebookPage.tsx` | New page |
| `web/src/pages/NotebookPage.tsx` | Share button in toolbar |
| `web/src/components/ShareModal.tsx` | New component |
| `web/src/pages/OrgSettingsPage.tsx` | Sharing toggle |
| `web/src/App.tsx` | Public route |
