# Filesystem + Permissions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add folder-based filesystem organization and fine-grained ACL permissions to all resource types (notebooks, connectors, dashboards).

**Architecture:** Two DB migrations add `folders`, `groups`, `group_members`, and `acl_entries` tables plus `folder_id` columns on all resource tables. A `checkPermission` function resolves effective access by walking the folder ancestor chain and matching ACL entries against user identity (direct, group, org_role), falling back to org-role defaults only when no ACL exists in the chain. Frontend adds a file-browser home page, a permissions slide-over panel, and a groups management page.

**Tech Stack:** Go (net/http, pgx v5, recursive CTEs), React + React Query + TypeScript (Lucide icons, existing CSS variables).

---

## Task 1: Migration 009 — Folders

**Files:**
- Create: `internal/database/migrations/009_folders.sql`

**Step 1: Write the migration**

```sql
CREATE TABLE folders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  parent_id UUID REFERENCES folders(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  is_home BOOLEAN NOT NULL DEFAULT false,
  owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, parent_id, name)
);

CREATE INDEX idx_folders_org ON folders (org_id);
CREATE INDEX idx_folders_parent ON folders (parent_id);

ALTER TABLE notebooks  ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE connectors ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE dashboards ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;

INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
SELECT om.org_id, u.name || '''s Home', true, u.id, u.id
FROM users u
JOIN org_members om ON om.user_id = u.id;
```

**Step 2: Run tests to verify migration applies cleanly**

Run: `task test:api 2>&1 | head -30`
Expected: Tests compile and migrations apply without error (existing tests may fail due to schema changes — that's OK, we'll fix them in later tasks).

**Step 3: Commit**

```bash
git add internal/database/migrations/009_folders.sql
git commit -m "feat: migration 009 — folders table and folder_id columns"
```

---

## Task 2: Migration 010 — Groups + ACL

**Files:**
- Create: `internal/database/migrations/010_groups_acl.sql`

**Step 1: Write the migration**

```sql
CREATE TABLE groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, name)
);

CREATE TABLE group_members (
  group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, user_id)
);

CREATE TABLE acl_entries (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('folder','notebook','connector','dashboard')),
  resource_id   UUID NOT NULL,
  subject_type  TEXT NOT NULL CHECK (subject_type IN ('user','group','org_role')),
  subject_id    TEXT NOT NULL,
  actions       TEXT[] NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (resource_type, resource_id, subject_type, subject_id)
);

CREATE INDEX idx_acl_resource ON acl_entries (resource_type, resource_id);
CREATE INDEX idx_acl_subject  ON acl_entries (subject_type, subject_id);

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'user', f.owner_id::text,
       ARRAY['view','create','edit','manage','delete']
FROM folders f WHERE f.is_home = true;
```

**Step 2: Run migrations**

Run: `task test:api 2>&1 | head -30`
Expected: Migrations apply without error.

**Step 3: Commit**

```bash
git add internal/database/migrations/010_groups_acl.sql
git commit -m "feat: migration 010 — groups and ACL tables"
```

---

## Task 3: Go Models — Folder, Group, ACLEntry

**Files:**
- Create: `internal/models/folder.go`
- Create: `internal/models/group.go`
- Create: `internal/models/acl.go`

**Step 1: Write folder.go**

```go
package models

import "time"

type Folder struct {
	ID        string     `json:"id"`
	OrgID     string     `json:"org_id"`
	ParentID  *string    `json:"parent_id,omitempty"`
	Name      string     `json:"name"`
	IsHome    bool       `json:"is_home"`
	OwnerID   *string    `json:"owner_id,omitempty"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
```

**Step 2: Write group.go**

```go
package models

import "time"

type Group struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type GroupMember struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}
```

**Step 3: Write acl.go**

```go
package models

import "time"

type ACLEntry struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	SubjectType  string    `json:"subject_type"`
	SubjectID    string    `json:"subject_id"`
	Actions      []string  `json:"actions"`
	CreatedAt    time.Time `json:"created_at"`
}
```

**Step 4: Verify compilation**

Run: `go build ./internal/...`
Expected: Compiles with no errors.

**Step 5: Commit**

```bash
git add internal/models/folder.go internal/models/group.go internal/models/acl.go
git commit -m "feat: models for Folder, Group, ACLEntry"
```

---

## Task 4: Permission Resolver

**Files:**
- Create: `internal/api/permissions.go`
- Create: `internal/api/permissions_test.go`

**Step 1: Write the failing tests first**

`internal/api/permissions_test.go`:

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPermission_OrgRoleFallback verifies that when no ACL exists anywhere in the
// chain, access falls back to org role defaults.
func TestPermission_OrgRoleFallback(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("perm-fallback-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Perm Org")

	// Create a notebook (no folder, no ACL entries)
	nbID := createNotebook(t, srv, token, "Test NB")

	// As editor (registerAndGetToken creates admin, so get a notebook via admin)
	// Admin can view and edit — no ACL = org role fallback
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin get notebook: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPermission_ACLGrantOverridesOrgRole verifies that an explicit ACL grant
// allows access even for a viewer who wouldn't get 'edit' via org role.
func TestPermission_ACLGrantOverridesOrgRole(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("perm-grant-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "ACL Org")

	// Get org info from token
	var orgID string
	var userID string
	{
		req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var me map[string]any
		json.NewDecoder(rec.Body).Decode(&me)
		userID = me["id"].(string)
		// get orgID from notebooks query
		body, _ := json.Marshal(map[string]string{"title": "ACL NB"})
		r2 := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
		r2.Header.Set("Content-Type", "application/json")
		r2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, r2)
		var nb map[string]any
		json.NewDecoder(rec2.Body).Decode(&nb)
		orgID = nb["org_id"].(string)
		_ = userID
	}

	// Create notebook
	nbID := createNotebook(t, srv, token, "Restricted NB")

	// Seed an ACL entry: only this user can view this notebook
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'org_role', 'admin', ARRAY['view','edit','delete'])`,
		orgID, nbID,
	)
	if err != nil {
		t.Fatalf("seed ACL: %v", err)
	}

	// Admin can view (org_role='admin' matches the entry)
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin view: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPermission_DenyWhenACLExistsButNoMatch verifies that when ACL entries exist
// in the chain but none match the user, access is denied.
func TestPermission_DenyWhenACLExistsButNoMatch(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("perm-deny-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Deny Org")

	// Get org_id
	body, _ := json.Marshal(map[string]string{"title": "X"})
	r := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	var nb map[string]any
	json.NewDecoder(rec.Body).Decode(&nb)
	orgID := nb["org_id"].(string)
	nbID := nb["id"].(string)

	// Insert an ACL entry for a DIFFERENT user (not our user)
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', '00000000-0000-0000-0000-000000000099', ARRAY['view'])`,
		orgID, nbID,
	)
	if err != nil {
		t.Fatalf("seed ACL: %v", err)
	}

	// Our user (admin by org role) should now be DENIED because ACL exists but doesn't match them
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("should be denied: expected 403, got %d: %s", rec2.Code, rec2.Body.String())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `task test:api -run TestPermission 2>&1`
Expected: FAIL — `checkPermission` not yet called from handlers / not yet implemented.

**Step 3: Write permissions.go**

`internal/api/permissions.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
)

// orgRoleActions maps org roles to their default allowed actions (fallback when no ACL exists).
var orgRoleActions = map[string]map[string]bool{
	"viewer": {"view": true},
	"editor": {"view": true, "run": true, "edit": true, "use": true},
	"admin":  {"view": true, "run": true, "edit": true, "use": true, "create": true, "share": true, "delete": true, "manage": true},
}

// resourceTable maps resource types to their DB table names.
var resourceTable = map[string]string{
	"notebook":  "notebooks",
	"connector": "connectors",
	"dashboard": "dashboards",
}

type aclCandidate struct {
	subjectType string
	subjectID   string
	actions     []string
	specificity int // -1 = resource itself, 0 = immediate parent folder, 1+ = ancestor
	subjectRank int // user=0, group=1, org_role=2
}

// checkPermission returns true if userID has action on resourceType/resourceID.
func (s *Server) checkPermission(ctx context.Context, userID, orgID, orgRole, resourceType, resourceID, action string) (bool, error) {
	// 1. Collect user's group memberships
	rows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, userID)
	if err != nil {
		return false, fmt.Errorf("group query: %w", err)
	}
	var groupIDs []string
	for rows.Next() {
		var gid string
		rows.Scan(&gid)
		groupIDs = append(groupIDs, gid)
	}
	rows.Close()

	// 2. ACL entries directly on the resource (specificity = -1)
	var candidates []aclCandidate
	resRows, err := s.db.Pool.Query(ctx,
		`SELECT subject_type, subject_id, actions FROM acl_entries
		 WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, orgID)
	if err != nil {
		return false, fmt.Errorf("acl resource query: %w", err)
	}
	for resRows.Next() {
		var c aclCandidate
		c.specificity = -1
		resRows.Scan(&c.subjectType, &c.subjectID, &c.actions)
		c.subjectRank = subjectRank(c.subjectType)
		candidates = append(candidates, c)
	}
	resRows.Close()

	anyACLInChain := len(candidates) > 0

	// 3. Find the folder to start ancestor walk from
	var ancestorFolderID *string
	if resourceType == "folder" {
		var pid *string
		err := s.db.Pool.QueryRow(ctx,
			`SELECT parent_id FROM folders WHERE id = $1 AND org_id = $2`,
			resourceID, orgID,
		).Scan(&pid)
		if err != nil && err != pgx.ErrNoRows {
			return false, fmt.Errorf("folder parent query: %w", err)
		}
		ancestorFolderID = pid
	} else if table, ok := resourceTable[resourceType]; ok {
		var fid *string
		err := s.db.Pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT folder_id FROM %s WHERE id = $1 AND org_id = $2`, table),
			resourceID, orgID,
		).Scan(&fid)
		if err != nil && err != pgx.ErrNoRows {
			return false, fmt.Errorf("resource folder query: %w", err)
		}
		ancestorFolderID = fid
	}

	// 4. Walk ancestor folders (if any)
	if ancestorFolderID != nil {
		folderRows, err := s.db.Pool.Query(ctx, `
			WITH RECURSIVE ancestors AS (
				SELECT id, parent_id, 0 AS depth FROM folders WHERE id = $1
				UNION ALL
				SELECT f.id, f.parent_id, a.depth + 1
				FROM folders f JOIN ancestors a ON f.id = a.parent_id
			)
			SELECT ae.subject_type, ae.subject_id, ae.actions, a.depth
			FROM ancestors a
			JOIN acl_entries ae ON ae.resource_type = 'folder' AND ae.resource_id = a.id AND ae.org_id = $2
			ORDER BY a.depth ASC
		`, *ancestorFolderID, orgID)
		if err != nil {
			return false, fmt.Errorf("ancestor acl query: %w", err)
		}
		for folderRows.Next() {
			var c aclCandidate
			folderRows.Scan(&c.subjectType, &c.subjectID, &c.actions, &c.specificity)
			c.subjectRank = subjectRank(c.subjectType)
			candidates = append(candidates, c)
			anyACLInChain = true
		}
		folderRows.Close()
	}

	// 5. Sort: most specific first; within same specificity, user > group > org_role
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity < candidates[j].specificity
		}
		return candidates[i].subjectRank < candidates[j].subjectRank
	})

	// 6. Find first entry that matches this user
	for _, c := range candidates {
		if !matchesUser(c, userID, orgRole, groupIDs) {
			continue
		}
		for _, a := range c.actions {
			if a == action {
				return true, nil
			}
		}
		return false, nil // matched but action not granted
	}

	// 7. No matching entry found
	if anyACLInChain {
		return false, nil // ACL exists but user not in it → DENY
	}

	// 8. Fallback to org role
	return orgRoleActions[orgRole][action], nil
}

func matchesUser(c aclCandidate, userID, orgRole string, groupIDs []string) bool {
	switch c.subjectType {
	case "user":
		return c.subjectID == userID
	case "group":
		for _, gid := range groupIDs {
			if c.subjectID == gid {
				return true
			}
		}
	case "org_role":
		return c.subjectID == orgRole
	}
	return false
}

func subjectRank(subjectType string) int {
	switch subjectType {
	case "user":
		return 0
	case "group":
		return 1
	default:
		return 2
	}
}

// requirePermission returns middleware that enforces a permission check on the
// resource identified by the given path parameter name.
func (s *Server) requirePermission(resourceType, idParam, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			resourceID := r.PathValue(idParam)
			allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, resourceType, resourceID, action)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "permission check failed")
				return
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

**Step 4: Run the permission tests**

Run: `task test:api -run TestPermission 2>&1`
Expected: TestPermission_OrgRoleFallback PASS, others may still fail because notebooks endpoint doesn't yet call checkPermission. That's expected — we wire it in Task 10.

Actually these tests will all pass trivially until `requirePermission` is wired to the notebook routes. For now, just check the package compiles:

Run: `go build ./internal/api/...`
Expected: no errors.

**Step 5: Commit**

```bash
git add internal/api/permissions.go internal/api/permissions_test.go
git commit -m "feat: permission resolver — checkPermission and requirePermission middleware"
```

---

## Task 5: Folder Handlers

**Files:**
- Create: `internal/api/folder_handlers.go`
- Create: `internal/api/folder_handlers_test.go`

**Step 1: Write the failing tests**

`internal/api/folder_handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFolderCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("folder-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Folder Org")

	// Create folder at root
	body, _ := json.Marshal(map[string]string{"name": "Engineering"})
	req := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	json.NewDecoder(rec.Body).Decode(&folder)
	folderID := folder["id"].(string)

	// Create sub-folder
	body2, _ := json.Marshal(map[string]any{"name": "Backend", "parent_id": folderID})
	req2 := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create sub-folder: expected 201, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var subFolder map[string]any
	json.NewDecoder(rec2.Body).Decode(&subFolder)
	subFolderID := subFolder["id"].(string)

	// Get root contents
	req3 := httptest.NewRequest("GET", "/api/v1/folders", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("list root: expected 200, got %d", rec3.Code)
	}

	// Get folder contents (has sub-folder)
	req4 := httptest.NewRequest("GET", "/api/v1/folders/"+folderID, nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("get folder: expected 200, got %d: %s", rec4.Code, rec4.Body.String())
	}
	var contents map[string]any
	json.NewDecoder(rec4.Body).Decode(&contents)
	folders := contents["folders"].([]any)
	if len(folders) != 1 {
		t.Errorf("expected 1 sub-folder, got %d", len(folders))
	}

	// Get ancestors breadcrumb
	req5 := httptest.NewRequest("GET", "/api/v1/folders/"+subFolderID+"/ancestors", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	rec5 := httptest.NewRecorder()
	srv.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("ancestors: expected 200, got %d: %s", rec5.Code, rec5.Body.String())
	}
	var ancestors []any
	json.NewDecoder(rec5.Body).Decode(&ancestors)
	if len(ancestors) != 2 { // Engineering, Backend
		t.Errorf("expected 2 ancestors, got %d", len(ancestors))
	}

	// Rename folder
	renameBody, _ := json.Marshal(map[string]string{"name": "Engineering Team"})
	req6 := httptest.NewRequest("PUT", "/api/v1/folders/"+folderID, bytes.NewReader(renameBody))
	req6.Header.Set("Content-Type", "application/json")
	req6.Header.Set("Authorization", "Bearer "+token)
	rec6 := httptest.NewRecorder()
	srv.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec6.Code, rec6.Body.String())
	}

	// Delete sub-folder (empty)
	req7 := httptest.NewRequest("DELETE", "/api/v1/folders/"+subFolderID, nil)
	req7.Header.Set("Authorization", "Bearer "+token)
	rec7 := httptest.NewRecorder()
	srv.ServeHTTP(rec7, req7)
	if rec7.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec7.Code, rec7.Body.String())
	}

	// Delete non-empty folder → 409 Conflict
	// Re-create sub-folder so parent is non-empty
	body8, _ := json.Marshal(map[string]any{"name": "Nonempty Child", "parent_id": folderID})
	req8 := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body8))
	req8.Header.Set("Content-Type", "application/json")
	req8.Header.Set("Authorization", "Bearer "+token)
	rec8 := httptest.NewRecorder()
	srv.ServeHTTP(rec8, req8)
	if rec8.Code != http.StatusCreated {
		t.Fatalf("re-create subfolder: expected 201, got %d", rec8.Code)
	}

	req9 := httptest.NewRequest("DELETE", "/api/v1/folders/"+folderID, nil)
	req9.Header.Set("Authorization", "Bearer "+token)
	rec9 := httptest.NewRecorder()
	srv.ServeHTTP(rec9, req9)
	if rec9.Code != http.StatusConflict {
		t.Fatalf("delete non-empty: expected 409, got %d: %s", rec9.Code, rec9.Body.String())
	}

	// Force delete
	req10 := httptest.NewRequest("DELETE", "/api/v1/folders/"+folderID+"?force=true", nil)
	req10.Header.Set("Authorization", "Bearer "+token)
	rec10 := httptest.NewRecorder()
	srv.ServeHTTP(rec10, req10)
	if rec10.Code != http.StatusNoContent {
		t.Fatalf("force delete: expected 204, got %d: %s", rec10.Code, rec10.Body.String())
	}
}
```

**Step 2: Run test to confirm it fails**

Run: `task test:api -run TestFolderCRUD 2>&1`
Expected: FAIL — routes not registered yet.

**Step 3: Write folder_handlers.go**

`internal/api/folder_handlers.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type folderContents struct {
	Folder     *models.Folder      `json:"folder,omitempty"`
	Folders    []models.Folder     `json:"folders"`
	Notebooks  []models.Notebook   `json:"notebooks"`
	Connectors []folderConnector   `json:"connectors"`
	Dashboards []models.Dashboard  `json:"dashboards"`
}

// folderConnector is a lightweight connector listing (no credentials).
type folderConnector struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	IsDefault bool   `json:"is_default"`
	FolderID  *string `json:"folder_id,omitempty"`
}

func (s *Server) handleListRootContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	result := folderContents{
		Folders:    []models.Folder{},
		Notebooks:  []models.Notebook{},
		Connectors: []folderConnector{},
		Dashboards: []models.Dashboard{},
	}

	// Child folders at root (parent_id IS NULL)
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE org_id = $1 AND parent_id IS NULL ORDER BY name`,
		claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f models.Folder
		rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome, &f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
		result.Folders = append(result.Folders, f)
	}

	// Resources at root (folder_id IS NULL)
	result.Notebooks = s.listNotebooksInFolder(ctx, claims.OrgID, nil)
	result.Connectors = s.listConnectorsInFolder(ctx, claims.OrgID, nil)
	result.Dashboards = s.listDashboardsInFolder(ctx, claims.OrgID, nil)

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetFolderContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE id = $1 AND org_id = $2`,
		folderID, claims.OrgID,
	).Scan(&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome, &folder.OwnerID,
		&folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	result := folderContents{
		Folder:     &folder,
		Folders:    []models.Folder{},
		Notebooks:  []models.Notebook{},
		Connectors: []folderConnector{},
		Dashboards: []models.Dashboard{},
	}

	// Child folders
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE org_id = $1 AND parent_id = $2 ORDER BY name`,
		claims.OrgID, folderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f models.Folder
		rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome, &f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
		result.Folders = append(result.Folders, f)
	}

	result.Notebooks = s.listNotebooksInFolder(ctx, claims.OrgID, &folderID)
	result.Connectors = s.listConnectorsInFolder(ctx, claims.OrgID, &folderID)
	result.Dashboards = s.listDashboardsInFolder(ctx, claims.OrgID, &folderID)

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetFolderAncestors(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE chain AS (
			SELECT id, parent_id, name, 0 AS depth FROM folders WHERE id = $1 AND org_id = $2
			UNION ALL
			SELECT f.id, f.parent_id, f.name, c.depth + 1
			FROM folders f JOIN chain c ON f.id = c.parent_id
		)
		SELECT id, name FROM chain ORDER BY depth DESC
	`, folderID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type breadcrumb struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var crumbs []breadcrumb
	for rows.Next() {
		var b breadcrumb
		rows.Scan(&b.ID, &b.Name)
		crumbs = append(crumbs, b)
	}
	if crumbs == nil {
		crumbs = []breadcrumb{}
	}
	writeJSON(w, http.StatusOK, crumbs)
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ctx := r.Context()
	var f models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO folders (org_id, parent_id, name, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at`,
		claims.OrgID, req.ParentID, req.Name, claims.UserID,
	).Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome, &f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	var req struct {
		Name     *string `json:"name,omitempty"`
		ParentID *string `json:"parent_id,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.ParentID == nil {
		writeError(w, http.StatusBadRequest, "name or parent_id required")
		return
	}

	ctx := r.Context()
	query := "UPDATE folders SET updated_at = NOW()"
	args := []any{}
	n := 1
	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", n)
		args = append(args, *req.Name)
		n++
	}
	if req.ParentID != nil {
		query += fmt.Sprintf(", parent_id = $%d", n)
		args = append(args, *req.ParentID)
		n++
	}
	query += fmt.Sprintf(" WHERE id = $%d AND org_id = $%d", n, n+1)
	args = append(args, folderID, claims.OrgID)
	query += " RETURNING id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at"

	var f models.Folder
	err := s.db.Pool.QueryRow(ctx, query, args...).
		Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome, &f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"
	ctx := r.Context()

	if !force {
		// Check if folder has any children (sub-folders or resources)
		var count int
		s.db.Pool.QueryRow(ctx, `
			SELECT count(*) FROM (
				SELECT id FROM folders WHERE parent_id = $1
				UNION ALL
				SELECT id FROM notebooks WHERE folder_id = $1
				UNION ALL
				SELECT id FROM connectors WHERE folder_id = $1
				UNION ALL
				SELECT id FROM dashboards WHERE folder_id = $1
			) sub
		`, folderID).Scan(&count)
		if count > 0 {
			writeError(w, http.StatusConflict, "folder is not empty; use ?force=true to delete recursively")
			return
		}
	}

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM folders WHERE id = $1 AND org_id = $2`, folderID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listNotebooksInFolder returns notebooks in a folder (nil = root).
func (s *Server) listNotebooksInFolder(ctx interface{ Query(context.Context, string, ...any) (pgx.Rows, error) }, orgID string, folderID *string) []models.Notebook {
	// Note: ctx here is r.Context(), passed through separately
	return nil // implemented below
}
```

Wait — the helpers `listNotebooksInFolder` etc. need a `context.Context`, not a query interface. Let me revise:

`internal/api/folder_handlers.go` — replace the helper stubs with the real implementations using `context.Context` and `s.db.Pool`:

```go
import "context"

func (s *Server) listNotebooksInFolder(ctx context.Context, orgID string, folderID *string) []models.Notebook {
	var q string
	var args []any
	if folderID == nil {
		q = `SELECT id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at
		     FROM notebooks WHERE org_id = $1 AND folder_id IS NULL ORDER BY updated_at DESC`
		args = []any{orgID}
	} else {
		q = `SELECT id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at
		     FROM notebooks WHERE org_id = $1 AND folder_id = $2 ORDER BY updated_at DESC`
		args = []any{orgID, *folderID}
	}
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return []models.Notebook{}
	}
	defer rows.Close()
	var nbs []models.Notebook
	for rows.Next() {
		var nb models.Notebook
		var connID *string
		var params []byte
		rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &connID, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
		if connID != nil { nb.ConnectorID = *connID }
		json.Unmarshal(params, &nb.Parameters)
		if nb.Parameters == nil { nb.Parameters = []models.Parameter{} }
		nbs = append(nbs, nb)
	}
	if nbs == nil { nbs = []models.Notebook{} }
	return nbs
}

func (s *Server) listConnectorsInFolder(ctx context.Context, orgID string, folderID *string) []folderConnector {
	var q string
	var args []any
	if folderID == nil {
		q = `SELECT id, name, type, is_default, folder_id FROM connectors WHERE org_id = $1 AND folder_id IS NULL ORDER BY name`
		args = []any{orgID}
	} else {
		q = `SELECT id, name, type, is_default, folder_id FROM connectors WHERE org_id = $1 AND folder_id = $2 ORDER BY name`
		args = []any{orgID, *folderID}
	}
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil { return []folderConnector{} }
	defer rows.Close()
	var conns []folderConnector
	for rows.Next() {
		var c folderConnector
		rows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID)
		conns = append(conns, c)
	}
	if conns == nil { conns = []folderConnector{} }
	return conns
}

func (s *Server) listDashboardsInFolder(ctx context.Context, orgID string, folderID *string) []models.Dashboard {
	var q string
	var args []any
	if folderID == nil {
		q = `SELECT id, org_id, title, settings, public_token, created_by, created_at, updated_at
		     FROM dashboards WHERE org_id = $1 AND folder_id IS NULL ORDER BY updated_at DESC`
		args = []any{orgID}
	} else {
		q = `SELECT id, org_id, title, settings, public_token, created_by, created_at, updated_at
		     FROM dashboards WHERE org_id = $1 AND folder_id = $2 ORDER BY updated_at DESC`
		args = []any{orgID, *folderID}
	}
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil { return []models.Dashboard{} }
	defer rows.Close()
	var dashes []models.Dashboard
	for rows.Next() {
		var d models.Dashboard
		var settings []byte
		rows.Scan(&d.ID, &d.OrgID, &d.Title, &settings, &d.PublicToken, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
		json.Unmarshal(settings, &d.Settings)
		dashes = append(dashes, d)
	}
	if dashes == nil { dashes = []models.Dashboard{} }
	return dashes
}
```

The full `folder_handlers.go` should contain all the above (combining the handler functions and the helper methods). Write the file as one complete unit.

**Step 4: Run tests**

Run: `task test:api -run TestFolderCRUD 2>&1`
Expected: FAIL — routes not registered yet.

**Step 5: Commit (handlers written, not yet wired)**

```bash
git add internal/api/folder_handlers.go internal/api/folder_handlers_test.go
git commit -m "feat: folder handlers (CRUD + contents + ancestors)"
```

---

## Task 6: Group Handlers

**Files:**
- Create: `internal/api/group_handlers.go`
- Create: `internal/api/group_handlers_test.go`

**Step 1: Write the failing test**

`internal/api/group_handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGroupCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Org")

	// Create group
	body, _ := json.Marshal(map[string]string{"name": "Analytics"})
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var g map[string]any
	json.NewDecoder(rec.Body).Decode(&g)
	groupID := g["id"].(string)

	// List groups
	req2 := httptest.NewRequest("GET", "/api/v1/groups", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list groups: expected 200, got %d", rec2.Code)
	}
	var groups []any
	json.NewDecoder(rec2.Body).Decode(&groups)
	if len(groups) == 0 {
		t.Error("expected at least one group")
	}

	// Get user ID to add as member
	meReq := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	srv.ServeHTTP(meRec, meReq)
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	userID := me["id"].(string)

	// Add member
	memberBody, _ := json.Marshal(map[string]string{"user_id": userID})
	req3 := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/members", bytes.NewReader(memberBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// Remove member
	req4 := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID+"/members/"+userID, nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d: %s", rec4.Code, rec4.Body.String())
	}

	// Rename group
	renameBody, _ := json.Marshal(map[string]string{"name": "Data Analytics"})
	req5 := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(renameBody))
	req5.Header.Set("Content-Type", "application/json")
	req5.Header.Set("Authorization", "Bearer "+token)
	rec5 := httptest.NewRecorder()
	srv.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("rename group: expected 200, got %d: %s", rec5.Code, rec5.Body.String())
	}

	// Delete group
	req6 := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID, nil)
	req6.Header.Set("Authorization", "Bearer "+token)
	rec6 := httptest.NewRecorder()
	srv.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusNoContent {
		t.Fatalf("delete group: expected 204, got %d: %s", rec6.Code, rec6.Body.String())
	}
}
```

**Step 2: Write group_handlers.go**

`internal/api/group_handlers.go`:

```go
package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx, `
		SELECT g.id, g.org_id, g.name, g.created_at,
		       COUNT(gm.user_id) AS member_count
		FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id
		WHERE g.org_id = $1
		GROUP BY g.id ORDER BY g.name
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var groups []models.Group
	for rows.Next() {
		var g models.Group
		rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt, &g.MemberCount)
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []models.Group{}
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct{ Name string `json:"name"` }
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	ctx := r.Context()
	var g models.Group
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, $2)
		 RETURNING id, org_id, name, created_at`,
		claims.OrgID, req.Name,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create group")
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	var req struct{ Name string `json:"name"` }
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	ctx := r.Context()
	var g models.Group
	err := s.db.Pool.QueryRow(ctx,
		`UPDATE groups SET name = $1 WHERE id = $2 AND org_id = $3
		 RETURNING id, org_id, name, created_at`,
		req.Name, groupID, claims.OrgID,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM groups WHERE id = $1 AND org_id = $2`, groupID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx, `
		SELECT u.id, u.email, u.name
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = $1
		AND EXISTS (SELECT 1 FROM groups g WHERE g.id = $1 AND g.org_id = $2)
		ORDER BY u.name
	`, groupID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var members []models.GroupMember
	for rows.Next() {
		var m models.GroupMember
		rows.Scan(&m.UserID, &m.Email, &m.Name)
		members = append(members, m)
	}
	if members == nil {
		members = []models.GroupMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	var req struct{ UserID string `json:"user_id"` }
	if err := decodeJSON(r, &req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	ctx := r.Context()
	// Verify group belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, `SELECT true FROM groups WHERE id = $1 AND org_id = $2`,
		groupID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		groupID, req.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	userID := r.PathValue("user_id")
	ctx := r.Context()
	// Verify group belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, `SELECT true FROM groups WHERE id = $1 AND org_id = $2`,
		groupID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	s.db.Pool.Exec(ctx,
		`DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 3: Commit**

```bash
git add internal/api/group_handlers.go internal/api/group_handlers_test.go
git commit -m "feat: group handlers (CRUD + member management)"
```

---

## Task 7: ACL Handlers

**Files:**
- Create: `internal/api/acl_handlers.go`
- Create: `internal/api/acl_handlers_test.go`

**Step 1: Write the failing test**

`internal/api/acl_handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestACLGetAndPut(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("acl-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "ACL Org2")

	// Get user's own ID
	meReq := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	srv.ServeHTTP(meRec, meReq)
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	userID := me["id"].(string)

	// Create a notebook
	nbID := createNotebook(t, srv, token, "ACL Test NB")

	// GET ACL — initially empty
	req := httptest.NewRequest("GET", "/api/v1/acl/notebook/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get acl: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []any
	json.NewDecoder(rec.Body).Decode(&entries)
	// may have 0 entries

	// PUT ACL — set new entries
	body, _ := json.Marshal(map[string]any{
		"entries": []map[string]any{
			{"subject_type": "user", "subject_id": userID, "actions": []string{"view", "edit"}},
		},
	})
	req2 := httptest.NewRequest("PUT", "/api/v1/acl/notebook/"+nbID, bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("put acl: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// GET ACL again — should have 1 entry
	req3 := httptest.NewRequest("GET", "/api/v1/acl/notebook/"+nbID, nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	var entries2 []any
	json.NewDecoder(rec3.Body).Decode(&entries2)
	if len(entries2) != 1 {
		t.Errorf("expected 1 ACL entry after PUT, got %d", len(entries2))
	}
}
```

**Step 2: Write acl_handlers.go**

`internal/api/acl_handlers.go`:

```go
package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleGetACL(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, resource_type, resource_id, subject_type, subject_id, actions, created_at
		 FROM acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3
		 ORDER BY subject_type, subject_id`,
		resourceType, resourceID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var entries []models.ACLEntry
	for rows.Next() {
		var e models.ACLEntry
		rows.Scan(&e.ID, &e.OrgID, &e.ResourceType, &e.ResourceID, &e.SubjectType, &e.SubjectID, &e.Actions, &e.CreatedAt)
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

type aclEntryInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
}

func (s *Server) handlePutACL(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")

	var req struct {
		Entries []aclEntryInput `json:"entries"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Delete existing entries for this resource
	_, err = tx.Exec(ctx,
		`DELETE FROM acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear ACL")
		return
	}

	// Insert new entries
	var inserted []models.ACLEntry
	for _, e := range req.Entries {
		if e.SubjectType == "" || e.SubjectID == "" || len(e.Actions) == 0 {
			continue
		}
		var entry models.ACLEntry
		err := tx.QueryRow(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			 VALUES ($1, $2, $3::uuid, $4, $5, $6)
			 ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
			 DO UPDATE SET actions = EXCLUDED.actions
			 RETURNING id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at`,
			claims.OrgID, resourceType, resourceID, e.SubjectType, e.SubjectID, e.Actions,
		).Scan(&entry.ID, &entry.OrgID, &entry.ResourceType, &entry.ResourceID,
			&entry.SubjectType, &entry.SubjectID, &entry.Actions, &entry.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
			return
		}
		inserted = append(inserted, entry)
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	if inserted == nil {
		inserted = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, inserted)
}
```

**Step 3: Commit**

```bash
git add internal/api/acl_handlers.go internal/api/acl_handlers_test.go
git commit -m "feat: ACL handlers — GET and PUT per-resource ACL entries"
```

---

## Task 8: Register New Routes

**Files:**
- Modify: `internal/api/router.go`

**Step 1: Read the current router.go** (already done; use the version you read earlier)

**Step 2: Add the new routes** inside the `routes()` function, before the `// Internal routes` comment:

```go
// Folder routes
s.mux.Handle("GET /api/v1/folders", authMW(http.HandlerFunc(s.handleListRootContents)))
s.mux.Handle("GET /api/v1/folders/{id}", authMW(s.requirePermission("folder", "id", "view")(http.HandlerFunc(s.handleGetFolderContents))))
s.mux.Handle("GET /api/v1/folders/{id}/ancestors", authMW(http.HandlerFunc(s.handleGetFolderAncestors)))
s.mux.Handle("POST /api/v1/folders", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateFolder))))
s.mux.Handle("PUT /api/v1/folders/{id}", authMW(s.requirePermission("folder", "id", "edit")(http.HandlerFunc(s.handleUpdateFolder))))
s.mux.Handle("DELETE /api/v1/folders/{id}", authMW(s.requirePermission("folder", "id", "delete")(http.HandlerFunc(s.handleDeleteFolder))))

// Group routes (admin-only for mutations; all members can list)
s.mux.Handle("GET /api/v1/groups", authMW(http.HandlerFunc(s.handleListGroups)))
s.mux.Handle("POST /api/v1/groups", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateGroup))))
s.mux.Handle("PUT /api/v1/groups/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateGroup))))
s.mux.Handle("DELETE /api/v1/groups/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteGroup))))
s.mux.Handle("GET /api/v1/groups/{id}/members", authMW(http.HandlerFunc(s.handleListGroupMembers)))
s.mux.Handle("POST /api/v1/groups/{id}/members", authMW(RequireRole("admin")(http.HandlerFunc(s.handleAddGroupMember))))
s.mux.Handle("DELETE /api/v1/groups/{id}/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleRemoveGroupMember))))

// ACL routes (manage permission on the resource required)
s.mux.Handle("GET /api/v1/acl/{resource_type}/{resource_id}", authMW(http.HandlerFunc(s.handleGetACL)))
s.mux.Handle("PUT /api/v1/acl/{resource_type}/{resource_id}", authMW(http.HandlerFunc(s.handlePutACL)))
```

**Step 3: Run all tests**

Run: `task test:api 2>&1 | tail -20`
Expected: TestFolderCRUD, TestGroupCRUD, TestACLGetAndPut all PASS. Existing tests remain green.

**Step 4: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register folder, group, and ACL routes"
```

---

## Task 9: Home Folder on Org Join

**Files:**
- Modify: `internal/api/auth_handlers.go` — legacy register path
- Modify: `internal/api/org_handlers.go` — handleOrgCreate and handleOrgJoin

**Step 1: Write the failing test**

Add to `internal/api/auth_handlers_test.go` (or a new file):

```go
func TestRegister_CreatesHomeFolder(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("home-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Home Org")

	// Get user ID
	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var me map[string]any
	json.NewDecoder(rec.Body).Decode(&me)
	userID := me["id"].(string)

	// Check that a home folder was created for this user
	var folderID string
	err := db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true`,
		userID,
	).Scan(&folderID)
	if err != nil {
		t.Fatalf("no home folder created: %v", err)
	}

	// Check that an ACL entry was seeded for the home folder
	var count int
	db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM acl_entries WHERE resource_type = 'folder' AND resource_id = $1::uuid AND subject_type = 'user' AND subject_id = $2`,
		folderID, userID,
	).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 ACL entry for home folder, got %d", count)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `task test:api -run TestRegister_CreatesHomeFolder 2>&1`
Expected: FAIL — home folder not created yet.

**Step 3: Add createHomeFolder helper**

In `internal/api/auth_handlers.go`, add this function (at the bottom of the file, before the end):

```go
// querier is implemented by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// createHomeFolder creates a home folder for userID in orgID and seeds its ACL entry.
// Runs inside the provided querier (can be a tx or pool).
func createHomeFolder(ctx context.Context, q querier, orgID, userID, userName string) error {
	var folderID string
	err := q.QueryRow(ctx,
		`INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
		 VALUES ($1, $2, true, $3, $3)
		 RETURNING id`,
		orgID, userName+"'s Home", userID,
	).Scan(&folderID)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view','create','edit','manage','delete'])`,
		orgID, folderID, userID,
	)
	return err
}
```

Note: you need to add `"github.com/jackc/pgconn"` import or use `pgconn.CommandTag`. Actually in pgx v5, `Exec` returns `pgconn.CommandTag`. Check existing imports in the file — `pgx/v5` package includes this. The `querier` interface can use:

```go
import (
    "context"
    "github.com/jackc/pgx/v5/pgconn"
)

type querier interface {
    Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

**Step 4: Call createHomeFolder in auth_handlers.go (legacy path)**

In the `handleRegister` function, after `tx.Exec(ctx, INSERT INTO org_members...)` succeeds and before `tx.Commit(ctx)`, add:

```go
if err := createHomeFolder(ctx, tx, orgID, userID, req.Name); err != nil {
    writeError(w, http.StatusInternalServerError, "failed to create home folder")
    return
}
```

Also in the auto-join path (after the auto-join `org_members` insert), add a non-tx call:
```go
var autoJoinUserName string
s.db.Pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, userID).Scan(&autoJoinUserName)
if err := createHomeFolder(ctx, s.db.Pool, autoJoinOrgID, userID, autoJoinUserName); err != nil {
    fmt.Printf("auto-join createHomeFolder failed: %v\n", err)
    // non-fatal — user still joins
}
```

**Step 5: Call createHomeFolder in org_handlers.go**

In `handleOrgCreate`, after the `INSERT INTO org_members` exec and before `tx.Commit(ctx)`:

```go
var uName string
tx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&uName)
if err := createHomeFolder(ctx, tx, orgID, claims.UserID, uName); err != nil {
    writeError(w, http.StatusInternalServerError, "failed to create home folder")
    return
}
```

In `handleOrgJoin`, after the successful `INSERT INTO org_members` exec and before issuing the token:

```go
var uName2 string
s.db.Pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&uName2)
if err := createHomeFolder(ctx, s.db.Pool, orgID, claims.UserID, uName2); err != nil {
    fmt.Printf("handleOrgJoin createHomeFolder failed: %v\n", err)
    // non-fatal
}
```

**Step 6: Run the test**

Run: `task test:api -run TestRegister_CreatesHomeFolder 2>&1`
Expected: PASS.

Run: `task test:api 2>&1 | tail -10`
Expected: All tests PASS.

**Step 7: Commit**

```bash
git add internal/api/auth_handlers.go internal/api/org_handlers.go
git commit -m "feat: create home folder and seed ACL on org join"
```

---

## Task 10: Add folder_id to Resource Handlers + Models

**Files:**
- Modify: `internal/models/notebook.go` — add FolderID field
- Modify: `internal/models/connector.go` — add FolderID field
- Modify: `internal/models/dashboard.go` — add FolderID field
- Modify: `internal/api/notebook_handlers.go` — accept folder_id on create/update, return folder_id in queries
- Modify: `internal/api/connector_handlers.go` — same
- Modify: `internal/api/dashboard_handlers.go` — same

**Step 1: Write a failing test**

Add to `internal/api/notebook_handlers_test.go`:

```go
func TestNotebook_FolderID(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("nb-folder-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "FolderNB Org")

	// Create a folder
	folderBody, _ := json.Marshal(map[string]string{"name": "My Folder"})
	req := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(folderBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: %d %s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	json.NewDecoder(rec.Body).Decode(&folder)
	folderID := folder["id"].(string)

	// Create notebook with folder_id
	body, _ := json.Marshal(map[string]any{"title": "Folder NB", "folder_id": folderID})
	req2 := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create nb with folder_id: %d %s", rec2.Code, rec2.Body.String())
	}
	var nb map[string]any
	json.NewDecoder(rec2.Body).Decode(&nb)
	if nb["folder_id"] != folderID {
		t.Errorf("expected folder_id=%s, got %v", folderID, nb["folder_id"])
	}

	nbID := nb["id"].(string)

	// Move notebook to root (folder_id: null via update)
	moveBody, _ := json.Marshal(map[string]any{"folder_id": nil})
	req3 := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, bytes.NewReader(moveBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("move to root: %d %s", rec3.Code, rec3.Body.String())
	}
	var updated map[string]any
	json.NewDecoder(rec3.Body).Decode(&updated)
	if updated["folder_id"] != nil {
		t.Errorf("expected folder_id=nil after move, got %v", updated["folder_id"])
	}
}
```

**Step 2: Add FolderID to models**

In `internal/models/notebook.go`, add `FolderID *string \`json:"folder_id,omitempty"\`` to `Notebook` struct after `ConnectorID`.

In `internal/models/connector.go`, add `FolderID *string \`json:"folder_id,omitempty"\`` after `IsDefault`.

In `internal/models/dashboard.go`, add `FolderID *string \`json:"folder_id,omitempty"\`` after `PublicToken`.

**Step 3: Update notebook_handlers.go**

In `handleCreateNotebook`:
- Add `FolderID *string \`json:"folder_id,omitempty"\`` to `createNotebookRequest`
- Add `folder_id` to the INSERT: change to `INSERT INTO notebooks (org_id, title, description, parameters, created_by, folder_id)` with `$6 = req.FolderID`
- Add `folder_id` to the RETURNING clause and scan it

In `handleListNotebooks`:
- Add `folder_id` to SELECT and scan into `nb.FolderID`

In `handleGetNotebook`:
- Add `folder_id` to SELECT and scan into `nb.FolderID`

In `handleUpdateNotebook`:
- Add `FolderID **string \`json:"folder_id"\`` to `updateNotebookRequest` (double pointer to distinguish "not provided" from "set to null")
- In the nil-check for empty update, include `req.FolderID == nil`
- Handle `folder_id` update: if `*req.FolderID == nil`, set to NULL; else set to `**req.FolderID`

Actually, using `**string` is idiomatic for "explicitly set to null". Use:
```go
// In updateNotebookRequest, add:
FolderID **string `json:"folder_id"` // present but null = move to root; absent = no change
```

In the update query builder, add:
```go
if req.FolderID != nil {
    if *req.FolderID == nil {
        query += ", folder_id = NULL"
    } else {
        query += fmt.Sprintf(", folder_id = $%d", argN)
        args = append(args, **req.FolderID)
        argN++
    }
}
```

Also update the nil check:
```go
if req.Title == nil && req.Description == nil && req.ConnectorID == nil && req.Parameters == nil && req.FolderID == nil {
```

**Step 4: Update connector_handlers.go**

Similarly, add `folder_id` support to the connector CREATE (if the connector handlers have a create handler that returns a connector). Look at `internal/api/connector_handlers.go` to find the create handler and update similarly.

**Step 5: Update dashboard_handlers.go**

Similarly for dashboards.

**Step 6: Run tests**

Run: `task test:api -run TestNotebook_FolderID 2>&1`
Expected: PASS.

Run: `task test:api 2>&1 | tail -10`
Expected: All tests PASS.

**Step 7: Commit**

```bash
git add internal/models/notebook.go internal/models/connector.go internal/models/dashboard.go
git add internal/api/notebook_handlers.go internal/api/connector_handlers.go internal/api/dashboard_handlers.go
git commit -m "feat: add folder_id to notebooks, connectors, dashboards — create/update/list"
```

---

## Task 11: Frontend Types

**Files:**
- Modify: `web/src/types/index.ts`

**Step 1: Add new types**

Append to the end of `web/src/types/index.ts`:

```typescript
export interface Folder {
  id: string
  org_id: string
  parent_id?: string
  name: string
  is_home: boolean
  owner_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface FolderContents {
  folder?: Folder
  folders: Folder[]
  notebooks: Notebook[]
  connectors: Array<{ id: string; name: string; type: string; is_default?: boolean; folder_id?: string }>
  dashboards: Dashboard[]
}

export interface Group {
  id: string
  org_id: string
  name: string
  member_count: number
  created_at: string
}

export interface GroupMember {
  user_id: string
  email: string
  name: string
}

export interface ACLEntry {
  id: string
  org_id: string
  resource_type: string
  resource_id: string
  subject_type: 'user' | 'group' | 'org_role'
  subject_id: string
  actions: string[]
  created_at: string
}
```

Also update `Notebook`, `Connector`, `Dashboard` interfaces to include `folder_id?: string`.

**Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit 2>&1 | head -20`
Expected: No errors (or only pre-existing errors unrelated to this change).

**Step 3: Commit**

```bash
git add web/src/types/index.ts
git commit -m "feat: frontend types for Folder, Group, ACLEntry"
```

---

## Task 12: Frontend File Browser (HomePage Rewrite)

**Files:**
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Rewrite HomePage.tsx**

The page reads `?folder=<uuid>` from the URL (absent = root). It fetches folder contents via the appropriate API endpoint and renders a breadcrumb + grid of folders + items.

```tsx
import { useNavigate, useSearchParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import type { FolderContents, Folder } from '../types'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { ErrorBanner } from '../components/ErrorBanner'
import { Folder as FolderIcon, BookOpen, LayoutDashboard, Database, MoreHorizontal, Home } from 'lucide-react'

export function HomePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const folderID = searchParams.get('folder')
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [creating, setCreating] = useState<null | 'folder' | 'notebook'>(null)
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const contentsURL = folderID ? `/api/v1/folders/${folderID}` : '/api/v1/folders'
  const { data, isLoading } = useQuery<FolderContents>({
    queryKey: ['folder-contents', folderID ?? 'root'],
    queryFn: () => api.get<FolderContents>(contentsURL),
  })

  const ancestorURL = folderID ? `/api/v1/folders/${folderID}/ancestors` : null
  const { data: ancestors = [] } = useQuery<Array<{ id: string; name: string }>>({
    queryKey: ['folder-ancestors', folderID],
    queryFn: () => api.get(`/api/v1/folders/${folderID}/ancestors`),
    enabled: !!folderID,
  })

  const createFolder = useMutation({
    mutationFn: (name: string) =>
      api.post<Folder>('/api/v1/folders', { name, parent_id: folderID }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents', folderID ?? 'root'] })
      setCreating(null); setNewName('')
    },
    onError: (e: Error) => setError(e.message),
  })

  const createNotebook = useMutation({
    mutationFn: (title: string) =>
      api.post<{ id: string }>('/api/v1/notebooks', { title, folder_id: folderID }),
    onSuccess: (nb) => navigate(`/notebooks/${nb.id}`),
    onError: (e: Error) => setError(e.message),
  })

  const deleteFolder = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/folders/${id}?force=true`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['folder-contents', folderID ?? 'root'] }),
    onError: (e: Error) => setError(e.message),
  })

  const deleteNotebook = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/notebooks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['folder-contents', folderID ?? 'root'] }),
    onError: (e: Error) => setError(e.message),
  })

  const isEmpty = data && data.folders.length === 0 && data.notebooks.length === 0 &&
    data.connectors.length === 0 && data.dashboards.length === 0

  return (
    <AppShell>
      <div style={{ maxWidth: 1280, margin: '0 auto' }}>
        {/* Breadcrumb */}
        <div style={styles.breadcrumb}>
          <button style={styles.crumbBtn} onClick={() => setSearchParams({})}>
            <Home size={14} /> Files
          </button>
          {ancestors.map((a) => (
            <span key={a.id}>
              <span style={styles.sep}>/</span>
              <button style={styles.crumbBtn} onClick={() => setSearchParams({ folder: a.id })}>
                {a.name}
              </button>
            </span>
          ))}
        </div>

        {/* Toolbar */}
        <div style={styles.toolbar}>
          <button style={styles.newBtn} onClick={() => { setCreating('folder'); setNewName('') }}>
            + New Folder
          </button>
          <button style={styles.newBtn} onClick={() => { setCreating('notebook'); setNewName('') }}>
            + New Notebook
          </button>
        </div>

        {/* Inline create form */}
        {creating && (
          <div style={styles.createForm}>
            <input
              style={styles.input}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={creating === 'folder' ? 'Folder name…' : 'Notebook title…'}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter' && newName.trim()) {
                  creating === 'folder' ? createFolder.mutate(newName.trim()) : createNotebook.mutate(newName.trim())
                }
                if (e.key === 'Escape') { setCreating(null); setNewName('') }
              }}
            />
            <button style={styles.createBtn} disabled={!newName.trim()} onClick={() =>
              creating === 'folder' ? createFolder.mutate(newName.trim()) : createNotebook.mutate(newName.trim())
            }>Create</button>
            <button style={styles.cancelBtn} onClick={() => { setCreating(null); setNewName('') }}>Cancel</button>
          </div>
        )}

        {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

        {isLoading && <div style={{ padding: 32, color: '#aaa' }}>Loading…</div>}

        {isEmpty && !creating && (
          <EmptyState
            icon={<FolderIcon size={32} />}
            title="This folder is empty"
            text="Create a folder or notebook to get started."
            action={{ label: '+ New Notebook', onClick: () => setCreating('notebook') }}
          />
        )}

        {/* Folders section */}
        {data && data.folders.length > 0 && (
          <section style={styles.section}>
            <div style={styles.sectionTitle}>Folders</div>
            <div style={styles.grid}>
              {data.folders.map((f) => (
                <FolderItem
                  key={f.id}
                  folder={f}
                  onClick={() => setSearchParams({ folder: f.id })}
                  onDelete={() => deleteFolder.mutate(f.id)}
                />
              ))}
            </div>
          </section>
        )}

        {/* Notebooks section */}
        {data && data.notebooks.length > 0 && (
          <section style={styles.section}>
            <div style={styles.sectionTitle}>Notebooks</div>
            <div style={styles.list}>
              {data.notebooks.map((nb) => (
                <div key={nb.id} style={styles.item}>
                  <Link to={`/notebooks/${nb.id}`} style={styles.itemLink}>
                    <BookOpen size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={styles.itemName}>{nb.title}</span>
                  </Link>
                  <button style={styles.delBtn} onClick={() => deleteNotebook.mutate(nb.id)}>Delete</button>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Connectors section */}
        {data && data.connectors.length > 0 && (
          <section style={styles.section}>
            <div style={styles.sectionTitle}>Connectors</div>
            <div style={styles.list}>
              {data.connectors.map((c) => (
                <div key={c.id} style={styles.item}>
                  <Link to={`/connectors`} style={styles.itemLink}>
                    <Database size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={styles.itemName}>{c.name}</span>
                    {c.is_default && <span style={styles.badge}>default</span>}
                  </Link>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Dashboards section */}
        {data && data.dashboards.length > 0 && (
          <section style={styles.section}>
            <div style={styles.sectionTitle}>Dashboards</div>
            <div style={styles.list}>
              {data.dashboards.map((d) => (
                <div key={d.id} style={styles.item}>
                  <Link to={`/dashboards/${d.id}`} style={styles.itemLink}>
                    <LayoutDashboard size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                    <span style={styles.itemName}>{d.title}</span>
                  </Link>
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </AppShell>
  )
}

function FolderItem({ folder, onClick, onDelete }: { folder: Folder; onClick: () => void; onDelete: () => void }) {
  return (
    <div style={styles.folderCard} className="card-hover">
      <button style={styles.folderBtn} onClick={onClick}>
        <FolderIcon size={18} style={{ color: 'var(--accent)' }} />
        <span style={styles.folderName}>{folder.name}</span>
        {folder.is_home && <span style={styles.badge}>home</span>}
      </button>
      <button style={styles.delBtn} onClick={(e) => { e.stopPropagation(); onDelete() }}>×</button>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  breadcrumb: { display: 'flex', alignItems: 'center', gap: 4, marginBottom: 20, flexWrap: 'wrap' },
  crumbBtn: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--accent)', fontSize: 13, fontWeight: 500, display: 'flex', alignItems: 'center', gap: 4, padding: '2px 4px' },
  sep: { color: '#ccc', margin: '0 2px' },
  toolbar: { display: 'flex', gap: 10, marginBottom: 20 },
  newBtn: { padding: '7px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  createForm: { display: 'flex', gap: 10, marginBottom: 20, padding: 16, background: '#fff', borderRadius: 4, border: '1px solid #e8e8e8' },
  input: { flex: 1, padding: '8px 12px', border: '1px solid #ddd', borderRadius: 4, fontSize: 14 },
  createBtn: { padding: '7px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '7px 14px', border: '1px solid #ddd', borderRadius: 4, background: 'none', fontSize: 13, cursor: 'pointer', color: '#555' },
  section: { marginBottom: 32 },
  sectionTitle: { fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: '#aaa', marginBottom: 10 },
  grid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 10 },
  folderCard: { display: 'flex', alignItems: 'center', background: '#fff', border: '1px solid #e8e8e8', borderRadius: 4, overflow: 'hidden', transition: 'border-color 0.15s' },
  folderBtn: { flex: 1, display: 'flex', alignItems: 'center', gap: 8, padding: '10px 12px', background: 'none', border: 'none', cursor: 'pointer', textAlign: 'left' },
  folderName: { fontSize: 13, fontWeight: 500, color: '#111', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  list: { display: 'flex', flexDirection: 'column', gap: 6 },
  item: { display: 'flex', alignItems: 'center', background: '#fff', border: '1px solid #e8e8e8', borderRadius: 4, padding: '8px 14px', gap: 10 },
  itemLink: { flex: 1, display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none' },
  itemName: { fontSize: 14, fontWeight: 500, color: '#111', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  badge: { fontSize: 10, fontWeight: 700, background: 'var(--accent-light)', color: 'var(--accent)', borderRadius: 3, padding: '1px 6px', letterSpacing: '0.05em', flexShrink: 0 },
  delBtn: { padding: '3px 8px', border: 'none', background: 'transparent', color: 'var(--error)', fontSize: 12, cursor: 'pointer', flexShrink: 0 },
}
```

**Step 2: Update document.title**

Add at the top of `HomePage`:
```tsx
import { useEffect } from 'react'
// inside HomePage function:
useEffect(() => {
  document.title = data?.folder ? `${data.folder.name} — hnb` : "Files — hnb"
}, [data?.folder?.name])
```

**Step 3: Verify the app compiles**

Run: `cd web && npx tsc --noEmit 2>&1 | head -20`
Expected: No new TypeScript errors.

**Step 4: Commit**

```bash
git add web/src/pages/HomePage.tsx
git commit -m "feat: file browser home page with folder navigation"
```

---

## Task 13: Permissions Panel

**Files:**
- Create: `web/src/components/PermissionsPanel.tsx`

**Step 1: Write the component**

The panel is a slide-over drawer that fetches and mutates ACL entries for a resource.

```tsx
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ACLEntry } from '../types'
import { X, Plus, Trash2 } from 'lucide-react'

interface Props {
  resourceType: string
  resourceId: string
  resourceName: string
  onClose: () => void
}

const ALL_ACTIONS: Record<string, string[]> = {
  folder:    ['view', 'create', 'edit', 'manage', 'delete'],
  notebook:  ['view', 'run', 'edit', 'share', 'delete'],
  connector: ['view', 'use', 'edit', 'share', 'delete'],
  dashboard: ['view', 'edit', 'share', 'delete'],
}

export function PermissionsPanel({ resourceType, resourceId, resourceName, onClose }: Props) {
  const qc = useQueryClient()
  const aclKey = ['acl', resourceType, resourceId]

  const { data: entries = [] } = useQuery<ACLEntry[]>({
    queryKey: aclKey,
    queryFn: () => api.get<ACLEntry[]>(`/api/v1/acl/${resourceType}/${resourceId}`),
  })

  const putACL = useMutation({
    mutationFn: (newEntries: Array<{ subject_type: string; subject_id: string; actions: string[] }>) =>
      api.put(`/api/v1/acl/${resourceType}/${resourceId}`, { entries: newEntries }),
    onSuccess: () => qc.invalidateQueries({ queryKey: aclKey }),
  })

  const [newSubjectType, setNewSubjectType] = useState<'user' | 'group' | 'org_role'>('user')
  const [newSubjectId, setNewSubjectId] = useState('')
  const [newActions, setNewActions] = useState<string[]>([])

  const actions = ALL_ACTIONS[resourceType] ?? ALL_ACTIONS.notebook

  const removeEntry = (entry: ACLEntry) => {
    const next = entries.filter((e) => e.id !== entry.id).map((e) => ({
      subject_type: e.subject_type, subject_id: e.subject_id, actions: e.actions,
    }))
    putACL.mutate(next)
  }

  const addEntry = () => {
    if (!newSubjectId.trim() || newActions.length === 0) return
    const next = [
      ...entries.map((e) => ({ subject_type: e.subject_type, subject_id: e.subject_id, actions: e.actions })),
      { subject_type: newSubjectType, subject_id: newSubjectId.trim(), actions: newActions },
    ]
    putACL.mutate(next)
    setNewSubjectId('')
    setNewActions([])
  }

  const toggleNewAction = (a: string) =>
    setNewActions((prev) => prev.includes(a) ? prev.filter((x) => x !== a) : [...prev, a])

  return (
    <div style={overlay}>
      <div style={panel}>
        <div style={header}>
          <div>
            <div style={title}>Permissions</div>
            <div style={subtitle}>{resourceName}</div>
          </div>
          <button style={closeBtn} onClick={onClose}><X size={16} /></button>
        </div>

        <div style={body}>
          {entries.length === 0 && (
            <p style={{ color: '#aaa', fontSize: 13, margin: '0 0 16px' }}>
              No explicit permissions set — falling back to org role defaults.
            </p>
          )}

          {entries.map((e) => (
            <div key={e.id} style={entryRow}>
              <div style={entryLeft}>
                <span style={badge}>{e.subject_type}</span>
                <span style={subjectName}>{e.subject_id}</span>
              </div>
              <div style={entryActions}>
                {actions.map((a) => (
                  <span key={a} style={{ ...actionChip, opacity: e.actions.includes(a) ? 1 : 0.25 }}>{a}</span>
                ))}
              </div>
              <button style={removeBtn} onClick={() => removeEntry(e)}><Trash2 size={12} /></button>
            </div>
          ))}

          {/* Add entry */}
          <div style={addSection}>
            <div style={{ fontSize: 11, fontWeight: 700, color: '#aaa', letterSpacing: '0.08em', textTransform: 'uppercase', marginBottom: 8 }}>Add permission</div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 8 }}>
              <select
                value={newSubjectType}
                onChange={(e) => setNewSubjectType(e.target.value as any)}
                style={selectStyle}
              >
                <option value="user">User (UUID)</option>
                <option value="group">Group (UUID)</option>
                <option value="org_role">Org Role</option>
              </select>
              <input
                style={inputStyle}
                value={newSubjectId}
                onChange={(e) => setNewSubjectId(e.target.value)}
                placeholder={newSubjectType === 'org_role' ? 'viewer / editor / admin' : 'UUID'}
              />
            </div>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 10 }}>
              {actions.map((a) => (
                <button
                  key={a}
                  type="button"
                  style={{ ...actionChip, cursor: 'pointer', opacity: newActions.includes(a) ? 1 : 0.35, background: newActions.includes(a) ? 'var(--accent-light)' : '#f5f5f5', border: '1px solid ' + (newActions.includes(a) ? 'var(--accent)' : '#e8e8e8') }}
                  onClick={() => toggleNewAction(a)}
                >{a}</button>
              ))}
            </div>
            <button style={addBtn} disabled={!newSubjectId.trim() || newActions.length === 0} onClick={addEntry}>
              <Plus size={13} /> Add
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

const overlay: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 1000, display: 'flex', justifyContent: 'flex-end' }
const panel: React.CSSProperties = { width: 420, maxWidth: '100vw', background: '#fff', height: '100%', display: 'flex', flexDirection: 'column', boxShadow: '-4px 0 24px rgba(0,0,0,0.12)' }
const header: React.CSSProperties = { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', padding: '20px 20px 16px', borderBottom: '1px solid #e8e8e8' }
const title: React.CSSProperties = { fontSize: 16, fontWeight: 700, color: '#111' }
const subtitle: React.CSSProperties = { fontSize: 13, color: '#888', marginTop: 2 }
const closeBtn: React.CSSProperties = { background: 'none', border: 'none', cursor: 'pointer', color: '#888', padding: 4 }
const body: React.CSSProperties = { flex: 1, overflowY: 'auto', padding: 20 }
const entryRow: React.CSSProperties = { display: 'flex', alignItems: 'center', gap: 10, padding: '10px 0', borderBottom: '1px solid #f0f0f0' }
const entryLeft: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 2, minWidth: 100 }
const entryActions: React.CSSProperties = { flex: 1, display: 'flex', flexWrap: 'wrap', gap: 4 }
const badge: React.CSSProperties = { fontSize: 9, fontWeight: 700, background: '#f0f0f0', color: '#888', borderRadius: 3, padding: '1px 5px', letterSpacing: '0.05em', textTransform: 'uppercase' }
const subjectName: React.CSSProperties = { fontSize: 12, color: '#333', fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }
const actionChip: React.CSSProperties = { fontSize: 11, borderRadius: 3, padding: '2px 7px', background: '#f5f5f5', border: '1px solid #e8e8e8', color: '#555' }
const removeBtn: React.CSSProperties = { background: 'none', border: 'none', cursor: 'pointer', color: '#e53935', padding: 4, flexShrink: 0 }
const addSection: React.CSSProperties = { marginTop: 20, padding: '16px', background: '#fafafa', borderRadius: 6, border: '1px solid #e8e8e8' }
const selectStyle: React.CSSProperties = { padding: '6px 8px', border: '1px solid #ddd', borderRadius: 4, fontSize: 13, background: '#fff' }
const inputStyle: React.CSSProperties = { flex: 1, minWidth: 120, padding: '6px 10px', border: '1px solid #ddd', borderRadius: 4, fontSize: 13 }
const addBtn: React.CSSProperties = { display: 'flex', alignItems: 'center', gap: 5, padding: '6px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' }
```

**Step 2: Wire PermissionsPanel into HomePage**

Add a `⋯` context button per item (folders and notebooks) that opens the panel. In `HomePage.tsx`, add:

```tsx
import { PermissionsPanel } from '../components/PermissionsPanel'

// State:
const [permTarget, setPermTarget] = useState<{ type: string; id: string; name: string } | null>(null)

// In JSX, after the sections:
{permTarget && (
  <PermissionsPanel
    resourceType={permTarget.type}
    resourceId={permTarget.id}
    resourceName={permTarget.name}
    onClose={() => setPermTarget(null)}
  />
)}
```

Add a `⋯` button to `FolderItem` and notebook rows that calls `onPermissions`:

```tsx
// FolderItem props:
onPermissions?: () => void

// In FolderItem JSX, after the delete button:
{onPermissions && (
  <button style={styles.menuBtn} onClick={(e) => { e.stopPropagation(); onPermissions() }} title="Permissions">
    <MoreHorizontal size={14} />
  </button>
)}
```

Wire it in the render: `onPermissions={() => setPermTarget({ type: 'folder', id: f.id, name: f.name })}`.

Similarly for notebook rows: add a `⋯` button that opens `setPermTarget({ type: 'notebook', id: nb.id, name: nb.title })`.

**Step 3: Verify compilation**

Run: `cd web && npx tsc --noEmit 2>&1 | head -20`
Expected: No errors.

**Step 4: Commit**

```bash
git add web/src/components/PermissionsPanel.tsx web/src/pages/HomePage.tsx
git commit -m "feat: PermissionsPanel slide-over for per-resource ACL management"
```

---

## Task 14: Groups Page + Route + Sidebar Link

**Files:**
- Create: `web/src/pages/GroupsPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Sidebar.tsx`

**Step 1: Write GroupsPage.tsx**

```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Group, GroupMember } from '../types'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { useAuth } from '../hooks/useAuth'
import { ChevronDown, ChevronRight, UserPlus, Trash2, Plus } from 'lucide-react'

export function GroupsPage() {
  useEffect(() => { document.title = "Groups — hnb" }, [])
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const qc = useQueryClient()

  const [expandedGroup, setExpandedGroup] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [newGroupName, setNewGroupName] = useState('')
  const [newMemberID, setNewMemberID] = useState<Record<string, string>>({})

  const { data: groups = [] } = useQuery<Group[]>({
    queryKey: ['groups'],
    queryFn: () => api.get<Group[]>('/api/v1/groups'),
  })

  const { data: members = [] } = useQuery<GroupMember[]>({
    queryKey: ['group-members', expandedGroup],
    queryFn: () => api.get<GroupMember[]>(`/api/v1/groups/${expandedGroup}/members`),
    enabled: !!expandedGroup,
  })

  const createGroup = useMutation({
    mutationFn: (name: string) => api.post<Group>('/api/v1/groups', { name }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); setCreating(false); setNewGroupName('') },
  })

  const deleteGroup = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/groups/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['groups'] }),
  })

  const addMember = useMutation({
    mutationFn: ({ groupID, userID }: { groupID: string; userID: string }) =>
      api.post(`/api/v1/groups/${groupID}/members`, { user_id: userID }),
    onSuccess: (_, { groupID }) => { qc.invalidateQueries({ queryKey: ['group-members', groupID] }); qc.invalidateQueries({ queryKey: ['groups'] }) },
  })

  const removeMember = useMutation({
    mutationFn: ({ groupID, userID }: { groupID: string; userID: string }) =>
      api.delete(`/api/v1/groups/${groupID}/members/${userID}`),
    onSuccess: (_, { groupID }) => { qc.invalidateQueries({ queryKey: ['group-members', groupID] }); qc.invalidateQueries({ queryKey: ['groups'] }) },
  })

  return (
    <AppShell>
      <div style={{ maxWidth: 800, margin: '0 auto' }}>
        <SectionHeader title="Groups" subtitle={`${groups.length} group${groups.length !== 1 ? 's' : ''}`}>
          {isAdmin && (
            <button style={styles.newBtn} onClick={() => setCreating(true)}>+ New Group</button>
          )}
        </SectionHeader>

        {creating && (
          <div style={styles.createForm}>
            <input
              style={styles.input}
              value={newGroupName}
              onChange={(e) => setNewGroupName(e.target.value)}
              placeholder="Group name…"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter' && newGroupName.trim()) createGroup.mutate(newGroupName.trim())
                if (e.key === 'Escape') { setCreating(false); setNewGroupName('') }
              }}
            />
            <button style={styles.createBtn} disabled={!newGroupName.trim()} onClick={() => createGroup.mutate(newGroupName.trim())}>Create</button>
            <button style={styles.cancelBtn} onClick={() => { setCreating(false); setNewGroupName('') }}>Cancel</button>
          </div>
        )}

        <div style={styles.list}>
          {groups.map((g) => {
            const expanded = expandedGroup === g.id
            return (
              <div key={g.id} style={styles.groupCard}>
                <div style={styles.groupHeader}>
                  <button style={styles.expandBtn} onClick={() => setExpandedGroup(expanded ? null : g.id)}>
                    {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                    <span style={styles.groupName}>{g.name}</span>
                    <span style={styles.memberCount}>{g.member_count} member{g.member_count !== 1 ? 's' : ''}</span>
                  </button>
                  {isAdmin && (
                    <button style={styles.delBtn} onClick={() => deleteGroup.mutate(g.id)}>
                      <Trash2 size={13} />
                    </button>
                  )}
                </div>

                {expanded && (
                  <div style={styles.memberList}>
                    {members.map((m) => (
                      <div key={m.user_id} style={styles.memberRow}>
                        <span style={styles.memberName}>{m.name}</span>
                        <span style={styles.memberEmail}>{m.email}</span>
                        {isAdmin && (
                          <button style={styles.removeBtn} onClick={() => removeMember.mutate({ groupID: g.id, userID: m.user_id })}>
                            <Trash2 size={12} />
                          </button>
                        )}
                      </div>
                    ))}
                    {isAdmin && (
                      <div style={styles.addMemberRow}>
                        <input
                          style={styles.addInput}
                          value={newMemberID[g.id] ?? ''}
                          onChange={(e) => setNewMemberID((p) => ({ ...p, [g.id]: e.target.value }))}
                          placeholder="User UUID…"
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' && (newMemberID[g.id] ?? '').trim()) {
                              addMember.mutate({ groupID: g.id, userID: (newMemberID[g.id] ?? '').trim() })
                              setNewMemberID((p) => ({ ...p, [g.id]: '' }))
                            }
                          }}
                        />
                        <button style={styles.addBtn}
                          disabled={!(newMemberID[g.id] ?? '').trim()}
                          onClick={() => {
                            addMember.mutate({ groupID: g.id, userID: (newMemberID[g.id] ?? '').trim() })
                            setNewMemberID((p) => ({ ...p, [g.id]: '' }))
                          }}>
                          <Plus size={13} /> Add
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  newBtn: { padding: '7px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  createForm: { display: 'flex', gap: 10, marginBottom: 20, padding: 16, background: '#fff', borderRadius: 4, border: '1px solid #e8e8e8' },
  input: { flex: 1, padding: '8px 12px', border: '1px solid #ddd', borderRadius: 4, fontSize: 14 },
  createBtn: { padding: '7px 14px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '7px 14px', border: '1px solid #ddd', borderRadius: 4, background: 'none', fontSize: 13, cursor: 'pointer', color: '#555' },
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  groupCard: { background: '#fff', border: '1px solid #e8e8e8', borderRadius: 4, overflow: 'hidden' },
  groupHeader: { display: 'flex', alignItems: 'center', padding: '0 12px 0 0' },
  expandBtn: { flex: 1, display: 'flex', alignItems: 'center', gap: 8, padding: '12px 14px', background: 'none', border: 'none', cursor: 'pointer', textAlign: 'left' },
  groupName: { fontSize: 14, fontWeight: 600, color: '#111' },
  memberCount: { fontSize: 12, color: '#aaa', marginLeft: 8 },
  delBtn: { background: 'none', border: 'none', cursor: 'pointer', color: '#e53935', padding: 6 },
  memberList: { padding: '4px 16px 12px', borderTop: '1px solid #f0f0f0' },
  memberRow: { display: 'flex', alignItems: 'center', gap: 10, padding: '8px 0', borderBottom: '1px solid #f8f8f8' },
  memberName: { fontSize: 13, fontWeight: 500, color: '#222', width: 140, flexShrink: 0 },
  memberEmail: { fontSize: 12, color: '#888', flex: 1 },
  removeBtn: { background: 'none', border: 'none', cursor: 'pointer', color: '#e53935', padding: 4 },
  addMemberRow: { display: 'flex', gap: 8, marginTop: 10 },
  addInput: { flex: 1, padding: '6px 10px', border: '1px solid #ddd', borderRadius: 4, fontSize: 13 },
  addBtn: { display: 'flex', alignItems: 'center', gap: 5, padding: '6px 12px', background: '#111', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, cursor: 'pointer' },
}
```

**Step 2: Add route in App.tsx**

Import `GroupsPage` and add the route after the `/members` route:

```tsx
import { GroupsPage } from './pages/GroupsPage'

// In AppRoutes:
<Route path="/groups" element={<ProtectedRoute><GroupsPage /></ProtectedRoute>} />
```

**Step 3: Add sidebar link**

In `web/src/components/Sidebar.tsx`, add to `NAV_ITEMS`:

```tsx
import { BookOpen, LayoutDashboard, Database, Users, ClipboardList, User, UsersRound } from 'lucide-react'

// Add after Members:
{ to: '/groups', title: 'Groups', icon: <UsersRound size={16} /> },
```

(If `UsersRound` is not available in the installed version of lucide-react, use `Users2` or `Users` instead. Check with: `grep -r "from 'lucide-react'" web/src/components/Sidebar.tsx` to see what's imported.)

**Step 4: Verify compilation and run all Go tests**

Run: `cd web && npx tsc --noEmit 2>&1 | head -20`
Run: `task test:api 2>&1 | tail -10`
Expected: Both pass cleanly.

**Step 5: Commit**

```bash
git add web/src/pages/GroupsPage.tsx web/src/App.tsx web/src/components/Sidebar.tsx
git commit -m "feat: groups page with member management + route + sidebar link"
```

---

## Final Verification

Run all backend tests: `task test:api 2>&1 | tail -20`
Expected: All tests PASS with `ok internal/api`.

Run TypeScript check: `cd web && npx tsc --noEmit`
Expected: No errors.

Build backend: `task build`
Expected: Builds cleanly.

Confirm new features work end-to-end:
1. Register a new user → home folder should appear at `/` (Files page)
2. Create a folder → appears in grid
3. Click folder → breadcrumb updates, folder contents shown
4. Create a notebook inside folder → notebook appears in folder contents
5. `⋯` on folder → PermissionsPanel opens, shows ACL entries
6. Add an ACL entry → saved and fetched back
7. Navigate to `/groups` → Groups page loads

---

## Summary of Files Changed

**New migrations:**
- `internal/database/migrations/009_folders.sql`
- `internal/database/migrations/010_groups_acl.sql`

**New Go files:**
- `internal/models/folder.go`
- `internal/models/group.go`
- `internal/models/acl.go`
- `internal/api/permissions.go`
- `internal/api/folder_handlers.go`
- `internal/api/group_handlers.go`
- `internal/api/acl_handlers.go`

**Modified Go files:**
- `internal/api/router.go` — register new routes
- `internal/api/auth_handlers.go` — home folder on register, createHomeFolder helper
- `internal/api/org_handlers.go` — home folder on org create/join
- `internal/models/notebook.go`, `connector.go`, `dashboard.go` — add FolderID
- `internal/api/notebook_handlers.go`, `connector_handlers.go`, `dashboard_handlers.go` — folder_id CRUD

**New test files:**
- `internal/api/permissions_test.go`
- `internal/api/folder_handlers_test.go`
- `internal/api/group_handlers_test.go`
- `internal/api/acl_handlers_test.go`

**New frontend files:**
- `web/src/pages/GroupsPage.tsx`
- `web/src/components/PermissionsPanel.tsx`

**Modified frontend files:**
- `web/src/types/index.ts`
- `web/src/pages/HomePage.tsx`
- `web/src/App.tsx`
- `web/src/components/Sidebar.tsx`
