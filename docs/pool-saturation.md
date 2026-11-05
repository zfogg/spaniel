# Pool saturation evidence

The trace detector recognizes explicitly instrumented `db.operation.name` values
`connection.acquire` and `connection.wait` with a nonempty
`db.client.connection.pool.name` and service name. It reports an error span with
`error.type` equal to `connection_acquire_timeout` or `connection_pool_timeout`,
or at least three acquisition/wait spans of 100 ms or longer in one trace and
one fixed five-minute event-time window (aligned to Unix time). It
links an example span and groups by service and pool within the trace and session.
Repeated analysis, including after late-arriving spans, updates the same finding
for that window. Windows do not combine waits across their boundary. Span session
IDs control grouping even if the active session has since changed.
Partial traces can provide positive evidence, but absence of a finding is not
evidence of health. Slow database queries alone are not pool evidence.

Metric evidence is evaluated after its ingestion batch commits. For a named
pool and service in one session and fixed five-minute event-time window, two
positive `db.client.connection.pending_requests` gauge points at least ten
seconds apart indicate sustained pending requests. An increase between two
`db.client.connection.timeouts` cumulative monotonic sum points with the same
stream and start timestamp indicates timeouts. Counter decreases and changed
start times are not treated as increments. Only points from the preceding two
minutes are considered; late points within that horizon cause re-evaluation.
The query reads at most 256 points, and a single worker coalesces up to 256
pending pool/window identities; additional work is dropped during overload.
Findings appear in lint results and their inbox notification links to the metric
series. Missing or stale metrics are not evidence of health. Utilization and
delta counters are not currently evaluated.
