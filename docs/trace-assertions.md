# Reusable telemetry assertions

Spaniel owns `github.com/zfogg/spaniel/traceassert` and the standalone
`github.com/zfogg/spaniel/cmd/spaniel-assert` command. They validate local captures
without exporting telemetry, executing SQL or requiring a collector. Other
projects keep their fixture setup and expected values; graph traversal, duplicate
checks, diagnostic validation, recursive redaction and polling live here.

Build with `make build-assert`, test with `make test-assert`. Consumers install the
CLI or import the package at a reviewed immutable Spaniel revision. The package
uses Sequel's native engine for canonical diagnostic validation and needs CGO;
it does not link DuckDB. Run normal Spaniel builds/tests through the Makefile on
Windows so its UCRT64 toolchain is selected.

```
spaniel-assert --input capture.json --spec assertions.json
spaniel-assert --input capture.pb --format otlp-proto --spec assertions.json
spaniel-assert --findings findings.json --spec lint-assertions.json
spaniel-assert --spaniel-url http://127.0.0.1:8080 --trace-id TRACE_HEX --spec assertions.json --wait 30s
```

Repeat `--input` or `--trace-id` to combine captures. Live fetching only reads
`/api/traces/{id}`. `SPANIEL_API_TOKEN` supplies an optional bearer token; change
the variable name with `--token-env`. The token is scoped to the configured
origin, cross-origin redirects are rejected and reports never echo captured
values. Each HTTP request has a 30-second timeout. `--wait` defaults to zero and
is capped at ten minutes; `--interval` defaults to 250ms and accepts 50ms–5s.
Every poll replaces the fetched capture so earlier snapshots cannot introduce
duplicates. Files and HTTP captures are limited to 64 MiB each, graph captures
to 100,000 spans, ancestry search to 1,000,000 states per rule and attribute
traversal to 128 levels. A limit produces a failure, never a passing partial check.

JSON stdout contains `version`, `span_count` and `failures`. Exit status is 0
for success, 1 for assertion failure, 2 for unreadable/invalid input or HTTP
failure. A final failed live assertion prints its report after the wait expires.

## Capture and specification

JSON accepts OTLP `resourceSpans`, a normalized span array, `{"spans":[...]}` or
Spaniel's `{"data":[...]}` trace response. Normalized fields are `trace_id`,
`span_id`, `parent_span_id`, `service_name`, `name`, `kind`, `status_code`,
`status_message`, `attributes`, `resource`, `events` and `links`. Attributes and
resources may be objects or Spaniel JSON-encoded objects. Event attributes and
Spaniel's `linked_trace_id`/`linked_span_id` target fields are normalized too.
Trace/span IDs are nonzero lowercase hexadecimal, 32/16 characters respectively.
Status numbering is OTLP: unset 0, OK 1, error 2. `FromReadOnlySpans` converts
SDK recorder captures and preserves resources, events, links and typed arrays;
it explicitly converts SDK status numbering to OTLP numbering.

```json
{
  "version": 1,
  "no_duplicate_span_ids": true,
  "counts": [
    {"name": "one-api", "select": {"service": "api", "kind": 2}, "min": 1, "max": 1},
    {"name": "no-errors", "select": {"status_code": 2}, "max": 0}
  ],
  "ancestry": [
    {"name": "browser-api-db", "path": [{"service": "browser"}, {"service": "api"}, {"has_attributes": ["db.system.name"]}]}
  ],
  "redaction": {
    "forbidden_keys": ["db.query.parameters"],
    "forbidden_values": ["fixture-secret"],
    "require_query_text_reason": true,
    "validate_diagnostics": true
  }
}
```

Unknown fields/versions, duplicate rule labels and invalid bounds fail closed.
Selectors AND all supplied predicates: `trace_id`, `parent_span_id`, `service`,
`service_prefix`, `name`, `name_prefix`, `kind`, `status_code`, exact typed JSON
`attributes`, `has_attributes` and `absent_attributes`. An empty selector matches
everything. Explicit `parent_span_id:""` selects roots. Count bounds are inclusive;
omitted `max` is unbounded. Optional `graph` checks `require_resolved_parents`,
`require_single_root` across the combined capture, and `require_connected` using
parent/link edges. Parent cycles always fail. Missing external parents are
otherwise allowed for deliberately partial captures.

Ancestry paths list ancestors first, terminal spans last. Every matching terminal
span must satisfy the path; one passing sample cannot conceal another broken
path. `min` defaults to one terminal span, `direct:true` requires immediate edges,
and `allow_links:true` allows descendant-to-ancestor causal links across traces
(for example Temporal). Cycles cannot invent a missing stage or inflate counts.

`no_duplicate_operations` rules have `name`, `select` and `group_by`, listing
`trace_id`, `parent_span_id`, `service_name`, `name` or `attributes.<key>` fields.
Missing tuple attributes fail. Duplicate span IDs are scoped by trace, so two
different traces may legitimately reuse a span ID.

Redaction recursively checks span names/status messages and all attributes,
resources, events and link attributes, including typed SDK arrays. SQL omission
reasons are required only for recognized SQL systems with query-analysis metadata;
Redis, bare DB metadata and transaction/pool lifecycle spans are exempt. Allowed
reasons are `policy`, `malformed`, `analysis_limit`, `analysis_error`, `empty` and
`cancelled`. A reason must be absent when SQL text is present.

`diagnostics` rules specify `name`, `select`, exact `codes` multiset and optional
`text_reason` (`""` requires no reason), `normalized_only` and positive `min`
(default 1). `codes:[]` requires a clean empty diagnostic array. General captures
accept SDK JSON-string arrays and structured objects. `normalized_only:true`
requires structured persisted objects. All formats use Sequel's versioned catalog;
unknown codes/schema versions, partial objects and invalid ranges fail.

`--findings` accepts a finding array or Spaniel `data` envelope with `rule_id`,
`span_id` and optional `trace_id`. Spec `findings` rules contain `name`, `rule_id`,
`span_ids` and `exact`. Exact rules reject unlisted affected span IDs, including
clean controls. The consumer captures findings; the shared checker owns comparison.

Go callers use `Decode`, `DecodeSpec`, `Check`, `Report.OK`, or
`CheckWithFindings` plus `DecodeFindings`. `FetchTrace` takes an explicit HTTP
client and context, letting callers supply authentication and timeouts. Spaniel's
own API/storage ancestry regression uses the same public package.
