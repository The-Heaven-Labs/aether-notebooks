package api

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// resourceSummary is the shared shape for favorites and search results: one
// catalog resource the caller can see, with the metadata both surfaces render.
type resourceSummary struct {
	ID           string    `json:"id"`
	ResourceType string    `json:"resource_type"`
	Title        string    `json:"title"`
	Subtitle     string    `json:"subtitle"`
	Snippet      *string   `json:"snippet,omitempty"`
	FolderID     *string   `json:"folder_id,omitempty"`
	FolderPath   string    `json:"folder_path,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	IsFavorite   bool      `json:"is_favorite"`
	CanEdit      bool      `json:"can_edit"`
}

// summaryGroup is one resource type's results, as the favorites and search
// endpoints group them.
type summaryGroup struct {
	ResourceType string            `json:"resource_type"`
	Results      []resourceSummary `json:"results"`
}

// favoriteResourceTypeOrder is the preferred response group order for the
// favorites listing. Types not named here follow in sorted order.
var favoriteResourceTypeOrder = []string{
	"folder", "notebook", "dashboard", "connector", "agent",
	"model_config", "skill", "mcp_server", "tool",
}

// favoriteResourceTypes is the canonical favorites whitelist and group order,
// derived from resourceTable — the types resolveActions supports — plus
// "folder" (folders are absent from resourceTable because they have no
// folder_id column). Deriving from resourceTable means PUT validation cannot
// drift from the resolver: a future resource type becomes favoritable
// immediately and groups after the ordered entries until
// favoriteResourceTypeOrder names it.
var favoriteResourceTypes = func() []string {
	supported := map[string]bool{"folder": true}
	for resourceType := range resourceTable {
		supported[resourceType] = true
	}
	ordered := make([]string, 0, len(supported))
	for _, resourceType := range favoriteResourceTypeOrder {
		if supported[resourceType] {
			ordered = append(ordered, resourceType)
			delete(supported, resourceType)
		}
	}
	extras := make([]string, 0, len(supported))
	for resourceType := range supported {
		extras = append(extras, resourceType)
	}
	sort.Strings(extras)
	return append(ordered, extras...)
}()

// favoriteResourceTypeSet is favoriteResourceTypes as a lookup set.
var favoriteResourceTypeSet = func() map[string]bool {
	set := make(map[string]bool, len(favoriteResourceTypes))
	for _, t := range favoriteResourceTypes {
		set[t] = true
	}
	return set
}()

// softDeletableResourceTables are the resource tables carrying a deleted_at
// column (V072). Favorites are live-resources-only: a row in one of these
// tables with deleted_at set is treated as missing, while ACL rows on it may
// still resolve (resolveActions does not filter trashed resources).
var softDeletableResourceTables = map[string]bool{
	"notebooks":  true,
	"connectors": true,
	"dashboards": true,
	"folders":    true,
}

// resourceSummarySubtitles are the per-type descriptors that do not depend on
// the row itself. Connectors use their type instead; mcp servers their fixed
// label.
var resourceSummarySubtitles = map[string]string{
	"notebook":     "Notebook",
	"dashboard":    "Dashboard",
	"folder":       "Folder",
	"agent":        "Agent",
	"model_config": "Model",
	"skill":        "Skill",
	"tool":         "Tool",
	"mcp_server":   "MCP server",
}

// resourceSummaryMeta is one resource's metadata, fetched before permissions
// are applied.
type resourceSummaryMeta struct {
	Title     string
	Subtitle  string
	FolderID  *string
	UpdatedAt time.Time
}

// summaryRequest is one resource type's candidate ids, in the caller's desired
// result order. buildResourceSummaries preserves both the request order (group
// order) and each request's id order (result order within a group).
type summaryRequest struct {
	ResourceType string
	IDs          []string
}

// summaryOverrides lets a caller substitute metadata it already fetched with
// extra per-caller semantics (search resolves effective dashboard titles per
// viewer). A type present in the map is not re-fetched from its table, and ids
// absent from the override group are treated exactly like a table miss:
// omitted from the output.
type summaryOverrides map[string]map[string]resourceSummaryMeta

// loadFavoriteIDs returns the caller's favorited resource ids per type. One
// query; shared by the summary builder's is_favorite flag and search's
// favorites filter.
func (s *Server) loadFavoriteIDs(ctx context.Context, userID, orgID string) (map[string]map[string]bool, error) {
	sets := make(map[string]map[string]bool)
	rows, err := s.db.Pool.Query(ctx,
		`SELECT resource_type, resource_id::text FROM favorites WHERE user_id = $1 AND org_id = $2`,
		userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("load favorites: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var resourceType, resourceID string
		if err := rows.Scan(&resourceType, &resourceID); err != nil {
			return nil, fmt.Errorf("scan favorite: %w", err)
		}
		if sets[resourceType] == nil {
			sets[resourceType] = make(map[string]bool)
		}
		sets[resourceType][resourceID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorite rows: %w", err)
	}
	return sets, nil
}

// folderPathsFor resolves each folder id to its display path: the ancestor
// chain, root first, joined with " / " (e.g. "Home / Finance"). A root-level
// folder resolves to its own name. Ids not in the org are absent from the
// map. Empty input returns an empty map.
func (s *Server) folderPathsFor(ctx context.Context, orgID string, folderIDs []string) (map[string]string, error) {
	paths := make(map[string]string, len(folderIDs))
	if len(folderIDs) == 0 {
		return paths, nil
	}
	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE ancestors(seed_id, id, name, parent_id, depth) AS (
			SELECT id, id, name, parent_id, 0 FROM folders
			WHERE id = ANY($1::uuid[]) AND org_id = $2
			UNION ALL
			SELECT a.seed_id, f.id, f.name, f.parent_id, a.depth + 1
			FROM folders f JOIN ancestors a ON f.id = a.parent_id
			WHERE f.org_id = $2
		)
		SELECT seed_id::text, name FROM ancestors ORDER BY seed_id, depth DESC
	`, folderIDs, orgID)
	if err != nil {
		return nil, fmt.Errorf("folder paths: %w", err)
	}
	defer rows.Close()
	// Rows arrive root-first (depth DESC) per seed, so appending yields the
	// root-to-leaf path.
	for rows.Next() {
		var seedID, name string
		if err := rows.Scan(&seedID, &name); err != nil {
			return nil, fmt.Errorf("scan folder path: %w", err)
		}
		if current, ok := paths[seedID]; ok {
			paths[seedID] = current + " / " + name
		} else {
			paths[seedID] = name
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("folder path rows: %w", err)
	}
	return paths, nil
}

// resourceLive reports whether the resource exists in the org and is not
// soft-deleted. Favorites are live-resources-only: a trashed notebook must not
// be favoritable even though its ACL rows (and therefore resolveActions' view)
// survive the trash.
func (s *Server) resourceLive(ctx context.Context, orgID, resourceType, resourceID string) (bool, error) {
	table, ok := resourceTable[resourceType]
	if resourceType == "folder" {
		table, ok = "folders", true
	}
	if !ok {
		return false, fmt.Errorf("unknown resource type %q", resourceType)
	}
	query := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1 AND org_id = $2)`, table)
	if softDeletableResourceTables[table] {
		query = fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL)`, table)
	}
	var exists bool
	if err := s.db.Pool.QueryRow(ctx, query, resourceID, orgID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check %s liveness: %w", resourceType, err)
	}
	return exists, nil
}

// fetchResourceSummaryMeta loads one type's metadata in one org-scoped query.
// Soft-deleted rows are filtered out, so a trashed resource is simply absent
// from the result — the live-only rule. Unknown types fail with an error.
func (s *Server) fetchResourceSummaryMeta(ctx context.Context, orgID, resourceType string, ids []string) (map[string]resourceSummaryMeta, error) {
	meta := make(map[string]resourceSummaryMeta, len(ids))
	if len(ids) == 0 {
		return meta, nil
	}
	var query string
	switch resourceType {
	case "notebook":
		query = `SELECT id::text, title, folder_id, updated_at FROM notebooks
			WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`
	case "dashboard":
		query = `SELECT id::text, title, folder_id, updated_at FROM dashboards
			WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`
	case "folder":
		query = `SELECT id::text, name, parent_id, updated_at FROM folders
			WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`
	case "connector":
		query = `SELECT id::text, name, folder_id, type, updated_at FROM connectors
			WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`
	case "agent":
		query = `SELECT id::text, name, folder_id, updated_at FROM agents
			WHERE id = ANY($1::uuid[]) AND org_id = $2`
	case "model_config":
		query = `SELECT id::text, name, folder_id, updated_at FROM model_configs
			WHERE id = ANY($1::uuid[]) AND org_id = $2`
	case "skill":
		query = `SELECT id::text, name, folder_id, updated_at FROM skills
			WHERE id = ANY($1::uuid[]) AND org_id = $2`
	case "tool":
		query = `SELECT id::text, name, folder_id, updated_at FROM tools
			WHERE id = ANY($1::uuid[]) AND org_id = $2`
	case "mcp_server":
		// mcp_servers has no folder_id column.
		query = `SELECT id::text, name, updated_at FROM mcp_servers
			WHERE id = ANY($1::uuid[]) AND org_id = $2`
	default:
		return nil, fmt.Errorf("unknown resource type %q", resourceType)
	}
	rows, err := s.db.Pool.Query(ctx, query, ids, orgID)
	if err != nil {
		return nil, fmt.Errorf("load %s summary meta: %w", resourceType, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var m resourceSummaryMeta
		var folderID *string
		switch resourceType {
		case "connector":
			if err := rows.Scan(&id, &m.Title, &folderID, &m.Subtitle, &m.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan %s summary meta: %w", resourceType, err)
			}
		case "mcp_server":
			m.Subtitle = resourceSummarySubtitles[resourceType]
			if err := rows.Scan(&id, &m.Title, &m.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan %s summary meta: %w", resourceType, err)
			}
		default:
			m.Subtitle = resourceSummarySubtitles[resourceType]
			if err := rows.Scan(&id, &m.Title, &folderID, &m.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan %s summary meta: %w", resourceType, err)
			}
		}
		m.FolderID = folderID
		meta[id] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s summary meta: %w", resourceType, err)
	}
	return meta, nil
}

// buildResourceSummaries assembles summary groups for the caller. Metadata is
// fetched in one query per type (or taken from overrides), visibility and
// can_edit resolve per type through one batched resolveActions call, folder
// paths resolve in one recursive query, and favorites load in one query.
// Requests are processed in order (group order) and each request's ids keep
// their order (result order); ids the caller cannot view, ids that no longer
// exist, and soft-deleted resources are omitted, so a group whose ids all fail
// disappears entirely.
//
// snippets maps resource type -> id -> snippet text (nil = no snippets, as in
// a favorites listing). overrides lets a caller substitute metadata it already
// fetched with extra per-caller semantics (search resolves effective dashboard
// titles per viewer); a type present there is not re-fetched, and ids absent
// from the override group are omitted like any other miss.
func (s *Server) buildResourceSummaries(ctx context.Context, ident permIdentity, requests []summaryRequest, snippets map[string]map[string]string, overrides summaryOverrides) ([]summaryGroup, error) {
	// Ordered unique types with the union of their ids in caller order. A
	// repeated type merges into one group; the first occurrence fixes the
	// group position and the id order.
	var types []string
	idsByType := make(map[string][]string, len(requests))
	seenIDs := make(map[string]map[string]bool, len(requests))
	for _, req := range requests {
		if _, ok := idsByType[req.ResourceType]; !ok {
			types = append(types, req.ResourceType)
			idsByType[req.ResourceType] = nil
			seenIDs[req.ResourceType] = make(map[string]bool)
		}
		for _, id := range req.IDs {
			if seenIDs[req.ResourceType][id] {
				continue
			}
			seenIDs[req.ResourceType][id] = true
			idsByType[req.ResourceType] = append(idsByType[req.ResourceType], id)
		}
	}

	// 1. Metadata per type: one query each, or the caller's override.
	metaByType := make(map[string]map[string]resourceSummaryMeta, len(types))
	for _, resourceType := range types {
		if len(idsByType[resourceType]) == 0 {
			continue
		}
		if override, ok := overrides[resourceType]; ok {
			metaByType[resourceType] = override
			continue
		}
		meta, err := s.fetchResourceSummaryMeta(ctx, ident.OrgID, resourceType, idsByType[resourceType])
		if err != nil {
			return nil, err
		}
		metaByType[resourceType] = meta
	}

	// 2. view/edit per type through the batched resolver, over the ids the
	//    metadata fetch actually found (a missing id is never probed).
	permsByType := make(map[string]map[string]map[string]bool, len(types))
	for _, resourceType := range types {
		meta := metaByType[resourceType]
		if len(meta) == 0 {
			continue
		}
		liveIDs := make([]string, 0, len(idsByType[resourceType]))
		for _, id := range idsByType[resourceType] {
			if _, ok := meta[id]; ok {
				liveIDs = append(liveIDs, id)
			}
		}
		if len(liveIDs) == 0 {
			continue
		}
		perms, err := s.resolveActions(ctx, ident, resourceType, liveIDs, []string{"view", "edit"})
		if err != nil {
			return nil, err
		}
		permsByType[resourceType] = perms
	}

	// 3. Folder paths for every distinct containing folder, one query.
	var folderIDs []string
	seenFolders := make(map[string]bool)
	for _, resourceType := range types {
		for _, id := range idsByType[resourceType] {
			m, ok := metaByType[resourceType][id]
			if !ok || m.FolderID == nil || seenFolders[*m.FolderID] {
				continue
			}
			seenFolders[*m.FolderID] = true
			folderIDs = append(folderIDs, *m.FolderID)
		}
	}
	folderPaths, err := s.folderPathsFor(ctx, ident.OrgID, folderIDs)
	if err != nil {
		return nil, err
	}

	// 4. The caller's favorites, one query.
	favorites, err := s.loadFavoriteIDs(ctx, ident.UserID, ident.OrgID)
	if err != nil {
		return nil, err
	}

	// 5. Assemble in request order, dropping invisible resources.
	var groups []summaryGroup
	for _, resourceType := range types {
		meta := metaByType[resourceType]
		perms := permsByType[resourceType]
		group := summaryGroup{ResourceType: resourceType, Results: []resourceSummary{}}
		for _, id := range idsByType[resourceType] {
			m, ok := meta[id]
			if !ok || !actionAllowed(perms, id, "view") {
				continue
			}
			summary := resourceSummary{
				ID:           id,
				ResourceType: resourceType,
				Title:        m.Title,
				Subtitle:     m.Subtitle,
				UpdatedAt:    m.UpdatedAt,
				IsFavorite:   favorites[resourceType][id],
				CanEdit:      actionAllowed(perms, id, "edit"),
			}
			if m.FolderID != nil {
				summary.FolderID = m.FolderID
				summary.FolderPath = folderPaths[*m.FolderID]
			}
			if text, ok := snippets[resourceType][id]; ok && text != "" {
				snippet := text
				summary.Snippet = &snippet
			}
			group.Results = append(group.Results, summary)
		}
		if len(group.Results) > 0 {
			groups = append(groups, group)
		}
	}
	return groups, nil
}
