import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { Loader2, Link, Heading, Code } from 'lucide-react'
import { getToken } from '../api/client'
import type { Cell } from '../types'
import { slugify } from './Cell'

export interface ResizableImageProps {
  src: string
  alt?: string
  width?: string
  onResize?: (src: string, newWidth: number) => void
  readOnly?: boolean
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

export function ResizableImage({ src, alt, width, onResize, readOnly }: ResizableImageProps) {
  const imgRef = useRef<HTMLImageElement>(null)
  const [blobUrl, setBlobUrl] = useState<string | null>(() => getCachedBlobUrl(src) ?? null)

  useEffect(() => {
    const cached = getCachedBlobUrl(src)
    if (cached) { setBlobUrl(cached); return }
    fetch(src, { headers: { Authorization: `Bearer ${getToken()}` } })
      .then(r => { if (!r.ok) throw new Error(`fetch failed: ${r.status}`); return r.blob() })
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
      {!readOnly && (
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
      )}
    </span>
  )
}

export function makeMarkdownComponents(onResize: (src: string, newWidth: number) => void, readOnly = false) {
  return {
    img: ({ src, alt, width }: React.ImgHTMLAttributes<HTMLImageElement>) => (
      <ResizableImage src={src ?? ''} alt={alt} width={width?.toString()} onResize={onResize} readOnly={readOnly} />
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



export function MarkdownView({ cell, notebookId, onSourceChange, onSave }: MarkdownViewProps) {
  const [source, setSource] = useState(cell.source)
  const [isFocused, setIsFocused] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<number | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const sourceRef = useRef(source)
  const onSaveRef = useRef(onSave)
  useEffect(() => { sourceRef.current = source }, [source])
  useEffect(() => { onSaveRef.current = onSave }, [onSave])

  // Sync when source changes externally (history restore, Yjs, etc.) — skip while editing
  useEffect(() => {
    if (isFocused) return
    setSource(cell.source)
  }, [cell.source, isFocused])

  const updateSource = useCallback((s: string) => {
    setSource(s)
    onSourceChange(cell.id, s)
  }, [cell.id, onSourceChange])

  const blurEditor = useCallback((s: string) => {
    setSource(s)
    onSave?.(cell.id, s)
    setIsFocused(false)
  }, [cell.id, onSave])

  const handleResize = useCallback((imgSrc: string, newWidth: number) => {
    const escaped = imgSrc.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const imgTagRegex = new RegExp(`<img\\b[^>]*\\bsrc="${escaped}"[^>]*>`, 'i')
    const match = sourceRef.current.match(imgTagRegex)
    if (!match) return
    const newTag = match[0]
      .replace(/\s+width="[^"]*"/i, '')
      .replace(/(\/?>)$/, ` width="${newWidth}"$1`)
    const updated = sourceRef.current.replace(imgTagRegex, newTag)
    if (updated === sourceRef.current) return
    setSource(updated)
    onSourceChange(cell.id, updated)
    onSaveRef.current?.(cell.id, updated)
  }, [cell.id, onSourceChange])

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

  const insertImageTag = useCallback((att: { id: string; filename: string }) => {
    const textarea = textareaRef.current
    const imgTag = `<img src="/api/v1/attachments/${att.id}" alt="${att.filename}" width="100%">`
    const current = sourceRef.current
    let nextSource: string
    if (textarea) {
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      nextSource = current.slice(0, start) + imgTag + current.slice(end)
    } else {
      nextSource = current + (current ? '\n\n' : '') + imgTag
    }
    setSource(nextSource)
    onSourceChange(cell.id, nextSource)
    onSaveRef.current?.(cell.id, nextSource)
  }, [cell.id, onSourceChange])

  const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.files).filter(f => f.type.startsWith('image/'))
    if (files.length === 0) return
    e.preventDefault()
    setUploading(true)
    setUploadProgress(0)
    try {
      // Simulate progress for better UX
      const progressInterval = setInterval(() => {
        setUploadProgress(prev => prev !== null && prev < 90 ? prev + 10 : prev)
      }, 100)
      const att = await uploadImage(files[0])
      clearInterval(progressInterval)
      setUploadProgress(100)
      insertImageTag(att)
      setTimeout(() => setUploadProgress(null), 300)
    }
    catch (err) { 
      console.error('Image upload failed:', err)
      setUploadProgress(null)
    }
    finally { setUploading(false) }
  }, [uploadImage, insertImageTag])

  const handleDrop = useCallback(async (e: React.DragEvent<HTMLDivElement>) => {
    const files = Array.from(e.dataTransfer.files).filter(f => f.type.startsWith('image/'))
    if (files.length === 0) return
    e.preventDefault()
    setDragOver(false)
    setUploading(true)
    setUploadProgress(0)
    try {
      const progressInterval = setInterval(() => {
        setUploadProgress(prev => prev !== null && prev < 90 ? prev + 10 : prev)
      }, 100)
      const att = await uploadImage(files[0])
      clearInterval(progressInterval)
      setUploadProgress(100)
      insertImageTag(att)
      setTimeout(() => setUploadProgress(null), 300)
    }
    catch (err) { 
      console.error('Image upload failed:', err)
      setUploadProgress(null)
    }
    finally { setUploading(false) }
  }, [uploadImage, insertImageTag])

  const handleFileSelect = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploading(true)
    setUploadProgress(0)
    try {
      const progressInterval = setInterval(() => {
        setUploadProgress(prev => prev !== null && prev < 90 ? prev + 10 : prev)
      }, 100)
      const att = await uploadImage(file)
      clearInterval(progressInterval)
      setUploadProgress(100)
      insertImageTag(att)
      setTimeout(() => setUploadProgress(null), 300)
    }
    catch (err) { 
      console.error('Image upload failed:', err)
      setUploadProgress(null)
    }
    finally { setUploading(false) }
  }, [uploadImage, insertImageTag])

  return (
    <div 
      style={{
        ...styles.mdContainer,
        ...(dragOver ? styles.mdDragOver : {}),
      }}
      onDragOver={(e) => {
        e.preventDefault()
        e.stopPropagation()
        setDragOver(true)
      }}
      onDragLeave={(e) => {
        e.preventDefault()
        e.stopPropagation()
        setDragOver(false)
      }}
      onDrop={handleDrop}
    >
      {dragOver && (
        <div style={styles.mdDragOverlay}>
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
            <circle cx="8.5" cy="8.5" r="1.5"/>
            <polyline points="21 15 16 10 5 21"/>
          </svg>
          <div>Drop image here</div>
        </div>
      )}
      
      {uploadProgress !== null && (
        <div style={styles.mdUploadProgress}>
          <div style={styles.mdUploadProgressBar}>
            <div style={{...styles.mdUploadProgressFill, width: `${uploadProgress}%`}} />
          </div>
          <div style={styles.mdUploadProgressText}>Uploading... {uploadProgress}%</div>
        </div>
      )}

      <textarea
        ref={textareaRef}
        style={{
          ...styles.mdTextarea,
          display: isFocused ? 'block' : 'none',
        }}
        value={source}
        onChange={(e) => {
          const el = e.target
          el.style.height = 'auto'
          el.style.height = el.scrollHeight + 'px'
          updateSource(e.target.value)
        }}
        onBlur={(e) => blurEditor(e.target.value)}
        onPaste={handlePaste}
        onFocus={() => {
          setIsFocused(true)
          const el = textareaRef.current
          if (el) {
            el.style.height = 'auto'
            el.style.height = el.scrollHeight + 'px'
          }
        }}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.currentTarget.blur()
          }
          // Tab key inserts spaces instead of changing focus
          if (e.key === 'Tab') {
            e.preventDefault()
            const el = e.currentTarget
            const start = el.selectionStart
            const end = el.selectionEnd
            const value = el.value
            el.value = value.substring(0, start) + '  ' + value.substring(end)
            el.selectionStart = el.selectionEnd = start + 2
            updateSource(el.value)
          }
        }}
        placeholder="Write markdown… (Ctrl+V to paste images, drag & drop supported)"
      />
      
      <div
        data-testid={!source.trim() ? 'md-empty-block' : undefined}
        style={{
          ...styles.mdPreview,
          display: isFocused ? 'none' : 'block',
          minHeight: source.trim() ? undefined : 48,
        }}
        onClick={() => {
          setIsFocused(true)
          setTimeout(() => textareaRef.current?.focus(), 0)
        }}
      >
        {source.trim()
          ? <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={markdownComponents}>{source}</ReactMarkdown>
          : <span style={styles.mdPlaceholder}>Write markdown… (Ctrl+V to paste images, drag & drop supported)</span>}
      </div>

      {isFocused && (
        <div style={styles.mdToolbar}>
          <div style={styles.mdToolbarLeft}>
            <button
              style={styles.mdToolbarBtn}
              disabled={uploading}
              onMouseDown={e => e.preventDefault()}
              onClick={() => fileInputRef.current?.click()}
              title="Upload image (or paste with Ctrl+V)"
            >
              {uploading ? (
                <Loader2 size={14} style={{ animation: 'spin 1s linear infinite' }} />
              ) : (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
                  <circle cx="8.5" cy="8.5" r="1.5"/>
                  <polyline points="21 15 16 10 5 21"/>
                </svg>
              )}
              <span>Image</span>
            </button>
            
            <div style={styles.mdToolbarDivider} />
            
            <button
              style={styles.mdToolbarBtn}
              onMouseDown={e => e.preventDefault()}
              onClick={() => {
                const textarea = textareaRef.current
                if (!textarea) return
                const start = textarea.selectionStart
                const end = textarea.selectionEnd
                const value = textarea.value
                const selected = value.substring(start, end)
                const replacement = `**${selected || 'bold text'}**`
                textarea.value = value.substring(0, start) + replacement + value.substring(end)
                textarea.selectionStart = start + 2
                textarea.selectionEnd = start + 2 + (selected || 'bold text').length
                textarea.focus()
                updateSource(textarea.value)
              }}
              title="Bold (Ctrl+B)"
            >
              <strong>B</strong>
            </button>
            
            <button
              style={styles.mdToolbarBtn}
              onMouseDown={e => e.preventDefault()}
              onClick={() => {
                const textarea = textareaRef.current
                if (!textarea) return
                const start = textarea.selectionStart
                const end = textarea.selectionEnd
                const value = textarea.value
                const selected = value.substring(start, end)
                const replacement = `*${selected || 'italic text'}*`
                textarea.value = value.substring(0, start) + replacement + value.substring(end)
                textarea.selectionStart = start + 1
                textarea.selectionEnd = start + 1 + (selected || 'italic text').length
                textarea.focus()
                updateSource(textarea.value)
              }}
              title="Italic (Ctrl+I)"
            >
              <em>I</em>
            </button>
            
            <button
              style={styles.mdToolbarBtn}
              onMouseDown={e => e.preventDefault()}
              onClick={() => {
                const textarea = textareaRef.current
                if (!textarea) return
                const start = textarea.selectionStart
                const end = textarea.selectionEnd
                const value = textarea.value
                const selected = value.substring(start, end)
                const replacement = `[${selected || 'link text'}](url)`
                textarea.value = value.substring(0, start) + replacement + value.substring(end)
                textarea.selectionStart = start + 1
                textarea.selectionEnd = start + 1 + (selected || 'link text').length
                textarea.focus()
                updateSource(textarea.value)
              }}
              title="Insert link"
            >
              <Link size={13} />
            </button>
            
            <button
              style={styles.mdToolbarBtn}
              onMouseDown={e => e.preventDefault()}
              onClick={() => {
                const textarea = textareaRef.current
                if (!textarea) return
                const start = textarea.selectionStart
                const value = textarea.value
                const lineStart = value.lastIndexOf('\n', start - 1) + 1
                const replacement = value.substring(0, lineStart) + '## ' + value.substring(lineStart)
                textarea.value = replacement
                textarea.selectionStart = textarea.selectionEnd = start + 3
                textarea.focus()
                updateSource(textarea.value)
              }}
              title="Insert heading"
            >
              <Heading size={13} />
            </button>
            
            <button
              style={styles.mdToolbarBtn}
              onMouseDown={e => e.preventDefault()}
              onClick={() => {
                const textarea = textareaRef.current
                if (!textarea) return
                const start = textarea.selectionStart
                const end = textarea.selectionEnd
                const value = textarea.value
                const selected = value.substring(start, end)
                const replacement = `\`${selected || 'code'}\``
                textarea.value = value.substring(0, start) + replacement + value.substring(end)
                textarea.selectionStart = start + 1
                textarea.selectionEnd = start + 1 + (selected || 'code').length
                textarea.focus()
                updateSource(textarea.value)
              }}
              title="Inline code"
            >
              <Code size={13} />
            </button>
          </div>
          
          <div style={styles.mdToolbarRight}>
            <span style={styles.mdToolbarHint}>
              {uploading ? 'Uploading...' : 'Ctrl+V to paste images'}
            </span>
          </div>
        </div>
      )}
      
      <input ref={fileInputRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={handleFileSelect} />
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
    position: 'relative',
    transition: 'background-color 0.2s',
  },
  mdDragOver: {
    backgroundColor: 'var(--accent-light)',
    borderColor: 'var(--accent)',
  },
  mdDragOverlay: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(128, 128, 128, 0.1)',
    border: '2px dashed var(--accent)',
    borderRadius: 4,
    color: 'var(--accent)',
    fontSize: 16,
    fontWeight: 600,
    gap: 8,
    zIndex: 10,
    pointerEvents: 'none',
  },
  mdUploadProgress: {
    padding: '8px 0',
  },
  mdUploadProgressBar: {
    height: 4,
    backgroundColor: 'var(--border)',
    borderRadius: 2,
    overflow: 'hidden',
    marginBottom: 4,
  },
  mdUploadProgressFill: {
    height: '100%',
    backgroundColor: 'var(--accent)',
    transition: 'width 0.2s',
  },
  mdUploadProgressText: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    textAlign: 'center',
  },
  mdTextarea: {
    display: 'block',
    width: '100%',
    fontFamily: 'var(--font-sans)',
    fontSize: 14,
    lineHeight: 1.75,
    color: 'var(--text-primary)',
    background: 'var(--bg-cell-text)',
    border: 'none',
    outline: 'none',
    resize: 'none',
    padding: 0,
    margin: 0,
    overflow: 'hidden',
    boxSizing: 'border-box' as const,
    caretColor: 'var(--text-primary)',
    minHeight: 100,
  },
  mdPreview: {
    minHeight: 48,
  },
  mdPlaceholder: {
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
  mdToolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    padding: '8px 12px',
    borderTop: '1px solid var(--border-light)',
    background: 'var(--bg-cell-code)',
    marginTop: 8,
  },
  mdToolbarLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  mdToolbarRight: {
    display: 'flex',
    alignItems: 'center',
  },
  mdToolbarDivider: {
    width: 1,
    height: 16,
    backgroundColor: 'var(--border)',
    margin: '0 4px',
  },
  mdToolbarBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '4px 8px',
    fontSize: 12,
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-secondary)',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    lineHeight: 1,
    gap: 4,
    transition: 'all 0.15s',
  },
  mdToolbarHint: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
  },
}
