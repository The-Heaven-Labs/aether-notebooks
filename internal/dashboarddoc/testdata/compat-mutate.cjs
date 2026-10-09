// compat-mutate.cjs is the JS half of TestCompat_NestedStructures in
// ../compat_test.go: it loads a ygo-seeded dashboard document, asserts the
// planned nested structure, mutates it the way the browser editor would, and
// writes the encoded result back for Go to project.
//
// Runs under CommonJS with `yjs` resolved via NODE_PATH=<repo>/web/node_modules
// (set by the Go test):
//   node compat-mutate.cjs <inPath> <outPath>
'use strict'
const assert = require('node:assert/strict')
const fs = require('node:fs')
const Y = require('yjs')

const [inPath, outPath] = process.argv.slice(2)
if (!inPath || !outPath) {
  console.error('usage: node compat-mutate.cjs <inPath> <outPath>')
  process.exit(2)
}

const doc = new Y.Doc()
Y.applyUpdate(doc, new Uint8Array(fs.readFileSync(inPath)))

// --- assert the structure ygo seeded ---
const meta = doc.getMap('meta')
assert.ok(meta instanceof Y.Map, 'meta must be a Y.Map')
assert.equal(meta.get('title'), 'Untitled Dashboard')

const settings = meta.get('settings')
assert.ok(settings instanceof Y.Map, 'meta.settings must be a nested Y.Map')
assert.equal(settings.get('grid_cols'), 12)
assert.equal(settings.get('auto_refresh_seconds'), 0)
assert.equal(settings.get('query_cache_seconds'), 30)
assert.equal(settings.get('public_live'), false)

const variables = meta.get('variables')
assert.ok(variables instanceof Y.Array, 'meta.variables must be a Y.Array')
assert.equal(variables.length, 1)
const v0 = variables.get(0)
assert.ok(v0 instanceof Y.Map, 'variables[0] must be a Y.Map inside the array')
assert.equal(v0.get('name'), 'region')
assert.equal(v0.get('type'), 'select')
assert.deepEqual(v0.get('options'), ['us', 'eu'])
assert.deepEqual(v0.get('depends_on'), [])

const widgets = doc.getMap('widgets')
const w1 = widgets.get('w-1')
assert.ok(w1 instanceof Y.Map, 'widgets.w-1 must be a Y.Map')
assert.equal(w1.get('type'), 'chart')
assert.equal(w1.get('connector_id'), 'conn-1')
assert.equal(w1.get('config'), '{"kind":"bar"}')
const layout1 = w1.get('layout')
assert.ok(layout1 instanceof Y.Map, 'widget layout must be a nested Y.Map')
assert.deepEqual(
  { row: layout1.get('row'), col: layout1.get('col'), width: layout1.get('width'), height: layout1.get('height') },
  { row: 0, col: 0, width: 6, height: 4 },
)
const q1 = w1.get('query')
assert.ok(q1 instanceof Y.Text, 'widget query must be a Y.Text')
assert.equal(q1.toString(), 'SELECT 1')
assert.ok(widgets.get('w-2') instanceof Y.Map, 'widgets.w-2 must be a Y.Map')

// --- mutate the way the editor would ---
doc.transact(() => {
  q1.insert(q1.length, '\n-- mutated')

  const wjs = new Y.Map()
  wjs.set('type', 'table')
  wjs.set('config', '{}')
  const layout = new Y.Map()
  layout.set('row', 2)
  layout.set('col', 0)
  layout.set('width', 6)
  layout.set('height', 4)
  wjs.set('layout', layout)
  const query = new Y.Text()
  query.insert(0, 'SELECT 3')
  wjs.set('query', query)
  widgets.set('w-js-1', wjs)
})

fs.writeFileSync(outPath, Y.encodeStateAsUpdate(doc))
console.log(`mutate: OK (${fs.statSync(inPath).size} bytes in, ${fs.statSync(outPath).size} bytes out)`)
