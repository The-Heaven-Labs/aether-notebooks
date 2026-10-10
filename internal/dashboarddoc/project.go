package dashboarddoc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// Project decodes a dashboard document (Yjs state) into a projection.
//
// An empty state decodes to an empty projection (zero Title, empty non-nil
// Settings/Variables/Widgets), matching Seed of an empty Projection.
//
// Unrecoverable corruption — undecodable state bytes or wrong root types — is
// returned as an error. Per-widget and per-variable problems skip just that
// entry and land in Projection.Warnings; see the package documentation for the
// split. Warnings are ordered by sorted widget ID (variables keep array order).
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
			proj.Settings[k] = normalizeDecoded(val)
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
				m[k] = normalizeDecoded(val)
			}
			proj.Variables = append(proj.Variables, m)
		}
	}

	widgets := doc.GetMap(rootWidgets)
	ids := widgets.Keys()
	sort.Strings(ids)
	for _, id := range ids {
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
		n, isInt := fv.(int64)
		if !isInt {
			return Layout{}, fmt.Sprintf("%s.%s is %T, want integer", keyLayout, f.key, fv)
		}
		if n < 0 {
			return Layout{}, fmt.Sprintf("%s.%s is negative (%d)", keyLayout, f.key, n)
		}
		if int64(int(n)) != n {
			return Layout{}, fmt.Sprintf("%s.%s does not fit in int (%d)", keyLayout, f.key, n)
		}
		*f.dst = int(n)
	}
	return l, ""
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
// types: int64 → int when it fits, float32 → float64, recursively through
// []any and map[string]any. Values that do not fit stay int64.
func normalizeDecoded(v any) any {
	switch t := v.(type) {
	case int64:
		if int64(int(t)) == t {
			return int(t)
		}
		return t
	case float32:
		return float64(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeDecoded(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = normalizeDecoded(e)
		}
		return out
	default:
		return v
	}
}
