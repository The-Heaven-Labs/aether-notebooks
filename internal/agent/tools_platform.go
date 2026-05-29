package agent

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	})
}

func makeListNotebooksHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			FolderID string `json:"folder_id"`
			Search   string `json:"search"`
		}
		json.Unmarshal(args, &req)

		query := `SELECT n.id, n.name, COALESCE(n.description, ''), n.folder_id, n.created_at
			FROM notebooks n WHERE n.org_id = $1`
		params := []any{ctx.OrgID}
		argIdx := 2

		if req.FolderID != "" {
			query += fmt.Sprintf(` AND n.folder_id = $%d`, argIdx)
			params = append(params, req.FolderID)
			argIdx++
		}
		if req.Search != "" {
			query += fmt.Sprintf(` AND n.name ILIKE '%%' || $%d || '%%'`, argIdx)
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
			var id, name, desc, created string
			var fID *string
			rows.Scan(&id, &name, &desc, &fID, &created)
			folderID := ""
			if fID != nil {
				folderID = *fID
			}
			notebooks = append(notebooks, map[string]any{
				"id": id, "name": name, "description": desc,
				"folder_id": folderID, "created_at": created,
			})
		}
		return map[string]any{"notebooks": notebooks, "count": len(notebooks)}, nil
	}
}

func makeListConnectorsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Search string `json:"search"`
		}
		json.Unmarshal(args, &req)

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
			var id, name, ctype, folderID, created string
			rows.Scan(&id, &name, &ctype, &folderID, &created)
			connectors = append(connectors, map[string]any{
				"id": id, "name": name, "type": ctype,
				"folder_id": folderID, "created_at": created,
			})
		}
		return map[string]any{"connectors": connectors, "count": len(connectors)}, nil
	}
}

func makeListFoldersHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ParentID string `json:"parent_id"`
		}
		json.Unmarshal(args, &req)

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
			rows.Scan(&id, &name, &parentID, &created)
			pid := ""
			if parentID != nil {
				pid = *parentID
			}
			folders = append(folders, map[string]any{
				"id": id, "name": name, "parent_id": pid, "created_at": created,
			})
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
			rows.Scan(&id, &name, &parentID, &depth, &path)
			pid := ""
			if parentID != nil {
				pid = *parentID
			}
			folders = append(folders, map[string]any{
				"id": id, "name": name, "parent_id": pid, "depth": depth, "path": path,
			})
		}
		return map[string]any{"folders": folders}, nil
	}
}
