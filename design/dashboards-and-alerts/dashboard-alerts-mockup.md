# Dashboards and alerts mockup — implementation companion

Companion to [`dashboard-alerts-mockup.html`](./dashboard-alerts-mockup.html). The HTML is a product/design prototype, not an implementation. This document describes the smallest coherent production path for the dashboard builder and Alerts surfaces shown there.

## Product boundary

Spaniel is a local OpenTelemetry viewer. Dashboards and alerts should build on the same local DuckDB data that powers Traces, Spans, Logs, Metrics, Service Map, and Sessions; they must not introduce a separate telemetry store or an unrestricted SQL console.

The user-facing model:

- **Dashboard**: a saved layout of query-backed panels plus optional user-defined variables.
- **Panel**: a title, display type, query, position, and presentation options.
- **Reusable variable**: a dashboard-defined control such as `$customer_tier`, mapped to a bounded attribute or fixed option list.
- **Magic variable**: a built-in value such as `$service_name`, `$environment`, `$window`, `$operation_name`, `$status`, or selection context (`$selected_trace_id`). It has fixed semantics and requires no dashboard setup.
- **Alert rule**: a saved query and evaluation policy; it produces alert instances keyed by bounded group labels.
- **Alert instance**: the stateful result for one rule/group: pending, firing, resolved, silenced, or acknowledged.

Keep magic variables distinct from reusable variables. A dashboard author can name a custom variable anything useful; they cannot redefine what `$service_name` means.

## SQL help and schema reference

SQL authors need three layers of help, using **one canonical schema catalog**:

1. The query composer gets a quiet `Schema & SQL` action beside the read-only SQL label. It opens the schema reference in the same tab and returns the author to their unsaved dashboard or alert draft.
2. `/docs/database-schema` is the full reference. It lists the stable `telemetry_*` DuckDB views, their columns/types/descriptions, panel-result-shape conventions, named parameter rules, and small working query samples.
3. Existing telemetry search remains the fast path: metrics, spans, traces, logs, and attributes can insert a known-good sample into the composer. The docs page is for understanding and adaptation, not a competing query builder.

Do not create a hard-coded “documentation schema” separate from the query catalog. Define an internal `SchemaCatalog` model and publish it through `GET /api/database-schema` (or extend the existing catalog response with a versioned `schema` resource). The catalog is the single source for:

- the documentation page and its table/column search;
- composer table/column tooltips and result-shape hints;
- query-catalog descriptions and generated sample SQL;
- backend result-shape validation messages where a human-readable column name is useful.

### Generated artifacts

Yes—generate the docs. Add `cmd/genschema` and make it a `make generate` step. It should combine two inputs:

- **Curated metadata in source**: view purpose, column descriptions, sensitivity, supported panel shapes, parameter notes, and hand-written examples. DuckDB cannot infer those useful semantics from a column type.
- **Live DuckDB introspection**: `DESCRIBE telemetry_spans`, `DESCRIBE telemetry_traces`, `DESCRIBE telemetry_logs`, and the other allowlisted views. The generator fails if the curated catalog is stale, points at a missing view/column, or exposes an unallowlisted physical table.

From that one validated catalog, generate:

```text
internal/generated/schema_catalog.json   # embedded by the binary and served by /api/database-schema
docs/database-schema.md                  # repository-readable reference
docs/database-schema.html                # static, standalone reference for GitHub Pages or file sharing
frontend/src/generated/schemaCatalog.ts  # optional typed fallback for editor loading states/tests
```

The runtime `/docs/database-schema` page should usually fetch the API catalog so it always matches the running binary. The generated HTML is a portable snapshot for people who want to browse documentation outside Spaniel; stamp it with the catalog version and Spaniel build version. Do not hand-edit any generated output.

### DuckDB introspection details

`cmd/genschema` should open the same initialized, read-only DuckDB database that Spaniel uses for query previews. It must inspect only an explicit allowlist of public query views; never enumerate and publish every table in the database.

For each catalog entry, issue both of these read-only queries:

```sql
DESCRIBE telemetry_spans;

SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'main' AND table_name = 'telemetry_spans'
ORDER BY ordinal_position;
```

`DESCRIBE` is convenient for a compact human-friendly report; `information_schema.columns` provides a stable machine-readable assertion. The generator compares the observed column names/types against the catalog entry, fails on a missing or incompatible documented column, and writes the observed schema fingerprint into the generated catalog. It should also inspect `duckdb_views()`/`information_schema.views` only to assert that each allowlisted `telemetry_*` view exists—not to discover extra public API surface automatically.

The runtime API should return the generated catalog rather than repeat introspection per browser request. Re-run generation whenever migrations or curated view definitions change. In tests, initialize a fresh DuckDB database, run migrations, load the catalog, and assert its fingerprint and each documented view/column.

### Where “Use it for” comes from

DuckDB knows a column is `BIGINT`; it does not know whether `duration_ns` represents latency or whether `trace_id` should become a clickable trace link. Therefore “Use it for” comes from a small, reviewed metadata record next to the query-view definitions. For example:

```go
ColumnSpec{
    Name: "duration_ns",
    Description: "Span duration in nanoseconds.",
    UseItFor: "Latency percentiles, slow-operation tables, and heatmap buckets.",
    SemanticType: "duration_ns",
    PanelRoles: []string{"value", "table", "heatmap"},
    Format: "duration",
}
```

Keep this metadata close to `internal/storage/catalog.go` (for example, `internal/storage/schema_catalog.go`), so the same `SemanticType`, `PanelRoles`, and `Format` drive:

- docs-table “Use it for” copy;
- composer column tooltips and SQL snippets;
- renderer defaults such as duration/timestamp formatting and trace links;
- validation hints such as “time series needs a timestamp and a numeric value.”

Write the prose once per stable semantic column. Reuse a shared `ColumnSpec` for columns with the same meaning across views (`trace_id`, `service_name`, `start_ns`, `duration_ns`) and only add a view-local override when the meaning changes. That keeps the copy coherent without pretending introspection can author product documentation.

### Where preview/sample SQL comes from

Do not generate a complete query just by seeing a table and its columns. The catalog should carry small, reviewed `SampleQuerySpec` records, each with a stable ID, title, target view(s), intended panel display type, SQL text, required named parameters, expected result columns, and a one-line explanation. Examples include “span count over time,” “slow spans,” “errored traces,” and “log volume by severity.”

The generator can use introspection to validate that every referenced view and column exists. Tests should run every sample against a seeded DuckDB fixture, bind representative safe parameter values, and verify its declared result shape. This lets the UI confidently offer:

- **Use sample SQL** in the panel or alert composer;
- a telemetry catalog that filters samples by signal and display type;
- schema-doc examples with “Preview sample · 30 rows” and “Use in current query” actions.

The author may then edit the sample freely. A future helper can assemble a draft from a selected view + column + panel role, but it must label that draft as a starting point and never claim it knows the user’s actual metric/filter intent.

### Preview execution

Dashboard and alert authors need an actual query preview before save. Add a shared `POST /api/query-preview` service (existing dashboard/alert preview routes can delegate to it) with this contract:

```text
input:  query_sql, named parameters, requested display type
policy: validate read-only SQL, bind only declared dashboard/alert parameters,
        apply the active session context, 30-second timeout, maximum 30 returned rows
output: columns, rows, truncated, resolved_parameters, duration_ms, warnings
```

The server, not the browser, enforces the 30-row cap. Prefer the storage query API’s row-limit argument; do not depend on a user-written `LIMIT`, and do not mutate a query string by naïvely appending `LIMIT 30`. If the query returns more than 30 rows, return `truncated: true` so the UI says “Showing the first 30 rows” and guides the author to add an intentional `ORDER BY`/`LIMIT`.

The composer’s `Preview` button stays near the SQL editor. Its result area renders a compact, horizontally scrollable table with at most 30 rows, column/type hints, resolved named parameters, and execution/error feedback. It must keep the editor contents intact on failure. For panel types, the normal visual renderer can appear above the raw grid, but the raw 30-row table is the debugging truth. For alert rules, show the scalar/grouped result table alongside the threshold evaluation.

Start with curated, stable query views—not physical DuckDB tables: `telemetry_spans`, `telemetry_traces`, `telemetry_logs`, `telemetry_metrics`, and any explicitly supported deployment/release view. Each catalog entry needs a view name, purpose, columns (`name`, DuckDB type, description, sensitivity), useful join/link metadata, and example queries. The storage migration that changes a view must update this catalog in the same change. Add a catalog-version test that asserts every documented view/column exists in the read-only DuckDB connection.

The only currently automatic magic parameter is `$session_id`. Dashboard and alert filters such as `$service` are explicit, user-defined named DuckDB parameters; do not advertise invented automatic service/environment/window values until the backend resolves them.

## Existing implementation anchors

Start from these files rather than creating parallel conventions:

| Area | Existing code | Role in this feature |
| --- | --- | --- |
| API registration | `internal/api/router.go` | Add dashboard and alert routes beside the existing `/api/metrics`, `/api/traces`, and `/api/logs` routes. |
| Request validation | `internal/api/validate.go` | Decode and validate write DTOs. |
| Storage models and queries | `internal/storage/db.go` | Follow the existing DB structs, scoped queries, and GORM/raw-SQL conventions. |
| Initial schema | `internal/storage/migrations/0001_init.sql` | Existing `spans`, `logs`, `metrics`, `sessions`, lint, and issue tables to query. Add a numbered migration; do not edit an applied migration. |
| Metric API | `internal/api/metrics.go` | Reuse metric catalog/series semantics and exemplar-to-trace links. |
| Live updates | `internal/ws/hub.go` | Publish dashboard/alert invalidations or state changes. |
| Client API schemas | `frontend/src/lib/api.ts` | Add Zod schemas and typed client methods. |
| App navigation | `frontend/src/App.tsx` | Add `Dashboards` and `Alerts` routes/nav. |
| Existing chart behavior | `frontend/src/pages/Metrics.tsx` | Reuse bucketing, range choices, charts, and trace overlay behavior where suitable. |
| Existing query catalog | `internal/storage/catalog.go` and `internal/api/dashboards.go` | Extend this backend-owned catalog rather than duplicating telemetry names/columns in the browser. |

## New files and primary responsibilities

## YAML ownership and precedence

Dashboards loaded from the configured dashboard directory are **file-managed**. Their YAML remains authoritative: the loader replaces their stored definition on each startup. The UI must label these dashboards as file-managed and treat them as read-only; users who want an editable local copy should export/import the YAML to create a new dashboard. Database-backed dashboards are owned by the UI/API and are not overwritten by the file loader.

Suggested names are intentionally boring:

```text
internal/storage/migrations/0009_dashboards_alerts.sql
internal/storage/dashboards.go
internal/storage/alerts.go
internal/storage/catalog.go
internal/storage/schema_catalog.go              # new canonical schema metadata
internal/storage/schema_catalog_test.go
internal/api/dashboards.go
internal/api/alerts.go
internal/api/database_schema.go                 # new GET /api/database-schema
internal/api/database_schema_test.go
internal/api/dashboards_test.go
internal/api/alerts_test.go
frontend/src/pages/DatabaseSchema.tsx           # /docs/database-schema
frontend/src/components/dashboard-panels/SchemaHelpLink.tsx
frontend/src/components/dashboard-panels/SchemaReferenceDrawer.tsx
frontend/src/pages/Dashboards.tsx
frontend/src/pages/Alerts.tsx
```

It is fine to defer a background evaluator until dashboard querying is correct. The persisted rule and alert state schema should still arrive together so there is no migration churn.

## Persistence

Store definitions as normalized metadata plus JSON for flexible settings. Panels and alerts store validated, read-only DuckDB SQL in `query_sql` with a `query_version`; Spaniel must reject mutations, multiple statements, unsafe extensions, and unbounded result shapes before persistence or execution.

```sql
CREATE TABLE dashboards (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE dashboard_variables (
  dashboard_id TEXT NOT NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,             -- attribute | enum | time
  source TEXT NOT NULL,           -- e.g. resource.service.name or user.plan
  options_json VARCHAR NOT NULL DEFAULT '[]',
  default_value TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (dashboard_id, name)
);

CREATE TABLE dashboard_panels (
  id TEXT PRIMARY KEY,
  dashboard_id TEXT NOT NULL,
  title TEXT NOT NULL,
  display_type TEXT NOT NULL,     -- single_value | time_series | table | heatmap | trace_list | log_list
  query_sql TEXT NOT NULL,        -- validated read-only DuckDB SQL
  query_version INTEGER NOT NULL,
  settings_json VARCHAR NOT NULL DEFAULT '{}',
  layout_json VARCHAR NOT NULL DEFAULT '{}',
  position INTEGER NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE alert_rules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  query_json VARCHAR NOT NULL,
  condition_json VARCHAR NOT NULL,
  group_by_json VARCHAR NOT NULL DEFAULT '[]',
  pending_for_ns BIGINT NOT NULL,
  cooldown_ns BIGINT NOT NULL DEFAULT 0,
  severity TEXT NOT NULL,
  annotations_json VARCHAR NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

CREATE TABLE alert_instances (
  rule_id TEXT NOT NULL,
  group_key TEXT NOT NULL,
  labels_json VARCHAR NOT NULL,
  state TEXT NOT NULL,
  value DOUBLE,
  first_pending_at BIGINT,
  fired_at BIGINT,
  resolved_at BIGINT,
  acknowledged_at BIGINT,
  last_evaluated_at BIGINT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (rule_id, group_key)
);
```

Add indexes on `dashboard_panels(dashboard_id, position)`, `dashboard_variables(dashboard_id)`, and `alert_instances(state, last_evaluated_at)`. Definition data should survive normal telemetry retention; alert history can have a separately configured retention period later.

## Query model and execution

Spaniel's query model is validated, read-only DuckDB SQL—not a second DSL. The browser may submit SQL only through the same lexical safety gate used by saved panels and alerts; execution is parameterized, time-bounded, row-limited, and result-shape-validated for the selected display type. Preserve the editable `query_sql` and `query_version` as the durable contract.

Reject a query before saving if its display type is incompatible: a single value must yield one scalar; a heatmap needs a numeric value and time; trace/log lists must yield their respective record shape.

Magic-variable resolution is request context, not persisted expansion. For example:

```text
$service_name       selected service or all services when unset
$environment        deployment.environment filter
$window             resolved from/to nanoseconds
$operation_name     http.route, rpc.method, or span name
$status             normalized error/ok/unset status
$selected_trace_id  populated only when created from a trace
```

Resolve custom variables only from a bounded attribute catalog. Never offer arbitrary high-cardinality IDs as dashboard-wide selectors by default.

### Queries against current tables

Named dashboard parameters are bound rather than interpolated. Examples, not literal copy/paste contracts:

```sql
-- p95 span duration by HTTP route
SELECT json_extract_string(attributes, '$.http.route') AS route,
       quantile_cont(duration_ns, 0.95) AS p95_ns
FROM spans
WHERE service_name = ? AND start_ns BETWEEN ? AND ?
GROUP BY route
ORDER BY p95_ns DESC;

-- log count by severity, optionally restricted by text
SELECT severity, count(*) AS n
FROM logs
WHERE service_name = ? AND timestamp_ns BETWEEN ? AND ?
  AND lower(body) LIKE '%' || lower(?) || '%'
GROUP BY severity
ORDER BY severity DESC;

-- root traces matching an error condition
SELECT trace_id, service_name, name, start_ns, duration_ns, status_code
FROM spans
WHERE (parent_span_id = '' OR parent_span_id IS NULL)
  AND service_name = ? AND status_code = 2
  AND start_ns BETWEEN ? AND ?
ORDER BY start_ns DESC
LIMIT ?;
```

`spans.attributes`, `spans.resource`, `logs.attributes`, and `metrics.attributes` are stored as serialized JSON/VARCHAR today. Centralize JSON extraction in the compiler and add expression/index support only after real dashboard queries show a need. Follow the session scoping model already used by `ListTraces`, `ListLogs`, and `GetMetricSeries`.

## API shape

Suggested endpoints:

```text
GET    /api/dashboards
POST   /api/dashboards
GET    /api/dashboards/{id}
PATCH  /api/dashboards/{id}
DELETE /api/dashboards/{id}
POST   /api/dashboards/{id}/query-preview
POST   /api/dashboards/{id}/panels
PATCH  /api/dashboards/{id}/panels/{panelId}
DELETE /api/dashboards/{id}/panels/{panelId}
GET    /api/query-catalog?signal=metrics&q=duration
GET    /api/database-schema

GET    /api/alerts
POST   /api/alerts
GET    /api/alerts/{id}
PATCH  /api/alerts/{id}
POST   /api/alerts/{id}/preview
POST   /api/alerts/{id}/acknowledge
POST   /api/alerts/{id}/silence
GET    /api/alerts/history
```

Preview must execute the exact same read-only SQL path used after save. Return a typed result envelope with `display_type`, columns, rows/series, resolved variables, warnings, and links to relevant trace IDs. The query catalog should aggregate known metric streams plus bounded observed span names, routes, log severities, and allowlisted attribute keys. `GET /api/database-schema` returns the canonical, versioned schema catalog; it must not execute user SQL or expose arbitrary physical tables.

## Alert evaluator

Run evaluation on a small local ticker initially (for example, every 15 seconds) and on relevant ingestion events if the cost remains bounded. For each enabled rule:

1. Resolve its window and group labels.
2. Compile and execute the query under a row/group cap.
3. Apply threshold/absence/anomaly policy.
4. Upsert `alert_instances` without resetting `first_pending_at` on every evaluation.
5. Transition pending → firing only after `pending_for_ns`; transition firing → resolved once false; honor cooldown/deduplication before notification delivery.
6. Publish an event through `internal/ws/hub.go` so the Alerts page updates without polling.

Keep notification delivery out of the first dashboard PR. Store notification configuration behind a small destination interface so webhook/email/Pushover can be added without coupling alert evaluation to transport.

## Frontend plan

Use the mockup as the interaction spec:

- **Dashboard list/editor**: saved dashboards, panel grid, edit mode, draft panel selection, and a query composer with title + display type.
- **Query composer**: telemetry search tabs (metrics/spans/traces/logs), reserved parameter insertion, custom variable editing, preview, and inline errors that retain the query. Put a compact `Schema & SQL` action in the SQL toolbar; it opens `/docs/database-schema` without discarding the draft.
- **Database schema docs**: `DatabaseSchema.tsx` consumes the same typed schema-catalog endpoint as composer tooltips. It provides a searchable view list, column/type/meaning table, parameter guardrails, a capped “Preview sample · 30 rows” result grid, and “Use this sample” actions that return to the originating dashboard or alert draft.
- **Panel renderer**: one component per display type, sharing a typed `PanelResult`; reuse the existing SVG metric chart work in `Metrics.tsx` before adding a chart library.
- **Alerts**: signal-board rows, a state filter, and inspector with query, resolved labels, timeline, trace links, acknowledgement, and eventual silence flow.

Add client schemas/methods to `frontend/src/lib/api.ts`, query keys to `frontend/src/lib/query.ts`, routes in `frontend/src/App.tsx`, and E2E coverage beside existing `frontend/e2e/*.spec.ts` tests.

## Minimum test matrix

- Read-only SQL rejection, named parameter binding, display-shape validation, and query cancellation/row limits.
- Schema catalog contract: every catalog entry resolves against the read-only DuckDB connection; every listed column exists; docs and composer consume the same API fixture/version.
- Storage CRUD and dashboard-scoped panel ordering.
- Preview response shape for each display type.
- Session scoping and time-window boundaries.
- Alert transition timing: inactive → pending → firing → resolved; absence rules; acknowledgements do not resolve an alert.
- Cardinality caps: reject/limit unbounded group-bys and catalog values.
- UI: add, edit, and remove a panel; insert a magic variable; create a custom variable; create and acknowledge an alert.

## Delivery order

1. Migration, storage CRUD, typed dashboard API, and a persisted blank dashboard.
2. Read-only metric/trace/log panel queries and preview endpoint.
3. Dashboard editor with variables and panel CRUD.
4. Alert rule persistence plus preview.
5. Evaluator, instance history, live updates, and acknowledgement.
6. Notification destinations, silences, templates, export/import, and version history.

Avoid shipping alert notifications until preview, state transitions, and grouped deduplication have deterministic tests. The local UI is useful early; noisy local notifications are not.
