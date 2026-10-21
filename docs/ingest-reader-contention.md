# OTLP metadata and dashboard reader contention

On 2026-10-20, Ratio's collector continued to return healthy status and zero
sampler drops while spans reached storage tens of seconds late. Those counters
do not measure exporter queueing or time waiting for a database connection.
The recent API end-to-received p95 reached approximately 40 seconds, with a
55.9 second maximum. A live 32-span batch containing an event and a causal link
on each span timed out after 65.453 seconds; the next two took 44.782 and 5.265
seconds.

Span rows already used pinned DuckDB appender connections, but their events
and links performed synchronous GORM inserts through the general SQL pool.
Dashboard reads could occupy that entire pool, stopping a batch between spans.
Events and links now use their own pinned appenders. All five telemetry
appenders share bounded buffering, request-end flush, graceful shutdown and
exclusive maintenance. The general SQL lane still has 16 connections; the
bounded total increases from 19 to 21 to reserve the two metadata connections.
The existing GORM methods remain available for direct metadata operations.

No schema migration is required. The legacy event/link attributes columns are
JSON, so their appender values use `json.RawMessage`; this preserves objects
and nested values instead of serializing the JSON text as a string. Ingestion
acknowledges only after flushing span rows, events and links.

Verification:

- `make test-ingest-backpressure BACKPRESSURE_TEST_RUN=.` passed the complete
  native ingestion and storage packages with race detection. A regression
  occupies every general-purpose SQL connection and ingests 32 spans with
  events and links before releasing any reader connection, for both normal
  and self telemetry. Additional checks preserve metadata through checkpoint,
  appender reopening and database reopening. Existing normalization, metadata,
  sampling, detector, retention and maintenance regressions also passed.
- An isolated container served 48 chart requests from eight concurrent readers
  over a 30,000-point metric series (2,845,068-byte responses). Six concurrent
  32-span/event/link batches completed in 16–218 ms. All 192 spans, 192 event
  objects and 192 link objects passed storage readback.
- After Ratio's rollout, the same three live metadata probes completed in
  141, 94 and 47 ms. All 96 spans, events and links were present with object
  attributes and preserved trace state. A later 60-second application window
  had API p95/max 165/340 ms (2,885 spans), maker 582/767 ms (1,065 spans), and
  worker 40/42 ms (18 spans). These are measured windows, not a guarantee of
  zero exporter delay under every future workload.

Image `spaniel:ingest-lane-20261020`, Docker image identity
`sha256:f8057fa2809755e97eab9d568b3caea23c598ea6e90c947d82f16ed677ada625`,
was deployed to Ratio, Spaniel and Stonkpile. Each database was backed up after
a graceful stop. Existing volumes, ports, environment, command, retention
settings and session-advancement preferences were preserved; the prior active
session was restored after startup. Stonkpile's preexisting `auto_prune=false`
was preserved. No findings or telemetry were cleared.

All three collectors passed health/MCP and trace/log/metric readback. Live
positive/negative controls still flag genuine server coverage gaps and serial
database phases while leaving client wait and parallel reads clean. Ratio's
original 20,000-trace/80,000-span fixture remained complete after restart and
another maintenance interval, with no dropped signals or storage-full flag.
This collector repair does not suppress genuine application query, lock or
financial-audit findings.
