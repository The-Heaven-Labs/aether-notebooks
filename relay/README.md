# Aether Relay — Hocuspocus Yjs WebSocket Relay

This directory contains the Hocuspocus WebSocket relay for real-time collaborative editing in Aether notebooks.

## Architecture

The relay sits between the browser clients and the Go API server:
- Clients connect via WebSocket to the relay (port 3001)
- The relay authenticates connections via the Go API's `/internal/auth/validate` endpoint
- Yjs document state is loaded from and stored to the Go API's `/internal/yjs/{notebook_id}` endpoint
- Multiple clients can edit the same notebook simultaneously via Yjs CRDT merging

## Configuration

| Variable | Default | Description |
|---|---|---|
| `AETHER_RELAY_PORT` | `3001` | WebSocket listen port |
| `AETHER_API_URL` | `http://localhost:8080` | Internal Go API URL for auth and document storage |

## Development

```bash
# Run in dev mode (with tsx watcher)
task dev:relay

# Build
task build:relay
```

## Known Caveats

- The `Dockerfile.dev` uses `tsx` as the TypeScript runner; local development uses `ts-node` from `package.json`. If running outside Docker, ensure `ts-node` is available or install `tsx` globally.
