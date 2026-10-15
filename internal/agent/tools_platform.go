package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// searchResourcesDefaultLimit mirrors the HTTP endpoint's per-type default
// page size (internal/api.searchDefaultLimit). The engine's shared normalizer
// owns the remaining limit rules — reject < 1, clamp > 20 — so the handler
// never reshapes a caller-supplied limit.
const searchResourcesDefaultLimit = 5

// searchResourceTypes is the searchable resource-type enum advertised in the
// search_resources tool schema (and the value set a model may pass for
// `types`). It must stay in step with the API's searchSupportedTypes; the
// internal/api drift test TestSearchResourcesToolTypeEnumMatchesSupportedTypes
// pins the two definitions.
var searchResourceTypes = []string{
	"notebook", "dashboard", "folder", "connector", "agent",
	"model_config", "skill", "tool", "mcp_server",
}

// searchResourceTypesEnum is searchResourceTypes as a JSON array for the tool
// schema, derived so the schema string cannot drift from the slice.
var searchResourceTypesEnum = func() string {
	encoded, _ := json.Marshal(searchResourceTypes)
	return string(encoded)
}()

func RegisterPlatformTools(reg *ToolRegistry, db *pgxpool.Pool) {
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_notebooks",
			Description: "List notebooks the user can access in the organization. Returns name, description, folder, and timestamps.",
			Parameters:  `{"type":"object","properties":{"folder_id":{"type":"string","description":"Filter by parent folder ID"},"search":{"type":"string","description":"Filter by name (case-insensitive)"}}}`,
		},
		Handler: makeListNotebooksHandler(db),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_connectors",
			Description: "List database connectors the user can access. Returns name, type, and folder info.",
			Parameters:  `{"type":"object","properties":{"search":{"type":"string","description":"Filter by name (case-insensitive)"}}}`,
		},
		Handler: makeListConnectorsHandler(db),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_folders",
			Description: "List folders in the organization. Returns child folders of the given parent (or root folders if no parent specified).",
			Parameters:  `{"type":"object","properties":{"parent_id":{"type":"string","description":"Parent folder ID. Omit to list root folders."}}}`,
		},
		Handler: makeListFoldersHandler(db),
		Timeout: 15 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "get_folder_tree",
			Description: "Get the full folder hierarchy for the organization as a flat list with depth and path information.",
			Parameters:  `{"type":"object","properties":{}}`,
		},
		Handler: makeGetFolderTreeHandler(db),
		Timeout: 30 * time.Second,
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "search_resources",
			Description: "Search the org's resources (notebooks, dashboards, folders, connectors, agents, models, skills, tools, MCP servers) by title and content. Returns grouped results with snippets. Refine the query instead of paging.",
			Parameters: fmt.Sprintf(
				`{"type":"object","properties":{"query":{"type":"string","description":"Search text (title/name and content)"},"types":{"type":"array","items":{"type":"string","enum":%s},"description":"Optional resource types to restrict to"},"limit":{"type":"integer","minimum":1,"maximum":20,"description":"Max results per type (default 5)"}},"required":["query"]}`,
				searchResourceTypesEnum),
		},
		Handler: makeSearchResourcesHandler(),
		Timeout: 20 * time.Second,
	})
}

// makeSearchResourcesHandler builds the search_resources tool handler. The
// search itself runs through ToolContext.SearchResourcesFunc — wired by the
// API server to the same leak-safe core GET /api/v1/search uses — so the
// handler only parses the arguments and passes the limit through untouched:
// the engine's shared normalizer applies the HTTP rules (a limit below 1 is
// rejected, above 20 is clamped), and clamping 0 up to 1 here would silently
// turn a caller error into a default page. A bare context without the engine
// fails closed rather than searching with no ACL filtering.
func makeSearchResourcesHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Query string   `json:"query"`
			Types []string `json:"types"`
			Limit *int     `json:"limit"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		query := strings.TrimSpace(req.Query)
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		if ctx.SearchResourcesFunc == nil {
			// Fail closed: without the shared search engine there is no way
			// to apply the caller's ACLs or the dashboard leak rules.
			return nil, fmt.Errorf("search resources: no search engine is wired in this context")
		}
		limit := searchResourcesDefaultLimit
		if req.Limit != nil {
			limit = *req.Limit
		}
		return ctx.SearchResourcesFunc(ctx.Context, ctx.UserID, ctx.OrgID, ctx.OrgRole, query, req.Types, limit)
	}
}

func makeListNotebooksHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			FolderID string `json:"folder_id"`
			Search   string `json:"search"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		query := `SELECT n.id, n.title, COALESCE(n.description, ''), n.folder_id, n.created_at
			FROM notebooks n WHERE n.org_id = $1`
		params := []any{ctx.OrgID}
		argIdx := 2

		if req.FolderID != "" {
			query += fmt.Sprintf(` AND n.folder_id = $%d`, argIdx)
			params = append(params, req.FolderID)
			argIdx++
		}
		if req.Search != "" {
			query += fmt.Sprintf(` AND n.title ILIKE '%%' || $%d || '%%'`, argIdx)
			params = append(params, req.Search)
			argIdx++
		}
		query += ` ORDER BY n.updated_at DESC LIMIT 50`

		rows, err := db.Query(ctx.Context, query, params...)
		if err != nil {
			return nil, fmt.Errorf("list notebooks: %w", err)
		}
		defer rows.Close()

		var notebooks []map[string]any
		for rows.Next() {
			var id, name, desc string
			var fID *string
			var created time.Time
			if err := rows.Scan(&id, &name, &desc, &fID, &created); err != nil {
				return nil, fmt.Errorf("scan notebook: %w", err)
			}
			folderID := ""
			if fID != nil {
				folderID = *fID
			}
			notebooks = append(notebooks, map[string]any{
				"id": id, "name": name, "description": desc,
				"folder_id": folderID, "created_at": created.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list notebooks iter: %w", err)
		}
		return map[string]any{"notebooks": notebooks, "count": len(notebooks)}, nil
	}
}

func makeListConnectorsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Search string `json:"search"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		query := `SELECT id, name, type, COALESCE(folder_id::text, ''), created_at
			FROM connectors WHERE org_id = $1`
		params := []any{ctx.OrgID}
		argIdx := 2

		if req.Search != "" {
			query += fmt.Sprintf(` AND name ILIKE '%%' || $%d || '%%'`, argIdx)
			params = append(params, req.Search)
			argIdx++
		}
		query += ` ORDER BY name ASC LIMIT 50`

		rows, err := db.Query(ctx.Context, query, params...)
		if err != nil {
			return nil, fmt.Errorf("list connectors: %w", err)
		}
		defer rows.Close()

		var connectors []map[string]any
		for rows.Next() {
			var id, name, ctype, folderID string
			var created time.Time
			if err := rows.Scan(&id, &name, &ctype, &folderID, &created); err != nil {
				return nil, fmt.Errorf("scan connector: %w", err)
			}
			connectors = append(connectors, map[string]any{
				"id": id, "name": name, "type": ctype,
				"folder_id": folderID, "created_at": created.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list connectors iter: %w", err)
		}
		return map[string]any{"connectors": connectors, "count": len(connectors)}, nil
	}
}

func makeListFoldersHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ParentID string `json:"parent_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		query := `SELECT id, name, parent_id, created_at FROM folders WHERE org_id = $1`
		params := []any{ctx.OrgID}

		if req.ParentID != "" {
			query += ` AND parent_id = $2`
			params = append(params, req.ParentID)
		} else {
			query += ` AND parent_id IS NULL`
		}
		query += ` ORDER BY name ASC`

		rows, err := db.Query(ctx.Context, query, params...)
		if err != nil {
			return nil, fmt.Errorf("list folders: %w", err)
		}
		defer rows.Close()

		var folders []map[string]any
		for rows.Next() {
			var id, name, created string
			var parentID *string
			if err := rows.Scan(&id, &name, &parentID, &created); err != nil {
				return nil, fmt.Errorf("scan folder: %w", err)
			}
			pid := ""
			if parentID != nil {
				pid = *parentID
			}
			folders = append(folders, map[string]any{
				"id": id, "name": name, "parent_id": pid, "created_at": created,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list folders iter: %w", err)
		}
		return map[string]any{"folders": folders, "count": len(folders)}, nil
	}
}

func makeGetFolderTreeHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		rows, err := db.Query(ctx.Context, `
			WITH RECURSIVE tree AS (
				SELECT id, name, parent_id, 0 as depth, name as path
				FROM folders WHERE org_id = $1 AND parent_id IS NULL
				UNION ALL
				SELECT f.id, f.name, f.parent_id, t.depth + 1, t.path || ' / ' || f.name
				FROM folders f JOIN tree t ON f.parent_id = t.id
			)
			SELECT id, name, parent_id, depth, path FROM tree ORDER BY path
		`, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("get folder tree: %w", err)
		}
		defer rows.Close()

		var folders []map[string]any
		for rows.Next() {
			var id, name, path string
			var parentID *string
			var depth int
			if err := rows.Scan(&id, &name, &parentID, &depth, &path); err != nil {
				return nil, fmt.Errorf("scan folder tree: %w", err)
			}
			pid := ""
			if parentID != nil {
				pid = *parentID
			}
			folders = append(folders, map[string]any{
				"id": id, "name": name, "parent_id": pid, "depth": depth, "path": path,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get folder tree iter: %w", err)
		}
		return map[string]any{"folders": folders}, nil
	}
}
