package dashboarddoc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/encoding"
)

// Project decodes a dashboard document (Yjs state) into a projection.
//
// An empty state decodes to an empty projection (zero Title, empty non-nil
// Settings/Variables/Widgets), matching Seed of an empty Projection.
//
// Wrong field types in the root meta map (non-string title, settings that is
// not a Y.Map, variables that is not a Y.Array) are returned as an error.
// Per-widget and per-variable problems skip just that entry and land in
// Projection.Warnings; see the package documentation for the split, including
// the root-container-kind limitation. Warnings are ordered by sorted widget ID
// (variables keep array order).
func Project(state []byte) (*Projection, error) {
	doc := crdt.New()
	if len(state) > 0 {
		if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
			return nil, fmt.Errorf("decode dashboard document: %w", err)
		}
	}

	proj := &Projection{
		Settings:  map[string]any{},
		Variables: []map[string]any{},
		Widgets:   map[string]WidgetDoc{},
	}

	meta := doc.GetMap(rootMeta)

	if v, ok := meta.Get(keyTitle); ok {
		title, isString := v.(string)
		if !isString {
			return nil, fmt.Errorf("meta.%s: got %T, want string", keyTitle, v)
		}
		proj.Title = title
	}

	if v, ok := meta.Get(keySettings); ok {
		settings, isMap := v.(*crdt.YMap)
		if !isMap {
			return nil, fmt.Errorf("meta.%s: got %T, want Y.Map", keySettings, v)
		}
		for _, k := range settings.Keys() {
			val, _ := settings.Get(k)
			nv, ok := normalizeDecoded(val)
			if !ok {
				proj.Warnings = append(proj.Warnings,
					fmt.Sprintf("settings[%q]: cannot convert %T to a plain value", k, val))
				continue
			}
			proj.Settings[k] = nv
		}
	}

	if v, ok := meta.Get(keyVariables); ok {
		variables, isArray := v.(*crdt.YArray)
		if !isArray {
			return nil, fmt.Errorf("meta.%s: got %T, want Y.Array", keyVariables, v)
		}
		for i := 0; i < variables.Len(); i++ {
			elem := variables.Get(i)
			vm, isMap := elem.(*crdt.YMap)
			if !isMap {
				proj.Warnings = append(proj.Warnings,
					fmt.Sprintf("variables[%d]: got %T, want Y.Map", i, elem))
				continue
			}
			m := make(map[string]any, len(vm.Keys()))
			for _, k := range vm.Keys() {
				val, _ := vm.Get(k)
				nv, ok := normalizeDecoded(val)
				if !ok {
					proj.Warnings = append(proj.Warnings,
						fmt.Sprintf("variables[%d][%q]: cannot convert %T to a plain value", i, k, val))
					continue
				}
				m[k] = nv
			}
			proj.Variables = append(proj.Variables, m)
		}
	}

	widgets := doc.GetMap(rootWidgets)
	ids := widgets.Keys()
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			proj.Warnings = append(proj.Warnings,
				fmt.Sprintf("widget %q: key is not a valid UUID", id))
			continue
		}
		v, _ := widgets.Get(id)
		wm, isMap := v.(*crdt.YMap)
		if !isMap {
			proj.Warnings = append(proj.Warnings,
				fmt.Sprintf("widget %q: got %T, want Y.Map", id, v))
			continue
		}
		w, warning := projectWidget(id, wm)
		if warning != "" {
			proj.Warnings = append(proj.Warnings, warning)
			continue
		}
		w.ID = id
		proj.Widgets[id] = w
	}

	return proj, nil
}

// projectWidget converts one widgets-map entry. It returns a non-empty warning
// (and a zero WidgetDoc) when the widget fails validation.
func projectWidget(id string, m *crdt.YMap) (WidgetDoc, string) {
	fail := func(format string, args ...any) (WidgetDoc, string) {
		return WidgetDoc{}, fmt.Sprintf("widget %q: %s", id, fmt.Sprintf(format, args...))
	}

	typ, ok := readString(m, keyType)
	if !ok || !widgetTypes[typ] {
		return fail("unknown type %s", describeValue(m, keyType))
	}
	language, ok := readString(m, keyLanguage)
	if !ok || !queryLanguages[language] {
		return fail("unsupported language %s", describeValue(m, keyLanguage))
	}

	layout, reason := readLayout(m)
	if reason != "" {
		return fail("invalid layout: %s", reason)
	}

	config, reason := readConfig(m)
	if reason != "" {
		return fail("invalid config: %s", reason)
	}

	var query *string
	if v, ok := m.Get(keyQuery); ok {
		text, isText := v.(*crdt.YText)
		if !isText {
			return fail("query is %T, want Y.Text", v)
		}
		if s := text.ToString(); s != "" {
			query = &s
		}
	}

	w := WidgetDoc{
		Type:     typ,
		Layout:   layout,
		Language: language,
		Config:   config,
		Query:    query,
	}
	pointerFields := []struct {
		key string
		dst **string
	}{
		{keyConnectorID, &w.ConnectorID},
		{keyNotebookID, &w.NotebookID},
		{keyCellID, &w.CellID},
	}
	for _, f := range pointerFields {
		s, ok := readString(m, f.key)
		if !ok {
			return fail("%s is %s, want string", f.key, describeValue(m, f.key))
		}
		if s != "" {
			*f.dst = &s
		}
	}
	return w, ""
}

// readLayout reads the widget's layout map. A missing layout (or missing
// field) defaults to zero; a present-but-invalid field fails the widget.
// Integral float32/float64 fields are accepted (with a fit check) because
// Seed-written ints above int32 range arrive as float64 on the wire.
func readLayout(m *crdt.YMap) (Layout, string) {
	v, ok := m.Get(keyLayout)
	if !ok {
		return Layout{}, ""
	}
	lm, isMap := v.(*crdt.YMap)
	if !isMap {
		return Layout{}, fmt.Sprintf("%s is %T, want Y.Map", keyLayout, v)
	}

	var l Layout
	fields := []struct {
		key string
		dst *int
	}{
		{keyRow, &l.Row},
		{keyCol, &l.Col},
		{keyWidth, &l.Width},
		{keyHeight, &l.Height},
	}
	for _, f := range fields {
		fv, ok := lm.Get(f.key)
		if !ok {
			continue
		}
		n, reason := layoutFieldInt(fv)
		if reason != "" {
			return Layout{}, fmt.Sprintf("%s.%s %s", keyLayout, f.key, reason)
		}
		if n < 0 {
			return Layout{}, fmt.Sprintf("%s.%s is negative (%d)", keyLayout, f.key, n)
		}
		*f.dst = n
	}
	return l, ""
}

// layoutFieldInt converts a layout field value to a plain int, accepting
// ygo's integer encodings: int64, integral float64 (tag 123), and integral
// float32 (tag 124). reason is non-empty when the value is not usable.
func layoutFieldInt(v any) (int, string) {
	switch t := v.(type) {
	case int64:
		if int64(int(t)) != t {
			return 0, fmt.Sprintf("does not fit in int (%d)", t)
		}
		return int(t), ""
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) || t != math.Trunc(t) {
			return 0, fmt.Sprintf("is not an integer (%v)", t)
		}
		n, ok := integralFloatToInt(t)
		if !ok {
			return 0, fmt.Sprintf("does not fit in int (%v)", t)
		}
		return n, ""
	case float32:
		return layoutFieldInt(float64(t))
	default:
		return 0, fmt.Sprintf("is %T, want integer", v)
	}
}

// readConfig reads the widget's config JSON string. A missing or empty value
// projects as an empty map; anything that is not a JSON object fails.
func readConfig(m *crdt.YMap) (map[string]any, string) {
	v, ok := m.Get(keyConfig)
	if !ok {
		return map[string]any{}, ""
	}
	s, isString := v.(string)
	if !isString {
		return nil, fmt.Sprintf("%s is %T, want JSON string", keyConfig, v)
	}
	if strings.TrimSpace(s) == "" {
		return map[string]any{}, ""
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(s), &config); err != nil {
		return nil, fmt.Sprintf("%s is not a JSON object: %v", keyConfig, err)
	}
	if config == nil {
		config = map[string]any{}
	}
	return config, ""
}

// readString reads a string field. A missing key reads as "" with ok=true; a
// present non-string value returns ok=false.
func readString(m *crdt.YMap, key string) (value string, ok bool) {
	v, exists := m.Get(key)
	if !exists {
		return "", true
	}
	s, isString := v.(string)
	if !isString {
		return "", false
	}
	return s, true
}

// describeValue renders a field's value for a warning message: a quoted string
// for strings, the Go type otherwise, "<missing>" when absent.
func describeValue(m *crdt.YMap, key string) string {
	v, ok := m.Get(key)
	if !ok {
		return "<missing>"
	}
	if s, isString := v.(string); isString {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("<%T>", v)
}

// normalizeDecoded converts a value read out of the document into plain Go
// types, so no ygo encoding leaks out of Project:
//
//   - int64 → int when it fits (otherwise int64)
//   - encoding.BigInt → int64
//   - integral float32/float64 → int when they fit, otherwise float64
//   - []byte → its base64 string (matching encoding/json)
//   - nested shared types (*crdt.YMap / *crdt.YArray / *crdt.YText) → their
//     ToJSON payload, normalized recursively
//   - []any and map[string]any recurse
//
// ok is false when a shared type cannot be converted to plain values; callers
// drop the entry and record a warning.
func normalizeDecoded(v any) (any, bool) {
	switch t := v.(type) {
	case int64:
		if int64(int(t)) == t {
			return int(t), true
		}
		return t, true
	case encoding.BigInt:
		return int64(t), true
	case float32:
		return normalizeDecoded(float64(t))
	case float64:
		if n, ok := integralFloatToInt(t); ok {
			return n, true
		}
		return t, true
	case []byte:
		return base64.StdEncoding.EncodeToString(t), true
	case *crdt.YMap:
		return normalizeSharedJSON(t.ToJSON)
	case *crdt.YArray:
		return normalizeSharedJSON(t.ToJSON)
	case *crdt.YText:
		return normalizeSharedJSON(t.ToJSON)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			ne, ok := normalizeDecoded(e)
			if !ok {
				return nil, false
			}
			out[i] = ne
		}
		return out, true
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			ne, ok := normalizeDecoded(e)
			if !ok {
				return nil, false
			}
			out[k] = ne
		}
		return out, true
	default:
		return v, true
	}
}

// normalizeSharedJSON converts a nested shared type's ToJSON payload into
// plain Go values. ygo's ToJSON unwraps nested shared types recursively.
func normalizeSharedJSON(toJSON func() ([]byte, error)) (any, bool) {
	raw, err := toJSON()
	if err != nil {
		return nil, false
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return normalizeDecoded(out)
}

// integralFloatToInt returns f as an int when f is integral and fits in int.
func integralFloatToInt(f float64) (int, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, false
	}
	// float64(1<<63) is exactly 2^63, one past the largest int64; reject it
	// before the conversion (out-of-range float→int conversions are
	// implementation-defined).
	if f >= 1<<63 || f < -(1<<63) {
		return 0, false
	}
	n := int64(f)
	if int64(int(n)) != n {
		return 0, false
	}
	return int(n), true
}
