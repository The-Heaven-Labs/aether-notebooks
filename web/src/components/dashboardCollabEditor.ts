import * as Y from 'yjs'
import { yCollab, ySyncFacet, YSyncConfig } from 'y-codemirror.next'
import type { EditorView } from '@codemirror/view'
import type { Compartment } from '@codemirror/state'
import type { DashboardCollab } from './dashboardCollabRuntime'

/**
 * Binds a dashboard widget's query `Y.Text` to a CodeMirror editor for
 * character-level co-editing. Follows the seed-once / origin-guard pattern of
 * `collabRuntime.attachCollabToEditor`:
 *
 * - when the shared text is empty it is seeded once from the editor content;
 * - otherwise the shared text wins and is applied to the editor before yCollab
 *   activates, so a stale local value can never overwrite fresher shared text;
 * - the seed transaction's origin and the `ySyncFacet` config are the same
 *   instance so the Yjs observer ignores the seed change.
 *
 * Loaded on demand from `SqlEditor` so pages that never bind an editor don't
 * ship (or activate) the y-codemirror stack. Returns a detach function that
 * removes the pending `synced` listener (if any).
 */
export function attachDashboardCollabToEditor({ view, compartment, collab, widgetId }: {
  view: EditorView
  compartment: Compartment
  collab: DashboardCollab
  widgetId: string
}): () => void {
  const attach = () => {
    const ytext = ensureWidgetQueryText(collab.doc, widgetId)
    if (!ytext) return
    const editorContent = view.state.doc.toString()
    const yjsContent = ytext.toString()
    const ySyncConfig = new YSyncConfig(ytext, collab.provider.awareness)
    if (ytext.length === 0 && editorContent.length > 0) {
      collab.doc.transact(() => {
        ytext.insert(0, editorContent)
      }, ySyncConfig)
    } else if (yjsContent !== editorContent) {
      // Apply the shared text to the editor before yCollab activates: the
      // change flows into local component state (so previews/chips stay
      // current) but cannot be echoed back into the shared document.
      view.dispatch({ changes: { from: 0, to: editorContent.length, insert: yjsContent } })
    }
    // Activate yCollab with our config last (overrides yCollab's internal one)
    // so the observer's origin guard matches the seed transaction above.
    view.dispatch({ effects: compartment.reconfigure([
      yCollab(ytext, collab.provider.awareness, { undoManager: false }),
      ySyncFacet.of(ySyncConfig),
    ]) })
  }

  let onSynced: (({ state }: { state: boolean }) => void) | null = null
  if (collab.synced) {
    attach()
  } else {
    onSynced = ({ state }) => { if (state) attach() }
    collab.provider.on('synced', onSynced)
  }

  return () => {
    if (onSynced) collab.provider.off('synced', onSynced)
  }
}

/** Returns the widget's shared query text, creating it when missing. */
function ensureWidgetQueryText(doc: Y.Doc, widgetId: string): Y.Text | null {
  const wm = doc.getMap('widgets').get(widgetId)
  if (!(wm instanceof Y.Map)) return null
  const existing = wm.get('query')
  if (existing instanceof Y.Text) return existing
  const text = new Y.Text()
  doc.transact(() => {
    wm.set('query', text)
  })
  return text
}
