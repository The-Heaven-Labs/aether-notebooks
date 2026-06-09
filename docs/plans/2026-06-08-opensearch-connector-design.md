# OpenSearch Connector — Design Document

**Date:** 2026-06-08  
**Status:** Approved  
**Branch:** `feature/opensearch-connector`

## Overview

Add an OpenSearch connector to HNB, enabling SQL-based querying of OpenSearch indices via the SQL plugin's REST API. This implementation also introduces a **Connector Driver Registry** to make the connector system extensible — paving the way for future plugin-style connectors.

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Authentication | Basic auth only (user/password), with unauthenticated support | Covers self-hosted and dev scenarios; AWS SigV4 can be added later |
| Pagination | Single-request with truncation warning | Cursor pagination doesn't work with aggregations; keeps executor stateless |
| Config model | Connector Driver Registry with per-driver config structs | Eliminates hardcoded type switches; enables future plugin system |
| Frontend forms | Hardcoded per-type (no dynamic form generation yet) | Ship faster; dynamic forms can come with actual plugin system |
| DB constraint | Drop CHECK constraint on `connectors.type` | Validation moves to driver registry; no migration needed per new type |
| Dev environment | OpenSearch Docker service + curl-based seed script | Mirrors existing pattern (cloudtrail-loader); zero dependencies |

---

## Architecture: Connector Driver Registry

### Problem

The current architecture hardcodes connector type knowledge across multiple layers:
- `ConnectorConfig` — flat struct mixing all types' fields
- `handleCreateConnector` — hardcoded type validation
- `buildExecutor` — switch statement on type
- `ConnectorsPage.tsx` — fixed form fields

Adding a new connector type means modifying all of these.

### Solution: `ConnectorDriver` Interface

```go
// ConnectorDriver defines what each connector type must implement
type ConnectorDriver interface {
    Type() models.ConnectorType
    ConfigSchema() ConfigSchema
    NewExecutor(rawConfig json.RawMessage) (Executor, error)
    TestConfig(ctx context.Context, rawConfig json.RawMessage) error
}

// ConfigSchema describes the configuration fields for a connector type
type ConfigSchema struct {
    Fields []ConfigField
}

type ConfigField struct {
    Name        string
    Type        string // "string", "int", "bool"
    Required    bool
    Default     interface{}
    Description string
}
```

### Registry

```go
// Global registry
var drivers = map[models.ConnectorType]ConnectorDriver{}

func RegisterDriver(d ConnectorDriver) {
    drivers[d.Type()] = d
}

// In init() or main():
RegisterDriver(&PostgresDriver{})
RegisterDriver(&ClickHouseDriver{})
RegisterDriver(&OpenSearchDriver{})
```

### API Handler Changes

- `handleCreateConnector` → validates type against registry, not hardcoded list
- `buildExecutor` → calls `drivers[connType].NewExecutor(configEnc)`
- `handleTestConnectorConfig` → calls `drivers[req.Type].TestConfig(ctx, configJSON)`
- No more switch statements scattered across handlers

### Model Changes

- `ConnectorConfig` struct is deprecated for storage
- `Connector.Config` becomes `json.RawMessage` in the model (or a wrapper that preserves raw JSON)
- Each driver defines its own internal config struct (e.g., `opensearchConfig{...}`)
- Existing Postgres/ClickHouse drivers get their own config structs too (`postgresConfig`, `clickhouseConfig`)

---

## OpenSearch Executor

### Config

```go
type opensearchConfig struct {
    Host     string `json:"host"`
    Port     int    `json:"port"`      // default 9200
    User     string `json:"user"`      // empty = unauthenticated
    Password string `json:"password"`
    UseTLS   bool   `json:"use_tls"`
}
```

### Executor Struct

```go
type OpenSearchExecutor struct {
    baseURL    string
    httpClient *http.Client
}
```

### Execute Flow

1. POST to `{baseURL}/_plugins/_sql` with `{"query": "<resolved SQL>"}`
2. Parse JDBC response: `schema` → `[]Column`, `datarows` → `[][]interface{}`
3. Apply `maxRows` cap on the client side (truncate rows if response exceeds it)
4. If `total > len(datarows)`, include a truncation note in the result
5. Map OpenSearch types (`long`, `integer`, `text`, `keyword`, `boolean`, `float`, `double`, `date`, etc.) to HNB column type strings

### Schema Implementation

- Execute `SHOW TABLES LIKE %` to list all indices
- For each index, execute `DESCRIBE <index>` to get field names and types
- Map to `SchemaInfo{Tables: []TableInfo{...}}`

### Databases Implementation

- OpenSearch doesn't have "databases" — it's a flat namespace of indices
- Return the list of index names (same data as Schema returns as tables)

### TestConnection

- Simple `GET /` or `GET /_cluster/health` to verify connectivity
- Also validates credentials if auth is configured

### Key Considerations

- No connection pooling (stateless HTTP)
- `httpClient` respects the connector's `timeout_seconds`
- TLS: `use_tls=true` → `https://`, otherwise `http://`
- `Close()` is a no-op

### OpenSearch SQL Limitations (documented for users)

- 10k row default limit (adjustable server-side via `plugins.query.size_limit`)
- No cursor pagination for aggregation queries
- No aggregation over expressions (`avg(log(age))` not supported)
- JOIN limited to two indices, no aggregation on joined results
- Pagination only works for basic SELECT queries

---

## Database Migration

Single migration to drop the CHECK constraint:

```sql
ALTER TABLE connectors DROP CONSTRAINT connectors_type_check;
```

- Column stays `TEXT NOT NULL`
- Existing `postgres` and `clickhouse` rows unaffected
- Validation moves to driver registry at the API layer

---

## Docker Dev Environment

### New Services in `docker-compose.dev.yml`

```yaml
hnb-opensearch:
  image: opensearchproject/opensearch:2.19.0
  environment:
    - discovery.type=single-node
    - DISABLE_SECURITY_PLUGIN=true
    - plugins.sql.enabled=true
  ports:
    - "9200:9200"
  volumes:
    - osdata:/usr/share/opensearch/data
  healthcheck:
    test: ["CMD-SHELL", "curl -sf http://localhost:9200/_cluster/health || exit 1"]
    interval: 10s
    timeout: 5s
    retries: 20

opensearch-loader:
  image: alpine:3.20
  depends_on:
    hnb-opensearch:
      condition: service_healthy
  volumes:
    - ./dev/opensearch-seed.sh:/seed.sh:ro
  entrypoint: ["sh", "/seed.sh"]
```

### Seed Script (`dev/opensearch-seed.sh`)

Creates two indices with sample data:

1. **`ecommerce`** — fields: id, product_name, category, price, quantity, order_date, customer_name (~200 docs)
2. **`logs`** — fields: timestamp, level, message, service, response_time_ms (~100 docs)

Two indices provide enough variety to test `SHOW TABLES`, `DESCRIBE`, `GROUP BY`, filtering, etc.

### New Volume

`osdata` added to the `volumes:` section.

---

## Frontend Changes

### ConnectorsPage.tsx

- Add `opensearch` to the `ConnectorType` union
- Add conditional rendering for OpenSearch-specific fields:
  - Host, Port (default 9200), User, Password, Use TLS (checkbox)
  - No "Database" or "SSL Mode" fields
- Port auto-switches to 9200 when type changes to `opensearch`

### Types (`web/src/types/index.ts`)

- Add `use_tls?: boolean` to the `Connector.config` interface

---

## Files Changed (Summary)

| File | Change |
|------|--------|
| `internal/executor/driver.go` | New: `ConnectorDriver` interface, `ConfigSchema`, registry |
| `internal/executor/postgres_driver.go` | New: `PostgresDriver` implementing `ConnectorDriver` |
| `internal/executor/clickhouse_driver.go` | New: `ClickHouseDriver` implementing `ConnectorDriver` |
| `internal/executor/opensearch.go` | New: `OpenSearchExecutor` |
| `internal/executor/opensearch_driver.go` | New: `OpenSearchDriver` implementing `ConnectorDriver` |
| `internal/executor/opensearch_test.go` | New: unit tests |
| `internal/models/connector.go` | Update: `ConnectorConfig` → raw JSON support, add `ConnectorOpenSearch` type |
| `internal/api/connector_handlers.go` | Refactor: use driver registry instead of hardcoded switches |
| `internal/api/execute_handlers.go` | Refactor: use driver registry for `buildExecutor` |
| `internal/database/migrations/0XX_drop_connector_type_check.sql` | New: drop CHECK constraint |
| `docker-compose.dev.yml` | Add OpenSearch service + loader |
| `dev/opensearch-seed.sh` | New: seed script |
| `web/src/pages/ConnectorsPage.tsx` | Add OpenSearch form fields |
| `web/src/types/index.ts` | Add `use_tls` to config |

---

## Future Work (Out of Scope)

- Dynamic frontend form generation from `ConfigSchema`
- AWS SigV4 authentication
- Cursor-based pagination for basic queries
- Connector plugin system (load drivers from external binaries)
- OpenSearch Dashboards integration for visual query building
