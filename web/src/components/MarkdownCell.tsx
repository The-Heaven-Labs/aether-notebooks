import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { getToken } from '../api/client'
import type { Cell } from '../types'
import { slugify, getFirstHeadingSlug } from './Cell'

export interface ResizableImageProps {
  src: string
  alt?: string
  width?: string
  onResize?: (src: string, newWidth: number) => void
}

const blobUrlCache = new Map<string, string>()

function getCachedBlobUrl(src: string): string | undefined {
  const url = blobUrlCache.get(src)
  if (url) {
    // move to end (most recently used)
    blobUrlCache.delete(src)
    blobUrlCache.set(src, url)
  }
  return url
}

function setCachedBlobUrl(src: string, url: string) {
  if (blobUrlCache.has(src)) {
    blobUrlCache.delete(src)
  }
  blobUrlCache.set(src, url)
  if (blobUrlCache.size > 200) {
    const firstKey = blobUrlCache.keys().next().value as string | undefined
    if (firstKey !== undefined) {
      const evicted = blobUrlCache.get(firstKey)
      blobUrlCache.delete(firstKey)
      if (evicted) URL.revokeObjectURL(evicted)
    }
  }
}

export function ResizableImage({ src, alt, width, onResize }: ResizableImageProps) {
  const imgRef = useRef<HTMLImageElement>(null)
  const [blobUrl, setBlobUrl] = useState<string | null>(() => getCachedBlobUrl(src) ?? null)

  useEffect(() => {
    const cached = getCachedBlobUrl(src)
    if (cached) { setBlobUrl(cached); return }
    fetch(src, { headers: { Authorization: `Bearer ${getToken()}` } })
      .then(r => r.blob())
      .then(blob => { const u = URL.createObjectURL(blob); setCachedBlobUrl(src, u); setBlobUrl(u) })
      .catch(() => setBlobUrl(src))
  }, [src])

  const handleMouseDown = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    const startWidth = imgRef.current?.getBoundingClientRect().width ?? (parseInt(width ?? '0') || 300)

    const onMouseMove = (ev: MouseEvent) => {
      const newWidth = Math.max(50, startWidth + (ev.clientX - startX))
      if (imgRef.current) imgRef.current.style.width = `${newWidth}px`
    }

    const onMouseUp = (ev: MouseEvent) => {
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
      const newWidth = Math.max(50, startWidth + (ev.clientX - startX))
      onResize?.(src, Math.round(newWidth))
    }

    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)
  }

  return (
    <span style={{ display: 'inline-block', position: 'relative' }} onClick={e => e.stopPropagation()}>
      <img ref={imgRef} src={blobUrl ?? ''} alt={alt} width={width} style={{ display: 'block', maxWidth: '100%', background: blobUrl ? undefined : 'var(--border)', minHeight: blobUrl ? undefined : 80 }} />
      <span
        className="img-resize-handle"
        onMouseDown={handleMouseDown}
        onClick={e => e.stopPropagation()}
        style={{
          position: 'absolute',
          right: 0,
          top: 0,
          bottom: 0,
          width: 8,
          cursor: 'ew-resize',
          background: 'rgba(0,0,0,0.15)',
        }}
      />
    </span>
  )
}

export function makeMarkdownComponents(onResize: (src: string, newWidth: number) => void) {
  return {
    img: ({ src, alt, width }: React.ImgHTMLAttributes<HTMLImageElement>) => (
      <ResizableImage src={src ?? ''} alt={alt} width={width?.toString()} onResize={onResize} />
    ),
    h1: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h1 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h1>
      )
    },
    h2: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h2 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h2>
      )
    },
    h3: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h3 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h3>
      )
    },
    h4: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h4 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h4>
      )
    },
    h5: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h5 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h5>
      )
    },
    h6: ({ children, ...props }: React.HTMLAttributes<HTMLHeadingElement>) => {
      const text = children?.toString() || ''
      const id = slugify(text)
      return (
        <h6 id={id ?? undefined} style={{ paddingRight: 0, ...props }}>
          {children}
        </h6>
      )
    },
  }
}

export function splitIntoBlocks(source: string): string[] {
  if (!source.trim()) return ['']
  const lines = source.split('\n')
  const blocks: string[] = []
  let current: string[] = []
  let inFence = false
  for (const line of lines) {
    if (line.match(/^```/)) inFence = !inFence
    if (!inFence && line.trim() === '' && current.length > 0) {
      blocks.push(current.join('\n'))
      current = []
    } else {
      current.push(line)
    }
  }
  if (current.length > 0) blocks.push(current.join('\n'))
  return blocks.length > 0 ? blocks : ['']
}

export function joinBlocks(blocks: string[]): string {
  return blocks.join('\n\n')
}

// ── MarkdownView ──────────────────────────────────────────────────────────────
// Block-based WYSIWYG: each paragraph renders as markdown; clicking a block
// enters edit mode for that block only.

export interface MarkdownViewProps {
  cell: Cell
  notebookId: string
  onSourceChange: (cellId: string, source: string) => void
  onSave?: (cellId: string, source: string) => void
}

interface MarkdownBlockProps {
  source: string
  focused: boolean
  onFocus: () => void
  onChange: (s: string) => void
  onBlur: (s: string) => void
  onKeyDown: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void
  onPaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => void
  onDrop: (e: React.DragEvent<HTMLDivElement>) => void
  markdownComponents: ReturnType<typeof makeMarkdownComponents>
  textareaRef?: React.RefObject<HTMLTextAreaElement | null>
}

function MarkdownBlock({ source, focused, onFocus, onChange, onBlur, onKeyDown, onPaste, onDrop, markdownComponents, textareaRef }: MarkdownBlockProps) {
  const localRef = useRef<HTMLTextAreaElement>(null)
  const ref = textareaRef ?? localRef

  useEffect(() => {
    if (focused && ref.current) {
      const el = ref.current
      el.style.height = 'auto'
      el.style.height = el.scrollHeight + 'px'
      el.focus()
    }
  }, [focused, ref])

  return (
    <>
      <textarea
        ref={ref}
        style={{ ...styles.mdBlockTextarea, display: focused ? 'block' : 'none' }}
        value={source}
        onChange={(e) => {
          const el = e.target
          el.style.height = 'auto'
          el.style.height = el.scrollHeight + 'px'
          onChange(e.target.value)
        }}
        onBlur={(e) => onBlur(e.target.value)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        placeholder="Write markdown…"
      />
      <div
        data-testid={!source.trim() ? 'md-empty-block' : undefined}
        style={{ ...styles.mdBlock, display: focused ? 'none' : 'block', minHeight: source.trim() ? undefined : 24 }}
        onClick={onFocus}
        onDragOver={e => e.preventDefault()}
        onDrop={onDrop}
      >
        {source.trim()
          ? <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={markdownComponents}>{source}</ReactMarkdown>
          : null}
      </div>
    </>
  )
}

export function MarkdownView({ cell, notebookId, onSourceChange, onSave }: MarkdownViewProps) {
  const [blocks, setBlocks] = useState(() => splitIntoBlocks(cell.source))
  const [focusedIdx, setFocusedIdx] = useState<number | null>(null)
  const [uploading, setUploading] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const blocksRef = useRef(blocks)
  const onSaveRef = useRef(onSave)
  useEffect(() => { blocksRef.current = blocks }, [blocks])
  useEffect(() => { onSaveRef.current = onSave }, [onSave])

  // Sync when source changes externally (history restore, Yjs, etc.) — skip while editing
  // to prevent cursor jumps and image flicker caused by re-splitting during active typing.
  useEffect(() => {
    if (focusedIdx !== null) return
    setBlocks(splitIntoBlocks(cell.source))
  }, [cell.source, focusedIdx])

  const updateBlock = useCallback((idx: number, s: string) => {
    setBlocks(prev => {
      const next = [...prev]; next[idx] = s
      onSourceChange(cell.id, joinBlocks(next))
      return next
    })
  }, [cell.id, onSourceChange])

  const blurBlock = useCallback((idx: number, s: string) => {
    setBlocks(prev => {
      const next = [...prev]; next[idx] = s
      onSave?.(cell.id, joinBlocks(next))
      return next
    })
    setFocusedIdx(null)
  }, [cell.id, onSave])

  const handleResize = useCallback((imgSrc: string, newWidth: number) => {
    const escaped = imgSrc.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const source = joinBlocks(blocksRef.current)
    const updated = source.replace(
      new RegExp(`(<img\\b[^>]*\\bsrc="${escaped}"[^>]*?)(?:\\s+width="[^"]*")?([^>]*?>)`, 'i'),
      (_, before, after) => `${before} width="${newWidth}"${after}`,
    )
    onSaveRef.current?.(cell.id, updated)
  }, [cell.id])

  const markdownComponents = useMemo(() => makeMarkdownComponents(handleResize), [handleResize])

  const uploadImage = useCallback(async (file: File) => {
    const form = new FormData()
    form.append('file', file)
    const res = await fetch(`/api/v1/notebooks/${notebookId}/attachments`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${getToken()}` },
      body: form,
    })
    if (!res.ok) throw new Error(`upload failed: ${res.status}`)
    return res.json() as Promise<{ id: string; filename: string }>
  }, [notebookId])

  const insertImageTag = useCallback((att: { id: string; filename: string }, idx: number) => {
    const textarea = textareaRef.current
    const imgTag = `<img src="/api/v1/attachments/${att.id}" alt="${att.filename}" width="100%">`
    setBlocks(prev => {
      const next = [...prev]
      if (textarea) {
        const start = textarea.selectionStart
        const end = textarea.selectionEnd
        next[idx] = next[idx].slice(0, start) + imgTag + next[idx].slice(end)
      } else {
        next[idx] = next[idx] + imgTag
      }
      onSourceChange(cell.id, joinBlocks(next))
      return next
    })
  }, [cell.id, onSourceChange])

  const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>, idx: number) => {
    const files = Array.from(e.clipboardData.files).filter(f => f.type.startsWith('image/'))
    if (files.length === 0) return
    e.preventDefault()
    setUploading(true)
    try { insertImageTag(await uploadImage(files[0]), idx) }
    catch (err) { console.error('Image upload failed:', err) }
    finally { setUploading(false) }
  }, [uploadImage, insertImageTag])

  const handleDrop = useCallback(async (e: React.DragEvent<HTMLDivElement>, idx: number) => {
    const files = Array.from(e.dataTransfer.files).filter(f => f.type.startsWith('image/'))
    if (files.length === 0) return
    e.preventDefault()
    setUploading(true)
    try { insertImageTag(await uploadImage(files[0]), idx) }
    catch (err) { console.error('Image upload failed:', err) }
    finally { setUploading(false) }
  }, [uploadImage, insertImageTag])

  const handleFileSelect = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file || focusedIdx === null) return
    e.target.value = ''
    setUploading(true)
    try { insertImageTag(await uploadImage(file), focusedIdx) }
    catch (err) { console.error('Image upload failed:', err) }
    finally { setUploading(false) }
  }, [uploadImage, insertImageTag, focusedIdx])

  return (
    <div style={styles.mdContainer}>
      {blocks.map((block, idx) => (
        <MarkdownBlock
          key={idx}
          source={block}
          focused={focusedIdx === idx}
          onFocus={() => setFocusedIdx(idx)}
          onChange={s => updateBlock(idx, s)}
          onBlur={s => blurBlock(idx, s)}
          onKeyDown={e => { if (e.key === 'Escape') e.currentTarget.blur() }}
          onPaste={e => handlePaste(e, idx)}
          onDrop={e => handleDrop(e, idx)}
          markdownComponents={markdownComponents}
          textareaRef={focusedIdx === idx ? textareaRef : undefined}
        />
      ))}
      {focusedIdx !== null && (
        <div style={styles.mdToolbar}>
          <button
            style={styles.mdToolbarBtn}
            disabled={uploading}
            onMouseDown={e => e.preventDefault()}
            onClick={() => fileInputRef.current?.click()}
            title="Upload image"
          >
            {uploading ? '...' : (
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
                <circle cx="8.5" cy="8.5" r="1.5"/>
                <polyline points="21 15 16 10 5 21"/>
              </svg>
            )}
          </button>
          <input ref={fileInputRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={handleFileSelect} />
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  mdContainer: {
    borderTop: '1px solid var(--border-light)',
    borderBottom: '1px solid var(--border-light)',
    padding: '14px 20px',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-sans)',
    cursor: 'text',
    minHeight: 48,
  },
  mdBlock: {
  },
  mdBlockTextarea: {
    display: 'block',
    width: '100%',
    fontFamily: 'var(--font-sans)',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    background: 'transparent',
    border: 'none',
    outline: 'none',
    resize: 'none',
    padding: 0,
    margin: 0,
    overflow: 'hidden',
    boxSizing: 'border-box' as const,
  },
  mdToolbar: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 12px',
    borderTop: '1px solid var(--border-light)',
    background: 'var(--bg-cell-code)',
  },
  mdToolbarBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 6px',
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 3,
    cursor: 'pointer',
    lineHeight: 1,
  },
}
