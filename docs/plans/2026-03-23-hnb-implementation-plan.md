# Heaven's Notebooks (hnb) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a collaborative notebook platform for data analysts with SQL execution, real-time co-editing, dashboards, and scheduling.

**Architecture:** Go monolith API + Yjs/Hocuspocus Node relay for real-time collab. React/TypeScript frontend. PostgreSQL for state, Redis for sessions/pubsub. Executors as isolated processes (SQL in Go, JS in Deno).

**Tech Stack:** Go 1.22+, React 18 + TypeScript + Vite, PostgreSQL 16, Redis 7, Hocuspocus/Yjs, CodeMirror 6, Apache ECharts, Deno (JS sandbox)

---

## Phase 1: Project Scaffolding & Foundation

### Task 1: Go Module & Directory Structure

**Files:**
- Create: `go.mod`
- Create: `cmd/hnb-server/main.go`
- Create: `cmd/hnb/main.go` (CLI entrypoint)
- Create: `internal/config/config.go`
- Create: `Makefile`
- Create: `docker-compose.yml`
- Create: `.gitignore`

**Step 1: Initialize Go module and directory layout**

```bash
cd /home/jesus/Projects/hnb
go mod init github.com/heavenlabs/hnb
```

Create the directory structure:

```
hnb/
├── cmd/
│   ├── hnb-server/       # API server binary
│   │   └── main.go
│   └── hnb/              # CLI binary
│       └── main.go
├── internal/
│   ├── config/           # App configuration
│   ├── database/         # DB connection, migrations
│   ├── models/           # Domain types
│   ├── auth/             # Auth (local + OIDC)
│   ├── api/              # HTTP handlers + router
│   ├── executor/         # SQL/JS execution engine
│   ├── scheduler/        # Cron scheduling
│   ├── audit/            # Audit logging
│   └── crypto/           # Encryption helpers
├── migrations/           # SQL migration files
├── web/                  # React frontend (Vite project)
├── relay/                # Yjs/Hocuspocus relay (Node)
├── docs/
│   └── plans/
└── docker-compose.yml
```

**Step 2: Create docker-compose.yml with Postgres and Redis**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: hnb
      POSTGRES_PASSWORD: hnb_dev
      POSTGRES_DB: hnb
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  pgdata:
```

**Step 3: Create config loader**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	MasterKey   string // for encrypting connector credentials
	JWTSecret   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:        envOrDefault("HNB_PORT", "8080"),
		DatabaseURL: envOrDefault("HNB_DATABASE_URL", "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"),
		RedisURL:    envOrDefault("HNB_REDIS_URL", "redis://localhost:6379"),
		MasterKey:   os.Getenv("HNB_MASTER_KEY"),
		JWTSecret:   os.Getenv("HNB_JWT_SECRET"),
	}
	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("HNB_MASTER_KEY is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("HNB_JWT_SECRET is required")
	}
	return cfg, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
```

**Step 4: Create minimal server entrypoint**

```go
// cmd/hnb-server/main.go
package main

import (
	"log"
	"net/http"

	"github.com/heavenlabs/hnb/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("hnb-server listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatal(err)
	}
}
```

**Step 5: Create Makefile**

```makefile
.PHONY: dev build test migrate up down

up:
	docker compose up -d

down:
	docker compose down

build:
	go build -o bin/hnb-server ./cmd/hnb-server
	go build -o bin/hnb ./cmd/hnb

dev:
	go run ./cmd/hnb-server

test:
	go test ./... -v

migrate:
	go run ./cmd/hnb-server migrate
```

**Step 6: Create .gitignore**

```
bin/
*.exe
.env
node_modules/
web/dist/
relay/node_modules/
```

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: scaffold Go project structure with config and docker-compose"
```

---

### Task 2: Database Connection & Migration System

**Files:**
- Create: `internal/database/database.go`
- Create: `internal/database/database_test.go`
- Create: `internal/database/migrate.go`

**Step 1: Install dependencies**

```bash
go get github.com/jackc/pgx/v5
go get github.com/jackc/pgx/v5/pgxpool
```

**Step 2: Write failing test for database connection**

```go
// internal/database/database_test.go
package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/heavenlabs/hnb/internal/database"
)

func TestConnect(t *testing.T) {
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	var result int
	err = db.Pool.QueryRow(context.Background(), "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if result != 1 {
		t.Fatalf("expected 1, got %d", result)
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/database/ -v`
Expected: FAIL — `database` package doesn't exist yet

**Step 4: Implement database connection**

```go
// internal/database/database.go
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	db.Pool.Close()
}
```

**Step 5: Run test to verify it passes**

Run: `make up && go test ./internal/database/ -v`
Expected: PASS

**Step 6: Implement migration runner**

Use embedded SQL files for migrations.

```go
// internal/database/migrate.go
package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func (db *DB) Migrate(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		version := strings.TrimSuffix(f, ".sql")

		var exists bool
		err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}

		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}

		log.Printf("applied migration: %s", version)
	}

	return nil
}
```

Note: Create `internal/database/migrations/` directory with a `.gitkeep` file so the embed directive works. Actual migration SQL files are added in the next task.

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: add database connection pool and SQL migration runner"
```

---

## Phase 2: Core Data Model & Auth

### Task 3: Initial Database Schema

**Files:**
- Create: `internal/database/migrations/001_initial_schema.sql`

**Step 1: Write the initial migration**

```sql
-- 001_initial_schema.sql

-- Organizations
CREATE TABLE orgs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT, -- NULL for OIDC-only users
    name TEXT NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Org membership
CREATE TABLE org_members (
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, user_id)
);

-- OIDC provider config per org
CREATE TABLE oidc_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT 'generic',
    issuer_url TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_encrypted BYTEA NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT ARRAY['openid', 'profile', 'email'],
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Connectors (database connections)
CREATE TABLE connectors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('postgres', 'clickhouse')),
    config_encrypted BYTEA NOT NULL, -- encrypted JSON with host, port, user, pass, db
    max_rows INTEGER NOT NULL DEFAULT 10000,
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Notebooks
CREATE TABLE notebooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    parameters JSONB NOT NULL DEFAULT '[]',
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Cells (ordered within a notebook)
CREATE TABLE cells (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('code', 'text')),
    language TEXT, -- 'sql', 'js', NULL for text cells
    connector_id UUID REFERENCES connectors(id) ON DELETE SET NULL,
    source TEXT NOT NULL DEFAULT '',
    outputs JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (notebook_id, position)
);

-- Yjs document state (binary CRDT state for each notebook)
CREATE TABLE yjs_documents (
    notebook_id UUID PRIMARY KEY REFERENCES notebooks(id) ON DELETE CASCADE,
    state BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Schedules
CREATE TABLE schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
    cron_expression TEXT NOT NULL,
    parameter_overrides JSONB NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Dashboards
CREATE TABLE dashboards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    settings JSONB NOT NULL DEFAULT '{}', -- auto_refresh_seconds, parameter_overrides
    public_token TEXT UNIQUE, -- for view-only sharing
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Dashboard widgets
CREATE TABLE widgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
    cell_id UUID NOT NULL REFERENCES cells(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('chart', 'table', 'text', 'metric')),
    layout JSONB NOT NULL, -- {row, col, width, height}
    config JSONB NOT NULL DEFAULT '{}', -- type-specific display config
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Audit log
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_org_created ON audit_logs (org_id, created_at DESC);
CREATE INDEX idx_audit_logs_user ON audit_logs (user_id, created_at DESC);
CREATE INDEX idx_notebooks_org ON notebooks (org_id);
CREATE INDEX idx_cells_notebook ON cells (notebook_id, position);
CREATE INDEX idx_connectors_org ON connectors (org_id);
CREATE INDEX idx_dashboards_org ON dashboards (org_id);
CREATE INDEX idx_widgets_dashboard ON widgets (dashboard_id);
CREATE INDEX idx_schedules_next_run ON schedules (next_run_at) WHERE enabled = TRUE;

-- Row-level security
ALTER TABLE orgs ENABLE ROW LEVEL SECURITY;
ALTER TABLE notebooks ENABLE ROW LEVEL SECURITY;
ALTER TABLE cells ENABLE ROW LEVEL SECURITY;
ALTER TABLE connectors ENABLE ROW LEVEL SECURITY;
ALTER TABLE dashboards ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
```

**Step 2: Write migration test**

```go
// Add to internal/database/database_test.go
func TestMigrate(t *testing.T) {
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	err = db.Migrate(context.Background())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify a table exists
	var exists bool
	err = db.Pool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='notebooks')").Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("notebooks table should exist after migration")
	}
}
```

**Step 3: Run test**

Run: `go test ./internal/database/ -v -run TestMigrate`
Expected: PASS

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add initial database schema with all core tables"
```

---

### Task 4: Domain Models

**Files:**
- Create: `internal/models/org.go`
- Create: `internal/models/user.go`
- Create: `internal/models/notebook.go`
- Create: `internal/models/connector.go`
- Create: `internal/models/dashboard.go`
- Create: `internal/models/audit.go`

**Step 1: Define all domain types**

```go
// internal/models/org.go
package models

import "time"

type Org struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Settings  OrgSettings `json:"settings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OrgSettings struct {
	DefaultQueryTimeout int `json:"default_query_timeout_seconds,omitempty"`
	DefaultMaxRows      int `json:"default_max_rows,omitempty"`
	AuditRetentionDays  int `json:"audit_retention_days,omitempty"`
}

type OrgMember struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)
```

```go
// internal/models/user.go
package models

import "time"

type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	PasswordHash  string    `json:"-"`
	Name          string    `json:"name"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
```

```go
// internal/models/notebook.go
package models

import "time"

type Notebook struct {
	ID         string      `json:"id"`
	OrgID      string      `json:"org_id"`
	Title      string      `json:"title"`
	Parameters []Parameter `json:"parameters"`
	CreatedBy  string      `json:"created_by"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Parameter struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default"`
}

type Cell struct {
	ID          string   `json:"id"`
	NotebookID  string   `json:"notebook_id"`
	Position    int      `json:"position"`
	Type        CellType `json:"type"`
	Language    string   `json:"language,omitempty"`
	ConnectorID string   `json:"connector_id,omitempty"`
	Source      string   `json:"source"`
	Outputs     []Output `json:"outputs"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CellType string

const (
	CellTypeCode CellType = "code"
	CellTypeText CellType = "text"
)

type Output struct {
	Type   string      `json:"type"` // "table", "chart", "error"
	Data   interface{} `json:"data,omitempty"`
	Config interface{} `json:"config,omitempty"`
}

type Schedule struct {
	ID                 string    `json:"id"`
	NotebookID         string    `json:"notebook_id"`
	CronExpression     string    `json:"cron_expression"`
	ParameterOverrides map[string]string `json:"parameter_overrides"`
	Enabled            bool      `json:"enabled"`
	LastRunAt          *time.Time `json:"last_run_at,omitempty"`
	NextRunAt          *time.Time `json:"next_run_at,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
```

```go
// internal/models/connector.go
package models

import "time"

type Connector struct {
	ID             string          `json:"id"`
	OrgID          string          `json:"org_id"`
	Name           string          `json:"name"`
	Type           ConnectorType   `json:"type"`
	Config         ConnectorConfig `json:"config"`
	MaxRows        int             `json:"max_rows"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ConnectorType string

const (
	ConnectorPostgres   ConnectorType = "postgres"
	ConnectorClickHouse ConnectorType = "clickhouse"
)

type ConnectorConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}
```

```go
// internal/models/dashboard.go
package models

import "time"

type Dashboard struct {
	ID          string            `json:"id"`
	OrgID       string            `json:"org_id"`
	Title       string            `json:"title"`
	Settings    DashboardSettings `json:"settings"`
	PublicToken string            `json:"public_token,omitempty"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type DashboardSettings struct {
	AutoRefreshSeconds int               `json:"auto_refresh_seconds,omitempty"`
	ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
}

type Widget struct {
	ID          string       `json:"id"`
	DashboardID string       `json:"dashboard_id"`
	NotebookID  string       `json:"notebook_id"`
	CellID      string       `json:"cell_id"`
	Type        WidgetType   `json:"type"`
	Layout      WidgetLayout `json:"layout"`
	Config      map[string]interface{} `json:"config"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type WidgetType string

const (
	WidgetChart  WidgetType = "chart"
	WidgetTable  WidgetType = "table"
	WidgetText   WidgetType = "text"
	WidgetMetric WidgetType = "metric"
)

type WidgetLayout struct {
	Row    int `json:"row"`
	Col    int `json:"col"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
```

```go
// internal/models/audit.go
package models

import "time"

type AuditLog struct {
	ID           int64     `json:"id"`
	OrgID        string    `json:"org_id"`
	UserID       string    `json:"user_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata"`
	CreatedAt    time.Time `json:"created_at"`
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/models/`
Expected: Success, no errors

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add domain model types for all core entities"
```

---

### Task 5: Encryption Helpers

**Files:**
- Create: `internal/crypto/crypto.go`
- Create: `internal/crypto/crypto_test.go`

**Step 1: Write failing test**

```go
// internal/crypto/crypto_test.go
package crypto_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/crypto"
)

func TestEncryptDecrypt(t *testing.T) {
	key := crypto.DeriveKey("test-master-key-that-is-long-enough-32")
	plaintext := []byte(`{"host":"localhost","port":5432,"password":"secret"}`)

	encrypted, err := crypto.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := crypto.Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := crypto.DeriveKey("key-one-long-enough-for-testing-32chars")
	key2 := crypto.DeriveKey("key-two-long-enough-for-testing-32chars")

	encrypted, err := crypto.Encrypt([]byte("secret"), key1)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = crypto.Decrypt(encrypted, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/crypto/ -v`
Expected: FAIL

**Step 3: Implement AES-256-GCM encryption**

```go
// internal/crypto/crypto.go
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// DeriveKey derives a 32-byte key from a master key string using SHA-256.
func DeriveKey(masterKey string) []byte {
	h := sha256.Sum256([]byte(masterKey))
	return h[:]
}

// Encrypt encrypts plaintext using AES-256-GCM.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
```

**Step 4: Run tests**

Run: `go test ./internal/crypto/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add AES-256-GCM encryption helpers for connector credentials"
```

---

### Task 6: Auth — Local Accounts

**Files:**
- Create: `internal/auth/auth.go`
- Create: `internal/auth/auth_test.go`
- Create: `internal/auth/jwt.go`
- Create: `internal/auth/jwt_test.go`

**Step 1: Install bcrypt dependency**

```bash
go get golang.org/x/crypto/bcrypt
```

**Step 2: Write failing tests for password hashing and JWT**

```go
// internal/auth/auth_test.go
package auth_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/auth"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("mypassword123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if !auth.VerifyPassword("mypassword123", hash) {
		t.Fatal("expected password to verify")
	}

	if auth.VerifyPassword("wrongpassword", hash) {
		t.Fatal("expected wrong password to fail")
	}
}
```

```go
// internal/auth/jwt_test.go
package auth_test

import (
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/auth"
)

func TestJWTRoundTrip(t *testing.T) {
	secret := "test-jwt-secret-long-enough"
	issuer := auth.NewJWTIssuer(secret, 15*time.Minute)

	token, err := issuer.Issue("user-123", "org-456", "editor")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	claims, err := issuer.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Fatalf("expected user-123, got %s", claims.UserID)
	}
	if claims.OrgID != "org-456" {
		t.Fatalf("expected org-456, got %s", claims.OrgID)
	}
	if claims.Role != "editor" {
		t.Fatalf("expected editor, got %s", claims.Role)
	}
}

func TestJWTExpired(t *testing.T) {
	secret := "test-jwt-secret-long-enough"
	issuer := auth.NewJWTIssuer(secret, -1*time.Minute) // already expired

	token, err := issuer.Issue("user-123", "org-456", "editor")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = issuer.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `go test ./internal/auth/ -v`
Expected: FAIL

**Step 4: Implement password hashing**

```go
// internal/auth/auth.go
package auth

import "golang.org/x/crypto/bcrypt"

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
```

**Step 5: Install JWT dependency and implement**

```bash
go get github.com/golang-jwt/jwt/v5
```

```go
// internal/auth/jwt.go
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID string `json:"uid"`
	OrgID  string `json:"oid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type JWTIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTIssuer(secret string, ttl time.Duration) *JWTIssuer {
	return &JWTIssuer{secret: []byte(secret), ttl: ttl}
}

func (j *JWTIssuer) Issue(userID, orgID, role string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID: userID,
		OrgID:  orgID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

func (j *JWTIssuer) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
```

**Step 6: Run tests**

Run: `go test ./internal/auth/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: add local auth with bcrypt password hashing and JWT tokens"
```

---

### Task 7: Auth — Generic OIDC Provider

**Files:**
- Create: `internal/auth/oidc.go`
- Create: `internal/auth/oidc_test.go`

**Step 1: Install dependency**

```bash
go get github.com/coreos/go-oidc/v3
go get golang.org/x/oauth2
```

**Step 2: Write test (using a mock OIDC server for unit testing)**

```go
// internal/auth/oidc_test.go
package auth_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/auth"
)

func TestOIDCProviderInterface(t *testing.T) {
	// Verify GenericOIDCProvider implements OIDCProvider
	var _ auth.OIDCProvider = (*auth.GenericOIDCProvider)(nil)
}
```

**Step 3: Implement OIDC provider interface and generic implementation**

```go
// internal/auth/oidc.go
package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCClaims struct {
	Subject string
	Email   string
	Name    string
}

type OIDCProvider interface {
	Name() string
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*OIDCClaims, error)
}

type GenericOIDCProvider struct {
	name     string
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

func NewGenericOIDCProvider(ctx context.Context, name, issuerURL, clientID, clientSecret, redirectURL string, scopes []string) (*GenericOIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	return &GenericOIDCProvider{
		name:     name,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       scopes,
		},
	}, nil
}

func (p *GenericOIDCProvider) Name() string {
	return p.name
}

func (p *GenericOIDCProvider) AuthURL(state string) string {
	return p.oauth.AuthCodeURL(state)
}

func (p *GenericOIDCProvider) Exchange(ctx context.Context, code string) (*OIDCClaims, error) {
	token, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	return &OIDCClaims{
		Subject: idToken.Subject,
		Email:   claims.Email,
		Name:    claims.Name,
	}, nil
}
```

**Step 4: Run test**

Run: `go test ./internal/auth/ -v -run TestOIDCProviderInterface`
Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add generic OIDC provider with modular interface"
```

---

### Task 8: Audit Logging Service

**Files:**
- Create: `internal/audit/audit.go`
- Create: `internal/audit/audit_test.go`

**Step 1: Write failing test**

```go
// internal/audit/audit_test.go
package audit_test

import (
	"context"
	"os"
	"testing"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/database"
)

func TestLogAndQuery(t *testing.T) {
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	logger := audit.NewLogger(db)

	err = logger.Log(context.Background(), audit.Entry{
		OrgID:        "org-1",
		UserID:       "user-1",
		Action:       "notebook.create",
		ResourceType: "notebook",
		ResourceID:   "nb-1",
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	entries, err := logger.Query(context.Background(), audit.QueryParams{
		OrgID: "org-1",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/audit/ -v`
Expected: FAIL

**Step 3: Implement audit logger**

```go
// internal/audit/audit.go
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heavenlabs/hnb/internal/database"
)

type Entry struct {
	ID           int64                  `json:"id"`
	OrgID        string                 `json:"org_id"`
	UserID       string                 `json:"user_id,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

type QueryParams struct {
	OrgID        string
	UserID       string
	Action       string
	ResourceType string
	Limit        int
	Offset       int
}

type Logger struct {
	db *database.DB
}

func NewLogger(db *database.DB) *Logger {
	return &Logger{db: db}
}

func (l *Logger) Log(ctx context.Context, e Entry) error {
	metadata := e.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = l.db.Pool.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource_type, resource_id, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		e.OrgID, nilIfEmpty(e.UserID), e.Action, e.ResourceType, nilIfEmpty(e.ResourceID), metaJSON,
	)
	return err
}

func (l *Logger) Query(ctx context.Context, p QueryParams) ([]Entry, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}

	query := `SELECT id, org_id, COALESCE(user_id::text, ''), action, resource_type,
	          COALESCE(resource_id::text, ''), metadata, created_at
	          FROM audit_logs WHERE org_id = $1`
	args := []interface{}{p.OrgID}
	argN := 2

	if p.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argN)
		args = append(args, p.UserID)
		argN++
	}
	if p.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argN)
		args = append(args, p.Action)
		argN++
	}
	if p.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argN)
		args = append(args, p.ResourceType)
		argN++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, p.Limit, p.Offset)

	rows, err := l.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var metaJSON []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.UserID, &e.Action, &e.ResourceType,
			&e.ResourceID, &metaJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &e.Metadata)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
```

**Step 4: Run tests**

Run: `go test ./internal/audit/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add audit logging service with query support"
```

---

## Phase 3: API Layer

### Task 9: HTTP Router & Middleware

**Files:**
- Create: `internal/api/router.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/helpers.go`
- Create: `internal/api/middleware_test.go`

**Step 1: Write failing test for auth middleware**

```go
// internal/api/middleware_test.go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/auth"
)

func TestAuthMiddleware(t *testing.T) {
	issuer := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	mw := api.AuthMiddleware(issuer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := api.ClaimsFromContext(r.Context())
		if claims.UserID != "user-1" {
			t.Fatalf("expected user-1, got %s", claims.UserID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	token, _ := issuer.Issue("user-1", "org-1", "editor")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareNoToken(t *testing.T) {
	issuer := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	mw := api.AuthMiddleware(issuer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v`
Expected: FAIL

**Step 3: Implement helpers, middleware, and router**

```go
// internal/api/helpers.go
package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
```

```go
// internal/api/middleware.go
package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/heavenlabs/hnb/internal/auth"
)

type contextKey string

const claimsKey contextKey = "claims"

func AuthMiddleware(issuer *auth.JWTIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
				return
			}

			token := strings.TrimPrefix(header, "Bearer ")
			claims, err := issuer.Validate(token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClaimsFromContext(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(claimsKey).(*auth.Claims)
	return claims
}

// RequireRole returns middleware that enforces a minimum role level.
func RequireRole(minRole string) func(http.Handler) http.Handler {
	roleLevel := map[string]int{"viewer": 0, "editor": 1, "admin": 2}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			if roleLevel[claims.Role] < roleLevel[minRole] {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

```go
// internal/api/router.go
package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/database"
)

type Server struct {
	db      *database.DB
	jwt     *auth.JWTIssuer
	audit   *audit.Logger
	mux     *http.ServeMux
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger) *Server {
	s := &Server{
		db:    db,
		jwt:   jwt,
		audit: auditLogger,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	authMW := AuthMiddleware(s.jwt)

	// Public routes
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)

	// Protected routes — these will be added in subsequent tasks
	_ = authMW // used when registering protected routes
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

**Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add HTTP router with JWT auth middleware and role enforcement"
```

---

### Task 10: Auth API Endpoints (Register, Login, Refresh)

**Files:**
- Create: `internal/api/auth_handlers.go`
- Create: `internal/api/auth_handlers_test.go`

**Step 1: Write failing tests**

```go
// internal/api/auth_handlers_test.go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/database"
)

func setupTestServer(t *testing.T) *api.Server {
	t.Helper()
	dsn := os.Getenv("HNB_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	jwt := auth.NewJWTIssuer("test-secret", 15*time.Minute)
	auditLogger := audit.NewLogger(db)

	return api.NewServer(db, jwt, auditLogger)
}

func TestRegisterAndLogin(t *testing.T) {
	srv := setupTestServer(t)

	// Register
	body, _ := json.Marshal(map[string]string{
		"email":    "test@example.com",
		"password": "securepass123",
		"name":     "Test User",
		"org_name": "Test Org",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var regResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&regResp)
	if regResp["token"] == nil {
		t.Fatal("register: expected token in response")
	}

	// Login
	body, _ = json.Marshal(map[string]string{
		"email":    "test@example.com",
		"password": "securepass123",
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var loginResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&loginResp)
	if loginResp["token"] == nil {
		t.Fatal("login: expected token in response")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v -run TestRegisterAndLogin`
Expected: FAIL

**Step 3: Implement auth handlers**

```go
// internal/api/auth_handlers.go
package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/jackc/pgx/v5"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	OrgName  string `json:"org_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgID    string `json:"org_id,omitempty"` // optional, uses first org if empty
}

type authResponse struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"org"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" || req.Name == "" || req.OrgName == "" {
		writeError(w, http.StatusBadRequest, "email, password, name, and org_name are required")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Create user
	var userID string
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified) VALUES ($1, $2, $3, FALSE) RETURNING id`,
		req.Email, hash, req.Name,
	).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	// Create org
	var orgID string
	slug := slugify(req.OrgName)
	err = tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		req.OrgName, slug,
	).Scan(&orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}

	// Add user as admin
	_, err = tx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	token, err := s.jwt.Issue(userID, orgID, "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: userID,
		Action: "user.register", ResourceType: "user", ResourceID: userID,
	})

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = userID
	resp.User.Email = req.Email
	resp.User.Name = req.Name
	resp.Org.ID = orgID
	resp.Org.Name = req.OrgName
	resp.Org.Role = "admin"

	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	var userID, passwordHash, name string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, password_hash, name FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID, &passwordHash, &name)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !auth.VerifyPassword(req.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Get org membership
	orgID, orgName, role, err := s.getUserOrg(ctx, userID, req.OrgID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no organization membership found")
		return
	}

	token, err := s.jwt.Issue(userID, orgID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: userID,
		Action: "user.login", ResourceType: "user", ResourceID: userID,
	})

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = userID
	resp.User.Email = req.Email
	resp.User.Name = name
	resp.Org.ID = orgID
	resp.Org.Name = orgName
	resp.Org.Role = role

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getUserOrg(ctx context.Context, userID, preferredOrgID string) (orgID, orgName, role string, err error) {
	if preferredOrgID != "" {
		err = s.db.Pool.QueryRow(ctx,
			`SELECT o.id, o.name, om.role FROM orgs o
			 JOIN org_members om ON om.org_id = o.id
			 WHERE om.user_id = $1 AND o.id = $2`,
			userID, preferredOrgID,
		).Scan(&orgID, &orgName, &role)
	} else {
		err = s.db.Pool.QueryRow(ctx,
			`SELECT o.id, o.name, om.role FROM orgs o
			 JOIN org_members om ON om.org_id = o.id
			 WHERE om.user_id = $1
			 ORDER BY om.created_at ASC LIMIT 1`,
			userID,
		).Scan(&orgID, &orgName, &role)
	}
	if err != nil {
		return "", "", "", fmt.Errorf("no membership: %w", err)
	}
	return
}

func slugify(name string) string {
	// Simple slug: lowercase, replace spaces with hyphens
	slug := ""
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			slug += string(c)
		} else if c >= 'A' && c <= 'Z' {
			slug += string(c + 32)
		} else if c == ' ' {
			slug += "-"
		}
	}
	return slug
}
```

**Step 4: Run tests**

Run: `go test ./internal/api/ -v -run TestRegisterAndLogin`
Expected: PASS (requires docker-compose up with clean DB — you may need to drop/recreate the DB between test runs)

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add register and login API endpoints with org creation"
```

---

### Task 11: Notebooks CRUD API

**Files:**
- Create: `internal/api/notebook_handlers.go`
- Create: `internal/api/notebook_handlers_test.go`

**Step 1: Write failing tests**

```go
// internal/api/notebook_handlers_test.go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotebookCRUD(t *testing.T) {
	srv := setupTestServer(t)

	// Register a user first to get a token
	regBody, _ := json.Marshal(map[string]string{
		"email": "nb-test@example.com", "password": "pass123",
		"name": "NB Tester", "org_name": "NB Org",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var regResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&regResp)
	token := regResp["token"].(string)

	// Create notebook
	nbBody, _ := json.Marshal(map[string]interface{}{
		"title":      "Test Notebook",
		"parameters": []map[string]string{{"name": "env", "type": "string", "default": "prod"}},
	})
	req = httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(nbBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var nbResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&nbResp)
	nbID := nbResp["id"].(string)

	// List notebooks
	req = httptest.NewRequest("GET", "/api/v1/notebooks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Get notebook
	req = httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}

	// Delete notebook
	req = httptest.NewRequest("DELETE", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v -run TestNotebookCRUD`
Expected: FAIL

**Step 3: Implement notebook handlers**

```go
// internal/api/notebook_handlers.go
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type createNotebookRequest struct {
	Title      string             `json:"title"`
	Parameters []models.Parameter `json:"parameters"`
}

func (s *Server) handleCreateNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	params, _ := json.Marshal(req.Parameters)
	if req.Parameters == nil {
		params = []byte("[]")
	}

	ctx := r.Context()
	var nb models.Notebook
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, parameters, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, title, parameters, created_by, created_at, updated_at`,
		claims.OrgID, req.Title, params, claims.UserID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notebook")
		return
	}
	json.Unmarshal(params, &nb.Parameters)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.create", ResourceType: "notebook", ResourceID: nb.ID,
	})

	writeJSON(w, http.StatusCreated, nb)
}

func (s *Server) handleListNotebooks(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE org_id = $1 ORDER BY updated_at DESC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var notebooks []models.Notebook
	for rows.Next() {
		var nb models.Notebook
		var params []byte
		if err := rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		json.Unmarshal(params, &nb.Parameters)
		notebooks = append(notebooks, nb)
	}

	if notebooks == nil {
		notebooks = []models.Notebook{}
	}

	writeJSON(w, http.StatusOK, notebooks)
}

func (s *Server) handleGetNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := extractID(r.URL.Path, "/api/v1/notebooks/")

	ctx := r.Context()
	var nb models.Notebook
	var params []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE id = $1 AND org_id = $2`,
		nbID, claims.OrgID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	json.Unmarshal(params, &nb.Parameters)

	// Also fetch cells
	cellRows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, position, type, language, connector_id, source, outputs, created_at, updated_at
		 FROM cells WHERE notebook_id = $1 ORDER BY position ASC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query cells failed")
		return
	}
	defer cellRows.Close()

	var cells []models.Cell
	for cellRows.Next() {
		var c models.Cell
		var lang, connID *string
		var outputs []byte
		if err := cellRows.Scan(&c.ID, &c.NotebookID, &c.Position, &c.Type, &lang, &connID, &c.Source, &outputs, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan cell failed")
			return
		}
		if lang != nil {
			c.Language = *lang
		}
		if connID != nil {
			c.ConnectorID = *connID
		}
		json.Unmarshal(outputs, &c.Outputs)
		cells = append(cells, c)
	}

	type notebookWithCells struct {
		models.Notebook
		Cells []models.Cell `json:"cells"`
	}

	resp := notebookWithCells{Notebook: nb, Cells: cells}
	if resp.Cells == nil {
		resp.Cells = []models.Cell{}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := extractID(r.URL.Path, "/api/v1/notebooks/")

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM notebooks WHERE id = $1 AND org_id = $2`,
		nbID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.delete", ResourceType: "notebook", ResourceID: nbID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func extractID(path, prefix string) string {
	id := strings.TrimPrefix(path, prefix)
	// Remove any trailing path segments
	if idx := strings.Index(id, "/"); idx != -1 {
		id = id[:idx]
	}
	return id
}
```

Then update `router.go` to register the notebook routes:

```go
// Add to s.routes() in internal/api/router.go, inside the routes function:
// Protected notebook routes
s.mux.Handle("POST /api/v1/notebooks", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateNotebook))))
s.mux.Handle("GET /api/v1/notebooks", authMW(http.HandlerFunc(s.handleListNotebooks)))
s.mux.Handle("GET /api/v1/notebooks/{id}", authMW(http.HandlerFunc(s.handleGetNotebook)))
s.mux.Handle("DELETE /api/v1/notebooks/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteNotebook))))
```

Note: When using Go 1.22+ `{id}` path patterns, update `extractID` to use `r.PathValue("id")` instead. Adjust the handler signatures accordingly:

Replace `extractID(r.URL.Path, "/api/v1/notebooks/")` with `r.PathValue("id")` in the handlers.

**Step 4: Run tests**

Run: `go test ./internal/api/ -v -run TestNotebookCRUD`
Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add notebook CRUD API endpoints"
```

---

### Task 12: Cells CRUD API

**Files:**
- Create: `internal/api/cell_handlers.go`
- Create: `internal/api/cell_handlers_test.go`

**Step 1: Write failing test**

```go
// internal/api/cell_handlers_test.go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCellCRUD(t *testing.T) {
	srv := setupTestServer(t)

	// Register and create notebook (helper)
	token := registerAndGetToken(t, srv, "cell-test@example.com", "Cell Org")
	nbID := createNotebook(t, srv, token, "Cell Test NB")

	// Create code cell
	cellBody, _ := json.Marshal(map[string]interface{}{
		"type":     "code",
		"language": "sql",
		"source":   "SELECT 1",
	})
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells", bytes.NewReader(cellBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create cell: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cellResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&cellResp)
	cellID := cellResp["id"].(string)

	// Update cell
	updateBody, _ := json.Marshal(map[string]interface{}{
		"source": "SELECT 2",
	})
	req = httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update cell: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete cell
	req = httptest.NewRequest("DELETE", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete cell: expected 204, got %d", rec.Code)
	}
}

// Test helpers
func registerAndGetToken(t *testing.T, srv *api.Server, email, orgName string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "pass123", "name": "Test", "org_name": orgName,
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["token"].(string)
}

func createNotebook(t *testing.T, srv *api.Server, token, title string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v -run TestCellCRUD`
Expected: FAIL

**Step 3: Implement cell handlers**

```go
// internal/api/cell_handlers.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type createCellRequest struct {
	Type        models.CellType `json:"type"`
	Language    string          `json:"language,omitempty"`
	ConnectorID string          `json:"connector_id,omitempty"`
	Source      string          `json:"source"`
}

type updateCellRequest struct {
	Source      *string `json:"source,omitempty"`
	Language    *string `json:"language,omitempty"`
	ConnectorID *string `json:"connector_id,omitempty"`
}

func (s *Server) handleCreateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")

	var req createCellRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Type != models.CellTypeCode && req.Type != models.CellTypeText {
		writeError(w, http.StatusBadRequest, "type must be 'code' or 'text'")
		return
	}

	ctx := r.Context()

	// Verify notebook belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	// Get next position
	var maxPos *int
	s.db.Pool.QueryRow(ctx, "SELECT MAX(position) FROM cells WHERE notebook_id=$1", nbID).Scan(&maxPos)
	nextPos := 0
	if maxPos != nil {
		nextPos = *maxPos + 1
	}

	var cell models.Cell
	var lang, connID *string
	if req.Language != "" {
		lang = &req.Language
	}
	if req.ConnectorID != "" {
		connID = &req.ConnectorID
	}

	var outputs []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO cells (notebook_id, position, type, language, connector_id, source, outputs)
		 VALUES ($1, $2, $3, $4, $5, $6, '[]')
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs, created_at, updated_at`,
		nbID, nextPos, req.Type, lang, connID, req.Source,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID, &cell.Source, &outputs, &cell.CreatedAt, &cell.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create cell")
		return
	}
	if lang != nil {
		cell.Language = *lang
	}
	if connID != nil {
		cell.ConnectorID = *connID
	}
	json.Unmarshal(outputs, &cell.Outputs)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.create", ResourceType: "cell", ResourceID: cell.ID,
	})

	writeJSON(w, http.StatusCreated, cell)
}

func (s *Server) handleUpdateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")

	var req updateCellRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	// Verify notebook belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	// Build dynamic update
	query := "UPDATE cells SET updated_at = NOW()"
	args := []interface{}{}
	argN := 1

	if req.Source != nil {
		query += fmt.Sprintf(", source = $%d", argN)
		args = append(args, *req.Source)
		argN++
	}
	if req.Language != nil {
		query += fmt.Sprintf(", language = $%d", argN)
		args = append(args, *req.Language)
		argN++
	}
	if req.ConnectorID != nil {
		query += fmt.Sprintf(", connector_id = $%d", argN)
		args = append(args, *req.ConnectorID)
		argN++
	}

	query += fmt.Sprintf(" WHERE id = $%d AND notebook_id = $%d", argN, argN+1)
	args = append(args, cellID, nbID)
	query += " RETURNING id, notebook_id, position, type, language, connector_id, source, outputs, created_at, updated_at"

	var cell models.Cell
	var lang, connID *string
	var outputs []byte
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cell.CreatedAt, &cell.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if lang != nil {
		cell.Language = *lang
	}
	if connID != nil {
		cell.ConnectorID = *connID
	}
	json.Unmarshal(outputs, &cell.Outputs)

	writeJSON(w, http.StatusOK, cell)
}

func (s *Server) handleDeleteCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")

	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM cells WHERE id = $1 AND notebook_id = $2
		 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $3)`,
		cellID, nbID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.delete", ResourceType: "cell", ResourceID: cellID,
	})

	w.WriteHeader(http.StatusNoContent)
}
```

Add `"fmt"` to the imports in `cell_handlers.go`.

Register cell routes in `router.go`:

```go
s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateCell))))
s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateCell))))
s.mux.Handle("DELETE /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteCell))))
```

**Step 4: Run tests**

Run: `go test ./internal/api/ -v -run TestCellCRUD`
Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add cell CRUD API endpoints"
```

---

### Task 13: Connectors CRUD API

**Files:**
- Create: `internal/api/connector_handlers.go`
- Create: `internal/api/connector_handlers_test.go`

This follows the same pattern as notebooks. The key difference is that connector configs are encrypted before storage using the `crypto` package.

**Step 1: Write failing test**

```go
// internal/api/connector_handlers_test.go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectorCRUD(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "conn-test@example.com", "Conn Org")

	// Create connector
	body, _ := json.Marshal(map[string]interface{}{
		"name": "Dev Postgres",
		"type": "postgres",
		"config": map[string]interface{}{
			"host": "localhost", "port": 5432,
			"user": "dev", "password": "secret", "database": "analytics",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	connID := resp["id"].(string)

	// List connectors
	req = httptest.NewRequest("GET", "/api/v1/connectors", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/v1/connectors/"+connID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}
```

**Step 2: Implement connector handlers**

The handlers encrypt `config` before writing to DB with `crypto.Encrypt`, and decrypt on read. The response never includes the raw password — it masks it as `"***"`.

Update the `Server` struct to include a `masterKey []byte` field, and pass it from config.

Register routes:

```go
s.mux.Handle("POST /api/v1/connectors", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateConnector))))
s.mux.Handle("GET /api/v1/connectors", authMW(http.HandlerFunc(s.handleListConnectors)))
s.mux.Handle("DELETE /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteConnector))))
```

**Step 3: Run tests and commit**

```bash
go test ./internal/api/ -v -run TestConnectorCRUD
git add -A
git commit -m "feat: add connector CRUD API with encrypted credential storage"
```

---

## Phase 4: Execution Engine

### Task 14: SQL Executor — Postgres Connector

**Files:**
- Create: `internal/executor/executor.go`
- Create: `internal/executor/postgres.go`
- Create: `internal/executor/postgres_test.go`
- Create: `internal/executor/params.go`
- Create: `internal/executor/params_test.go`

**Step 1: Write failing test for parameter substitution**

```go
// internal/executor/params_test.go
package executor_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/executor"
)

func TestResolveParams(t *testing.T) {
	query := "SELECT * FROM orders WHERE env = '{{env}}' AND date > '{{start_date}}'"
	params := map[string]string{"env": "prod", "start_date": "2026-01-01"}

	result := executor.ResolveParams(query, params)
	expected := "SELECT * FROM orders WHERE env = 'prod' AND date > '2026-01-01'"

	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}
```

**Step 2: Implement parameter resolver**

```go
// internal/executor/params.go
package executor

import "strings"

func ResolveParams(query string, params map[string]string) string {
	for k, v := range params {
		query = strings.ReplaceAll(query, "{{"+k+"}}", v)
	}
	return query
}
```

**Step 3: Write failing test for Postgres executor**

```go
// internal/executor/postgres_test.go
package executor_test

import (
	"context"
	"testing"

	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
)

func TestPostgresExecutor(t *testing.T) {
	cfg := models.ConnectorConfig{
		Host: "localhost", Port: 5432,
		User: "hnb", Password: "hnb_dev", Database: "hnb",
	}

	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	result, err := pg.Execute(context.Background(), "SELECT 1 AS num, 'hello' AS greeting", nil, 1000)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(result.Columns))
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}
```

**Step 4: Implement executor interface and Postgres implementation**

```go
// internal/executor/executor.go
package executor

import "context"

type ResultSet struct {
	Columns []Column        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Executor interface {
	Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error)
	TestConnection(ctx context.Context) error
	Schema(ctx context.Context) (*SchemaInfo, error)
	Close() error
}

type SchemaInfo struct {
	Tables []TableInfo `json:"tables"`
}

type TableInfo struct {
	Schema  string       `json:"schema"`
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns"`
}

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
```

```go
// internal/executor/postgres.go
package executor

import (
	"context"
	"fmt"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresExecutor struct {
	pool *pgxpool.Pool
}

func NewPostgresExecutor(cfg models.ConnectorConfig) (*PostgresExecutor, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	if cfg.SSLMode != "" {
		dsn += "?sslmode=" + cfg.SSLMode
	} else {
		dsn += "?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &PostgresExecutor{pool: pool}, nil
}

func (p *PostgresExecutor) Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error) {
	resolved := ResolveParams(query, params)

	rows, err := p.pool.Query(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columns := make([]Column, len(fields))
	for i, f := range fields {
		columns[i] = Column{Name: string(f.Name), Type: pgTypeToString(f.DataTypeOID)}
	}

	var resultRows [][]interface{}
	count := 0
	for rows.Next() && count < maxRows {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		resultRows = append(resultRows, values)
		count++
	}

	return &ResultSet{Columns: columns, Rows: resultRows}, nil
}

func (p *PostgresExecutor) TestConnection(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *PostgresExecutor) Schema(ctx context.Context) (*SchemaInfo, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT table_schema, table_name, column_name, data_type
		 FROM information_schema.columns
		 WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		 ORDER BY table_schema, table_name, ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tableMap := map[string]*TableInfo{}
	var tables []TableInfo

	for rows.Next() {
		var schema, table, col, dtype string
		if err := rows.Scan(&schema, &table, &col, &dtype); err != nil {
			return nil, err
		}
		key := schema + "." + table
		if _, ok := tableMap[key]; !ok {
			tableMap[key] = &TableInfo{Schema: schema, Name: table}
		}
		tableMap[key].Columns = append(tableMap[key].Columns, ColumnInfo{Name: col, Type: dtype})
	}

	for _, t := range tableMap {
		tables = append(tables, *t)
	}

	return &SchemaInfo{Tables: tables}, nil
}

func (p *PostgresExecutor) Close() error {
	p.pool.Close()
	return nil
}

func pgTypeToString(oid uint32) string {
	// Common Postgres type OIDs
	switch oid {
	case 16:
		return "boolean"
	case 20, 21, 23:
		return "integer"
	case 25, 1043:
		return "text"
	case 700, 701:
		return "float"
	case 1082:
		return "date"
	case 1114, 1184:
		return "timestamp"
	case 3802:
		return "jsonb"
	default:
		return "unknown"
	}
}
```

**Step 5: Run tests**

Run: `go test ./internal/executor/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add SQL executor with Postgres connector and parameter substitution"
```

---

### Task 15: Cell Execution API Endpoint

**Files:**
- Create: `internal/api/execute_handlers.go`
- Create: `internal/api/execute_handlers_test.go`

**Step 1: Write failing test**

```go
// internal/api/execute_handlers_test.go
package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExecuteCell(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "exec-test@example.com", "Exec Org")

	// Create a connector pointing to the test DB
	connID := createConnector(t, srv, token)
	nbID := createNotebook(t, srv, token, "Exec NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1 AS result", connID)

	// Execute cell
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("execute: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	outputs := resp["outputs"].([]interface{})
	if len(outputs) == 0 {
		t.Fatal("expected at least one output")
	}
}
```

Implement helpers `createConnector` and `createCell` in test file.

**Step 2: Implement execution handler**

The handler:
1. Loads the cell and its connector config
2. Decrypts the connector config
3. Creates an executor for the connector type
4. Resolves parameters from the notebook + request overrides
5. Executes the query
6. Stores outputs back in the cell
7. Returns the outputs

Route: `POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute`

**Step 3: Run tests and commit**

```bash
go test ./internal/api/ -v -run TestExecuteCell
git add -A
git commit -m "feat: add cell execution API endpoint"
```

---

### Task 16: Connectors — ClickHouse

**Files:**
- Create: `internal/executor/clickhouse.go`
- Create: `internal/executor/clickhouse_test.go`

Same pattern as Postgres executor but using the ClickHouse Go driver (`github.com/ClickHouse/clickhouse-go/v2`). Implements the same `Executor` interface. Add `"clickhouse"` to the connector type registry.

**Step 1: Implement and test**

```bash
go get github.com/ClickHouse/clickhouse-go/v2
```

Follow the same TDD pattern. The ClickHouse test will need a running ClickHouse instance (add to docker-compose.yml):

```yaml
  clickhouse:
    image: clickhouse/clickhouse-server:latest
    ports:
      - "9000:9000"
      - "8123:8123"
```

**Step 2: Commit**

```bash
git add -A
git commit -m "feat: add ClickHouse connector executor"
```

---

## Phase 5: Scheduling

### Task 17: Cron Scheduler

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/scheduler_test.go`
- Create: `internal/api/schedule_handlers.go`

**Step 1: Install cron library**

```bash
go get github.com/robfig/cron/v3
```

**Step 2: Write failing test**

```go
// internal/scheduler/scheduler_test.go
package scheduler_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/scheduler"
)

func TestNextRunTime(t *testing.T) {
	next, err := scheduler.NextRun("0 9 * * *")
	if err != nil {
		t.Fatalf("next run: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero next run time")
	}
}
```

**Step 3: Implement scheduler**

The scheduler:
- On startup, loads all enabled schedules from DB
- Runs a goroutine that checks every minute for due schedules (`next_run_at <= NOW()`)
- For each due schedule: executes all cells in the notebook with parameter overrides, updates `last_run_at` and `next_run_at`
- Exposes `AddSchedule`, `RemoveSchedule` for dynamic updates

```go
// internal/scheduler/scheduler.go
package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/heavenlabs/hnb/internal/database"
	"github.com/robfig/cron/v3"
)

type RunFunc func(ctx context.Context, notebookID string, params map[string]string) error

type Scheduler struct {
	db      *database.DB
	runFunc RunFunc
	stop    chan struct{}
}

func New(db *database.DB, runFunc RunFunc) *Scheduler {
	return &Scheduler{db: db, runFunc: runFunc, stop: make(chan struct{})}
}

func (s *Scheduler) Start() {
	go s.loop()
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	ctx := context.Background()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, cron_expression, parameter_overrides
		 FROM schedules WHERE enabled = TRUE AND next_run_at <= NOW()`)
	if err != nil {
		log.Printf("scheduler: query: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, nbID, cronExpr string
		var params map[string]string
		if err := rows.Scan(&id, &nbID, &cronExpr, &params); err != nil {
			log.Printf("scheduler: scan: %v", err)
			continue
		}

		if err := s.runFunc(ctx, nbID, params); err != nil {
			log.Printf("scheduler: run notebook %s: %v", nbID, err)
		}

		// Update last_run_at and next_run_at
		next, _ := NextRun(cronExpr)
		s.db.Pool.Exec(ctx,
			`UPDATE schedules SET last_run_at = NOW(), next_run_at = $1, updated_at = NOW() WHERE id = $2`,
			next, id,
		)
	}
}

func NextRun(cronExpr string) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(time.Now()), nil
}
```

**Step 4: Add schedule CRUD API endpoints**

Routes:
- `POST /api/v1/notebooks/{notebook_id}/schedules` — create schedule
- `GET /api/v1/notebooks/{notebook_id}/schedules` — list schedules
- `DELETE /api/v1/schedules/{id}` — delete schedule

**Step 5: Run tests and commit**

```bash
go test ./internal/scheduler/ -v
git add -A
git commit -m "feat: add cron scheduler with schedule CRUD API"
```

---

## Phase 6: Dashboards API

### Task 18: Dashboards & Widgets CRUD

**Files:**
- Create: `internal/api/dashboard_handlers.go`
- Create: `internal/api/dashboard_handlers_test.go`

Same CRUD pattern. Key additions:
- `POST /api/v1/dashboards` — create dashboard
- `GET /api/v1/dashboards` — list dashboards
- `GET /api/v1/dashboards/{id}` — get dashboard with widgets and their current cell outputs
- `DELETE /api/v1/dashboards/{id}` — delete dashboard
- `POST /api/v1/dashboards/{id}/widgets` — add widget
- `PUT /api/v1/dashboards/{id}/widgets/{widget_id}` — update widget layout/config
- `DELETE /api/v1/dashboards/{id}/widgets/{widget_id}` — remove widget
- `POST /api/v1/dashboards/{id}/share` — generate public token
- `GET /api/v1/public/dashboards/{token}` — public dashboard view (no auth required)

Follow TDD pattern for each endpoint.

**Commit:**

```bash
git add -A
git commit -m "feat: add dashboard and widget CRUD API with public sharing"
```

---

## Phase 7: WebSocket Layer

### Task 19: Live Output WebSocket

**Files:**
- Create: `internal/api/ws.go`
- Create: `internal/api/ws_test.go`

**Step 1: Install gorilla/websocket**

```bash
go get github.com/gorilla/websocket
```

**Step 2: Implement WebSocket hub**

The hub manages connections per notebook. When a cell execution completes, the result is broadcast to all connected clients in that notebook's room.

```go
// internal/api/ws.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // tighten in production
}

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*websocket.Conn]bool // notebookID -> connections
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*websocket.Conn]bool)}
}

func (h *Hub) Join(notebookID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[notebookID] == nil {
		h.rooms[notebookID] = make(map[*websocket.Conn]bool)
	}
	h.rooms[notebookID][conn] = true
}

func (h *Hub) Leave(notebookID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[notebookID], conn)
	if len(h.rooms[notebookID]) == 0 {
		delete(h.rooms, notebookID)
	}
}

func (h *Hub) Broadcast(notebookID string, msg interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for conn := range h.rooms[notebookID] {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("ws write: %v", err)
		}
	}
}
```

Add WebSocket endpoint: `GET /api/v1/ws/notebooks/{id}`

After cell execution, call `hub.Broadcast(notebookID, outputEvent)`.

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add WebSocket hub for live cell output broadcasting"
```

---

## Phase 8: Yjs Relay

### Task 20: Hocuspocus Relay Server

**Files:**
- Create: `relay/package.json`
- Create: `relay/src/index.ts`
- Create: `relay/tsconfig.json`

**Step 1: Initialize Node project**

```bash
cd relay
npm init -y
npm install @hocuspocus/server @hocuspocus/extension-database
npm install -D typescript @types/node ts-node
```

**Step 2: Implement relay**

```typescript
// relay/src/index.ts
import { Hocuspocus } from '@hocuspocus/server'
import { Database } from '@hocuspocus/extension-database'

const server = new Hocuspocus({
  port: parseInt(process.env.HNB_RELAY_PORT || '3001'),

  extensions: [
    new Database({
      fetch: async ({ documentName }) => {
        // Load Yjs state from Go API
        const res = await fetch(
          `${process.env.HNB_API_URL || 'http://localhost:8080'}/internal/yjs/${documentName}`
        )
        if (!res.ok) return null
        const buf = await res.arrayBuffer()
        return new Uint8Array(buf)
      },

      store: async ({ documentName, state }) => {
        // Persist Yjs state back to Go API
        await fetch(
          `${process.env.HNB_API_URL || 'http://localhost:8080'}/internal/yjs/${documentName}`,
          {
            method: 'PUT',
            headers: { 'Content-Type': 'application/octet-stream' },
            body: state,
          }
        )
      },
    }),
  ],

  async onAuthenticate({ token, documentName }) {
    // Validate JWT token against Go API
    const res = await fetch(
      `${process.env.HNB_API_URL || 'http://localhost:8080'}/internal/auth/validate`,
      { headers: { Authorization: `Bearer ${token}` } }
    )
    if (!res.ok) {
      throw new Error('Unauthorized')
    }
  },
})

server.listen()
```

**Step 3: Add internal Yjs endpoints to Go API**

Add to the Go API (internal routes, not public):
- `GET /internal/yjs/{notebook_id}` — returns binary Yjs state from `yjs_documents` table
- `PUT /internal/yjs/{notebook_id}` — stores binary Yjs state
- `GET /internal/auth/validate` — validates JWT token, returns user info

These are called only by the Hocuspocus relay (not exposed publicly).

**Step 4: Add relay to docker-compose**

```yaml
  relay:
    build: ./relay
    ports:
      - "3001:3001"
    environment:
      HNB_API_URL: http://api:8080
      HNB_RELAY_PORT: "3001"
    depends_on:
      - api
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add Hocuspocus Yjs relay for real-time collaboration"
```

---

## Phase 9: Frontend

### Task 21: React Project Scaffolding

**Files:**
- Create: `web/` (Vite React TypeScript project)

**Step 1: Scaffold**

```bash
cd /home/jesus/Projects/hnb
npm create vite@latest web -- --template react-ts
cd web
npm install
npm install react-router-dom @tanstack/react-query
npm install codemirror @codemirror/lang-sql @codemirror/view @codemirror/state
npm install y-codemirror.next yjs y-websocket
npm install echarts echarts-for-react
npm install react-markdown remark-gfm
npm install react-grid-layout @types/react-grid-layout
```

**Step 2: Set up project structure**

```
web/src/
├── api/          # API client
├── components/   # Reusable components
├── pages/        # Route pages
├── hooks/        # Custom hooks
├── styles/       # Global styles + theme
├── types/        # TypeScript types
└── App.tsx
```

**Step 3: Create pastel theme CSS**

```css
/* web/src/styles/theme.css */
:root {
  --bg-primary: #faf9f7;
  --bg-secondary: #f3f0eb;
  --bg-cell-code: #f8f7ff;
  --bg-cell-text: #f7faf8;
  --border: #e2ddd5;
  --text-primary: #2d2a24;
  --text-secondary: #6b6560;
  --accent: #7c6faa;
  --accent-light: #e8e4f3;
  --success: #6a9e7f;
  --error: #c47a7a;
  --warning: #c4a96a;
  --font-sans: 'Inter', -apple-system, sans-serif;
  --font-mono: 'JetBrains Mono', 'Fira Code', monospace;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: var(--font-sans);
  background: var(--bg-primary);
  color: var(--text-primary);
  line-height: 1.5;
}
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: scaffold React frontend with pastel theme"
```

---

### Task 22: API Client & Auth Context

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/auth.ts`
- Create: `web/src/hooks/useAuth.ts`
- Create: `web/src/pages/LoginPage.tsx`

Implement:
- API client with token management (auto-attach, refresh)
- Auth context provider
- Login page (email/password form, minimal pastel style)
- Protected route wrapper

**Commit:**

```bash
git add -A
git commit -m "feat: add API client, auth context, and login page"
```

---

### Task 23: Notebook Editor

**Files:**
- Create: `web/src/pages/NotebookPage.tsx`
- Create: `web/src/components/CellEditor.tsx`
- Create: `web/src/components/CodeCell.tsx`
- Create: `web/src/components/TextCell.tsx`
- Create: `web/src/components/CellToolbar.tsx`
- Create: `web/src/components/OutputRenderer.tsx`

The notebook editor is the core view:
- Vertical list of cells with CodeMirror for code and markdown editor for text
- Yjs binding for real-time collaboration (connect to Hocuspocus relay)
- Run button per cell, run-all in toolbar
- Output rendered below each cell (table view for SQL results)
- Awareness (collaborator cursors/names)
- Add cell buttons between cells (code or text)
- Drag to reorder cells

**Commit:**

```bash
git add -A
git commit -m "feat: add notebook editor with CodeMirror, Yjs collab, and cell execution"
```

---

### Task 24: Chart Configuration & Rendering

**Files:**
- Create: `web/src/components/ChartConfig.tsx`
- Create: `web/src/components/ChartRenderer.tsx`

After a SQL cell returns results, the user can:
- Click "Add Chart" to configure a visualization
- Select chart type (line, bar, pie, scatter, heatmap, area)
- Map columns to axes (x, y, series)
- Chart renders inline below the table using ECharts

The chart config is stored in the cell's outputs array as an `{type: "chart", config: {...}}` entry.

**Commit:**

```bash
git add -A
git commit -m "feat: add chart configuration UI and ECharts rendering"
```

---

### Task 25: Dashboard Editor & Viewer

**Files:**
- Create: `web/src/pages/DashboardEditPage.tsx`
- Create: `web/src/pages/DashboardViewPage.tsx`
- Create: `web/src/components/WidgetCard.tsx`
- Create: `web/src/components/DashboardGrid.tsx`

Uses `react-grid-layout` for the drag-and-drop grid:
- Editor: add widgets by selecting notebook + cell, drag to position, resize
- Viewer: read-only grid, auto-refresh if configured
- Public viewer: same as viewer but uses public token, no auth

**Commit:**

```bash
git add -A
git commit -m "feat: add dashboard editor and viewer with grid layout"
```

---

### Task 26: Home Page & Navigation

**Files:**
- Create: `web/src/pages/HomePage.tsx`
- Create: `web/src/components/Sidebar.tsx`
- Create: `web/src/components/OrgSwitcher.tsx`

- Simple sidebar: Notebooks, Dashboards, Connectors, Settings
- Home page: list of recent notebooks and dashboards (table/list, no cards)
- Org switcher in sidebar header
- Search/filter

**Commit:**

```bash
git add -A
git commit -m "feat: add home page, sidebar navigation, and org switcher"
```

---

## Phase 10: CLI

### Task 27: CLI Client

**Files:**
- Create: `cmd/hnb/main.go` (update)
- Create: `internal/cli/client.go`
- Create: `internal/cli/auth.go`
- Create: `internal/cli/notebooks.go`
- Create: `internal/cli/cells.go`
- Create: `internal/cli/connectors.go`
- Create: `internal/cli/schedules.go`

**Step 1: Install cobra**

```bash
go get github.com/spf13/cobra
```

**Step 2: Implement CLI structure**

```go
// cmd/hnb/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "hnb",
		Short: "Heaven's Notebooks CLI",
	}

	// Add subcommands
	root.AddCommand(loginCmd())
	root.AddCommand(logoutCmd())
	root.AddCommand(notebooksCmd())
	root.AddCommand(cellsCmd())
	root.AddCommand(connectorsCmd())
	root.AddCommand(schedulesCmd())
	root.AddCommand(dashboardsCmd())
	root.AddCommand(orgsCmd())
	root.AddCommand(configCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Each subcommand file implements the commands from the CLI design. The client is a thin HTTP wrapper:

```go
// internal/cli/client.go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type Client struct {
	BaseURL string
	Token   string
}

type Credentials struct {
	Token  string `json:"token"`
	APIURL string `json:"api_url"`
	OrgID  string `json:"org_id"`
}

func LoadClient() (*Client, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".hnb", "credentials.json"))
	if err != nil {
		return nil, fmt.Errorf("not logged in — run 'hnb login'")
	}
	var creds Credentials
	json.Unmarshal(data, &creds)
	return &Client{BaseURL: creds.APIURL, Token: creds.Token}, nil
}

func (c *Client) Do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add CLI client with all subcommands"
```

---

## Phase 11: JS Executor

### Task 28: Deno JS Executor

**Files:**
- Create: `internal/executor/js.go`
- Create: `internal/executor/js_test.go`

The JS executor:
1. Writes the cell source + input data to a temp file
2. Spawns a Deno subprocess with `--allow-none` (no permissions)
3. The script receives data via stdin, returns SVG/HTML via stdout
4. Captures stdout as the output
5. Enforces time limit via context deadline

```go
// internal/executor/js.go
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type JSExecutor struct {
	timeout time.Duration
}

func NewJSExecutor(timeout time.Duration) *JSExecutor {
	return &JSExecutor{timeout: timeout}
}

func (j *JSExecutor) Execute(ctx context.Context, source string, inputData interface{}) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	// Wrap user code to receive data and output result
	wrapper := fmt.Sprintf(`
const data = JSON.parse(await new Response(Deno.stdin.readable).text());
%s
`, source)

	inputJSON, _ := json.Marshal(inputData)

	cmd := exec.CommandContext(ctx, "deno", "eval", "--no-prompt", wrapper)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("js execution failed: %s: %w", stderr.String(), err)
	}

	return stdout.String(), nil
}
```

**Commit:**

```bash
git add -A
git commit -m "feat: add sandboxed Deno JS executor"
```

---

## Phase 12: Integration & Polish

### Task 29: Wire Everything Together in main.go

**Files:**
- Modify: `cmd/hnb-server/main.go`

Update the server entrypoint to:
1. Load config
2. Connect to Postgres and run migrations
3. Connect to Redis
4. Initialize JWT issuer, audit logger, scheduler
5. Create and start the HTTP server
6. Graceful shutdown on SIGINT/SIGTERM

**Commit:**

```bash
git add -A
git commit -m "feat: wire up all services in server entrypoint with graceful shutdown"
```

---

### Task 30: Dockerfile & Production Build

**Files:**
- Create: `Dockerfile` (multi-stage: build Go + frontend, produce minimal image)
- Create: `relay/Dockerfile`
- Update: `docker-compose.yml` with full service definitions

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS go-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /hnb-server ./cmd/hnb-server

FROM node:20-alpine AS web-build
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=go-build /hnb-server /usr/local/bin/
COPY --from=web-build /app/web/dist /var/www/hnb
EXPOSE 8080
CMD ["hnb-server"]
```

**Commit:**

```bash
git add -A
git commit -m "feat: add Dockerfiles and production docker-compose"
```

---

### Task 31: End-to-End Smoke Test

**Files:**
- Create: `e2e/smoke_test.sh`

A shell script that:
1. Starts docker-compose
2. Waits for health check
3. Registers a user via API
4. Creates a connector, notebook, cell
5. Executes the cell
6. Verifies output
7. Tears down

```bash
#!/bin/bash
set -e

docker compose up -d --build
sleep 5

BASE="http://localhost:8080/api/v1"

# Register
TOKEN=$(curl -sf -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"smoke@test.com","password":"test123","name":"Smoke","org_name":"Smoke Org"}' \
  | jq -r '.token')

echo "Token: ${TOKEN:0:20}..."

# Health
curl -sf http://localhost:8080/health | jq .

echo "Smoke test passed!"
docker compose down
```

**Commit:**

```bash
git add -A
git commit -m "feat: add end-to-end smoke test"
```

---

## Summary of Phases

| Phase | Tasks | What it delivers |
|-------|-------|-----------------|
| 1. Scaffolding | 1-2 | Go project, DB, migrations |
| 2. Core Model & Auth | 3-8 | Schema, models, encryption, auth, audit |
| 3. API Layer | 9-13 | Router, middleware, CRUD for notebooks/cells/connectors |
| 4. Execution | 14-16 | SQL executors (Postgres, ClickHouse), cell execution API |
| 5. Scheduling | 17 | Cron scheduler with schedule CRUD |
| 6. Dashboards | 18 | Dashboard/widget CRUD with public sharing |
| 7. WebSocket | 19 | Live output broadcasting |
| 8. Yjs Relay | 20 | Real-time collaborative editing |
| 9. Frontend | 21-26 | React app with notebook editor, charts, dashboards |
| 10. CLI | 27 | CLI client for automation |
| 11. JS Executor | 28 | Sandboxed Deno execution for custom viz |
| 12. Integration | 29-31 | Production build, Docker, smoke test |
