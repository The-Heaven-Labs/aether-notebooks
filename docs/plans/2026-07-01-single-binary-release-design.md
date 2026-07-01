# Single-Binary Release Design

**Date:** 2026-07-01
**Status:** Approved
**Branch:** `jesus/single-binary-release`

## Goal

Eliminate the need to build anything — ship `aether-server` and `aether` CLI as a single tarball with the web frontend embedded, distributed via GitHub Releases.

## Approach

GoReleaser + `//go:embed` — standard Go tooling with minimal custom code.

## Changes

### 1. Go code — embed web frontend

New file `internal/api/embed.go`:

```go
package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:web/dist
var frontendFS embed.FS

func frontendHandler() http.Handler {
	sub, err := fs.Sub(frontendFS, "web/dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
```

In `router.go`, add catch-all route at the end of `routes()`:

```go
s.mux.Handle("GET /", frontendHandler())
```

Go 1.22 ServeMux pattern matching ensures API routes take priority.

### 2. Dev environment — eliminate Vite

- Remove `web` service from `docker-compose.dev.yml` (Vite dev server)
- Add a `web-builder` sidecar that runs `npm run build --watch`, outputting to a named volume
- Mount `web/dist` into the `api` container so Air detects frontend changes
  and triggers a Go rebuild
- Update `.air.toml` to watch `web/dist` in addition to `*.go` files
- The `api` container now serves everything on `:8080` (API + frontend)
- Remove Vite proxy configuration from Vite config (no longer needed)

Dev stack becomes: api, relay, web-builder, postgres, redis, clickhouse, opensearch, keycloak

### 3. GoReleaser config

`.goreleaser.yaml` with:
- `before` hook: `cd web && npm ci && npm run build`
- 4 builds: server + cli × linux/darwin × amd64/arm64
- Archives as tarballs with LICENSE + README
- Checksums file

### 4. CI release workflow

`.github/workflows/release.yml`:
- Trigger: push tags `v*`
- Steps: checkout, setup Go 1.25, setup Node 20, `npm ci` in web, goreleaser release
- Publishes to GitHub Releases

### 5. Production Dockerfile

Simplify: remove the separate web build stage since the frontend is now embedded
at compile time. The Go build already includes it via `//go:embed`.

### 6. Remaining services (not in tarball)

- **Relay** — stays as Docker image only. Not included in release tarballs.
- **Infrastructure** — Postgres, Redis are external dependencies the user provides.

## Outcome

```bash
# User workflow:
curl -LO https://github.com/The-Heaven-Labs/aether-notebooks/releases/download/v1.0.0/aether_v1.0.0_linux_amd64.tar.gz
tar xzf aether_v1.0.0_linux_amd64.tar.gz
./aether-server
# → serves API + frontend on :8080
```
