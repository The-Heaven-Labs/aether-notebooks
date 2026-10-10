package dashboarddoc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
)

// TestCompat_NestedStructures is the compatibility gate for the live dashboard
// document format. It seeds the planned document shape with ygo (Go), hands the
// encoded state to the JS yjs package the relay uses, which asserts the
// structure, mutates it (appends text to a nested Y.Text and adds a whole
// widget), and encodes the result; Go then projects the mutated state and
// verifies both the mutations and the surviving originals.
//
// The node half needs a JS toolchain, so the test skips (rather than fails)
// when node or web/node_modules/yjs is absent — e.g. a Go-only CI job. The
// committed driver is internal/dashboarddoc/testdata/compat-mutate.cjs.
func TestCompat_NestedStructures(t *testing.T) {
	repoRoot := compatRepoRoot(t)
	yjsDir := filepath.Join(repoRoot, "web", "node_modules", "yjs")
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found in PATH; skipping Go<->JS Yjs compatibility check")
	}
	if _, err := os.Stat(yjsDir); err != nil {
		t.Skipf("yjs not installed at %s; run `npm install` in web/ to enable this test", yjsDir)
	}

	scriptPath := filepath.Join(compatSourceDir(t), "testdata", "compat-mutate.cjs")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("compat mutate script missing at %s: %v", scriptPath, err)
	}

	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.bin")
	mutatedPath := filepath.Join(dir, "mutated.bin")

	seed := compatBuildSeed(t)
	require.NoError(t, os.WriteFile(seedPath, seed, 0o644), "write seed state")
	t.Logf("seed state: %d bytes at %s", len(seed), seedPath)

	cmd := exec.Command(nodePath, scriptPath, seedPath, mutatedPath)
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(repoRoot, "web", "node_modules"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "node compat mutation failed:\n%s", out)
	t.Logf("node: %s", out)

	mutated, err := os.ReadFile(mutatedPath)
	require.NoError(t, err, "read mutated state")
	require.NotEmpty(t, mutated)
	t.Logf("mutated state: %d bytes", len(mutated))

	compatAssertProjection(t, mutated)
}

// compatSourceDir returns the directory containing this test file
// (<repo>/internal/dashboarddoc), derived from the build-time file path.
func compatSourceDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed to locate the test file")
	return filepath.Dir(file)
}

// compatRepoRoot walks up from the test file: internal/dashboarddoc -> internal
// -> repo root.
func compatRepoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(filepath.Dir(compatSourceDir(t)))
}

// Fixed widget UUIDs shared with testdata/compat-mutate.cjs.
const (
	compatWidget1  = "11111111-1111-4111-8111-111111111111"
	compatWidget2  = "22222222-2222-4222-8222-222222222222"
	compatWidgetJS = "33333333-3333-4333-8333-333333333333"
)

// compatBuildSeed builds the exact planned dashboard document through the
// production Seed path (not hand-rolled ygo calls), so this gate fails if Seed
// ever regresses the Go<->JS representation:
//
//	meta    Y.Map { title, settings Y.Map, variables Y.Array<Y.Map> }
//	widgets Y.Map<uuid, Y.Map { type, config, layout Y.Map, query Y.Text }>
func compatBuildSeed(t *testing.T) []byte {
	t.Helper()
	connectorID := "conn-1"
	query1 := "SELECT 1"
	query2 := "SELECT 2"

	state, err := Seed(Projection{
		Title: "Untitled Dashboard",
		Settings: map[string]any{
			"grid_cols":            12,
			"auto_refresh_seconds": 0,
			"query_cache_seconds":  30,
			"public_live":          false,
		},
		Variables: []map[string]any{{
			"name":       "region",
			"label":      "Region",
			"type":       "select",
			"default":    "us",
			"options":    []any{"us", "eu"},
			"depends_on": []any{},
		}},
		Widgets: map[string]WidgetDoc{
			compatWidget1: {
				ID:          compatWidget1,
				Type:        "chart",
				Layout:      Layout{Row: 0, Col: 0, Width: 6, Height: 4},
				ConnectorID: &connectorID,
				Query:       &query1,
				Language:    "sql",
				Config:      map[string]any{"kind": "bar"},
			},
			compatWidget2: {
				ID:     compatWidget2,
				Type:   "text",
				Layout: Layout{Row: 4, Col: 0, Width: 12, Height: 2},
				Query:  &query2,
				Config: map[string]any{},
			},
		},
	})
	require.NoError(t, err, "seed compat document")
	return state
}

// compatAssertProjection loads a state produced by the JS peer (seed + its
// mutations) and checks that every part of the planned shape is reachable and
// carries the expected value.
func compatAssertProjection(t *testing.T, state []byte) {
	t.Helper()
	doc := crdt.New()
	require.NoError(t, crdt.ApplyUpdateV1(doc, state, nil), "apply mutated state")

	meta := doc.GetMap("meta")

	titleAny, ok := meta.Get("title")
	require.True(t, ok, "meta.title missing")
	require.Equal(t, "Untitled Dashboard", titleAny)

	settingsAny, ok := meta.Get("settings")
	require.True(t, ok, "meta.settings missing (nested Y.Map unreachable)")
	settings, ok := settingsAny.(*crdt.YMap)
	require.True(t, ok, "meta.settings is %T, want *crdt.YMap", settingsAny)
	require.Equal(t, int64(12), compatMapValue(t, settings, "grid_cols"))
	require.Equal(t, int64(0), compatMapValue(t, settings, "auto_refresh_seconds"))
	require.Equal(t, int64(30), compatMapValue(t, settings, "query_cache_seconds"))
	require.Equal(t, false, compatMapValue(t, settings, "public_live"))

	variablesAny, ok := meta.Get("variables")
	require.True(t, ok, "meta.variables missing (nested Y.Array unreachable)")
	variables, ok := variablesAny.(*crdt.YArray)
	require.True(t, ok, "meta.variables is %T, want *crdt.YArray", variablesAny)
	require.Equal(t, 1, variables.Len(), "variables must survive the JS round-trip")
	region, ok := variables.Get(0).(*crdt.YMap)
	require.True(t, ok, "variables[0] is %T, want *crdt.YMap", variables.Get(0))
	require.Equal(t, "region", compatMapValue(t, region, "name"))
	require.Equal(t, "select", compatMapValue(t, region, "type"))
	require.Equal(t, []any{"us", "eu"}, compatMapValue(t, region, "options"))
	require.Equal(t, []any{}, compatMapValue(t, region, "depends_on"))

	widgets := doc.GetMap("widgets")
	require.ElementsMatch(t, []string{compatWidget1, compatWidget2, compatWidgetJS}, widgets.Keys(),
		"widgets must contain the two seeded widgets plus the JS-added one")

	// Seeded widget, including the JS-appended Y.Text mutation.
	w1 := compatMap(t, widgets, compatWidget1)
	require.Equal(t, "chart", compatMapValue(t, w1, "type"))
	require.Equal(t, `{"kind":"bar"}`, compatMapValue(t, w1, "config"))
	layout1 := compatMap(t, w1, "layout")
	require.Equal(t, int64(6), compatMapValue(t, layout1, "width"))
	require.Equal(t, "SELECT 1\n-- mutated", compatText(t, w1, "query"))

	// Second seeded widget: untouched by the JS peer.
	w2 := compatMap(t, widgets, compatWidget2)
	require.Equal(t, "text", compatMapValue(t, w2, "type"))
	require.Equal(t, "SELECT 2", compatText(t, w2, "query"))

	// Widget created entirely by the JS peer, with nested layout + Y.Text.
	wjs := compatMap(t, widgets, compatWidgetJS)
	require.Equal(t, "table", compatMapValue(t, wjs, "type"))
	layoutJS := compatMap(t, wjs, "layout")
	require.Equal(t, int64(2), compatMapValue(t, layoutJS, "row"))
	require.Equal(t, int64(6), compatMapValue(t, layoutJS, "width"))
	require.Equal(t, "SELECT 3", compatText(t, wjs, "query"))

	// The production projection must accept the JS-mutated state without
	// warnings: this pins that Seed and Project agree with the JS peer.
	proj, err := Project(state)
	require.NoError(t, err, "Project on JS-mutated state")
	require.Empty(t, proj.Warnings, "JS-mutated state must project without warnings")
	require.Equal(t, "Untitled Dashboard", proj.Title)
	require.Len(t, proj.Widgets, 3)
	require.Contains(t, proj.Widgets, compatWidgetJS)

	pw1 := proj.Widgets[compatWidget1]
	require.NotNil(t, pw1.Query)
	require.Equal(t, "SELECT 1\n-- mutated", *pw1.Query)
	pw2 := proj.Widgets[compatWidget2]
	require.NotNil(t, pw2.Query)
	require.Equal(t, "SELECT 2", *pw2.Query)
	pwjs := proj.Widgets[compatWidgetJS]
	require.NotNil(t, pwjs.Query)
	require.Equal(t, "SELECT 3", *pwjs.Query)
	require.Equal(t, Layout{Row: 2, Col: 0, Width: 6, Height: 4}, pwjs.Layout)
}

// compatMap returns the nested Y.Map stored under key, failing the test if the
// key is missing or holds a different type.
func compatMap(t *testing.T, m *crdt.YMap, key string) *crdt.YMap {
	t.Helper()
	v, ok := m.Get(key)
	require.Truef(t, ok, "%s missing", key)
	nested, isMap := v.(*crdt.YMap)
	require.Truef(t, isMap, "%s is %T, want *crdt.YMap", key, v)
	return nested
}

// compatMapValue returns a plain (non-shared-type) value stored under key.
func compatMapValue(t *testing.T, m *crdt.YMap, key string) any {
	t.Helper()
	v, ok := m.Get(key)
	require.Truef(t, ok, "%s missing", key)
	return v
}

// compatText returns the string content of the nested Y.Text stored under key.
func compatText(t *testing.T, m *crdt.YMap, key string) string {
	t.Helper()
	v, ok := m.Get(key)
	require.Truef(t, ok, "%s missing", key)
	text, isText := v.(*crdt.YText)
	require.Truef(t, isText, "%s is %T, want *crdt.YText", key, v)
	return text.ToString()
}
