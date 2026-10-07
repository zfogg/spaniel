# Spaniel database schema

Catalog version `1`; schema fingerprint `e533c99d42e2794520bd1a11a1ff1cf0146a2b904998649318759aaceb288f63`.

## `telemetry_spans`

One row per recorded span; use for operation latency and errors.

| Column | DuckDB type | Use it for |
|---|---|---|
| `trace_id` | `VARCHAR` | Trace-list links and correlating logs with spans. |
| `span_id` | `VARCHAR` | Span-list links and parent-child investigation. |
| `parent_span_id` | `VARCHAR` |  |
| `service_name` | `VARCHAR` | Grouping or filtering by service. |
| `name` | `VARCHAR` | Operation breakdowns and metric selection. |
| `kind` | `INTEGER` |  |
| `start_ns` | `BIGINT` | Time-series buckets and ordering. |
| `end_ns` | `BIGINT` |  |
| `duration_ns` | `BIGINT` | Latency percentiles, slow-operation tables, and heatmaps. |
| `status_code` | `INTEGER` | Error-rate filters and status summaries. |
| `status_message` | `VARCHAR` |  |
| `attributes` | `VARCHAR` | Filtering known semantic-convention fields with json_extract_string. |
| `resource` | `VARCHAR` | Filtering deployment and service resource fields. |
| `session_id` | `VARCHAR` | The automatic $session_id scope; normally do not hard-code it. |
| `session_label` | `VARCHAR` |  |
| `received_at` | `BIGINT` |  |
| `sampled` | `BOOLEAN` |  |

### Span count over time

```sql
SELECT (start_ns // 60000000000) * 60000000000 AS timestamp_ns, count(*) AS value FROM telemetry_spans WHERE session_id = $session_id GROUP BY 1 ORDER BY 1
```

### Slow spans

```sql
SELECT service_name, name, duration_ns / 1000000.0 AS duration_ms, trace_id, span_id FROM telemetry_spans WHERE session_id = $session_id ORDER BY duration_ns DESC LIMIT 30
```
## `telemetry_traces`

One row per trace, derived from its spans.

| Column | DuckDB type | Use it for |
|---|---|---|
| `session_id` | `VARCHAR` | The automatic $session_id scope; normally do not hard-code it. |
| `trace_id` | `VARCHAR` | Trace-list links and correlating logs with spans. |
| `start_ns` | `BIGINT` | Time-series buckets and ordering. |
| `duration_ns` | `BIGINT` | Latency percentiles, slow-operation tables, and heatmaps. |
| `service_name` | `VARCHAR` | Grouping or filtering by service. |
| `name` | `VARCHAR` | Operation breakdowns and metric selection. |
| `span_count` | `BIGINT` |  |

### Recent traces

```sql
SELECT trace_id, service_name, name, start_ns, duration_ns, span_count FROM telemetry_traces WHERE session_id = $session_id ORDER BY start_ns DESC LIMIT 30
```
## `telemetry_logs`

One row per structured log record.

| Column | DuckDB type | Use it for |
|---|---|---|
| `timestamp_ns` | `BIGINT` | Time-series buckets and ordering. |
| `trace_id` | `VARCHAR` | Trace-list links and correlating logs with spans. |
| `span_id` | `VARCHAR` | Span-list links and parent-child investigation. |
| `severity` | `INTEGER` | Severity breakdowns and alert filters. |
| `body` | `VARCHAR` | Log search and log-list panels. |
| `attributes` | `VARCHAR` | Filtering known semantic-convention fields with json_extract_string. |
| `service_name` | `VARCHAR` | Grouping or filtering by service. |
| `session_id` | `VARCHAR` | The automatic $session_id scope; normally do not hard-code it. |
| `received_at` | `BIGINT` |  |

### Log volume by severity

```sql
SELECT (timestamp_ns // 60000000000) * 60000000000 AS timestamp_ns, severity AS group_value, count(*) AS value FROM telemetry_logs WHERE session_id = $session_id GROUP BY 1, 2 ORDER BY 1, 2
```
## `telemetry_metrics`

One row per metric point.

| Column | DuckDB type | Use it for |
|---|---|---|
| `name` | `VARCHAR` | Operation breakdowns and metric selection. |
| `description` | `VARCHAR` |  |
| `unit` | `VARCHAR` |  |
| `type` | `VARCHAR` |  |
| `aggregation_temporality` | `VARCHAR` |  |
| `is_monotonic` | `BOOLEAN` |  |
| `start_timestamp_ns` | `BIGINT` |  |
| `timestamp_ns` | `BIGINT` | Time-series buckets and ordering. |
| `flags` | `UINTEGER` |  |
| `value` | `DOUBLE` | Metric charts and scalar values. |
| `histogram_count` | `UBIGINT` |  |
| `histogram_sum` | `DOUBLE` |  |
| `histogram_min` | `DOUBLE` |  |
| `histogram_max` | `DOUBLE` |  |
| `explicit_bounds` | `VARCHAR` |  |
| `bucket_counts` | `VARCHAR` |  |
| `exp_scale` | `INTEGER` |  |
| `exp_zero_count` | `UBIGINT` |  |
| `exp_zero_threshold` | `DOUBLE` |  |
| `exp_positive_offset` | `INTEGER` |  |
| `exp_positive_counts` | `VARCHAR` |  |
| `exp_negative_offset` | `INTEGER` |  |
| `exp_negative_counts` | `VARCHAR` |  |
| `summary_count` | `UBIGINT` |  |
| `summary_sum` | `DOUBLE` |  |
| `summary_quantiles` | `VARCHAR` |  |
| `attributes` | `VARCHAR` | Filtering known semantic-convention fields with json_extract_string. |
| `resource` | `VARCHAR` | Filtering deployment and service resource fields. |
| `series_attributes` | `VARCHAR` |  |
| `series_key` | `VARCHAR` |  |
| `scope_name` | `VARCHAR` |  |
| `scope_version` | `VARCHAR` |  |
| `scope_schema_url` | `VARCHAR` |  |
| `scope_attributes` | `VARCHAR` |  |
| `exemplars` | `VARCHAR` |  |
| `service_name` | `VARCHAR` | Grouping or filtering by service. |
| `session_id` | `VARCHAR` | The automatic $session_id scope; normally do not hard-code it. |

### Metric values over time

```sql
SELECT timestamp_ns, avg(value) AS value FROM telemetry_metrics WHERE session_id = $session_id GROUP BY 1 ORDER BY 1
```
