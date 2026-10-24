package model

// AgentQueries are bounded read projections for agent investigations.
type AgentQueries interface {
	// WITH selected AS (
	//   SELECT
	//     'spans' AS signal,
	//     'attributes' AS scope,
	//     attributes AS attrs
	//   FROM spans
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'spans'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'attributes'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR start_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR start_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'spans' AS signal,
	//     'resource' AS scope,
	//     resource AS attrs
	//   FROM spans
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'spans'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'resource'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR start_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR start_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'logs' AS signal,
	//     'attributes' AS scope,
	//     attributes AS attrs
	//   FROM logs
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'logs'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'attributes'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR timestamp_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR timestamp_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'metrics' AS signal,
	//     'attributes' AS scope,
	//     attributes AS attrs
	//   FROM metrics
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'metrics'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'attributes'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR timestamp_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR timestamp_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'metrics' AS signal,
	//     'resource' AS scope,
	//     resource AS attrs
	//   FROM metrics
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'metrics'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'resource'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR timestamp_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR timestamp_ns <= @toNs
	//     )
	// ),
	// entries AS (
	//   SELECT
	//     signal,
	//     scope,
	//     k.key,
	//     json_extract(
	//       selected.attrs,
	//       '/' || replace(replace(k.key, '~', '~0'), '/', '~1')
	//     ) AS value
	//   FROM selected,
	//   UNNEST(json_keys(selected.attrs)) k(key)
	// )
	// SELECT
	//   signal,
	//   scope,
	//   key,
	//   COUNT(*) AS count
	// FROM entries
	// WHERE
	//   (
	//     @prefix = ''
	//     OR starts_with(key, @prefix)
	//   )
	// GROUP BY
	//   signal,
	//   scope,
	//   key
	// ORDER BY
	//   count DESC,
	//   signal,
	//   scope,
	//   key
	// LIMIT @limit
	// OFFSET @offset
	DiscoverAttributeKeys(sessionID, service, signal, scope, prefix string, fromNs, toNs int64, limit, offset int) ([]AttributeKeyRow, error)

	// WITH selected AS (
	//   SELECT
	//     'spans' AS signal,
	//     'attributes' AS scope,
	//     attributes AS attrs
	//   FROM spans
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'spans'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'attributes'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR start_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR start_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'spans' AS signal,
	//     'resource' AS scope,
	//     resource AS attrs
	//   FROM spans
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'spans'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'resource'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR start_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR start_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'logs' AS signal,
	//     'attributes' AS scope,
	//     attributes AS attrs
	//   FROM logs
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'logs'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'attributes'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR timestamp_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR timestamp_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'metrics' AS signal,
	//     'attributes' AS scope,
	//     attributes AS attrs
	//   FROM metrics
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'metrics'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'attributes'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR timestamp_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR timestamp_ns <= @toNs
	//     )
	//   UNION ALL
	//   SELECT
	//     'metrics' AS signal,
	//     'resource' AS scope,
	//     resource AS attrs
	//   FROM metrics
	//   WHERE
	//     (
	//       @signal = ''
	//       OR @signal = 'metrics'
	//     )
	//     AND (
	//       @scope = ''
	//       OR @scope = 'resource'
	//     )
	//     AND (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR timestamp_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR timestamp_ns <= @toNs
	//     )
	// ),
	// entries AS (
	//   SELECT
	//     signal,
	//     scope,
	//     k.key,
	//     json_extract(
	//       selected.attrs,
	//       '/' || replace(replace(k.key, '~', '~0'), '/', '~1')
	//     ) AS value
	//   FROM selected,
	//   UNNEST(json_keys(selected.attrs)) k(key)
	// )
	// SELECT
	//   signal,
	//   scope,
	//   key,
	//   COALESCE(json_extract_string(value, '$'), 'null') AS value,
	//   json_type(value) AS value_type,
	//   COUNT(*) AS count
	// FROM entries
	// WHERE
	//   key = @key
	// GROUP BY
	//   signal,
	//   scope,
	//   key,
	//   value,
	//   value_type
	// ORDER BY
	//   count DESC,
	//   signal,
	//   scope,
	//   value,
	//   value_type
	// LIMIT @limit
	// OFFSET @offset
	DiscoverAttributeValues(sessionID, service, signal, scope, key string, fromNs, toNs int64, limit, offset int) ([]AttributeValueRow, error)

	// SELECT
	//   COUNT(*) AS span_count,
	//   COUNT(DISTINCT trace_id) AS trace_count,
	//   COUNT(*) FILTER (WHERE status_code = 2) AS error_count,
	//   CAST(COALESCE(quantile_cont(duration_ns, 0.5), 0) AS BIGINT) AS p50_ns,
	//   CAST(COALESCE(quantile_cont(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns,
	//   CAST(COALESCE(quantile_cont(duration_ns, 0.99), 0) AS BIGINT) AS p99_ns,
	//   COALESCE(MAX(start_ns), 0) AS last_span_ns
	// FROM spans
	// WHERE
	//   (
	//     @sessionID = ''
	//     OR session_id = @sessionID
	//   )
	//   AND (
	//     @service = ''
	//     OR service_name = @service
	//   )
	//   AND (
	//     @fromNs = 0
	//     OR start_ns >= @fromNs
	//   )
	//   AND (
	//     @toNs = 0
	//     OR start_ns <= @toNs
	//   )
	ServiceOverview(sessionID, service string, fromNs, toNs int64) ([]ServiceOverviewRow, error)

	// SELECT
	//   name,
	//   kind,
	//   COUNT(*) AS count,
	//   COUNT(*) FILTER (WHERE status_code = 2) AS error_count,
	//   CAST(COALESCE(quantile_cont(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns
	// FROM spans
	// WHERE
	//   (
	//     @sessionID = ''
	//     OR session_id = @sessionID
	//   )
	//   AND (
	//     @service = ''
	//     OR service_name = @service
	//   )
	//   AND (
	//     @fromNs = 0
	//     OR start_ns >= @fromNs
	//   )
	//   AND (
	//     @toNs = 0
	//     OR start_ns <= @toNs
	//   )
	// GROUP BY
	//   name,
	//   kind
	// ORDER BY
	//   count DESC,
	//   name,
	//   kind
	// LIMIT @limit
	ServiceOperations(sessionID, service string, fromNs, toNs int64, limit int) ([]ServiceOperationRow, error)

	// WITH selected AS (
	//   SELECT
	//     trace_id,
	//     span_id,
	//     session_id
	//   FROM spans
	//   WHERE
	//     (
	//       @sessionID = ''
	//       OR session_id = @sessionID
	//     )
	//     AND (
	//       @service = ''
	//       OR service_name = @service
	//     )
	//     AND (
	//       @fromNs = 0
	//       OR start_ns >= @fromNs
	//     )
	//     AND (
	//       @toNs = 0
	//       OR start_ns <= @toNs
	//     )
	// ),
	// findings AS (
	//   SELECT
	//     'detector' AS source,
	//     i.kind AS rule_id,
	//     i.trace_id,
	//     i.wasted_ns
	//   FROM trace_issues i
	//   WHERE
	//     EXISTS (
	//       SELECT
	//         1
	//       FROM selected s
	//       WHERE
	//         s.trace_id = i.trace_id
	//         AND s.session_id = i.session_id
	//         AND (
	//           s.span_id = i.parent_span_id
	//           OR s.span_id = i.example_span_id
	//         )
	//     )
	//   UNION ALL
	//   SELECT
	//     'lint' AS source,
	//     w.rule_id,
	//     w.trace_id,
	//     0 AS wasted_ns
	//   FROM lint_warnings w
	//   WHERE
	//     EXISTS (
	//       SELECT
	//         1
	//       FROM selected s
	//       WHERE
	//         s.span_id = w.span_id
	//         AND s.session_id = w.session_id
	//     )
	// )
	// SELECT
	//   source,
	//   rule_id,
	//   COUNT(*) AS count,
	//   COUNT(DISTINCT trace_id) AS trace_count,
	//   SUM(wasted_ns) AS wasted_ns,
	//   MIN(trace_id) AS example_trace_id
	// FROM findings
	// GROUP BY
	//   source,
	//   rule_id
	// ORDER BY
	//   count DESC,
	//   source,
	//   rule_id
	// LIMIT @limit
	ServiceFindings(sessionID, service string, fromNs, toNs int64, limit int) ([]ServiceFindingRow, error)

	// SELECT
	//   'logs' AS signal,
	//   COUNT(*) AS count,
	//   COUNT(*) FILTER (WHERE severity >= 17) AS error_count,
	//   COALESCE(MAX(timestamp_ns), 0) AS last_ns
	// FROM logs
	// WHERE
	//   (
	//     @sessionID = ''
	//     OR session_id = @sessionID
	//   )
	//   AND (
	//     @service = ''
	//     OR service_name = @service
	//   )
	//   AND (
	//     @fromNs = 0
	//     OR timestamp_ns >= @fromNs
	//   )
	//   AND (
	//     @toNs = 0
	//     OR timestamp_ns <= @toNs
	//   )
	// UNION ALL
	// SELECT
	//   'metrics' AS signal,
	//   COUNT(*) AS count,
	//   0 AS error_count,
	//   COALESCE(MAX(timestamp_ns), 0) AS last_ns
	// FROM metrics
	// WHERE
	//   (
	//     @sessionID = ''
	//     OR session_id = @sessionID
	//   )
	//   AND (
	//     @service = ''
	//     OR service_name = @service
	//   )
	//   AND (
	//     @fromNs = 0
	//     OR timestamp_ns >= @fromNs
	//   )
	//   AND (
	//     @toNs = 0
	//     OR timestamp_ns <= @toNs
	//   )
	ServiceSignals(sessionID, service string, fromNs, toNs int64) ([]ServiceSignalRow, error)
}

type AttributeKeyRow struct {
	Signal string `json:"signal"`
	Scope  string `json:"scope"`
	Key    string `json:"key"`
	Count  int64  `json:"count"`
}
type AttributeValueRow struct {
	Signal    string `json:"signal"`
	Scope     string `json:"scope"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	ValueType string `json:"value_type"`
	Count     int64  `json:"count"`
}
type ServiceOverviewRow struct{ SpanCount, TraceCount, ErrorCount, P50Ns, P95Ns, P99Ns, LastSpanNs int64 }
type ServiceOperationRow struct {
	Name                     string
	Kind                     int
	Count, ErrorCount, P95Ns int64
}
type ServiceFindingRow struct {
	Source, RuleID              string
	Count, TraceCount, WastedNs int64
	ExampleTraceID              string
}
type ServiceSignalRow struct {
	Signal                    string
	Count, ErrorCount, LastNs int64
}
