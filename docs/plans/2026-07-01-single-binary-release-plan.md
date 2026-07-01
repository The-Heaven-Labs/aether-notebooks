# Single-Binary Release Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship `aether-server` with an embedded web frontend as a single tarball via GitHub Releases, eliminating the need to build anything.

**Architecture:** Embed `web/dist` into the Go binary via `//go:embed`, serve it as a catch-all static file handler. Use GoReleaser for cross-platform releases. Eliminate Vite dev server — dev uses `npm run build --watch` in a sidecar container, with Air watching `web/dist` for Go rebuild triggers.

**Tech Stack:** Go 1.25 `//go:embed`, GoReleaser v2, GitHub Actions, Vite (build only)

**Branch:** `jesus/single-binary-release`

---

### Task 1: Add `internal/api/embed.go` — embed web frontend

**Files:**
- Create: `internal/api/embed.go`
- Modify: `internal/api/router.go`
- Test: Manual — start server, verify frontend loads on `/`

**Step 1: Create embed.go**

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

**Step 2: Add catch-all route in router.go**

Add at the end of `routes()`, after all API/WS/internal routes:

```go
// Serve embedded frontend SPA — must be last, API routes take priority
s.mux.Handle("GET /", frontendHandler())
```

**Step 3: Manual verification**

```bash
# Build and run
go build -o /tmp/aether-server ./cmd/aether-server
/tmp/aether-server &
# Visit http://localhost:8080 — should serve the app
# Visit http://localhost:8080/api/v1/auth/config — should still return JSON
kill %1
```

### Task 2: Update `.air.toml` — watch web/dist for frontend changes

**Files:**
- Modify: `.air.toml`

**Step 1: Update air config**

Change to include web/dist in watched files:

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -buildvcs=false -o ./tmp/main ./cmd/aether-server"
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "bin", "relay", "docs", "e2e", "migrations"]
  exclude_file = []
  include_ext = ["go", "css", "js", "html", "json", "svg", "png", "ico"]
  exclude_regex = ["_test\\.go$"]
  log = "build-errors.log"
  kill_delay = "0s"
  send_interrupt = true

[log]
  time = true

[color]
  build   = "yellow"
  runner  = "green"
  watcher = "cyan"
  app     = ""

[screen]
  clear_on_rebuild = false
  keep_scroll      = true
```

Key changes:
- Remove `web` from `exclude_dir` — keep it specific to non-web dirs
- Add frontend extensions to `include_ext` — Air restarts when embedded assets change
- `exclude_file` is explicitly empty (was implicit)

### Task 3: Update `Dockerfile.dev` — add Node.js for web build

**Files:**
- Modify: `Dockerfile.dev`

**Step 1: Add Node.js to dev container**

```dockerfile
FROM golang:1.25-alpine

ENV GOTOOLCHAIN=local

RUN apk add --no-cache git nodejs npm && \
    go install github.com/air-verse/air@latest

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

RUN cd web && npm ci

CMD ["sh", "-c", "cd web && npm run build --watch & air -c /app/.air.toml"]
```

### Task 4: Update `web/Dockerfile.dev` — switch to build-only mode

**Files:**
- Modify: `web/Dockerfile.dev`

**Step 1: Replace Vite dev server with build watch**

```dockerfile
FROM node:20-alpine

WORKDIR /app

COPY package*.json ./
RUN npm install

# Source is mounted at runtime; output goes to web/dist/
CMD ["npm", "run", "build", "--watch"]
```

### Task 5: Add `build:watch` script to `web/package.json`

**Files:**
- Modify: `web/package.json`

**Step 1: Add build:watch script**

Find the `"build"` line and add:

```
    "build:watch": "vite build --watch",
```

This avoids running `tsc -b` in watch mode (which would double-compile TypeScript) — Vite handles TS compilation during build.

### Task 6: Simplify `docker-compose.dev.yml`

**Files:**
- Modify: `docker-compose.dev.yml`

**Step 1: Replace the `web` service**

Change from Vite dev server to builder:

```yaml
  web-builder:
    build:
      context: ./web
      dockerfile: Dockerfile.dev
    volumes:
      - ./web:/app
      - web-node-modules:/app/node_modules
    # No ports exposed — outputs to web/dist on shared volume
    # The api container picks up changes via Air
```

**Step 2: Mount web/dist into the api container**

Add to api service's `volumes`:

```yaml
      - ./web/dist:/app/web/dist
```

This lets Air inside the api container detect when web/dist changes (rebuild triggered by npm in web-builder) and restart the Go server.

**Step 3: Update dependencies**

api service `depends_on`:
```yaml
    depends_on:
      aether-postgres:
        condition: service_healthy
      aether-redis:
        condition: service_started
      web-builder:
        condition: service_started
```

**Step 4: Remove frontend URL env from api**

The `AETHER_FRONTEND_URL` env is still needed for OIDC redirects (the frontend URL is used in callback URLs and CORS). Keep it as-is.

**Step 5: Update the header comment**

Update the dev compose top comment to reflect the simplified stack.

### Task 7: Simplify production `Dockerfile`

**Files:**
- Modify: `Dockerfile`

**Step 1: Remove web build stage**

The `//go:embed` directive embeds web/dist at compile time. The Go binary now includes the frontend. The separate web-build stage is no longer needed.

```dockerfile
# ---- Go build ----
FROM golang:1.22-alpine AS go-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /aether-server ./cmd/aether-server

# ---- Final image ----
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
RUN addgroup -S aether && adduser -S aether -G aether
COPY --from=go-build /aether-server /usr/local/bin/
USER aether
EXPOSE 8080
CMD ["aether-server"]
```

### Task 8: Add `.goreleaser.yaml`

**Files:**
- Create: `.goreleaser.yaml`

**Step 1: Create GoReleaser config**

```yaml
version: 2
project_name: aether

before:
  hooks:
    - cd web && npm ci && npm run build

builds:
  - id: server
    binary: aether-server
    main: ./cmd/aether-server
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

  - id: cli
    binary: aether
    main: ./cmd/aether
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64

archives:
  - id: release
    name_template: "{{ .ProjectName }}_{{ .Tag }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: The-Heaven-Labs
    name: aether-notebooks
```

### Task 9: Add release CI workflow

**Files:**
- Create: `.github/workflows/release.yml`

**Step 1: Create the workflow**

```yaml
name: release
on:
  push:
    tags:
      - "v*"
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
      - run: npm ci
        working-directory: web
      - uses: goreleaser/goreleaser-action@v6
        with:
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### Task 10: Update README

**Files:**
- Modify: `README.md`

**Step 1: Update Quick Start**

The quick start should show both Docker and bare-metal:

```bash
# Docker (recommended for evaluation)
docker compose -f docker-compose.dev.yml up -d

# Or download a pre-built release and run directly
curl -LO https://github.com/The-Heaven-Labs/aether-notebooks/releases/latest/download/aether_latest_linux_amd64.tar.gz
tar xzf aether_latest_linux_amd64.tar.gz
./aether-server
```

**Step 2: Update Development section**

Replace "task dev:web" references to reflect that the Go binary now serves the frontend. Add a note that the web-builder container auto-rebuilds on frontend changes.

**Step 3: Add a Release section**

Document the release process:
- Tag with `v*` → CI builds + publishes
- Tarballs available under GitHub Releases

### Task 11: Clean up Vite config

**Files:**
- Modify: `web/vite.config.ts`

**Step 1: Remove proxy configuration**

The Vite dev server is gone — no more proxy. The SPA is served directly by the Go server. The vite.config.ts is still used for `vitest` and `vite build`, but the `server.proxy` block is dead code.

Remove the entire `server` block (lines 22-63). Only keep `plugins` and `test` config.

**Step 2: Remove `API_URL` env var references**

The `apiTarget` variable and its usage in the proxy config are removed. Keep the import for path/fileURLToPath since `test` config uses them.

### Task 12: Verify full dev stack

```bash
docker compose -f docker-compose.dev.yml up -d
# Wait for startup
# Visit http://localhost:8080 — should show the app
# Edit web/src/App.tsx → wait for web-builder to rebuild → Air triggers Go rebuild → server restarts → changes visible
```

### Task 13: Remove Vite proxy — final config

After the proxy config is removed in Task 11, the `vite.config.ts` no longer needs `API_URL` env var. The file is now purely for build + test tooling.
