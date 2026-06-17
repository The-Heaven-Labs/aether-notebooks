import { Server } from '@hocuspocus/server'

const API_URL = process.env.HNB_API_URL || 'http://localhost:8080'
const PORT = parseInt(process.env.HNB_RELAY_PORT || '3001')

const server = new Server({
  port: PORT,

  async onLoadDocument({ documentName }) {
    const res = await fetch(`${API_URL}/internal/yjs/${documentName}`)
    if (!res.ok) return null
    const buf = await res.arrayBuffer()
    if (buf.byteLength === 0) return null
    return new Uint8Array(buf)
  },

  async onStoreDocument({ documentName, state }) {
    await fetch(`${API_URL}/internal/yjs/${documentName}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: state,
    })
  },

  async onAuthenticate({ token }) {
    if (!token) throw new Error('Unauthorized')
    const res = await fetch(`${API_URL}/internal/auth/validate`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!res.ok) throw new Error('Unauthorized')
  },
})

server.listen().then(() => {
  console.log(`Hocuspocus relay listening on port ${PORT}`)
})
