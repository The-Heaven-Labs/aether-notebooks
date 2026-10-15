package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
)

// filterFoldersWithVisibleDescendantContent answers folderHasDescendantContent
// for many folders in one query: one recursive walk seeded with all requested
// folder ids, aggregated per seed. Callers use it for child folders that fail
// their own view check, to decide whether the folder is still worth showing
// because something inside it is visible. Admin mode is deliberately not
// considered: callers only probe folders that failed resolution, which cannot
// include an admin-mode-visible folder.
//
// Keep in sync with filterFoldersWithVisibleDescendantContentOrFolders: they
// deliberately differ (this one is content-only and seed-inclusive; the other
// is folder-aware and strictly-descendant), but they share the three content
// EXISTS branches (notebooks/connectors/dashboards), which must change in
// lockstep.
//
// The content predicates are lifted verbatim from folderHasDescendantContent:
// every seed's descendants (the seed itself included) are probed for
// notebooks, connectors, and dashboards whose folder_id matches and that carry
// an ACL entry for the caller's user id, one of their groups, or Everyone. As
// in the single-folder query, owner_id, deleted_at, and the granted actions
// are deliberately not consulted — an ACL row's subject match alone counts.
func (s *Server) filterFoldersWithVisibleDescendantContent(ctx context.Context, ident permIdentity, folderIDs []string) (map[string]bool, error) {
	// Dedupe the requested seeds: the result map keys dedupe anyway, but the
	// query should see each id once. Every requested id gets a false entry
	// before any probe runs, so the caller always sees a uniform shape.
	unique := make([]string, 0, len(folderIDs))
	byLower := make(map[string][]string, len(folderIDs))
	result := make(map[string]bool, len(folderIDs))
	for _, id := range folderIDs {
		if _, seen := result[id]; seen {
			continue
		}
		unique = append(unique, id)
		result[id] = false
		lower := strings.ToLower(id)
		byLower[lower] = append(byLower[lower], id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE descendants(seed_id, id, parent_id) AS (
			SELECT id, id, parent_id FROM folders WHERE id = ANY($1::uuid[])
			UNION ALL
			SELECT d.seed_id, f.id, f.parent_id
			FROM folders f
			JOIN descendants d ON f.parent_id = d.id
			WHERE f.org_id = $2
		)
		SELECT DISTINCT d.seed_id::text
		FROM descendants d
		WHERE
		EXISTS (
			SELECT 1 FROM notebooks nb
			WHERE nb.folder_id = d.id AND nb.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM connectors c
			WHERE c.folder_id = d.id AND c.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM dashboards d2
			WHERE d2.folder_id = d.id AND d2.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
	`, unique, ident.OrgID, ident.UserID, ident.GroupIDs)
	if err != nil {
		return nil, fmt.Errorf("descendant content query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seedID string
		if err := rows.Scan(&seedID); err != nil {
			return nil, fmt.Errorf("scan descendant content seed: %w", err)
		}
		// The query returns canonical lowercase uuid text while a requested id
		// may carry an uppercase spelling (path parameters); Postgres parses
		// both to the same uuid, so match case-insensitively to keep the
		// single-folder wrapper equivalent to its former query.
		for _, id := range byLower[strings.ToLower(seedID)] {
			result[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("descendant content rows: %w", err)
	}
	return result, nil
}

// folderHasDescendantContent checks if any notebook, connector, or dashboard
// directly in folderID or in any descendant folder has an ACL entry granting access to the user.
// It is a thin wrapper over filterFoldersWithVisibleDescendantContent so the
// single-folder and batched answers can never drift.
func (s *Server) folderHasDescendantContent(ctx context.Context, folderID, orgID, userID string, groupIDs []string) (bool, error) {
	got, err := s.filterFoldersWithVisibleDescendantContent(ctx, permIdentity{
		UserID:   userID,
		OrgID:    orgID,
		GroupIDs: groupIDs,
	}, []string{folderID})
	if err != nil {
		return false, err
	}
	return got[folderID], nil
}

// filterFoldersWithACLSubjectMatch answers, for many folders in one query,
// whether the caller is named by any ACL entry on the folder itself. It exists
// for the /home listing, whose former inline EXISTS matched ACL subjects
// without evaluating the entry's actions, the resolution order, or the folder
// ancestry — semantics deliberately wider than a view check — so this helper
// reproduces that predicate instead of calling resolveActions. The result
// carries an entry for every requested id (deduped); ids without a matching
// entry are false.
func (s *Server) filterFoldersWithACLSubjectMatch(ctx context.Context, folderIDs []string, userID string, groupIDs []string) (map[string]bool, error) {
	unique := make([]string, 0, len(folderIDs))
	byLower := make(map[string][]string, len(folderIDs))
	result := make(map[string]bool, len(folderIDs))
	for _, id := range folderIDs {
		if _, seen := result[id]; seen {
			continue
		}
		unique = append(unique, id)
		result[id] = false
		lower := strings.ToLower(id)
		byLower[lower] = append(byLower[lower], id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT resource_id::text
		FROM acl_entries
		WHERE resource_type = 'folder'
		  AND resource_id = ANY($1::uuid[])
		  AND (
			(subject_type = 'user' AND subject_id = $2::text)
			OR (subject_type = 'group' AND subject_id = ANY($3::text[]))
			OR (subject_type = 'org_role' AND subject_id = 'everyone')
		  )
	`, unique, userID, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("folder acl subject query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan folder acl subject: %w", err)
		}
		// The query returns canonical lowercase uuid text; match
		// case-insensitively so an uppercase requested spelling still lands.
		for _, requested := range byLower[strings.ToLower(id)] {
			result[requested] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("folder acl subject rows: %w", err)
	}
	return result, nil
}

// filterFoldersWithVisibleDescendantContentOrFolders is the folder-aware
// descendant probe used by the /home listing. For every requested seed it
// answers whether any STRICTLY DESCENDANT folder is visible to the caller: as a
// folder the caller owns or holds a folder ACL entry on (any actions), or as
// the home of a notebook, connector, or dashboard carrying a matching ACL
// entry. It reproduces the listing's former inline predicate exactly and is
// deliberately WIDER than filterFoldersWithVisibleDescendantContent, which is
// content-only and seed-inclusive: here a descendant folder counts even when
// nothing inside it is visible, so a folder whose only reachable descendant is
// a folder stem stays listed. The seed itself is never probed (the /home
// listing checks a direct ACL subject match separately). Admin mode is
// deliberately not considered: callers only probe folders that failed
// resolution, which cannot include an admin-mode-visible folder.
//
// Keep in sync with filterFoldersWithVisibleDescendantContent: they
// deliberately differ (this one is folder-aware and strictly-descendant; the
// other is content-only and seed-inclusive), but they share the three content
// EXISTS branches (notebooks/connectors/dashboards), which must change in
// lockstep.
func (s *Server) filterFoldersWithVisibleDescendantContentOrFolders(ctx context.Context, ident permIdentity, folderIDs []string) (map[string]bool, error) {
	unique := make([]string, 0, len(folderIDs))
	byLower := make(map[string][]string, len(folderIDs))
	result := make(map[string]bool, len(folderIDs))
	for _, id := range folderIDs {
		if _, seen := result[id]; seen {
			continue
		}
		unique = append(unique, id)
		result[id] = false
		lower := strings.ToLower(id)
		byLower[lower] = append(byLower[lower], id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE descendants(seed_id, id) AS (
			SELECT p.id, c.id
			FROM folders p
			JOIN folders c ON c.parent_id = p.id AND c.org_id = p.org_id
			WHERE p.id = ANY($1::uuid[]) AND p.org_id = $2
			UNION ALL
			SELECT d.seed_id, f.id
			FROM folders f
			JOIN descendants d ON f.parent_id = d.id
			WHERE f.org_id = $2
		)
		SELECT DISTINCT d.seed_id::text
		FROM descendants d
		WHERE
		EXISTS (
			SELECT 1 FROM folders f2
			WHERE f2.id = d.id
			AND (
				f2.owner_id = $3
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
		)
		OR EXISTS (
			SELECT 1 FROM notebooks nb
			WHERE nb.folder_id = d.id AND nb.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM connectors c
			WHERE c.folder_id = d.id AND c.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM dashboards d2
			WHERE d2.folder_id = d.id AND d2.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
	`, unique, ident.OrgID, ident.UserID, ident.GroupIDs)
	if err != nil {
		return nil, fmt.Errorf("folder-aware descendant query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seedID string
		if err := rows.Scan(&seedID); err != nil {
			return nil, fmt.Errorf("scan folder-aware descendant seed: %w", err)
		}
		for _, id := range byLower[strings.ToLower(seedID)] {
			result[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("folder-aware descendant rows: %w", err)
	}
	return result, nil
}

type folderNotebook struct {
	models.Notebook
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanShare  bool `json:"can_share"`
}

type folderDashboard struct {
	models.Dashboard
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanShare  bool `json:"can_share"`
}

type folderItemFolder struct {
	models.Folder
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanShare  bool `json:"can_share"`
}

type folderContents struct {
	Folder     *models.Folder     `json:"folder,omitempty"`
	Folders    []folderItemFolder `json:"folders"`
	Notebooks  []folderNotebook   `json:"notebooks"`
	Connectors []folderConnector  `json:"connectors"`
	Dashboards []folderDashboard  `json:"dashboards"`
	// Totals and HasMore always carry the notebooks/connectors/dashboards
	// keys: Totals is the exact number of visible rows in the section (not
	// just the page), HasMore says whether the first page left rows behind.
	Totals  map[string]int  `json:"totals"`
	HasMore map[string]bool `json:"has_more"`
}

// newFolderContents seeds a listing response with the always-present shape:
// empty arrays for every section and zeroed Totals/HasMore maps, so an empty
// folder still reports the full pagination contract.
func newFolderContents(folder *models.Folder) folderContents {
	return folderContents{
		Folder:     folder,
		Folders:    []folderItemFolder{},
		Notebooks:  []folderNotebook{},
		Connectors: []folderConnector{},
		Dashboards: []folderDashboard{},
		Totals:     map[string]int{"notebooks": 0, "connectors": 0, "dashboards": 0},
		HasMore:    map[string]bool{"notebooks": false, "connectors": false, "dashboards": false},
	}
}

const (
	// folderSectionDefaultLimit is the first-page size of a paginated folder
	// listing section when the request carries no limit.
	folderSectionDefaultLimit = 50
	// folderSectionMaxLimit caps a requested page size. Larger values clamp
	// instead of failing, so "give me everything" stays expressible.
	folderSectionMaxLimit = 200
)

// folderSectionLimit parses the listing's `limit` query parameter: absent is
// the 50-row default, values above the 200 maximum clamp, and a non-numeric
// value or one below 1 is rejected (ok=false). An out-of-range number (e.g.
// more digits than int can hold) clamps to the maximum when positive and is
// rejected when negative.
func folderSectionLimit(r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return folderSectionDefaultLimit, true
	}
	n, err := strconv.Atoi(raw)
	switch {
	case err == nil && n >= 1:
		if n > folderSectionMaxLimit {
			n = folderSectionMaxLimit
		}
		return n, true
	case errors.Is(err, strconv.ErrRange) && n > 0:
		// Overflowed positively (Atoi saturates): every such ask wants more
		// than the maximum, so it clamps like any other oversized value.
		return folderSectionMaxLimit, true
	default:
		return 0, false
	}
}

// folderSectionIDs loads the org- and folder-scoped ids of one listing section
// in display order. folderID nil selects the root section. Only ids are read —
// the full rows stay in the database until the page is known. orderBy must end
// in a unique tiebreaker written as `id::text` so the ordering is explicit
// (uuid text order equals uuid order) and stable for offset paging.
// table/orderBy are compile-time constants at every call site.
func (s *Server) folderSectionIDs(ctx context.Context, table, orderBy, orgID string, folderID *string) ([]string, error) {
	query := fmt.Sprintf(`SELECT id::text FROM %s
		 WHERE org_id = $1 AND folder_id IS NULL AND deleted_at IS NULL
		 ORDER BY %s`, table, orderBy)
	args := []any{orgID}
	if folderID != nil {
		query = fmt.Sprintf(`SELECT id::text FROM %s
		 WHERE org_id = $1 AND folder_id = $2 AND deleted_at IS NULL
		 ORDER BY %s`, table, orderBy)
		args = append(args, *folderID)
	}
	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s ids: %w", table, err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan %s id: %w", table, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s ids: %w", table, err)
	}
	return ids, nil
}

// folderSectionPageIDs filters an ordered section id list down to the visible
// rows (view action) and slices one page, returning the page ids, the exact
// visible total, and whether more visible rows exist beyond the page. Hidden
// items are filtered before the totals are computed, so totals and has_more
// describe what the caller can actually see. An offset at or past the end is a
// legitimate empty page (a client can race a deletion): it yields no ids and
// has_more false because nothing follows it.
//
// Precondition: offset must be >= 0; the load-more handlers parse and reject
// negative offsets before calling. A negative offset is clamped to 0
// defensively anyway so a future caller cannot panic the slice below.
func folderSectionPageIDs(perms map[string]map[string]bool, orderedIDs []string, offset, limit int) (pageIDs []string, total int, hasMore bool) {
	if offset < 0 {
		offset = 0
	}
	visible := make([]string, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if actionAllowed(perms, id, "view") {
			visible = append(visible, id)
		}
	}
	total = len(visible)
	if offset >= total {
		return []string{}, total, false
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return visible[offset:end], total, total > offset+limit
}

// folderNotebookSection is the notebooks section of a folder listing, resolved
// in two phases: phase 1 fetches only the section's ids (org- and
// folder-scoped, in stable display order) and the batched permissions, so the
// exact visible total is known without touching full rows; phase 2 hydrates
// the requested page's full rows and attaches the can_* flags from the already
// resolved action map. folderID nil selects the root section.
//
// This helper (with its connector and dashboard siblings) is the ONLY way a
// paginated listing section may be produced: every page, including each
// load-more page, must hydrate through here so no caller can re-implement the
// pipeline and drop a rule (e.g. the dashboard snapshot substitution).
func (s *Server) folderNotebookSection(ctx context.Context, ident permIdentity, folderID *string, offset, limit int) ([]folderNotebook, int, bool, error) {
	orderedIDs, err := s.folderSectionIDs(ctx, "notebooks", "updated_at DESC, id::text", ident.OrgID, folderID)
	if err != nil {
		return nil, 0, false, err
	}
	perms, err := s.resolveActions(ctx, ident, "notebook", orderedIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		return nil, 0, false, err
	}
	pageIDs, total, hasMore := folderSectionPageIDs(perms, orderedIDs, offset, limit)
	page := make([]folderNotebook, 0, len(pageIDs))
	if len(pageIDs) == 0 {
		return page, total, hasMore, nil
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, description, connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`,
		pageIDs, ident.OrgID,
	)
	if err != nil {
		return nil, 0, false, fmt.Errorf("hydrate notebooks: %w", err)
	}
	defer rows.Close()
	hydrated := make(map[string]folderNotebook, len(pageIDs))
	for rows.Next() {
		nb, err := scanNotebook(rows)
		if err != nil {
			return nil, 0, false, fmt.Errorf("scan notebook: %w", err)
		}
		hydrated[nb.ID] = folderNotebook{Notebook: nb}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, fmt.Errorf("iterate notebooks: %w", err)
	}
	for _, id := range pageIDs {
		nb, ok := hydrated[id]
		if !ok {
			// The row vanished between resolve and hydrate; holding the rest
			// of the page order is more useful than failing the listing.
			continue
		}
		nb.CanEdit = actionAllowed(perms, id, "edit")
		nb.CanDelete = actionAllowed(perms, id, "delete")
		nb.CanShare = actionAllowed(perms, id, "share")
		page = append(page, nb)
	}
	return page, total, hasMore, nil
}

// folderConnectorSection is the connectors section, following the same
// two-phase pattern as folderNotebookSection (ordered by name, then id::text)
// and carrying the same "all page hydration goes through here" rule.
func (s *Server) folderConnectorSection(ctx context.Context, ident permIdentity, folderID *string, offset, limit int) ([]folderConnector, int, bool, error) {
	orderedIDs, err := s.folderSectionIDs(ctx, "connectors", "name, id::text", ident.OrgID, folderID)
	if err != nil {
		return nil, 0, false, err
	}
	perms, err := s.resolveActions(ctx, ident, "connector", orderedIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		return nil, 0, false, err
	}
	pageIDs, total, hasMore := folderSectionPageIDs(perms, orderedIDs, offset, limit)
	page := make([]folderConnector, 0, len(pageIDs))
	if len(pageIDs) == 0 {
		return page, total, hasMore, nil
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id, COALESCE(created_by::text, ''), created_at::text, COALESCE(updated_at::text, '')
		 FROM connectors WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`,
		pageIDs, ident.OrgID,
	)
	if err != nil {
		return nil, 0, false, fmt.Errorf("hydrate connectors: %w", err)
	}
	defer rows.Close()
	hydrated := make(map[string]folderConnector, len(pageIDs))
	for rows.Next() {
		var c folderConnector
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, false, fmt.Errorf("scan connector: %w", err)
		}
		hydrated[c.ID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, fmt.Errorf("iterate connectors: %w", err)
	}
	for _, id := range pageIDs {
		c, ok := hydrated[id]
		if !ok {
			continue
		}
		c.CanEdit = actionAllowed(perms, id, "edit")
		c.CanDelete = actionAllowed(perms, id, "delete")
		c.CanShare = actionAllowed(perms, id, "share")
		page = append(page, c)
	}
	return page, total, hasMore, nil
}

// folderDashboardSection is the dashboards section, following the same
// two-phase pattern (ordered by updated_at, then id::text) and carrying the
// same "all page hydration goes through here" rule. The page's non-editor rows
// additionally receive the published-snapshot substitution
// (loadLatestDashboardSnapshots) so the listing matches GET /dashboards and
// GET /dashboards/{id}: viewers see the latest snapshot's title/settings,
// editors always see the draft, and a never-published dashboard keeps its
// draft for everyone. Routing every page (including load-more pages) through
// this helper keeps that substitution impossible to forget.
func (s *Server) folderDashboardSection(ctx context.Context, ident permIdentity, folderID *string, offset, limit int) ([]folderDashboard, int, bool, error) {
	orderedIDs, err := s.folderSectionIDs(ctx, "dashboards", "updated_at DESC, id::text", ident.OrgID, folderID)
	if err != nil {
		return nil, 0, false, err
	}
	perms, err := s.resolveActions(ctx, ident, "dashboard", orderedIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		return nil, 0, false, err
	}
	pageIDs, total, hasMore := folderSectionPageIDs(perms, orderedIDs, offset, limit)
	page := make([]folderDashboard, 0, len(pageIDs))
	if len(pageIDs) == 0 {
		return page, total, hasMore, nil
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, created_by, created_at, updated_at
		 FROM dashboards WHERE id = ANY($1::uuid[]) AND org_id = $2 AND deleted_at IS NULL`,
		pageIDs, ident.OrgID,
	)
	if err != nil {
		return nil, 0, false, fmt.Errorf("hydrate dashboards: %w", err)
	}
	defer rows.Close()
	hydrated := make(map[string]folderDashboard, len(pageIDs))
	for rows.Next() {
		d, err := scanDashboard(rows)
		if err != nil {
			return nil, 0, false, fmt.Errorf("scan dashboard: %w", err)
		}
		hydrated[d.ID] = folderDashboard{Dashboard: d}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, fmt.Errorf("iterate dashboards: %w", err)
	}

	// One batched snapshot load covers the page's non-editor rows only; a
	// missing id means never published, so the draft stands.
	var nonEditorIDs []string
	for _, id := range pageIDs {
		if !actionAllowed(perms, id, "edit") {
			nonEditorIDs = append(nonEditorIDs, id)
		}
	}
	snapshots := map[string]publishedDashboardMeta{}
	if len(nonEditorIDs) > 0 {
		snapshots, err = s.loadLatestDashboardSnapshots(ctx, ident.OrgID, nonEditorIDs)
		if err != nil {
			return nil, 0, false, fmt.Errorf("load dashboard snapshots: %w", err)
		}
	}
	for _, id := range pageIDs {
		d, ok := hydrated[id]
		if !ok {
			continue
		}
		if !actionAllowed(perms, id, "edit") {
			if snapshot, ok := snapshots[id]; ok {
				d.Title = snapshot.Title
				d.Settings = snapshot.Settings
			}
		}
		d.CanEdit = actionAllowed(perms, id, "edit")
		d.CanDelete = actionAllowed(perms, id, "delete")
		d.CanShare = actionAllowed(perms, id, "share")
		page = append(page, d)
	}
	return page, total, hasMore, nil
}

// folderConnector is a lightweight connector listing (no encrypted credentials).
type folderConnector struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	IsDefault bool    `json:"is_default"`
	FolderID  *string `json:"folder_id,omitempty"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	CanEdit   bool    `json:"can_edit"`
	CanDelete bool    `json:"can_delete"`
	CanShare  bool    `json:"can_share"`
}

// handleListHomeFolders returns all home folders for the org, grouped by owner.
// Each entry includes the home folder info and a list of sub-folders the user can access.
// @Summary List home folders
// @Description List all home folders for the current user
// @Tags folders
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /home [get]
func (s *Server) handleListHomeFolders(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	type homeFolderEntry struct {
		models.Folder
		OwnerName  string          `json:"owner_name"`
		SubFolders []models.Folder `json:"sub_folders"`
	}

	// One identity per request: group memberships (including the implicit
	// Everyone group) are resolved once and shared by every batched probe.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Get all home folders for the org with owner names
	rows, err := s.db.Pool.Query(ctx,
		`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at,
		        COALESCE(u.name, '') as owner_name
		 FROM folders f
		 JOIN users u ON u.id = f.owner_id
		 WHERE f.org_id = $1 AND f.is_home = true
		 ORDER BY f.name`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var entries []homeFolderEntry
	for rows.Next() {
		var e homeFolderEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ParentID, &e.Name, &e.IsHome,
			&e.OwnerID, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.OwnerName); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		entries = append(entries, e)
	}

	// Filter home folders: show if user owns it OR has ACL access OR has access to any content inside
	var filteredEntries []homeFolderEntry
	// Org admins bypass ACLs only when admin mode is enabled — they see all home folders
	isOrgAdmin := ident.AdminMode && ident.OrgRole == "admin"
	if isOrgAdmin {
		filteredEntries = entries
	} else {
		// Batched visibility resolution, replacing the former per-folder
		// queries: ownership is decided in Go, the direct ACL subject match
		// runs once over every candidate, and the folder-aware descendant
		// walk runs once over the remaining candidates.
		candidates := make([]string, 0, len(entries))
		owned := make(map[string]bool, len(entries))
		for _, entry := range entries {
			if entry.OwnerID != nil && *entry.OwnerID == ident.UserID {
				owned[entry.ID] = true
				continue
			}
			candidates = append(candidates, entry.ID)
		}
		aclMatch, err := s.filterFoldersWithACLSubjectMatch(ctx, candidates, ident.UserID, ident.GroupIDs)
		if err != nil {
			// Deliberate fail-loud: a resolution error must not read as "no access".
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		remaining := make([]string, 0, len(candidates))
		for _, id := range candidates {
			if !aclMatch[id] {
				remaining = append(remaining, id)
			}
		}
		descendantMatch, err := s.filterFoldersWithVisibleDescendantContentOrFolders(ctx, ident, remaining)
		if err != nil {
			// Deliberate fail-loud: same policy for the descendant probe.
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		for _, entry := range entries {
			if owned[entry.ID] || aclMatch[entry.ID] || descendantMatch[entry.ID] {
				filteredEntries = append(filteredEntries, entry)
			}
		}
	}

	// For every filtered home folder, list the sub-folders the user can
	// access. The predicates are resolved in batch: one query loads all direct
	// sub-folders, then a batched ACL subject match plus the content-only
	// descendant filter decide visibility (admins skip both).
	if len(filteredEntries) > 0 {
		homeIDs := make([]string, 0, len(filteredEntries))
		for i := range filteredEntries {
			homeIDs = append(homeIDs, filteredEntries[i].ID)
		}
		subRows, err := s.db.Pool.Query(ctx,
			`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
			 FROM folders f
			 WHERE f.parent_id = ANY($1::uuid[]) AND f.org_id = $2
			 ORDER BY f.name`,
			homeIDs, claims.OrgID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		var subs []models.Folder
		for subRows.Next() {
			var sub models.Folder
			if err := subRows.Scan(&sub.ID, &sub.OrgID, &sub.ParentID, &sub.Name, &sub.IsHome,
				&sub.OwnerID, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
				subRows.Close()
				writeError(w, http.StatusInternalServerError, "scan failed")
				return
			}
			subs = append(subs, sub)
		}
		subRows.Close()
		if err := subRows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}

		visible := make(map[string]bool, len(subs))
		if isOrgAdmin {
			for _, sub := range subs {
				visible[sub.ID] = true
			}
		} else {
			candidates := make([]string, 0, len(subs))
			for _, sub := range subs {
				candidates = append(candidates, sub.ID)
			}
			aclMatch, err := s.filterFoldersWithACLSubjectMatch(ctx, candidates, ident.UserID, ident.GroupIDs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "query failed")
				return
			}
			// Only folders that are neither owned nor ACL-matched need the
			// content probe.
			remaining := make([]string, 0, len(candidates))
			for _, sub := range subs {
				ownedByCaller := sub.OwnerID != nil && *sub.OwnerID == ident.UserID
				if !ownedByCaller && !aclMatch[sub.ID] {
					remaining = append(remaining, sub.ID)
				}
			}
			contentMatch, err := s.filterFoldersWithVisibleDescendantContent(ctx, ident, remaining)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "query failed")
				return
			}
			for _, sub := range subs {
				ownedByCaller := sub.OwnerID != nil && *sub.OwnerID == ident.UserID
				visible[sub.ID] = ownedByCaller || aclMatch[sub.ID] || contentMatch[sub.ID]
			}
		}

		// Append in scan (name) order per parent so each entry keeps the
		// single-query ORDER BY f.name it had before batching.
		byParent := make(map[string][]models.Folder, len(filteredEntries))
		for _, sub := range subs {
			if visible[sub.ID] && sub.ParentID != nil {
				byParent[*sub.ParentID] = append(byParent[*sub.ParentID], sub)
			}
		}
		for i := range filteredEntries {
			filteredEntries[i].SubFolders = byParent[filteredEntries[i].ID]
		}
	}

	if filteredEntries == nil {
		filteredEntries = []homeFolderEntry{}
	}

	writeJSON(w, http.StatusOK, filteredEntries)
}

// @Summary List root contents
// @Description List folders and resources at the root level
// @Tags folders
// @Produce json
// @Success 200 {object} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /folders [get]
func (s *Server) handleListRootContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims.OrgID == "" {
		writeError(w, http.StatusForbidden, "organization required — complete onboarding first")
		return
	}
	ctx := r.Context()

	// One identity per request: group memberships (including the implicit
	// Everyone group) and admin mode are resolved once and shared by the batch
	// resolver calls below.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// The paginated sections' page size: default 50, clamped to 200, invalid
	// values rejected before any work runs.
	limit, ok := folderSectionLimit(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}

	contents := newFolderContents(nil)

	isOrgAdmin := ident.AdminMode && ident.OrgRole == "admin"

	// Folders at root: include folders user can access AND has content inside, EXCLUDE is_home folders
	var folderQuery string
	var folderArgs []any
	if isOrgAdmin {
		folderQuery = `SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
			 FROM folders f
			 WHERE f.org_id = $1 AND f.parent_id IS NULL AND f.is_home = false AND f.deleted_at IS NULL
			 ORDER BY f.name`
		folderArgs = []any{claims.OrgID}
	} else {
		folderQuery = `SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
		 FROM folders f
		 WHERE f.org_id = $1 AND f.parent_id IS NULL AND f.is_home = false AND f.deleted_at IS NULL
		 AND (
		   f.owner_id = $2
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'user' AND subject_id = $2::text)
		   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'org_role' AND subject_id = 'everyone')
		   OR (
			   -- Direct accessible content in this folder
			   EXISTS (SELECT 1 FROM notebooks nb WHERE nb.folder_id = f.id AND nb.org_id = $1 AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $2::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (SELECT 1 FROM connectors c WHERE c.folder_id = f.id AND c.org_id = $1 AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $2::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (SELECT 1 FROM dashboards d2 WHERE d2.folder_id = f.id AND d2.org_id = $1 AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $2::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
		   )
		   OR (
			   -- Has accessible content at any depth in the hierarchy
			   EXISTS (
					WITH RECURSIVE desc_ids AS (
						SELECT id FROM folders WHERE parent_id = f.id AND org_id = f.org_id
						UNION ALL
						SELECT f2.id FROM folders f2
						JOIN desc_ids d ON f2.parent_id = d.id
						WHERE f2.org_id = f.org_id
					)
					SELECT 1 FROM desc_ids d
					WHERE (
						EXISTS (
							SELECT 1 FROM folders f3
							WHERE f3.id = d.id
							AND (
								f3.owner_id = $2
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f3.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f3.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f3.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
						OR EXISTS (
							SELECT 1 FROM notebooks nb
							WHERE nb.folder_id = d.id AND nb.org_id = f.org_id
							AND (
								EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
						OR EXISTS (
							SELECT 1 FROM connectors c
							WHERE c.folder_id = d.id AND c.org_id = f.org_id
							AND (
								EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
						OR EXISTS (
							SELECT 1 FROM dashboards d2
							WHERE d2.folder_id = d.id AND d2.org_id = f.org_id
							AND (
								EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
					)
					LIMIT 1
			   )
		   )
		 )
		 ORDER BY f.name`
		folderArgs = []any{claims.OrgID, ident.UserID, ident.GroupIDs}
	}
	rows, err := s.db.Pool.Query(ctx, folderQuery, folderArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var rootFolders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome,
			&f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		rootFolders = append(rootFolders, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Batched edit/delete/manage flags for every listed folder. Visibility
	// itself stays decided by the query above (owner / ACL-subject predicates
	// are deliberately wider than a resolved view).
	rootFolderIDs := make([]string, 0, len(rootFolders))
	for _, f := range rootFolders {
		rootFolderIDs = append(rootFolderIDs, f.ID)
	}
	folderPerms, err := s.resolveActions(ctx, ident, "folder", rootFolderIDs, []string{"edit", "delete", "manage"})
	if err != nil {
		// Deliberate fail-loud: the former per-item checks silently hid rows/flags on error.
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, f := range rootFolders {
		contents.Folders = append(contents.Folders, folderItemFolder{
			Folder:    f,
			CanEdit:   actionAllowed(folderPerms, f.ID, "edit"),
			CanDelete: actionAllowed(folderPerms, f.ID, "delete"),
			CanShare:  actionAllowed(folderPerms, f.ID, "manage"),
		})
	}

	// Notebooks at root (no folder): the first page plus exact visible totals.
	notebooks, notebookTotal, notebooksHasMore, err := s.folderNotebookSection(ctx, ident, nil, 0, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	contents.Notebooks = notebooks
	contents.Totals["notebooks"] = notebookTotal
	contents.HasMore["notebooks"] = notebooksHasMore

	// Connectors at root (no folder): the first page plus exact visible totals.
	connectors, connectorTotal, connectorsHasMore, err := s.folderConnectorSection(ctx, ident, nil, 0, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	contents.Connectors = connectors
	contents.Totals["connectors"] = connectorTotal
	contents.HasMore["connectors"] = connectorsHasMore

	// Dashboards at root (no folder): the first page plus exact visible
	// totals, with the published-snapshot substitution for non-editors.
	dashboards, dashboardTotal, dashboardsHasMore, err := s.folderDashboardSection(ctx, ident, nil, 0, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	contents.Dashboards = dashboards
	contents.Totals["dashboards"] = dashboardTotal
	contents.HasMore["dashboards"] = dashboardsHasMore

	writeJSON(w, http.StatusOK, contents)
}

// @Summary Get folder contents
// @Description Get all items in a folder (subfolders, notebooks, connectors, dashboards)
// @Tags folders
// @Produce json
// @Param id path string true "Folder ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id} [get]
func (s *Server) handleGetFolderContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE id = $1 AND org_id = $2`,
		folderID, claims.OrgID,
	).Scan(&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// The paginated sections' page size: default 50, clamped to 200, invalid
	// values rejected before any work runs.
	limit, ok := folderSectionLimit(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}

	contents := newFolderContents(&folder)

	// One identity per request: group memberships (including the implicit
	// Everyone group) and admin mode are resolved once and shared by the
	// batched resolution below.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Sub-folders
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE org_id = $1 AND parent_id = $2 AND deleted_at IS NULL ORDER BY name`,
		claims.OrgID, folderID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	var subFolders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome,
			&f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		subFolders = append(subFolders, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Batched view/edit/delete/manage for the requested folder and every
	// child. folder.ID (the canonical scan result) is the resolution key, so
	// an uppercase path spelling still resolves like the uuid cast did.
	childFolderIDs := make([]string, 0, len(subFolders)+1)
	childFolderIDs = append(childFolderIDs, folder.ID)
	for _, f := range subFolders {
		childFolderIDs = append(childFolderIDs, f.ID)
	}
	folderPerms, err := s.resolveActions(ctx, ident, "folder", childFolderIDs, []string{"view", "edit", "delete", "manage"})
	if err != nil {
		// Deliberate fail-loud: the former per-item checks silently hid rows on error.
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}

	// Permission check: user must have view on the folder OR have access to
	// content inside (the single-folder content probe, as before). A folder
	// resolved visible is never probed.
	if !actionAllowed(folderPerms, folder.ID, "view") {
		hasContent, cErr := s.folderHasDescendantContent(ctx, folder.ID, ident.OrgID, ident.UserID, ident.GroupIDs)
		if cErr != nil || !hasContent {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	// Children that fail their own view check are probed in one batch for
	// visible descendant content (content-only, matching the former per-child
	// probe). A probe error keeps them hidden, exactly as the per-child error
	// path did.
	var hiddenChildIDs []string
	for _, f := range subFolders {
		if !actionAllowed(folderPerms, f.ID, "view") {
			hiddenChildIDs = append(hiddenChildIDs, f.ID)
		}
	}
	var descendantVisible map[string]bool
	if len(hiddenChildIDs) > 0 {
		descendantVisible, err = s.filterFoldersWithVisibleDescendantContent(ctx, ident, hiddenChildIDs)
		if err != nil {
			descendantVisible = nil
		}
	}
	for _, f := range subFolders {
		if actionAllowed(folderPerms, f.ID, "view") {
			contents.Folders = append(contents.Folders, folderItemFolder{
				Folder:    f,
				CanEdit:   actionAllowed(folderPerms, f.ID, "edit"),
				CanDelete: actionAllowed(folderPerms, f.ID, "delete"),
				CanShare:  actionAllowed(folderPerms, f.ID, "manage"),
			})
		} else if descendantVisible[f.ID] {
			// Hidden folder with accessible descendant content (deep sharing).
			contents.Folders = append(contents.Folders, folderItemFolder{Folder: f, CanEdit: false, CanDelete: false, CanShare: false})
		}
	}

	// Notebooks in the folder: the first page plus exact visible totals.
	notebooks, notebookTotal, notebooksHasMore, err := s.folderNotebookSection(ctx, ident, &folderID, 0, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	contents.Notebooks = notebooks
	contents.Totals["notebooks"] = notebookTotal
	contents.HasMore["notebooks"] = notebooksHasMore

	// Connectors in the folder: the first page plus exact visible totals.
	connectors, connectorTotal, connectorsHasMore, err := s.folderConnectorSection(ctx, ident, &folderID, 0, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	contents.Connectors = connectors
	contents.Totals["connectors"] = connectorTotal
	contents.HasMore["connectors"] = connectorsHasMore

	// Dashboards in the folder: the first page plus exact visible totals, with
	// the published-snapshot substitution for non-editors.
	dashboards, dashboardTotal, dashboardsHasMore, err := s.folderDashboardSection(ctx, ident, &folderID, 0, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	contents.Dashboards = dashboards
	contents.Totals["dashboards"] = dashboardTotal
	contents.HasMore["dashboards"] = dashboardsHasMore

	writeJSON(w, http.StatusOK, contents)
}

// folderSectionResponse is the load-more envelope: one section page, the exact
// visible total of the whole section, whether more visible rows follow the
// page, and the requested offset echoed back.
type folderSectionResponse struct {
	Items   any  `json:"items"`
	Total   int  `json:"total"`
	HasMore bool `json:"has_more"`
	Offset  int  `json:"offset"`
}

// folderSectionQuery parses and validates the load-more endpoints' query
// parameters per the settled contract: `type` is required and must be
// notebook, connector, or dashboard; `offset` is absent = 0, and non-numeric
// or negative values are rejected (a positively overflowed number saturates
// and acts as a genuinely past-the-end offset instead of failing); `limit`
// follows the listing contract (absent = 50, non-numeric or below 1 rejected,
// above 200 clamped, including positive overflow). ok=false means the request
// must be answered with 400.
func folderSectionQuery(r *http.Request) (sectionType string, offset, limit int, ok bool) {
	query := r.URL.Query()
	sectionType = query.Get("type")
	switch sectionType {
	case "notebook", "connector", "dashboard":
	default:
		return "", 0, 0, false
	}

	if raw := query.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		switch {
		case err == nil && n >= 0:
			offset = n
		case errors.Is(err, strconv.ErrRange) && n > 0:
			// Overflowed positively: every such ask skips past the end of any
			// real section, so it degrades to an empty page rather than a 400.
			offset = n
		default:
			return "", 0, 0, false
		}
	}

	limit, ok = folderSectionLimit(r)
	if !ok {
		return "", 0, 0, false
	}
	return sectionType, offset, limit, true
}

// writeFolderSection serves one load-more page for a section type at the
// requested offset. Every page — first or load-more — is produced by the B1
// section helpers (folderSectionIDs → resolveActions → folderSectionPageIDs →
// per-type hydration): this function only parses the request and shapes the
// envelope, so no caller can re-implement the pipeline and drop a rule (e.g.
// the dashboard snapshot substitution). folderID nil selects the root section.
func (s *Server) writeFolderSection(w http.ResponseWriter, ctx context.Context, ident permIdentity, sectionType string, folderID *string, offset, limit int) {
	var (
		items   any
		total   int
		hasMore bool
		err     error
	)
	switch sectionType {
	case "notebook":
		items, total, hasMore, err = s.folderNotebookSection(ctx, ident, folderID, offset, limit)
	case "connector":
		items, total, hasMore, err = s.folderConnectorSection(ctx, ident, folderID, offset, limit)
	case "dashboard":
		items, total, hasMore, err = s.folderDashboardSection(ctx, ident, folderID, offset, limit)
	default:
		// Unreachable: folderSectionQuery validates the type before calling.
		writeError(w, http.StatusBadRequest, "invalid section type")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, folderSectionResponse{
		Items:   items,
		Total:   total,
		HasMore: hasMore,
		Offset:  offset,
	})
}

// @Summary Get a folder section page
// @Description Load-more page for one folder section (notebooks, connectors, or dashboards), with the exact visible total
// @Tags folders
// @Produce json
// @Param id path string true "Folder ID"
// @Param type query string true "Section type: notebook, connector, or dashboard"
// @Param offset query int false "Rows to skip (default 0)"
// @Param limit query int false "Page size (default 50, max 200)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id}/contents [get]
func (s *Server) handleGetFolderContentsSection(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	sectionType, offset, limit, ok := folderSectionQuery(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid section query")
		return
	}

	// The folder must exist in the org, exactly like the full listing.
	folderID := r.PathValue("id")
	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE id = $1 AND org_id = $2`,
		folderID, claims.OrgID,
	).Scan(&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// One identity per request, shared by the access check and the section
	// helpers' batched resolution.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Same access rule as the full listing (GET /folders/{id}): view on the
	// folder, or visible descendant content (deep sharing). The page's rows
	// are ACL-filtered per item by the shared section helpers anyway.
	folderPerms, err := s.resolveActions(ctx, ident, "folder", []string{folder.ID}, []string{"view"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !actionAllowed(folderPerms, folder.ID, "view") {
		hasContent, cErr := s.folderHasDescendantContent(ctx, folder.ID, ident.OrgID, ident.UserID, ident.GroupIDs)
		if cErr != nil || !hasContent {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	s.writeFolderSection(w, ctx, ident, sectionType, &folder.ID, offset, limit)
}

// @Summary List a root section page
// @Description Load-more page for one root-level section (notebooks, connectors, or dashboards), with the exact visible total
// @Tags folders
// @Produce json
// @Param type query string true "Section type: notebook, connector, or dashboard"
// @Param offset query int false "Rows to skip (default 0)"
// @Param limit query int false "Page size (default 50, max 200)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /folders/contents [get]
func (s *Server) handleListRootContentsSection(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims.OrgID == "" {
		writeError(w, http.StatusForbidden, "organization required — complete onboarding first")
		return
	}

	sectionType, offset, limit, ok := folderSectionQuery(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid section query")
		return
	}

	ident, err := s.permIdentity(r.Context(), claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Root sections have no resource to gate: the page's rows are
	// ACL-filtered per item by the shared section helpers.
	s.writeFolderSection(w, r.Context(), ident, sectionType, nil, offset, limit)
}

// @Summary Get folder ancestors
// @Description Get the ancestor chain for a folder (root first, leaf last)
// @Tags folders
// @Produce json
// @Param id path string true "Folder ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id}/ancestors [get]
func (s *Server) handleGetFolderAncestors(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	// Verify folder exists in org
	var exists bool
	s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id = $1 AND org_id = $2)`,
		folderID, claims.OrgID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	groupIDs := []string{claims.UserID}
	groupRows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, claims.UserID)
	if err == nil {
		for groupRows.Next() {
			var gid string
			groupRows.Scan(&gid)
			groupIDs = append(groupIDs, gid)
		}
		groupRows.Close()
	}

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", folderID, "view"); err != nil || !allowed {
		hasContent, cErr := s.folderHasDescendantContent(ctx, folderID, claims.OrgID, claims.UserID, groupIDs)
		if cErr != nil || !hasContent {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	// Recursive CTE: start from folder itself (depth=0), walk up via parent_id
	// ORDER BY depth DESC gives root first, leaf last
	rows, err := s.db.Pool.Query(ctx,
		`WITH RECURSIVE ancestors AS (
		   SELECT id, name, parent_id, 0 AS depth
		   FROM folders WHERE id = $1 AND org_id = $2
		   UNION ALL
		   SELECT f.id, f.name, f.parent_id, a.depth + 1
		   FROM folders f
		   JOIN ancestors a ON f.id = a.parent_id
		   WHERE f.org_id = $2
		 )
		 SELECT id, name FROM ancestors ORDER BY depth DESC`,
		folderID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type ancestorItem struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var ancestors []ancestorItem
	for rows.Next() {
		var a ancestorItem
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		ancestors = append(ancestors, a)
	}
	if ancestors == nil {
		ancestors = []ancestorItem{}
	}

	writeJSON(w, http.StatusOK, ancestors)
}

// @Summary Create a folder
// @Description Create a new folder
// @Tags folders
// @Accept json
// @Produce json
// @Param request body object true "Folder details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /folders [post]
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var req struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Check create permission on the parent folder (or root-level if no parent)
	if req.ParentID != nil && *req.ParentID != "" {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", *req.ParentID, "create")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions to create folder here")
			return
		}
	}

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO folders (org_id, parent_id, name, owner_id, created_by)
		 VALUES ($1, $2, $3, $4, $4)
		 RETURNING id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at`,
		claims.OrgID, req.ParentID, req.Name, claims.UserID,
	).Scan(&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}

	// Seed ACL entry so the creator can manage this folder and create subfolders
	s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view','create','edit','manage','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, folder.ID, claims.UserID,
	)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "folder.create", ResourceType: "folder", ResourceID: folder.ID,
	})

	writeJSON(w, http.StatusCreated, folder)
}

// @Summary Update a folder
// @Description Update a folder's name or parent
// @Tags folders
// @Accept json
// @Produce json
// @Param id path string true "Folder ID"
// @Param request body object true "Folder updates"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id} [put]
func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	var rawReq map[string]json.RawMessage
	if err := decodeJSON(r, &rawReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var name *string
	if nameRaw, ok := rawReq["name"]; ok {
		json.Unmarshal(nameRaw, &name)
	}

	setParentID := false
	var parentID *string
	if parentRaw, ok := rawReq["parent_id"]; ok {
		setParentID = true
		json.Unmarshal(parentRaw, &parentID)
	}

	if name == nil && !setParentID {
		writeError(w, http.StatusBadRequest, "name or parent_id required")
		return
	}

	// Cycle guard: ensure the new parent is not the folder itself or a descendant.
	if setParentID && parentID != nil {
		var isCycle bool
		err := s.db.Pool.QueryRow(ctx, `
			WITH RECURSIVE desc AS (
				SELECT id FROM folders WHERE id = $1
				UNION ALL
				SELECT f.id FROM folders f
				JOIN desc d ON f.parent_id = d.id
				WHERE f.org_id = $3
			)
			SELECT EXISTS(SELECT 1 FROM desc WHERE id = $2)
		`, folderID, *parentID, claims.OrgID).Scan(&isCycle)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cycle check failed")
			return
		}
		if isCycle {
			writeError(w, http.StatusBadRequest, "cannot move folder under itself or a descendant")
			return
		}
	}

	// Build dynamic SET clause
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *name)
		argIdx++
	}
	if setParentID {
		setClauses = append(setClauses, fmt.Sprintf("parent_id = $%d", argIdx))
		args = append(args, parentID)
		argIdx++
	}

	args = append(args, folderID, claims.OrgID)
	query := fmt.Sprintf(
		`UPDATE folders SET %s WHERE id = $%d AND org_id = $%d
		 RETURNING id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at`,
		strings.Join(setClauses, ", "), argIdx, argIdx+1,
	)

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update folder")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "folder.update", ResourceType: "folder", ResourceID: folderID,
	})

	writeJSON(w, http.StatusOK, folder)
}

// @Summary Ensure home folder
// @Description Create the current user's home folder if it doesn't exist
// @Tags folders
// @Produce json
// @Success 200 {object} map[string]string
// @Success 201 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /users/me/home [post]
func (s *Server) handleEnsureHomeFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var userName string
	if err := s.db.Pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&userName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch user")
		return
	}

	// Get user's org membership
	var orgID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT om.org_id FROM org_members om WHERE om.user_id = $1 ORDER BY om.created_at LIMIT 1`,
		claims.UserID,
	).Scan(&orgID)
	if err != nil {
		writeError(w, http.StatusForbidden, "user is not a member of any org")
		return
	}

	// Check if home folder already exists
	var existingID string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true LIMIT 1`,
		claims.UserID,
	).Scan(&existingID)
	if err == nil {
		// Home folder already exists
		writeJSON(w, http.StatusOK, map[string]string{"id": existingID})
		return
	}
	if err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Create the home folder
	if err := createHomeFolder(ctx, s.db.Pool, orgID, claims.UserID, userName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create home folder")
		return
	}

	// Fetch the newly created folder ID
	var folderID string
	s.db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true LIMIT 1`,
		claims.UserID,
	).Scan(&folderID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": folderID})
}

// @Summary Delete a folder
// @Description Delete a folder and all its contents
// @Tags folders
// @Param id path string true "Folder ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id} [delete]
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"
	ctx := r.Context()

	// Verify folder exists in org
	var exists bool
	s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id = $1 AND org_id = $2)`,
		folderID, claims.OrgID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	if !force {
		// Check if any children exist (scoped to org to avoid cross-org false positives)
		var hasChildren bool
		s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(
			   SELECT 1 FROM folders WHERE parent_id = $1 AND org_id = $2 AND deleted_at IS NULL
			   UNION ALL
			   SELECT 1 FROM notebooks WHERE folder_id = $1 AND org_id = $2 AND deleted_at IS NULL
			   UNION ALL
			   SELECT 1 FROM connectors WHERE folder_id = $1 AND org_id = $2 AND deleted_at IS NULL
			   UNION ALL
			   SELECT 1 FROM dashboards WHERE folder_id = $1 AND org_id = $2 AND deleted_at IS NULL
			 )`,
			folderID, claims.OrgID,
		).Scan(&hasChildren)
		if hasChildren {
			writeError(w, http.StatusConflict, "folder is not empty; use ?force=true to delete recursively")
			return
		}
	}

	result, err := s.db.Pool.Exec(ctx,
		`UPDATE folders SET deleted_at = NOW() WHERE id = $1 AND org_id = $2`,
		folderID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "folder.delete", ResourceType: "folder", ResourceID: folderID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// scanNotebook scans a notebook row (id, org_id, title, description, connector_id, parameters,
// created_by, created_at, updated_at).
func scanNotebook(rows interface {
	Scan(dest ...any) error
}) (models.Notebook, error) {
	var nb models.Notebook
	var connectorID *string
	var paramsJSON []byte
	var description *string
	if err := rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &description,
		&connectorID, &paramsJSON, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt); err != nil {
		return nb, err
	}
	if description != nil {
		nb.Description = *description
	}
	if connectorID != nil {
		nb.ConnectorID = *connectorID
	}
	if paramsJSON != nil {
		json.Unmarshal(paramsJSON, &nb.Parameters)
	}
	if nb.Parameters == nil {
		nb.Parameters = []models.Parameter{}
	}
	return nb, nil
}

// scanDashboard scans a dashboard row (id, org_id, title, settings,
// created_by, created_at, updated_at).
func scanDashboard(rows interface {
	Scan(dest ...any) error
}) (models.Dashboard, error) {
	var d models.Dashboard
	var settingsJSON []byte
	if err := rows.Scan(&d.ID, &d.OrgID, &d.Title, &settingsJSON,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return d, err
	}
	if settingsJSON != nil {
		json.Unmarshal(settingsJSON, &d.Settings)
	}
	return d, nil
}
