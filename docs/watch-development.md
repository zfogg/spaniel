# Watch development handoff

Foundation: [#125](https://github.com/zfogg/spaniel/issues/125), parent [#123](https://github.com/zfogg/spaniel/issues/123). Feature PRs target `codex/watch-dashboard`. The foundation must merge before feature branches start. The integration coordinator owns shared infrastructure and final assembly (#132).

## File ownership and constructors

Every constructor below is wired in `internal/watch/app.go`. Feature packages own their views; shared command registration, composition, shell, protocol, client, contracts and generators remain integration-owned. Request shared changes through the coordinator.

| Issue | Owned files | Stable entry points |
| --- | --- | --- |
| #126 | `internal/watch/tabs/traces/**`, `internal/watch/tabs/spans/**` | `traces.New(protocol.Env) tea.Model`, `spans.New(protocol.Env) tea.Model`, `traces.NewDetails() protocol.Details` |
| #127 | `internal/watch/tabs/logs/**` | `logs.New(protocol.Env) tea.Model` |
| #128 | `internal/watch/tabs/metrics/**` | `metrics.New(protocol.Env) tea.Model` |
| #129 | `internal/watch/tabs/lints/**` | `lints.New(protocol.Env) tea.Model` |
| #130 | `internal/watch/tabs/dashboards/**` | `dashboards.New(protocol.Env) tea.Model` |
| #131 | `internal/watch/tabs/alerts/**` | `alerts.New(protocol.Env) tea.Model` |

All seven feature packages are implemented and composed by the shared shell. `internal/watch/foundation` remains a test/support view and is not used by the production dashboard. Feature rendering belongs in the packages above; shared trace/span details serve every telemetry entry path.

The seven ordered tabs are traces, spans, metrics, logs, lints, dashboards, alerts. `spaniel watch` uses the shared shell on a TTY; piped output and existing span filters keep their original line format. `spaniel tui` is deprecated and calls the same command implementation. `--api` selects the HTTP base; `--session ID` pins the dashboard scope; omission follows the active session. The heading and terminal title are `spaniel - <tab title>`.

## Tab lifecycle and messages

`protocol.Env` contains `Context context.Context`, `API *client.Client`, `Refresh *client.Refreshes`, `Owner string`, and `Filters protocol.Filters` (`Service`, `ErrorsOnly`, `N1Only`). Constructors receive an immutable environment; every tab gets its own stable owner (`traces`, `spans`, etc.). Detail instances get unique `detail:<sequence>` owners. Use `env.Owner` rather than inventing tab names for result routing.

Register initial requests in the constructor or in `Update`, on the UI goroutine:

```go
env.Refresh.Register(client.Spec{
    Key: client.Key{Owner: env.Owner, Name: "list"},
    Resources: []liveevent.Resource{"logs"},
    Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
        return api.Logs(ctx, client.Scope(query, session))
    },
})
```

Capture a private immutable copy of filters/query values in `Fetch`. Do not read or mutate model fields from its worker. `client.Scope(url.Values, session)` clones the parameters and adds `sessionId`. HTTP methods return `client.Envelope[T]{Data T, Meta *apigen.Meta, Silences []apigen.AlertSilence}`. In `Update`, type-assert `protocol.Result.Data` to the corresponding `client.Envelope[T]` and inspect `Err`. `*client.HTTPError` exposes `Status` and `Message`; transport/deadline errors remain wrapped standard errors. Model loading, empty, stale/disconnected, and error states must stay distinct.

`client.Spec` fields are `Key`, `Resources`, `Fetch`, and optional `Interval time.Duration`. Re-register the same key on selection/filter changes to cancel and supersede the previous request. `Remove(Key)` cancels a subscription; `RemoveOwner(owner)` cancels all subscriptions for a closed detail. The shell owns global resource/session invalidation and calls `Due`, `Complete`, and snapshot replay. Features must not call these scheduling methods.

| Message | Delivery and responsibility |
| --- | --- |
| `tea.WindowSizeMsg` | All tabs and all open details, including hidden ones; dimensions already reserve shell header/footer. |
| `protocol.Live{Event liveevent.Message}` | All tabs/details. Use `Type`, `Decoded` (typed payload pointer), or `Payload` (raw JSON) to patch matching rows. |
| `protocol.Result` (alias of `client.Result`) | Only its `Key.Owner`, even when hidden. Fields: `Key`, `Sequence`, `Epoch`, `Session`, `Data`, `Err`, `Pending`. Old connection/session/request results are rejected before delivery. `Pending` means a durable invalidation raced the snapshot and an authoritative repair is queued. |
| `protocol.Connection` (alias of `client.Status`) | All tabs/details. Fields: `Connected`, `Err`, `Epoch`, `Resync`. Keep old data visibly stale during outages. |
| `protocol.SessionChanged{ID}` | All tabs/details. Clear old-session selections/caches promptly. New requests use the new session generation. |
| `protocol.Focus{Active}` | Entering/leaving the focused tab/detail. Background live updates continue. |
| `protocol.Tick{Time}` | All tabs/details every 250ms. For local UI time/age display, not independent HTTP polling. |
| `protocol.Owned{Owner, Message tea.Msg}` | Non-HTTP async/widget message to its owner. `env.Command(cmd)` wraps a leaf widget command. Wrap leaf commands before combining with `tea.Batch`/`tea.Sequence`; the shell owns global terminal commands. |
| `protocol.ReturnContext{Tab, Selection}` | Delivered to the restored view after Back; preserve a stable row selection rather than a screen offset. |

Return a command producing `protocol.OpenTrace{TraceID, SpanID, Return}` to open the shared trace viewer with an optional selected span. Use `protocol.OpenSpan{TraceID, SpanID, Return}` for the same shared span inspector from spans/logs/lints/alerts. Those packages must not import each other. The traces feature implements `protocol.Details`:

```go
type Details interface {
    Trace(protocol.Env, protocol.OpenTrace) tea.Model
    Span(protocol.Env, protocol.OpenSpan) tea.Model
}
```

For a feature-owned full log or other detail, emit `protocol.ShowDetail{New protocol.Factory, Return protocol.ReturnContext}`. The factory receives a new owner/environment; register its own requests under that owner. `protocol.Back{}` or Escape pops one view. Each tab retains its detail stack when switching tabs; all retained details keep receiving live updates and owner results. Closing a detail removes its subscriptions; late results are discarded.

The shell renders the focused model’s `ShortHelp() []key.Binding` in exactly one bottom row. The active tab, retained detail, subview, selection, pause state and text input determine the available actions. Useful actions that fit are followed by `? help`; the styled popup contains every current contextual binding and applicable global, with arrow/page/Home/End scrolling and Escape to close. Captured fields keep a literal `?` and advertise non-printable `F2 help` instead. F1 paging is removed. Features render data, filter syntax, state and scroll position without inline shortcut legends. Connection status is clipped into the one-row heading, including long sanitized reconnect errors. Every pane receives a viewport reserving exactly the heading and footer; popup help overlays that viewport without expanding either.

The shell owns Tab/Shift-Tab, Ctrl+C, `q`, heading/title, and Escape/Backspace in details. Features own arrows, scrolling, Enter, filter entry, and pause behavior. Implement `protocol.KeyCapture.CapturesKeys() bool` while text input or another modal consumes `q`/Escape/Tab; Ctrl+C always quits. Pause must pause presentation, retain bounded live state, and reconcile when unpaused. Never stop the shared stream for a single tab.

## HTTP API surface

`client.New(base string, client.Options) (*client.Client, error)` accepts an absolute HTTP(S) base, including a path prefix. `Options` permits an injected `*http.Client` (for auth/TLS) and bounded `Timeout`, `MinBackoff`, `MaxBackoff`, `StaleAfter`, `Queue`. No mutation methods are exposed by this terminal client. `Preview` is the existing read-only POST.

Every method below takes `context.Context`, and all except `Preview` take a final `url.Values`. Single-object/detail methods also take an ID before the query. All return `(client.Envelope[T], error)`:

| Methods | T / endpoint semantics |
| --- | --- |
| `Traces` | `[]apigen.TraceRow`; `/api/traces` |
| `Trace(id)` | `[]apigen.Span`; `/api/traces/{id}` including span events and links |
| `IncomingLinks(id)` | `[]apigen.SpanLink`; `/api/traces/{id}/incoming-links` |
| `Spans`, `SpanGroups` | `[]apigen.SpanRow`, `[]apigen.SpanGroup`; `/api/spans`. Use `Spans` for flat rows; use the canonical grouping/view parameters with `SpanGroups`. |
| `Span(id)` | `apigen.Span`; `/api/spans/{id}` |
| `Logs` | `[]apigen.Log`; `/api/logs` |
| `Metrics`, `MetricSeries`, `MetricCardinality` | `[]apigen.MetricCatalogEntry`, `apigen.MetricSeries`, `[]apigen.MetricCardinalityStream`; `/api/metrics`, `/api/metrics/series`, `/api/metrics/cardinality` |
| `Lints`, `Issues` | `[]apigen.LintWarning`, `[]apigen.TraceIssue`; `/api/lint`, `/api/issues`. Both are required by #129. |
| `Dashboards`, `Dashboard(id)` | `[]apigen.Dashboard`, `apigen.Dashboard`; variables/panels are included in hydrated definitions. |
| `QueryCatalog` | `[]apigen.QueryCatalogEntry`; `/api/query-catalog` |
| `Preview(id, apigen.PreviewDashboardQueryJSONBody)` | `apigen.QueryPreview`; `/api/dashboards/{id}/query-preview`. The request supports query SQL, display type, variables and optional `sessionId`. The server-owned `$session_id` uses that selected scope; omission follows active. Variable-map overrides cannot replace it. |
| `Alerts`, `Alert(id)` | `apigen.AlertList`, `apigen.AlertRule`; definitions, hydrated instances, evaluator state. |
| `AlertEvents(id)`, `AlertHistory` | `[]apigen.AlertEvent`; paginated rule/global history. Rule events preserve `Envelope.Silences`. |
| `Sessions`, `ActiveSession` | `[]apigen.Session`, `apigen.ActiveSession`; `/api/sessions`, `/api/sessions/active` |

Trace/span detail and incoming-link reads accept optional `sessionId`; omission preserves existing unscoped detail lookup. Events and links are attached by owning session/trace/span, including unscoped web calls. The shared client uses `json.Decoder.UseNumber` for dynamic SQL/metric attributes, preserving exact integers above 2^53 through normalization and typed tables. Canonical measurement doubles remain doubles. Alert/dashboard object reads set canonical `markSeen=false`, preserving read-only observation while existing browser callers retain their default explicit-access acknowledgement. Metric catalog/inspectors subscribe to the durable cardinality endpoint; its recording-scoped admission counts are independent of visible window samples.

Use exact query parameter names from `api/openapi.json` or the generated `apigen.*Params` definitions; these remain the canonical HTTP contract. Examples: telemetry session scope is `sessionId`; metrics select `name`, `service`, `operation` and bounds. No private storage DTO or DuckDB connection belongs in a feature. Canonical OpenAPI timestamp fields use `integer/int64`; numeric measurements use `number/double`, avoiding the previous generated Go `float32` precision loss. Browser JavaScript retains its existing number representation.

## Live contract, invalidation and durable ordering

`internal/liveevent` depends only on the Go standard library. It owns payloads, constructors, the `Payloads` event registry, `Message`/`Decode`, `Invalidations`, `Patches`, `Changes(Message)`, and `Affects(Message)`. `internal/ws` retains compatibility aliases, including `SpanEvent` and existing constructors. New server code should import `liveevent`; the hub remains the transport adapter. No existing event name, JSON property or row shape is removed.

`make generate` runs `cmd/genliveevent` and formats `frontend/src/generated/liveevent.ts`, which derives the browser discriminated union, runtime decoder and resource matrix from the Go registry. Dynamic HTTP row references use the existing OpenAPI-derived browser DTOs. Do not edit generated TypeScript. `Decode` accepts unknown event names for forward compatibility and rejects invalid envelopes/payload field types; optional/absent legacy fields are tolerated. The browser drops unknown/malformed frames without stopping reconnect.

| Event/source | HTTP dirtiness / patch policy |
| --- | --- |
| `span` | traces/list/detail, spans/list/detail, services, service map, coverage, sessions, dashboard panel results. Complete row/trace payloads patch browser first pages. |
| `log` | logs, services, panel results. Complete log rows patch matching browser first pages. |
| `metric` | catalog, selected series, services, panel results. Browser row patches match name/service/session; terminal features must also match selected dimensions. `catalogOnly` never triggers HTTP. |
| `issue` | detector findings and trace detail. Semantic lints are a distinct durable resource. |
| `self_trace`, `self_span`, `self_log` | Row patches only, never HTTP invalidation. Browser open self-trace/span details also patch locally. |
| completed semantic lint writes | `resources_changed` for `lint` and `trace`, emitted after successful durable insertion, including asynchronous and complete-tree analysis. |
| `definition_sync` | On `updated` only: dashboards plus panel results, or alerts. Existing updating/error events remain available for status UI. |
| successful dashboard writes/removals | `resources_changed` for dashboards/panels, from storage completion; covers HTTP, MCP and definition synchronization. |
| `alert`, `alert_sync` | alerts/current instances/history; alert also affects notifications. Existing evaluator status events are retained. Durable batched instance/event/evaluation completion additionally publishes `resources_changed` for alerts, repairing pre-flush status notifications. |
| reset | After the delete transaction commits, `resources_changed` clears telemetry and active-session caches, even if the subsequent checkpoint fails. The shell subscribes to `active-session` and reapplies authoritative scope; context copies share the cleared active-session state. |
| `active_session` | replace active-session metadata and dirty session-scoped telemetry/results. The shell increments the session generation; pinned `--session` views retain their scope. |
| retention / successful session deletion | `resources_changed` for all retained telemetry lists/details, lints/issues, catalog/series, panel results, alert state, session summaries, footer resources, services and coverage. Errors after partial retention also dirty state conservatively. |
| `live_state`, `throughput`, `forwarder` | complete snapshots/rates patched locally; do not fan these into HTTP telemetry reads. |
| reconnect / browser visibility restoration | authoritative resync; only active browser observers refetch. |

`storage.DB.SetChangeNotifier(func([]string, string))` is installed by server startup **before serving requests**. `WithContext` inherits the sink. The callback must remain nonblocking and must not query storage: startup translates resources to `liveevent.NewResourcesChangedEvent` and broadcasts through the existing hub. The hub retains its 256-frame slow-consumer bound, heartbeat cadence, visible-viewer gating and hidden-browser filtering; hidden sockets still receive durable changes/alerts. Browser invalidations coalesce at one second and stay visibility aware. The `alerts` resource includes the browser keys `alert-rule`, `alert-events`, `alert-history`, and `alert-rule-sparklines`, so durable evaluator/definition changes repair selected or off-page rules without a transition frame.

The terminal uses exactly one `Client.Stream(ctx)` per app; a second simultaneous subscription is rejected. `/ws?ready=1` opts into a registration-confirming first heartbeat, sent only after hub subscription. Existing clients receive their original frame order/cadence. HTTP bootstrap starts after that confirmation. On older servers the first data/heartbeat frame provides confirmation. Buffered in-flight row events are replayed to the snapshot owner after its result; one pending authoritative refresh repairs non-row changes. Each in-flight subscription retains at most 256 replay frames. Duplicate stream delivery must be idempotent in feature caches.

Defaults: 10s context-bound HTTP/dial timeout, 15s silence threshold, exponential reconnect delay 500ms–30s, 256 updates in the terminal queue, 8MiB WS frame limit, 32MiB HTTP decode limit, four concurrent HTTP workers, one queued dirty bit per subscription, at least one second between ordinary successful refreshes, at least five seconds after an HTTP error. Queue overflow closes the socket and reconnects/resyncs. Context cancellation closes transport, timers and HTTP work. Connection, session and monotonic request generations prevent stale snapshots from overwriting new state. All refresh registry operations run on the UI goroutine. Newly created details receive the current connection, session and size synchronously before initialization and focus, including details opened during reconnect.

For relative time-window metric series and dashboard panels register `Spec.Interval` (recommended 5s) so an idle window can expire/move; zero means event-only. Avoid intervals for static definitions/lists. The browser uses its existing visible-query refresh preference (default 5s, zero disables) for panels/annotations and its existing periodic live-view refresh. Alerts remain server-owned: the existing evaluator runs every 15s independently of viewers and publishes durable changes; no terminal evaluation or alert mutation controls. A self row must never trigger a refresh or an idle-loop restart.

Use `client.MergeByID[T](rows, incoming, limit, id)` for bounded replacement/upsert where useful. Snapshot replacement, removals, sorting and pagination remain authoritative HTTP concerns. Recommended identities: traces `(session, trace_id)`; spans `(session, trace_id, span_id)`; logs all durable row fields including `received_at` (logs have no separate server ID); metric catalog `(session, service, name)` and points `(series_key, timestamp_ns)`; lints `(session, trace_id, span_id, rule_id)`; detector findings `id` / fingerprint; dashboards/rules `id`; alert instances `(rule_id, group_key)`; events `id`. Do not count a repeated row as a new arrival. Identical log records without distinct durable fields are indistinguishable on the current wire contract.

## Charts and dependency ownership

Pinned dependency: `github.com/NimbleMarkets/ntcharts v0.5.1`. Its module uses Bubble Tea v1, Bubbles v0.20 and Lip Gloss v1, compatible with Spaniel's existing v1 packages. Stonkpile's `github.com/NimbleMarkets/ntcharts/v2 v2.2.0` uses Charm v2 and its wrapper cannot be copied unchanged. [Upstream compatibility notes](https://github.com/NimbleMarkets/ntcharts#bubbletea-version-compatibility) distinguish these module paths. The foundation installs the compatible dependency; feature branches must not migrate Charm or edit go.mod/go.sum.

Shared boundary in `internal/watch/charts`:

- `Point{Time time.Time, Value float64}` and `Series{Name string, Points []Point}`.
- `NewTimeSeries(width, height int, from, to time.Time, minY, maxY float64) timeserieslinechart.Model` returns a styled v1 chart, with safe minimum dimensions/nonzero ranges.
- `RenderTimeSeries(width, height int, from, to time.Time, series []Series) string` sorts/deduplicates timestamp points, bounds each dataset to its latest 2048 samples, and renders braille lines or an explicit empty state.

Features own units, labels, legends, aggregations, histogram/summary semantics and table/stat fallbacks. The adapter fabricates no samples and performs no data aggregation. #128 and #130 share it without sharing feature renderers.

## Validation and review

Run all Go commands through Make on Windows to select the pinned MSYS2 UCRT64 GCC 15 toolchain. `make lint-go` is the shared `go vet ./...` target; bare `make` keeps its existing `dev` default. Run gofmt on every changed Go file and confirm no changed file is listed by `gofmt -l` before committing. There is no additional configured Go linter in this checkout.

Required commands: `make generate` after generator inputs change, `make test-watch` for focused race-enabled transport/shell/seeded terminal checks, `make test`, `make build`, `make lint-go`, and `make test-extensive` for architecture/integration milestones. From `frontend`, run `pnpm run format`, `pnpm run format:check`, `pnpm run lint`, and `pnpm run build` (includes TypeScript checking). On local Node versions whose experimental global Web Storage shadows jsdom, use `NODE_OPTIONS=--no-experimental-webstorage` for frontend tests; this is an environment workaround, not an application change. Playwright requires its Chromium installation.

Tests cover failure/reconnect/resync, stale heartbeat and bounded queue overflow, cancellation, precision/HTTP errors, coalesced snapshot races/replay, generation/session guards, owner routing while hidden, retained nested detail stacks, seven-tab order/title, input capture, shared durable notifications and an actual seeded Bubble Tea run against HTTP plus the real WebSocket hub. Existing command tests retain piped filtering/output coverage. New helper listeners bind `127.0.0.1`; use `make run`/`make dev` for a listening Spaniel server, never `go run`.

Implementation PRs must publish inspected screenshots of actual behavior in their description/comment or a reviewer-accessible artifact. Capture safe example telemetry only. Keep PNGs/recordings under ignored `bin/` or a temporary directory and inspect the staged diff to ensure no image files are committed. Report upload blockers explicitly. Feature screenshots must demonstrate their real implemented rows/details; foundation status views are labeled pending and do not count as completed feature screenshots under #123.


The seeded terminal smoke test can record real renderer output before teardown (including a forced socket failure). The local capture script interprets that ANSI screen with a temporary xterm tool and writes PNGs under ignored `bin/`. No CI workflow is added. Local reproduction:

```powershell
$env:SPANIEL_WATCH_CAPTURE_DIR = "$PWD/bin/watch-captures"
make test-watch
npm install --prefix .local/watch-capture-tools --no-package-lock --no-save @xterm/headless@6.0.0
node scripts/capture-watch.cjs bin/watch-captures .local/watch-capture-tools/node_modules
```

Inspect all generated PNGs before attaching them to a PR. The capture script rasterizes the recorded production renderer through xterm, preserving styles and cursor/erase operations. The seeded smoke test records all seven tabs, the shared trace/span views, filter input and a forced disconnect. These HTTP fixtures verify footer integration; feature terminal tests additionally exercise actual DuckDB-backed API reads and ingestion.


## Integrated commands and boundaries

`spaniel trace <id|short-id> [--session ID]` requires a TTY and reuses the shared client, one-tab shell, `traces.NewDetails`, waterfall and span inspector. Exact-ID preflight preserves unscoped lookup when no session is supplied, then pins the selected recording for live repair. Prefix lookup retains its existing latest-500-trace bound; use a full ID for older traces. The obsolete command-local renderer is removed. `watch` pipe filters/output and the deprecated `tui` alias retain their existing behavior.

Logs now send canonical `text` and `severity=error+` for server-wide literal substring/error filtering before COUNT/LIMIT/OFFSET. Service and exact severity bands intersect those filters. Text also matches attributes, IDs and service. The loaded page uses the same text semantics for live patches. An inspector revalidates at most 1000 records at its selected timestamp and distinguishes exhausted lookup uncertainty from confirmed removal. Byte-identical records without distinct durable fields remain indistinguishable on the wire.

Traces/spans retain explicit loaded-page text/error/N+1 toggles and server service/paging filters. Lints retain the latest 2000 findings and resolve list services from two supported 1000-span pages; unknown services are unavailable, not guessed. Alert rule state/severity filters are explicitly page-local (25 rows), while search/history filters use the canonical server. Unsupported metric distributions/panels retain useful raw/table fallbacks. Trace, span and full-log identity/timing fields use aligned two-column tables; long metadata cells end with an ellipsis instead of wrapping. Arbitrary user SQL/results are preserved; telemetry/resource/log/metric-label and alert maps use wrapping typed tables. Intraday shared chart ticks show UTC clock times; multi-day windows retain dates.

`make test-watch-api` runs focused canonical scope/filter/read-only race checks. `make test-watch-footer` runs focused shell/popup and seven-tab renderer checks. `make test-watch` includes a complete real-storage/API/ingestion/hub scenario covering every production tab, shared navigation and Back, pinned preview/attributes/admission, hidden repairs, reconnect with missed rows, external definitions/deletion and recording removal. Set `SPANIEL_INTEGRATION_CAPTURE_DIR` to an ignored directory to record that real renderer. The one-row popup smoke can additionally use `SPANIEL_WATCH_CAPTURE_DIR`; narrow captures record a physical resize boundary for faithful xterm replay.
