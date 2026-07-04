import { memo, useState, useEffect } from 'react'
import { Loader2 } from 'lucide-react'
import { getToken } from '../api/client'
import { ImageViewer } from './ImageViewer'

export const AgentMessageImages = memo(({ images }: { images: string[] }) => {
  const [blobUrls, setBlobUrls] = useState<Record<string, string>>({})
  const [viewerSrc, setViewerSrc] = useState<string | null>(null)
  useEffect(() => {
    for (const id of images) {
      if (blobUrls[id]) continue
      fetch(`/api/v1/agent-attachments/${id}`, {
        headers: { Authorization: `Bearer ${getToken()}` },
      })
        .then(r => r.ok ? r.blob() : null)
        .then(blob => {
          if (blob) {
            const url = URL.createObjectURL(blob)
            setBlobUrls(prev => ({ ...prev, [id]: url }))
          }
        })
        .catch(() => {})
    }
    return () => {
      for (const url of Object.values(blobUrls)) {
        URL.revokeObjectURL(url)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [images.join(',')])
  return (
    <>
      {viewerSrc && <ImageViewer src={viewerSrc} onClose={() => setViewerSrc(null)} />}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 6 }}>
        {images.map(id => (
          blobUrls[id] ? (
            <img key={id} src={blobUrls[id]} alt="" onClick={() => setViewerSrc(blobUrls[id])} style={{ maxWidth: '100%', maxHeight: 300, borderRadius: 6, objectFit: 'contain', cursor: 'pointer' }} />
          ) : (
            <div key={id} style={{ width: 60, height: 60, borderRadius: 6, background: 'var(--bg-elevated)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Loader2 size={16} style={{ animation: 'spin 1s linear infinite' }} />
            </div>
          )
        ))}
      </div>
    </>
  )
})
