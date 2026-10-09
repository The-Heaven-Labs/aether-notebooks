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

// compatBuildSeed builds the exact planned dashboard document:
//
//	meta    Y.Map { title, settings Y.Map, variables Y.Array<Y.Map> }
//	widgets Y.Map<uuid, Y.Map { type, config, layout Y.Map, query Y.Text }>
func compatBuildSeed(t *testing.T) []byte {
	t.Helper()
	doc := crdt.New()

	// Root containers must be resolved before Transact (Doc.GetMap locks).
	meta := doc.GetMap("meta")
	widgets := doc.GetMap("widgets")

	doc.Transact(func(txn *crdt.Transaction) {
		meta.Set(txn, "title", "Untitled Dashboard")

		settings := crdt.NewMapPrelim()
		settings.Set(txn, "grid_cols", int64(12))
		settings.Set(txn, "auto_refresh_seconds", int64(0))
		settings.Set(txn, "query_cache_seconds", int64(30))
		settings.Set(txn, "public_live", false)
		meta.Set(txn, "settings", settings)

		variables := crdt.NewArrayPrelim()
		region := crdt.NewMapPrelim()
		region.Set(txn, "name", "region")
		region.Set(txn, "label", "Region")
		region.Set(txn, "type", "select")
		region.Set(txn, "default", "us")
		region.Set(txn, "options", []any{"us", "eu"})
		region.Set(txn, "depends_on", []any{})
		variables.PushType(txn, region)
		meta.Set(txn, "variables", variables)

		w1 := crdt.NewMapPrelim()
		w1.Set(txn, "type", "chart")
		w1.Set(txn, "connector_id", "conn-1")
		w1.Set(txn, "language", "sql")
		w1.Set(txn, "notebook_id", "nb-1")
		w1.Set(txn, "cell_id", "cell-1")
		w1.Set(txn, "config", `{"kind":"bar"}`)
		layout1 := crdt.NewMapPrelim()
		layout1.Set(txn, "row", int64(0))
		layout1.Set(txn, "col", int64(0))
		layout1.Set(txn, "width", int64(6))
		layout1.Set(txn, "height", int64(4))
		w1.Set(txn, "layout", layout1)
		query1 := crdt.NewTextPrelim()
		query1.Insert(txn, 0, "SELECT 1", nil)
		w1.Set(txn, "query", query1)
		widgets.Set(txn, "w-1", w1)

		w2 := crdt.NewMapPrelim()
		w2.Set(txn, "type", "text")
		w2.Set(txn, "config", "{}")
		layout2 := crdt.NewMapPrelim()
		layout2.Set(txn, "row", int64(4))
		layout2.Set(txn, "col", int64(0))
		layout2.Set(txn, "width", int64(12))
		layout2.Set(txn, "height", int64(2))
		w2.Set(txn, "layout", layout2)
		query2 := crdt.NewTextPrelim()
		query2.Insert(txn, 0, "SELECT 2", nil)
		w2.Set(txn, "query", query2)
		widgets.Set(txn, "w-2", w2)
	})

	return doc.EncodeStateAsUpdate()
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
	require.ElementsMatch(t, []string{"w-1", "w-2", "w-js-1"}, widgets.Keys(),
		"widgets must contain the two seeded widgets plus the JS-added one")

	// Seeded widget, including the JS-appended Y.Text mutation.
	w1 := compatMap(t, widgets, "w-1")
	require.Equal(t, "chart", compatMapValue(t, w1, "type"))
	require.Equal(t, `{"kind":"bar"}`, compatMapValue(t, w1, "config"))
	layout1 := compatMap(t, w1, "layout")
	require.Equal(t, int64(6), compatMapValue(t, layout1, "width"))
	require.Equal(t, "SELECT 1\n-- mutated", compatText(t, w1, "query"))

	// Second seeded widget: untouched by the JS peer.
	w2 := compatMap(t, widgets, "w-2")
	require.Equal(t, "text", compatMapValue(t, w2, "type"))
	require.Equal(t, "SELECT 2", compatText(t, w2, "query"))

	// Widget created entirely by the JS peer, with nested layout + Y.Text.
	wjs := compatMap(t, widgets, "w-js-1")
	require.Equal(t, "table", compatMapValue(t, wjs, "type"))
	layoutJS := compatMap(t, wjs, "layout")
	require.Equal(t, int64(2), compatMapValue(t, layoutJS, "row"))
	require.Equal(t, int64(6), compatMapValue(t, layoutJS, "width"))
	require.Equal(t, "SELECT 3", compatText(t, wjs, "query"))
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
