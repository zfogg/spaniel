# Pool saturation evidence

The trace detector recognizes explicitly instrumented `db.operation.name` values
`connection.acquire` and `connection.wait` with a nonempty
`db.client.connection.pool.name` and service name. It reports an error span with
`error.type` equal to `connection_acquire_timeout` or `connection_pool_timeout`,
or at least three acquisition/wait spans of 100 ms or longer in one trace. It
links an example span and groups by service and pool within the trace and session.
Repeated analysis, including after late-arriving spans, updates the same finding.
Partial traces can provide positive evidence, but absence of a finding is not
evidence of health. Slow database queries alone are not pool evidence.

This initial detector does not evaluate utilization, pending-request or timeout
counter metrics. In particular it cannot detect saturation without supported
spans, or infer health from missing or stale metrics. Metric windows, resets,
and metric-only evidence need a separate session-scoped evaluation lifecycle and
telemetry links; the trace issue storage currently cannot represent such links.
