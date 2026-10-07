import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'
import { yCollab, ySyncFacet, YSyncConfig } from 'y-codemirror.next'
import type { EditorView } from '@codemirror/view'
import type { Compartment } from '@codemirror/state'
import type { MutableRefObject } from 'react'
import { getRelayUrl } from '../config'

/**
 * Collaboration runtime for notebook cells: Yjs document + Hocuspocus relay
 * provider + y-codemirror bindings.
 *
 * Loaded on demand from Cell so the notebook page doesn't ship (or connect)
 * the collaboration stack before an editor actually mounts.
 */

export interface NotebookCollab {
  doc: Y.Doc
  provider: HocuspocusProvider
  refCount: number
  synced: boolean
}

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return h
}

/** Creates and registers the shared document + relay provider for a notebook. */
export function createCollab(notebookId: string, cache: Map<string, NotebookCollab>): NotebookCollab {
  const doc = new Y.Doc()
  const token = localStorage.getItem('aether_token') ?? ''
  const userName = localStorage.getItem('aether_user_name') ?? ''
  const userEmail = localStorage.getItem('aether_user_email') ?? ''

  const provider = new HocuspocusProvider({
    url: getRelayUrl(),
    name: notebookId,
    document: doc,
    token,
    onAuthenticationFailed: () => console.warn('[yjs] Relay auth failed'),
  })

  provider.awareness?.setLocalStateField('user', {
    name: userName || userEmail || 'Anonymous',
    email: userEmail,
    color: `hsl(${Math.abs(hashStr(userEmail || userName)) % 360}, 70%, 55%)`,
  })

  const entry: NotebookCollab = { doc, provider, refCount: 1, synced: false }
  provider.on('synced', ({ state }: { state: boolean }) => { if (state) entry.synced = true })
  cache.set(notebookId, entry)
  window.dispatchEvent(new CustomEvent('aether-collab', { detail: { notebookId } }))
  return entry
}

interface AttachOptions {
  view: EditorView
  compartment: Compartment
  collab: NotebookCollab
  cellId: string
  applyingYjsRef: MutableRefObject<boolean>
}

/**
 * Binds the shared document for a cell to its editor view. When the provider
 * has not synced yet, waits for the `synced` event. Returns a detach function
 * that removes the listener.
 */
export function attachCollabToEditor({ view, compartment, collab, cellId, applyingYjsRef }: AttachOptions): () => void {
  const ytext = collab.doc.getText(`cell:${cellId}`)

  const attachCollab = () => {
    const editorContent = view.state.doc.toString()
    const yjsContent = ytext.toString()
    // Single config instance: the seed transaction origin and the ySync facet
    // must be identical so the observer's origin guard ignores the seed change.
    const ySyncConfig = new YSyncConfig(ytext, collab.provider.awareness)
    // Seed Yjs from the database-backed editor only when the shared doc is
    // empty. Otherwise Yjs wins: the shared text is applied to the editor
    // below, which is what fixes the stale-update race (an agent update can
    // land in Yjs before the provider has synced).
    if (ytext.length === 0 && editorContent.length > 0) {
      collab.doc.transact(() => {
        ytext.insert(0, editorContent)
      }, ySyncConfig)
    } else if (yjsContent !== editorContent) {
      // Apply the shared text to the editor before activating yCollab so
      // this programmatic change is not echoed back into the shared doc, and
      // suppress onSourceChange so a stale shared doc can never be
      // autosaved over fresher database content.
      applyingYjsRef.current = true
      try {
        view.dispatch({
          changes: { from: 0, to: editorContent.length, insert: yjsContent },
        })
      } finally {
        applyingYjsRef.current = false
      }
    }
    // Activate yCollab with our config last (overrides yCollab's internal one)
    // so the observer's origin guard matches our transact origin above.
    view.dispatch({ effects: compartment.reconfigure([
      yCollab(ytext, collab.provider.awareness, { undoManager: false }),
      ySyncFacet.of(ySyncConfig),
    ]) })
  }

  let onSynced: (({ state }: { state: boolean }) => void) | null = null
  if (collab.synced) {
    attachCollab()
  } else {
    onSynced = ({ state }: { state: boolean }) => { if (state) attachCollab() }
    collab.provider.on('synced', onSynced)
  }

  return () => {
    if (onSynced) collab.provider.off('synced', onSynced)
  }
}
