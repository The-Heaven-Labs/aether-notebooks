package dashboarddoc

import (
	"errors"
	"fmt"

	"github.com/reearth/ygo/crdt"
)

// This file implements the backend-originated mutation operations on a
// dashboard document: REST widget CRUD, field-level widget writes, layout
// writes, SQL replacement, and meta (title/settings/variables) writes.
//
// Each op takes the current document state bytes, decodes them into a live ygo
// document, applies the mutation in one transaction, and returns the full
// merged state bytes. Callers persist the result and publish it to relays. An
// empty or nil state is treated as a fresh empty document, so ops also work on
// a dashboard that has never been seeded.
//
// Inputs are validated before the document is touched, so a failed op returns
// no state (never partially mutated bytes). State produced by an op projects
// without warnings, with one deliberate exception: SetQuery can clear a
// connector widget's SQL, producing the transient connector-without-query
// shape that Project skips with a warning until a query is set again (the
// materializer then skips that widget rather than aborting the store).
//
// # Merge semantics
//
// UpdateLayout, SetQuery and the field-level setters (SetWidgetConnector,
// SetWidgetType, SetWidgetConfig, SetWidgetLanguage) write individual fields
// and merge at the CRDT level: a concurrent editor's changes to other fields
// or other widgets survive a merge of the two states (pinned by the
// concurrent-merge tests). None of them replaces the widget's Y.Map or its
// query Y.Text, so a keystroke typed in the editor while a REST write is in
// flight is never dropped by the write.
//
// UpsertWidget, DeleteWidget and the settings/variables branches of UpdateMeta
// are whole-value writes: the entire widget (or settings/variables container)
// is replaced or removed as one last-write-wins unit, so concurrent edits
// inside it are dropped when the states merge. That is deliberate — these ops
// back REST forms whose submitted value is authoritative. UpsertWidget in
// particular replaces the widget's Y.Text rather than editing it in place, so
// it does not have to reconcile a form submission with in-flight keystrokes
// from the editor; partial widget updates therefore go through the field-level
// setters instead.
//
// A widget ID that is missing from the document is an error (ErrWidgetNotFound)
// for DeleteWidget, UpdateLayout, SetQuery and the field-level setters rather
// than a silent no-op, so the REST layer can answer 404.

// ErrWidgetNotFound reports that an operation targets a widget ID that is not
// present in the document. Malformed (non-UUID) widget IDs are plain
// validation errors instead.
var ErrWidgetNotFound = errors.New("widget not found")

// decodeDoc builds a live document from state bytes. Empty or nil state
// decodes to a fresh empty document, so every op also works on a dashboard
// that has not been seeded yet. Project shares it, so both entry points report
// the same decode error.
func decodeDoc(state []byte) (*crdt.Doc, error) {
	doc := crdt.New()
	if len(state) > 0 {
		if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
			return nil, fmt.Errorf("decode dashboard document: %w", err)
		}
	}
	return doc, nil
}

// UpsertWidget inserts or replaces one widget and returns the merged state.
//
// The widget is validated exactly like Seed validates widgets (UUID ID, known
// type, supported language, non-negative layout, JSON-representable config).
// Replacement deletes the previous widget map and attaches a fresh one, so
// fields the new widget does not carry (connector, notebook/cell link, query)
// are reset rather than merged over the old values.
func UpsertWidget(state []byte, w WidgetDoc) ([]byte, error) {
	canonical, config, err := validateWidget(w.ID, w)
	if err != nil {
		return nil, err
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}

	// Root containers must be resolved before Transact (Doc.GetMap locks).
	widgets := doc.GetMap(rootWidgets)

	doc.Transact(func(txn *crdt.Transaction) {
		widgets.Delete(txn, canonical)
		widgets.Set(txn, canonical, widgetPrelim(txn, w, config))
	})

	return doc.EncodeStateAsUpdate(), nil
}

// DeleteWidget removes the widget with the given ID and returns the merged
// state. A widget that is not present is an error (ErrWidgetNotFound), not a
// no-op. The ID is canonicalized, so any UUID spelling addresses the same
// widget.
func DeleteWidget(state []byte, widgetID string) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	if _, err := findWidget(doc, canonical); err != nil {
		return nil, err
	}

	widgets := doc.GetMap(rootWidgets)
	doc.Transact(func(txn *crdt.Transaction) {
		widgets.Delete(txn, canonical)
	})

	return doc.EncodeStateAsUpdate(), nil
}

// UpdateLayout writes all four layout fields of one widget and returns the
// merged state. A missing layout map is created; a present-but-invalid layout
// map is an error, as is a missing widget (ErrWidgetNotFound) or a negative
// layout. The ID is canonicalized, so any UUID spelling addresses the same
// widget.
func UpdateLayout(state []byte, widgetID string, l Layout) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	if l.Row < 0 || l.Col < 0 || l.Width < 0 || l.Height < 0 {
		return nil, fmt.Errorf("widget %q: negative layout %+v", canonical, l)
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	wm, err := findWidget(doc, canonical)
	if err != nil {
		return nil, err
	}

	// Read the current layout before the transaction: YMap.Get must not be
	// called from inside a Transact callback.
	var layout *crdt.YMap
	if v, ok := wm.Get(keyLayout); ok {
		lm, isMap := v.(*crdt.YMap)
		if !isMap {
			return nil, fmt.Errorf("widget %q: layout is %T, want Y.Map", canonical, v)
		}
		layout = lm
	}

	doc.Transact(func(txn *crdt.Transaction) {
		if layout == nil {
			lm := crdt.NewMapPrelim()
			lm.Set(txn, keyRow, int64(l.Row))
			lm.Set(txn, keyCol, int64(l.Col))
			lm.Set(txn, keyWidth, int64(l.Width))
			lm.Set(txn, keyHeight, int64(l.Height))
			wm.Set(txn, keyLayout, lm)
			return
		}
		layout.Set(txn, keyRow, int64(l.Row))
		layout.Set(txn, keyCol, int64(l.Col))
		layout.Set(txn, keyWidth, int64(l.Width))
		layout.Set(txn, keyHeight, int64(l.Height))
	})

	return doc.EncodeStateAsUpdate(), nil
}

// SetQuery replaces one widget's SQL text in place and returns the merged
// state. The existing Y.Text is preserved and edited (delete + insert in one
// transaction), so concurrent editor characters still merge at the CRDT
// level. An empty sql clears the text; a widget whose query field is not a
// Y.Text is an error, as is a missing widget (ErrWidgetNotFound). A widget
// with no query field at all gets a fresh Y.Text. The ID is canonicalized, so
// any UUID spelling addresses the same widget.
func SetQuery(state []byte, widgetID, sql string) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	wm, err := findWidget(doc, canonical)
	if err != nil {
		return nil, err
	}

	// Read the current text before the transaction: YMap.Get must not be
	// called from inside a Transact callback.
	var text *crdt.YText
	if v, ok := wm.Get(keyQuery); ok {
		t, isText := v.(*crdt.YText)
		if !isText {
			return nil, fmt.Errorf("widget %q: query is %T, want Y.Text", canonical, v)
		}
		text = t
	}

	doc.Transact(func(txn *crdt.Transaction) {
		if text == nil {
			qt := crdt.NewTextPrelim()
			qt.Insert(txn, 0, sql, nil)
			wm.Set(txn, keyQuery, qt)
			return
		}
		text.Delete(txn, 0, text.Len())
		text.Insert(txn, 0, sql, nil)
	})

	return doc.EncodeStateAsUpdate(), nil
}

// SetWidgetConnector sets one widget's connector_id and clears its
// notebook_id/cell_id in the same transaction, and returns the merged state.
// Clearing mirrors the source invariant (widgets_source_check): a connector
// widget cannot reference a notebook or cell, so the write always leaves the
// widget in the connector shape. An empty connector ID is rejected, and a
// missing widget is an error (ErrWidgetNotFound). The ID is canonicalized, so
// any UUID spelling addresses the same widget.
func SetWidgetConnector(state []byte, widgetID, connectorID string) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	if connectorID == "" {
		return nil, fmt.Errorf("widget %q: connector_id cannot be empty", canonical)
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	wm, err := findWidget(doc, canonical)
	if err != nil {
		return nil, err
	}

	doc.Transact(func(txn *crdt.Transaction) {
		wm.Set(txn, keyConnectorID, connectorID)
		wm.Set(txn, keyNotebookID, "")
		wm.Set(txn, keyCellID, "")
	})

	return doc.EncodeStateAsUpdate(), nil
}

// SetWidgetType replaces one widget's type in place and returns the merged
// state. The type is validated exactly like validateWidget validates it
// (chart, table, metric, text); anything else is an error before the document
// is touched. A missing widget is an error (ErrWidgetNotFound). The ID is
// canonicalized, so any UUID spelling addresses the same widget.
func SetWidgetType(state []byte, widgetID, widgetType string) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	if !widgetTypes[widgetType] {
		return nil, fmt.Errorf("widget %q: unknown type %q", canonical, widgetType)
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	wm, err := findWidget(doc, canonical)
	if err != nil {
		return nil, err
	}

	doc.Transact(func(txn *crdt.Transaction) {
		wm.Set(txn, keyType, widgetType)
	})

	return doc.EncodeStateAsUpdate(), nil
}

// SetWidgetConfig replaces one widget's config in place and returns the merged
// state. The map is normalized and encoded exactly like validateWidget encodes
// a widget config: a nil or empty map becomes "{}", shared yjs types and
// non-finite numbers are rejected, and the encoded string is capped at
// maxWidgetConfigBytes. Config is a whole-value field, so the JSON string is
// replaced rather than merged field-wise. A missing widget is an error
// (ErrWidgetNotFound). The ID is canonicalized, so any UUID spelling addresses
// the same widget.
func SetWidgetConfig(state []byte, widgetID string, config map[string]any) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	configJSON, err := configJSONForDoc(config)
	if err != nil {
		return nil, fmt.Errorf("widget %q: config: %w", canonical, err)
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	wm, err := findWidget(doc, canonical)
	if err != nil {
		return nil, err
	}

	doc.Transact(func(txn *crdt.Transaction) {
		wm.Set(txn, keyConfig, configJSON)
	})

	return doc.EncodeStateAsUpdate(), nil
}

// SetWidgetLanguage replaces one widget's language in place and returns the
// merged state. The language is validated against the document's allowed set
// ("" or "sql"), exactly like validateWidget validates it. A missing widget is
// an error (ErrWidgetNotFound). The ID is canonicalized, so any UUID spelling
// addresses the same widget.
func SetWidgetLanguage(state []byte, widgetID, language string) ([]byte, error) {
	canonical, err := canonicalWidgetID(widgetID)
	if err != nil {
		return nil, err
	}
	if !queryLanguages[language] {
		return nil, fmt.Errorf("widget %q: unsupported language %q", canonical, language)
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}
	wm, err := findWidget(doc, canonical)
	if err != nil {
		return nil, err
	}

	doc.Transact(func(txn *crdt.Transaction) {
		wm.Set(txn, keyLanguage, language)
	})

	return doc.EncodeStateAsUpdate(), nil
}

// UpdateMeta updates the document's title, settings and variables and returns
// the merged state.
//
// A nil title/settings/variables leaves that field unchanged; a non-nil value
// replaces the whole field (an empty non-nil map/slice clears it). Settings
// and variables values are validated exactly like Seed validates them
// (JSON-representable, no shared yjs types, no non-finite numbers).
func UpdateMeta(state []byte, title *string, settings map[string]any, variables []map[string]any) ([]byte, error) {
	// Validate and normalize everything before touching the document.
	var normalizedSettings map[string]any
	if settings != nil {
		var err error
		normalizedSettings, err = normalizeMap("settings", settings)
		if err != nil {
			return nil, err
		}
	}
	var normalizedVariables []map[string]any
	if variables != nil {
		normalizedVariables = make([]map[string]any, len(variables))
		for i, v := range variables {
			nv, err := normalizeMap(fmt.Sprintf("variables[%d]", i), v)
			if err != nil {
				return nil, err
			}
			normalizedVariables[i] = nv
		}
	}

	doc, err := decodeDoc(state)
	if err != nil {
		return nil, err
	}

	meta := doc.GetMap(rootMeta)

	doc.Transact(func(txn *crdt.Transaction) {
		if title != nil {
			meta.Set(txn, keyTitle, *title)
		}
		if settings != nil {
			meta.Delete(txn, keySettings)
			sm := crdt.NewMapPrelim()
			for _, k := range sortedKeys(normalizedSettings) {
				sm.Set(txn, k, normalizedSettings[k])
			}
			meta.Set(txn, keySettings, sm)
		}
		if variables != nil {
			meta.Delete(txn, keyVariables)
			arr := crdt.NewArrayPrelim()
			for _, v := range normalizedVariables {
				vm := crdt.NewMapPrelim()
				for _, k := range sortedKeys(v) {
					vm.Set(txn, k, v[k])
				}
				arr.PushType(txn, vm)
			}
			meta.Set(txn, keyVariables, arr)
		}
	})

	return doc.EncodeStateAsUpdate(), nil
}

// findWidget resolves the live widget map stored under id. It returns
// ErrWidgetNotFound when id is a valid UUID with no widget stored under it; a
// malformed UUID or a non-Y.Map entry is a plain validation error. The ID is
// canonicalized, so lookups accept any UUID spelling.
func findWidget(doc *crdt.Doc, id string) (*crdt.YMap, error) {
	canonical, err := canonicalWidgetID(id)
	if err != nil {
		return nil, err
	}
	widgets := doc.GetMap(rootWidgets)
	v, ok := widgets.Get(canonical)
	if !ok {
		return nil, fmt.Errorf("widget %q: %w", canonical, ErrWidgetNotFound)
	}
	wm, isMap := v.(*crdt.YMap)
	if !isMap {
		return nil, fmt.Errorf("widget %q: got %T, want Y.Map", canonical, v)
	}
	return wm, nil
}
