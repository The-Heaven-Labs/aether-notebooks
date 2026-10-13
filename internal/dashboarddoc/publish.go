package dashboarddoc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file owns the publishable snapshot shape and the version insert. It
// lives in dashboarddoc (not internal/api) because the REST publish/versions
// handlers and the agent publish_dashboard tool must share one canonical
// snapshot, hash, and version-numbering implementation — internal/agent cannot
// import internal/api. The caller loads (or lazily seeds) the document state
// and passes it in; the audit entry stays with each caller.

// ErrSnapshotWarnings reports that a dashboard document cannot be published
// because one or more widgets failed projection validation. The concrete
// SnapshotWarningsError carries the warnings; errors.Is(err, ErrSnapshotWarnings)
// matches it and errors.As retrieves them.
var ErrSnapshotWarnings = errors.New("dashboard snapshot refused: document has invalid widgets")

// SnapshotWarningsError carries the per-widget projection warnings that
// refused a publish.
type SnapshotWarningsError struct {
	Warnings []string
}

func (e *SnapshotWarningsError) Error() string {
	return fmt.Sprintf("%s (%d warning(s))", ErrSnapshotWarnings.Error(), len(e.Warnings))
}

func (e *SnapshotWarningsError) Unwrap() error { return ErrSnapshotWarnings }

// SnapshotWidget is one widget's full definition inside a published snapshot.
// Its JSON shape matches the content fields of models.Widget, so a snapshot
// can be served through the existing dashboard REST shapes without
// translation.
type SnapshotWidget struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Layout      Layout                 `json:"layout"`
	ConnectorID *string                `json:"connector_id,omitempty"`
	Query       *string                `json:"query,omitempty"`
	Language    string                 `json:"language"`
	NotebookID  *string                `json:"notebook_id,omitempty"`
	CellID      *string                `json:"cell_id,omitempty"`
	Config      map[string]interface{} `json:"config"`
}

// Snapshot is the publishable content of a dashboard document: title, settings
// (with variables folded in as settings["variables"]), and widgets sorted by
// ID.
type Snapshot struct {
	Title    string
	Settings map[string]any
	Widgets  []SnapshotWidget
}

// SnapshotFromProjection builds the canonical snapshot for a projection.
// Settings copy the projection's map and re-inject variables under
// settings["variables"] — the shape models.DashboardSettings stores and the
// shape published execute/options validate against. Widgets are sorted by ID
// so the snapshot encoding is deterministic.
func SnapshotFromProjection(proj *Projection) *Snapshot {
	settings := make(map[string]any, len(proj.Settings)+1)
	for k, v := range proj.Settings {
		settings[k] = v
	}
	variables := proj.Variables
	if variables == nil {
		variables = []map[string]any{}
	}
	settings["variables"] = variables

	widgets := make([]SnapshotWidget, 0, len(proj.Widgets))
	for _, w := range proj.Widgets {
		widgets = append(widgets, SnapshotWidget{
			ID:          w.ID,
			Type:        w.Type,
			Layout:      w.Layout,
			ConnectorID: w.ConnectorID,
			Query:       w.Query,
			Language:    w.Language,
			NotebookID:  w.NotebookID,
			CellID:      w.CellID,
			Config:      w.Config,
		})
	}
	sort.Slice(widgets, func(i, j int) bool { return widgets[i].ID < widgets[j].ID })
	return &Snapshot{Title: proj.Title, Settings: settings, Widgets: widgets}
}

// SnapshotFromDoc projects a dashboard document and refuses to snapshot it
// when any widget failed validation: publishing a document whose widget set is
// partially unreadable would silently drop those widgets from the published
// app. The refusal is ErrSnapshotWarnings via SnapshotWarningsError, carrying
// the warnings.
//
// Two deliberate policies:
//
//   - Only widget warnings refuse a publish. Settings/variables warnings are
//     skipped by Project and are simply dropped from the snapshot, matching
//     the materializer, so one malformed setting can never block publishing.
//   - Restore cannot remove draft widgets that fail projection: the restore
//     diff is driven by Projection.Widgets, which omits warned widgets, so a
//     corrupt widget survives in the document (invisible to readers and the
//     materializer) until a raw-key document primitive can delete it by ID.
func SnapshotFromDoc(state []byte) (*Snapshot, error) {
	proj, err := Project(state)
	if err != nil {
		return nil, err
	}
	if len(proj.WidgetWarnings) > 0 {
		return nil, &SnapshotWarningsError{Warnings: proj.WidgetWarnings}
	}
	return SnapshotFromProjection(proj), nil
}

// ContentHash returns the canonical SHA-256 (hex) of a dashboard's publishable
// content. Both publish and the versions status endpoint use it, so "has
// unpublished changes" is a hash comparison. Canonicalization is JSON-based:
// encoding/json sorts map keys, and widgets are sorted by ID before hashing,
// so map iteration order and widget order never affect the hash while any
// content change (title, settings, a widget field) does.
func ContentHash(title string, settings map[string]any, widgets []SnapshotWidget) string {
	sorted := make([]SnapshotWidget, len(widgets))
	copy(sorted, widgets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	payload := struct {
		Title    string           `json:"title"`
		Settings map[string]any   `json:"settings"`
		Widgets  []SnapshotWidget `json:"widgets"`
	}{Title: title, Settings: settings, Widgets: sorted}

	raw, err := json.Marshal(payload)
	if err != nil {
		// Unreachable for projection/JSONB-domain values (Project normalizes
		// its output and snapshots are decoded from JSONB); hash the error so
		// the function stays total instead of panicking on the request path.
		raw = []byte("marshal-error:" + err.Error())
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SnapshotWidgetToDoc converts a stored snapshot widget into the document
// write shape. Layout fields map one-to-one, including the tab placement.
func SnapshotWidgetToDoc(w SnapshotWidget) WidgetDoc {
	return WidgetDoc{
		ID:          w.ID,
		Type:        w.Type,
		Layout:      w.Layout,
		ConnectorID: w.ConnectorID,
		Query:       w.Query,
		Language:    w.Language,
		NotebookID:  w.NotebookID,
		CellID:      w.CellID,
		Config:      w.Config,
	}
}

// PublishVersion snapshots a dashboard document's current state into a new
// dashboard_versions row. The caller owns loading/seeding state (and auditing
// the publish); this function owns the snapshot, hash, and version numbering.
//
// The version number is allocated by the INSERT ... SELECT MAX(version)+1 and
// the UNIQUE (dashboard_id, version) constraint arbitrates concurrent
// publishes: a unique violation is retried once against the then-current max.
// A document with invalid widgets returns ErrSnapshotWarnings (via
// *SnapshotWarningsError).
func PublishVersion(ctx context.Context, pool *pgxpool.Pool, orgID, dashID, userID string, state []byte) (version int, contentHash string, createdAt time.Time, err error) {
	snap, err := SnapshotFromDoc(state)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	settingsJSON, err := json.Marshal(snap.Settings)
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("encode dashboard version settings: %w", err)
	}
	widgetsJSON, err := json.Marshal(snap.Widgets)
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("encode dashboard version widgets: %w", err)
	}
	contentHash = ContentHash(snap.Title, snap.Settings, snap.Widgets)

	for attempt := 0; attempt < 2; attempt++ {
		err = pool.QueryRow(ctx, `
			INSERT INTO dashboard_versions
				(dashboard_id, org_id, version, title, settings, widgets, content_hash, created_by)
			SELECT $1, $2, COALESCE(MAX(version), 0) + 1, $3, $4, $5, $6, $7
			FROM dashboard_versions
			WHERE dashboard_id = $1
			RETURNING version, created_at`,
			dashID, orgID, snap.Title, settingsJSON, widgetsJSON, contentHash, userID,
		).Scan(&version, &createdAt)
		if err == nil {
			break
		}
		if attempt == 0 && isUniqueViolation(err) {
			continue
		}
		return 0, "", time.Time{}, err
	}
	return version, contentHash, createdAt, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505), the signal that a concurrent publish claimed the
// version number first.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
