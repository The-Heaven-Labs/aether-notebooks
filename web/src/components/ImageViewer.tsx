import { useState, useEffect, useCallback } from 'react'
import { X, ZoomIn, ZoomOut, Maximize2 } from 'lucide-react'

interface ImageViewerProps {
  src: string
  alt?: string
  onClose: () => void
}

export function ImageViewer({ src, alt, onClose }: ImageViewerProps) {
  const [zoom, setZoom] = useState(1)

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') onClose()
    if (e.key === '+' || e.key === '=') setZoom(z => Math.min(z + 0.25, 4))
    if (e.key === '-') setZoom(z => Math.max(z - 0.25, 0.25))
    if (e.key === '0') setZoom(1)
  }, [onClose])

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  const handleWheel = useCallback((e: React.WheelEvent) => {
    e.preventDefault()
    setZoom(z => {
      const next = z + (e.deltaY < 0 ? 0.1 : -0.1)
      return Math.max(0.25, Math.min(4, next))
    })
  }, [])

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.toolbar} onClick={e => e.stopPropagation()}>
        <button style={styles.toolBtn} onClick={() => setZoom(z => Math.min(z + 0.25, 4))} title="Zoom in (+)">
          <ZoomIn size={16} />
        </button>
        <span style={styles.zoomText}>{Math.round(zoom * 100)}%</span>
        <button style={styles.toolBtn} onClick={() => setZoom(z => Math.max(z - 0.25, 0.25))} title="Zoom out (-)">
          <ZoomOut size={16} />
        </button>
        <button style={styles.toolBtn} onClick={() => setZoom(1)} title="Reset zoom (0)">
          <Maximize2 size={16} />
        </button>
        <div style={{ flex: 1 }} />
        <button style={styles.toolBtn} onClick={onClose} title="Close (Esc)">
          <X size={16} />
        </button>
      </div>
      <div style={styles.imageContainer} onClick={e => e.stopPropagation()} onWheel={handleWheel}>
        <img src={src} alt={alt} style={{ ...styles.image, transform: `scale(${zoom})` }} />
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.85)',
    zIndex: 2000, display: 'flex', flexDirection: 'column',
  },
  toolbar: {
    display: 'flex', alignItems: 'center', gap: 8, padding: '8px 16px',
    background: 'rgba(0,0,0,0.6)', zIndex: 1,
  },
  toolBtn: {
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    width: 32, height: 32, borderRadius: 4, border: 'none',
    background: 'rgba(255,255,255,0.1)', color: '#fff', cursor: 'pointer',
  },
  zoomText: { fontSize: 13, color: '#fff', fontFamily: 'var(--font-mono)', minWidth: 40, textAlign: 'center' as const },
  imageContainer: { flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', overflow: 'hidden' },
  image: { maxWidth: '90vw', maxHeight: '85vh', transition: 'transform 0.15s ease', objectFit: 'contain' as const },
}
