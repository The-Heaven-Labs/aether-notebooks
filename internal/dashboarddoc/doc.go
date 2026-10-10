// Package dashboarddoc defines the Yjs document format for live dashboard
// co-editing and the seed/project pair that converts between the CRDT
// document and the Go projection used by the REST and materialization layers.
//
// # Document shape
//
// Relay document name: "dashboard:{uuid}".
//
//	meta            Y.Map
//	 ├ title        string
//	 ├ settings     Y.Map  { grid_cols, auto_refresh_seconds, query_cache_seconds, public_live }
//	 └ variables    Y.Array<Y.Map>   (order matters in the parameter strip)
//	widgets         Y.Map<widgetUUID, Y.Map>
//	 ├ type, connector_id, language, notebook_id, cell_id   (strings; "" = unset)
//	 ├ config       JSON string      (whole-value LWW; chart options don't merge field-wise)
//	 ├ layout       Y.Map { row, col, width, height }
//	 └ query        Y.Text           (SQL — character-level co-editing in the drawer)
//
// This matches docs/plans/2026-10-09-dashboard-live-collab-and-shared-cache-design.md
// (Feature 2) and the S2 compatibility gate in compat_test.go.
//
// # Representation choices
//
//   - config is stored as a JSON string, deliberately: it is a whole-value LWW
//     field (chart options don't merge field-wise), and keeping it opaque also
//     bounds the doc shape the materializer must validate.
//   - settings and variable fields are stored as native JSON values (string,
//     bool, numbers, []any, map[string]any), not as JSON strings. ygo v1.51.5's
//     encoding.Encoder.WriteAny supports nested []any/map[string]any (wire
//     tags 117/118) and lib0/yjs decode and re-encode them symmetrically, so a
//     variable whose options are a list of {label,value} objects stays a plain
//     JS array of objects. This was verified empirically against the pinned
//     ygo v1.51.5 and web/node_modules/yjs before choosing it; the S2 compat
//     test pins the same native representation (options as []any).
//   - Seed accepts any JSON-marshalable Go value. Exact scalars and exact
//     []any / map[string]any containers pass through untouched; anything else
//     (structs, named slice/map types, json.Number) is normalized through
//     encoding/json, so values that reach the encoder are always in WriteAny's
//     supported domain. JSON-normalized values follow JSON number semantics
//     (float64).
//   - WidgetDoc pointer fields (ConnectorID, Query, NotebookID, CellID) map to
//     the doc's string fields with nil ⇔ "". Seed canonicalizes a pointer to
//     "" as unset; Project returns nil for "".
//   - Project returns only plain JSON-domain Go values; no ygo encoding leaks
//     out. Shared types nested in plain positions (settings/variables) are
//     converted through their ToJSON payload (a nested Y.Text becomes its
//     string) and normalized recursively; a value that cannot be converted is
//     dropped with a warning. []byte becomes its base64 string, matching
//     encoding/json.
//
// # Number handling
//
// ygo's WriteAny/lib0 encoding does not preserve Go integer types. Verified
// against ygo v1.51.5: integers within int32 range decode as int64 (tag 125);
// integers outside int32 range but within ±2^53 decode as float64 (tag 123);
// integers beyond ±2^53 decode as encoding.BigInt (tag 122); a float64 that is
// float32-lossless decodes as float32 (tag 124), otherwise float64.
//
// Project normalizes decoded values recursively so callers see only int, int64
// and float64: int64 → int when it fits (out-of-range values stay int64),
// encoding.BigInt → int64, integral float32/float64 → int when they fit
// (non-integral or out-of-range floats stay float64), float64 otherwise
// unchanged. Layout is a plain Go int struct; readLayout accepts integral
// floats with a fit check because Seed-written ints above int32 range arrive
// as float64 on the wire.
//
// # Validation split
//
// Project returns an error only for state bytes that fail to decode or for
// wrong field types in the root meta map: a non-string meta.title, a
// meta.settings that is not a Y.Map, or a meta.variables that is not a Y.Array.
// A wrong root container kind (for example state whose "meta" root is actually
// a Y.Text) is NOT detected: ygo's typed root getters return an empty
// container for it, so such state projects as an empty projection without an
// error. Task 10's materializer must not treat a non-empty state that projects
// empty as "delete all rows" without an explicit guard.
//
// Per-widget problems — unknown type, a map key that is not a UUID, invalid or
// negative layout, config that is not a JSON object, unsupported language,
// non-Y.Map widget values — skip that widget and are recorded in
// Projection.Warnings, so one bad widget can never block materialization.
// Malformed individual variable entries are likewise skipped with a warning.
// Warnings are emitted in sorted widget-ID order for determinism.
//
// A missing layout map (or missing layout fields) defaults to zero values;
// only present-but-invalid fields skip the widget.
package dashboarddoc

// Root and field keys of the dashboard document. Unexported: the format is
// consumed through Seed/Project within this package.
const (
	rootMeta    = "meta"
	rootWidgets = "widgets"

	keyTitle     = "title"
	keySettings  = "settings"
	keyVariables = "variables"

	keyType        = "type"
	keyConnectorID = "connector_id"
	keyLanguage    = "language"
	keyNotebookID  = "notebook_id"
	keyCellID      = "cell_id"
	keyConfig      = "config"
	keyLayout      = "layout"
	keyQuery       = "query"

	keyRow    = "row"
	keyCol    = "col"
	keyWidth  = "width"
	keyHeight = "height"
)

// widgetTypes is the set of widget types allowed in the document, mirroring
// the widgets.type CHECK constraint.
var widgetTypes = map[string]bool{
	"chart":  true,
	"table":  true,
	"metric": true,
	"text":   true,
}

// queryLanguages is the set of widget query languages allowed in the document.
// The empty string means "unset" (e.g. text widgets).
var queryLanguages = map[string]bool{
	"":    true,
	"sql": true,
}

// Layout is a widget's position on the dashboard grid.
type Layout struct {
	Row    int
	Col    int
	Width  int
	Height int
}

// WidgetDoc is the projection of one widget in the dashboard document.
//
// ID is the widget UUID (the widgets map key). Seed requires the map key to be
// a valid UUID and rejects a non-empty ID that disagrees with it; Project sets
// ID from the key and skips + warns widgets whose key is not a UUID (the
// materializer writes keys into a UUID column).
//
// ConnectorID, Query, NotebookID and CellID are nil when unset, mapping to/from
// the document's "" placeholder. Config is stored as a JSON string in the
// document; a nil or empty map is stored as "{}" and projects back as an empty
// (non-nil) map.
type WidgetDoc struct {
	ID          string
	Type        string
	Layout      Layout
	ConnectorID *string
	Query       *string
	Language    string
	NotebookID  *string
	CellID      *string
	Config      map[string]any
}

// Projection is the Go view of a dashboard document.
//
// Settings, Variables and Widgets are always non-nil after Project, even for an
// empty document. Variables preserves the document's Y.Array order.
//
// Warnings holds per-widget (and per-variable) validation failures whose
// entries were skipped; see the package documentation for the split between
// warnings and errors.
type Projection struct {
	Title     string
	Settings  map[string]any
	Variables []map[string]any
	Widgets   map[string]WidgetDoc
	Warnings  []string
}
