package dashboarddoc

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/reearth/ygo/crdt"
)

// Seed builds a fresh dashboard document (Yjs state) from a projection.
//
// It is the backend-originated write path: REST widget CRUD, settings and
// variables changes, and lazy seeding from the dashboards/widgets rows all
// produce a doc through Seed.
//
// Seed fails fast on invalid input — an unknown widget type, an unsupported
// language, a negative layout, a widget ID that disagrees with its map key, or
// a config/settings/variable value that is not JSON-marshalable (including
// NaN/±Inf) — because the document is the source of truth and a projection
// that cannot be represented should surface at the write, not as a skipped
// widget during later materialization. An empty projection seeds a valid empty
// document.
func Seed(p Projection) ([]byte, error) {
	settings, err := normalizeMap("settings", p.Settings)
	if err != nil {
		return nil, err
	}

	variables := make([]map[string]any, len(p.Variables))
	for i, v := range p.Variables {
		nv, err := normalizeMap(fmt.Sprintf("variables[%d]", i), v)
		if err != nil {
			return nil, err
		}
		variables[i] = nv
	}

	// Normalize and validate widgets before touching the document so every
	// failure returns an error without building partial state.
	type preparedWidget struct {
		id     string
		w      WidgetDoc
		config string
	}
	prepared := make([]preparedWidget, 0, len(p.Widgets))
	for id, w := range p.Widgets {
		if id == "" {
			return nil, fmt.Errorf("widget map contains an empty ID key")
		}
		if w.ID != "" && w.ID != id {
			return nil, fmt.Errorf("widget %q: ID %q does not match its map key", id, w.ID)
		}
		if !widgetTypes[w.Type] {
			return nil, fmt.Errorf("widget %q: unknown type %q", id, w.Type)
		}
		if !queryLanguages[w.Language] {
			return nil, fmt.Errorf("widget %q: unsupported language %q", id, w.Language)
		}
		if w.Layout.Row < 0 || w.Layout.Col < 0 || w.Layout.Width < 0 || w.Layout.Height < 0 {
			return nil, fmt.Errorf("widget %q: negative layout %+v", id, w.Layout)
		}
		config := "{}"
		if len(w.Config) > 0 {
			raw, err := json.Marshal(w.Config)
			if err != nil {
				return nil, fmt.Errorf("widget %q: config: %w", id, err)
			}
			config = string(raw)
		}
		prepared = append(prepared, preparedWidget{id: id, w: w, config: config})
	}
	// Deterministic document construction: map iteration order is random.
	sort.Slice(prepared, func(i, j int) bool { return prepared[i].id < prepared[j].id })

	doc := crdt.New()

	// Root containers must be resolved before Transact (Doc.GetMap locks).
	meta := doc.GetMap(rootMeta)
	widgets := doc.GetMap(rootWidgets)

	doc.Transact(func(txn *crdt.Transaction) {
		meta.Set(txn, keyTitle, p.Title)

		settingsMap := crdt.NewMapPrelim()
		for _, k := range sortedKeys(settings) {
			settingsMap.Set(txn, k, settings[k])
		}
		meta.Set(txn, keySettings, settingsMap)

		variablesArr := crdt.NewArrayPrelim()
		for _, v := range variables {
			vm := crdt.NewMapPrelim()
			for _, k := range sortedKeys(v) {
				vm.Set(txn, k, v[k])
			}
			variablesArr.PushType(txn, vm)
		}
		meta.Set(txn, keyVariables, variablesArr)

		for _, pw := range prepared {
			wm := crdt.NewMapPrelim()
			wm.Set(txn, keyType, pw.w.Type)
			wm.Set(txn, keyConnectorID, stringOrEmpty(pw.w.ConnectorID))
			wm.Set(txn, keyLanguage, pw.w.Language)
			wm.Set(txn, keyNotebookID, stringOrEmpty(pw.w.NotebookID))
			wm.Set(txn, keyCellID, stringOrEmpty(pw.w.CellID))
			wm.Set(txn, keyConfig, pw.config)

			layout := crdt.NewMapPrelim()
			layout.Set(txn, keyRow, int64(pw.w.Layout.Row))
			layout.Set(txn, keyCol, int64(pw.w.Layout.Col))
			layout.Set(txn, keyWidth, int64(pw.w.Layout.Width))
			layout.Set(txn, keyHeight, int64(pw.w.Layout.Height))
			wm.Set(txn, keyLayout, layout)

			query := crdt.NewTextPrelim()
			if pw.w.Query != nil && *pw.w.Query != "" {
				query.Insert(txn, 0, *pw.w.Query, nil)
			}
			wm.Set(txn, keyQuery, query)

			widgets.Set(txn, pw.id, wm)
		}
	})

	return doc.EncodeStateAsUpdate(), nil
}

// normalizeMap normalizes every value of a settings/variables map into the
// exact type domain ygo's WriteAny accepts. label prefixes errors.
func normalizeMap(label string, m map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		nv, err := normalizeForDoc(v)
		if err != nil {
			return nil, fmt.Errorf("%s[%q]: %w", label, k, err)
		}
		out[k] = nv
	}
	return out, nil
}

// normalizeForDoc converts v into a value ygo can encode. Scalars and exact
// []any / map[string]any containers pass through recursively; any other type
// (structs, named slices/maps, json.Number, ...) is converted via
// encoding/json, which yields JSON's own type domain (float64 numbers).
// NaN and ±Inf are rejected: they are not valid JSON and cannot be represented
// in the derived Postgres JSONB rows.
func normalizeForDoc(v any) (any, error) {
	switch t := v.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return t, nil
	case float32:
		if math.IsNaN(float64(t)) || math.IsInf(float64(t), 0) {
			return nil, fmt.Errorf("non-finite number %v is not JSON-representable", t)
		}
		return t, nil
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil, fmt.Errorf("non-finite number %v is not JSON-representable", t)
		}
		return t, nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			ne, err := normalizeForDoc(e)
			if err != nil {
				return nil, err
			}
			out[i] = ne
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			ne, err := normalizeForDoc(e)
			if err != nil {
				return nil, fmt.Errorf("[%q]: %w", k, err)
			}
			out[k] = ne
		}
		return out, nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

// stringOrEmpty maps a nil pointer to the document's "" placeholder.
func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// sortedKeys returns m's keys in sorted order, so Seed stages map entries in a
// stable order regardless of Go map iteration order. (Encoded state bytes still
// vary between calls because crdt.New assigns a random client ID.)
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
