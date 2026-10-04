# Design: Databricks Connector Type (SQL only)

- **Date:** 2026-10-03
- **Status:** Validated design (brainstormed against `main` @ `1bc61f23`)
- **Branch:** `feat/databricks-connector`
- **Upstream target:** new `internal/executor/databricks{,_driver}.go`, connector-config refactor in
  `internal/api/connector_handlers.go`, frontend `ConnectorsPage` additions

## 1. Problem

Aether has no way to query Databricks. Users want to run SQL against a Databricks SQL warehouse
(or all-purpose cluster) from notebook cells, the agent, MCP, and dashboard widgets, exactly as
they do with Postgres and ClickHouse connectors today.

The platform already constrains execution to SQL (`language == "sql"`, see
`internal/api/execute_handlers.go`), so no language work is required. The work is:

1. A new `ConnectorDriver` implementation that speaks Databricks' SQL protocol.
2. Config plumbing that can carry driver-specific fields — the shared
   `models.ConnectorConfig` struct (host/port/user/password/database/ssl_mode) cannot express
   Databricks' `http_path`, `token`/OAuth credentials, `catalog`, `schema`. This is the same
   limitation that silently drops OpenSearch's `use_tls` today (`opensearch.go:18` vs
   `connector_handlers.go:22`).

Everything downstream of the driver registry (`query_runner.go`, agent tools, MCP, dashboards,
CLI) already routes through `executor.GetDriver(type)` and needs no per-type changes.
ClickHouse warehouse/permission machinery is explicitly ClickHouse-only and out of scope;
Databricks uses stored connector credentials like Postgres.

## 2. Decisions (validated)

| # | Question | Decision |
|---|---|---|
| 1 | Runtime driver | **Official `github.com/databricks/databricks-sql-go`** (v1.16.0, Apache-2.0, Databricks-maintained). Default pure-Go Thrift backend via `database/sql`, `CGO_ENABLED=0`-compatible. Rejected: hand-rolled Statement Execution REST client (reimplements auth/polling/paging/types, loses Cloud Fetch). |
| 2 | Authentication | **PAT + OAuth M2M (service principal)** in v1. Auth-type selector; `token` or `client_id`/`client_secret`. Interactive U2M excluded — the server cannot own a browser flow for a shared connector. |
| 3 | Config plumbing | **Raw JSON config end-to-end** (see §6). Drivers keep typed configs internally; `ConfigField` gains `Secret` so `ConfigSchema()` drives masking. Fixes OpenSearch `use_tls` and CLI parity as side effects. Rejected: extending the shared struct (bloat, doesn't fix anything); nested `options` map (inconsistent API shape). |
| 4 | Compute target | SQL warehouse or all-purpose cluster — identical from the driver's perspective (`http_path`). No distinction in code or UI. |
| 5 | Schema introspection | `information_schema` per accessible catalog (Unity Catalog and UC-enabled hive_metastore). Legacy non-`information_schema` fallback deliberately out of scope (see §7). |
| 6 | Testing | Unit + API-handler tests in CI; **env-gated live integration tests** against the developer's workspace for PAT and M2M; mandatory real-browser validation with agent-browser. |
| 7 | Scope | SQL cells only (platform default). No jobs, no Python/Spark, no U2M, no ClickHouse-style warehouse permission integration, no legacy schema fallback. |

## 3. Architecture

```
                          Aether                                         Databricks
─────────────────────────────────────────────               ─────────────────────────
 cell run / agent tool / MCP / dashboard widget
        │
        ▼
 openQuery / openAgentExecutor  (existing, type-agnostic)
        │  config_encrypted → crypto.Decrypt
        ▼
 executor.GetDriver("databricks") → DatabricksDriver.NewExecutor
        │
        ▼
 DatabricksExecutor (database/sql)  ──── HTTPS/Thrift ───▶  SQL warehouse / cluster
        │                                                    (token endpoint when M2M)
        ▼
 ResultSet (row/byte capped via shared rowAccumulator)
```

- One new driver, self-registered via `init()` (`internal/executor/driver.go:47`).
- New `models.ConnectorDatabricks ConnectorType = "databricks"`.
- No DB migration: `connectors_type_check` was dropped in
  `internal/database/migrations/V053__drop_connector_type_check.sql`.

## 4. Connector config

| Field | Required | Secret | Notes |
|---|---|---|---|
| `host` | yes | no | Workspace hostname (`dbc-….cloud.databricks.com`); driver strips `https://`/trailing slash |
| `http_path` | yes | no | From workspace "Connection details" (`/sql/1.0/warehouses/…`) |
| `auth_type` | yes, default `pat` | no | `pat` \| `oauth_m2m` |
| `token` | if `pat` | **yes** | Databricks PAT |
| `client_id` | if `oauth_m2m` | no | Service principal application ID |
| `client_secret` | if `oauth_m2m` | **yes** | Service principal OAuth secret |
| `catalog` | no | no | Initial catalog (`WithInitialNamespace`) |
| `schema` | no | no | Initial schema (`WithInitialNamespace`) |

Port is fixed at 443 (Databricks is HTTPS-only); no user field. Validation lives in the driver
(`auth_type` conditional requirements, non-empty `host`/`http_path`), surfaced by
`POST /connectors/test` as `{"ok": false, "error": …}`.

Driver construction (`internal/executor/databricks.go`):

```go
dbsql.NewConnector(
    dbsql.WithServerHostname(host),        // scheme stripped, trailing "/" removed
    dbsql.WithPort(443),
    dbsql.WithHTTPPath(httpPath),
    dbsql.WithAccessToken(token),           // or WithClientCredentials(clientID, clientSecret)
    dbsql.WithInitialNamespace(catalog, schema), // when set
)
db := sql.OpenDB(conn)
```

`WithClientCredentials` makes the driver mint and refresh short-lived tokens per connection;
Aether stores only the client ID/secret (AES-encrypted like a password).

## 5. Executor behavior

`internal/executor/databricks_driver.go`:

- `Type()` → `models.ConnectorDatabricks`
- `ConfigSchema()` → §4 fields, `Secret: true` on `token`/`client_secret`
- `NewExecutor(raw)` → parse typed config, validate, construct executor
- `TestConfig(ctx, raw)` → `NewExecutor` + `TestConnection`

`internal/executor/databricks.go`:

- **Construction:** validate conditional auth; normalize host; `sql.OpenDB`; bound `PingContext`
  at 50 s (warehouses auto-stop; cold starts can take tens of seconds; 50 s stays under the
  server's 60 s write timeout). Failure closes the handle.
  The session timezone is pinned to UTC so DATE/TIMESTAMP parsing is deterministic across warehouses.
- **Execute:** identical shape to the other executors:
  1. `ResolveParams` (`{{param}}` substitution).
  2. Prepend `/* aether_user:<email> */` when `CtxUserEmail` is set (as Postgres/ClickHouse do).
  3. Classify leading keywords (`USE`, `SET`, `CREATE`, `DROP`, `ALTER`, `INSERT`, `UPDATE`,
     `DELETE`, `TRUNCATE`, `MERGE`, `GRANT`, `REVOKE`, `OPTIMIZE`, `VACUUM`, `REFRESH`, `MSCK`,
     `COPY`, `CACHE`, `UNCACHE`, `COMMENT`, `ANALYZE`) → `ExecContext` (no result set), else
     `QueryContext`.
  4. Scan `[]any` per row, normalize values (timestamps → RFC3339; `sql.RawBytes` →
     string/bytes as JSON-friendly; DECIMAL stays the driver's exact string; nested
     ARRAY/MAP/STRUCT/VARIANT arrive as JSON strings from the driver and pass through).
  5. Enforce `OutputLimits` via the shared `newRowAccumulator` (`executor.go:176`) so byte/row
     caps and truncation metadata are consistent with every other connector.
  6. Column names from `rows.Columns()`; type names from `rows.ColumnTypes().DatabaseTypeName()`
     with `"unknown"` fallback.
- **TestConnection:** `PingContext` (driver supports context cancellation).
- **Schema / Databases:** §7.
- **Close:** idempotent; closes the `database/sql` handle.

## 6. Raw-JSON config refactor (backend)

`internal/models/connector.go`, `internal/api/connector_handlers.go`, `internal/executor/driver.go`:

1. `ConfigField` gains `Secret bool`.
2. `createConnectorRequest.Config` / `updateConnectorRequest.Config` / the test-config request
   become `json.RawMessage`.
   - Create: `{}` when omitted; must be a JSON object; encrypted as-is.
   - Test: passed through untouched to `driver.TestConfig`.
3. Update becomes a key-level merge: decrypt stored → `map[string]any`; incoming keys overwrite,
   except a key declared `Secret` by the driver whose incoming value is empty/missing — that
   keeps the stored value (today's password semantics, generalized).
4. Responses decrypt to `map[string]any` and mask every `Secret` field as `***`; fallback
   key-name masking (`password`, `token`, `client_secret`) if a driver is unknown.
5. Drivers mark secrets: Postgres/ClickHouse/OpenSearch `password`, Databricks `token` +
   `client_secret`. (OpenSearch `password` is already masked today; now it is declared.)
6. `models.ConnectorConfig` remains as an internal type for the ClickHouse warehouse/pool paths
   (`warehouse_sync.go`, `execution_target.go`, `executor/pool.go`), which keep unmarshalling it;
   only the API layer stops using it.

Backward compatibility: response JSON shape for existing types is unchanged; CLI already posts
free-form config maps, and its latent `use_tls` drop disappears. Regression tests cover
postgres/clickhouse/opensearch round-trips.

## 7. Schema introspection

- **`Databases(ctx)`** → catalogs: `SHOW CATALOGS`, skip `system`, sorted. The UI's database
  picker therefore lists Unity Catalog catalogs.
- **`Schema(ctx)`** → per accessible catalog:

  ```sql
  SELECT c.table_schema, c.table_name, c.column_name, c.data_type, c.comment, t.comment
  FROM `<catalog>`.information_schema.columns c
  LEFT JOIN `<catalog>`.information_schema.tables t
    ON t.table_catalog = c.table_catalog
   AND t.table_schema  = c.table_schema
   AND t.table_name    = c.table_name
  WHERE c.table_schema <> 'information_schema'
  ORDER BY c.table_schema, c.table_name, c.ordinal_position
  ```

- Aether's two-level model flattens Databricks' three-level names:
  `TableInfo.Schema = "catalog.schema"`, `TableInfo.Name = table`, so `main.sales.orders`
  is unambiguous for the schema browser, allow/deny lists, and agents.
- Catalog identifiers come from `SHOW CATALOGS` and are validated (`^[A-Za-z0-9_]+$`) before
  interpolation (identifiers cannot be parameterized).
- Inaccessible catalogs are skipped; if every catalog fails, the first error surfaces.
- Legacy non-`information_schema` workspaces are out of scope for v1: a `SHOW`/`DESCRIBE`
  fallback would be N+1 and column-poor; add later only if a real legacy workspace appears.

## 8. Frontend

`web/src/pages/ConnectorsPage.tsx`:

- Add `databricks` to the `ConnectorType` union and both create/edit dropdowns; update the
  subtitle text.
- Conditional Databricks form block: host, http_path, auth-type select, token **or**
  client_id + client_secret, catalog, schema. Port/database/SSL hidden for this type.
- Secrets render as password inputs; on edit they start blank and are omitted from the payload
  (server merge keeps the stored value). Payload construction follows the existing conditional
  pattern (`...(type === 'opensearch' ? { use_tls } : {})`).

`web/src/types/index.ts`: widen the connector config type with optional fields for all types
(host/port/database/user/ssl_mode/use_tls/token/http_path/auth_type/client_id/client_secret/
catalog/schema).

SQL editor: map `databricks` → CodeMirror `StandardSQL` dialect in
`web/src/components/Cell.tsx` and `SqlEditor.tsx` (closest keyword set; cosmetic, easily changed).
`ConnectorSelector`, warehouse settings, and notebook pinning are type-agnostic or
ClickHouse-gated and need no changes.

## 9. Testing

- **Unit (`internal/executor/databricks_*_test.go`):** config parsing/validation (PAT vs M2M,
  missing fields, defaults), host normalization, `ConfigSchema` secret flags, statement
  classification, value normalization helpers.
- **API handler tests (real DB, no mocks, `testhelpers_test.go`):**
  - databricks connector create/get/list/update round-trip;
  - `token`/`client_secret` masked as `***` in responses;
  - update with empty secret keeps the stored value; non-empty rotates it;
  - OpenSearch `use_tls` regression (round-trips through create/get/update);
  - `POST /connectors/test` invalid config → `{"ok": false, "error": …}`.
- **Live integration (env-gated, skipped by default, never in CI):**
  `AETHER_TEST_DATABRICKS_HOST`, `_HTTP_PATH`, `_TOKEN`, and optionally
  `_CLIENT_ID`/`_CLIENT_SECRET`. Covers: `TestConnection`, `SELECT 1`, type round-trip
  (decimal, timestamp, array), `Schema`, `Databases` — for PAT and M2M.
- **Frontend:** ConnectorsPage component test for the Databricks form + create payload;
  `npx tsc --noEmit`; `npm run build`; `task test:e2e`.
- **Real-browser validation (mandatory):** dev stack + agent-browser — create connector with
  Test, run a SELECT cell in a notebook, open the schema browser.

## 10. Rollout

1. **Config refactor + secret flags** — independent, fully regression-tested; lands first so
   the driver work builds on it.
2. **Databricks driver + executor + unit/API tests.**
3. **Frontend form + dialect + tests.**
4. **Live integration validation against the developer workspace, docs (README/FRONTEND.md),
   Swagger regen** — then PR.

Single feature branch (`feat/databricks-connector`); CI gates: `task check`,
`cd web && npx tsc --noEmit`, `cd web && npm run build`, relay build, `task test:e2e`.

## 11. Out of scope

- Interactive OAuth U2M (browser flow) for the connector.
- Databricks Jobs / notebooks execution, Python/Spark cells.
- ClickHouse-style managed warehouse permissions and per-user identities.
- Legacy (non-`information_schema`) schema browsing fallback.
- Telemetry tuning for the driver (left at driver defaults).
- Table allow/deny enforcement changes (inherited from existing platform behavior).

## 12. References

- Driver: `github.com/databricks/databricks-sql-go` v1.16.0 (`dbsql.NewConnector`,
  `WithAccessToken`, `WithClientCredentials`, `WithInitialNamespace`, `WithHTTPPath`).
- Executor interface: `internal/executor/driver.go`; shared limits/params:
  `internal/executor/executor.go`, `params.go`.
- Extension points validated as type-agnostic: `internal/api/query_runner.go` (default branch),
  `internal/agent/execution_target.go` (`openAgentExecutor` default branch),
  `internal/cli/connectors.go` (free-form config JSON).
- Config/data-loss precedent: `internal/executor/opensearch.go:13-19` (`use_tls`) vs
  `internal/api/connector_handlers.go:19-28`.
- Type-check removal precedent: `internal/database/migrations/V053__drop_connector_type_check.sql`.
