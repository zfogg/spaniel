# Collector size-cap rejection loop, 2026-10-20

Ratio's collector on `88ef2b2` repeatedly returned OTLP HTTP 503 with
`storage full`. The configured limit was 500 MiB with automatic pruning enabled.
Gotify's telemetry incidents matched the collector's rejection logs. The HTTP
process stayed healthy; this was storage backpressure rather than a crash.

The previous liveness repair limited each automatic pass to one deletion batch,
but still waited five seconds between passes. Cleanup could not keep up with
ingestion. It also stopped as soon as storage fell below the 95% trigger instead
of continuing to the intended 65% target. The resulting small amount of restored
space was quickly consumed, alternating rejections and brief recovery.

Automatic pruning now starts at 80% and keeps its catch-up state until the 65%
target is reached. Each pass still owns maintenance for only one batch and
releases it before an ingestion window proportional to batch duration (100 ms
to one second). Successful deletions schedule the
next pass promptly even if allocated blocks have not fallen yet: checkpoint
merging can require several deletion batches. Errors and empty batches use the
ordinary five-second interval, avoiding a busy loop at an irreducible size cap.
Disabling automatic pruning cancels catch-up and preserves the hard-cap behavior.

Size-only passes no longer checkpoint before deleting, and deletion reclamation
uses one checkpoint instead of a checkpoint/VACUUM/checkpoint sequence. DuckDB's
[space reclamation documentation](https://duckdb.org/docs/current/operations_manual/footprint_of_duckdb/reclaiming_space)
specifies that CHECKPOINT reclaims deleted space; VACUUM does not.

Regression coverage in `cmd/spaniel/retention_test.go` verifies watermarks,
continuation below the trigger, scheduling while allocated blocks remain flat,
backoff without progress, and live policy cancellation. Storage tests continue
to verify bounded batches, writes between passes, and preserved active sessions.

## Live flush-queue follow-up

A later live cycle remained below the cap but timed out exports. A SIGQUIT dump
captured the gate owner inside `duckdb_appender_flush`; retention itself was
waiting for the gate. Canceled HTTP exports were also queued for appends/flushes
and still retained ingestion admission slots. The original WithContext store
only applied cancellation to GORM, leaving columnar appends unaffected.

The batcher gate is now a context-aware weighted semaphore. Request-scoped
appends and flushes leave the queue immediately on cancellation, without
interrupting native DuckDB work already running. Trace, log and metric paths
propagate their contexts through these writes, including metric-series metadata.
Admission rechecks cancellation after acquiring a slot. A storage regression
holds the native gate and proves canceled span/log/metric/flush calls return
before it is released, write no rows, and leave fresh ingestion usable.

Maintenance also drains complete admitted exports before closing appenders,
using a shared request gate. Previously its row-level mutex could interrupt a
large export repeatedly between individual records. Periodic WAL checkpoints
use the same request gate. A queued maintenance writer prevents fresh exports
from starving cleanup; active exports finish first, and waiting exports remain
cancellable. Buffered sampler writes use this gate as well. A regression proves
an admitted export can append and flush while maintenance waits, and that new
exports cannot bypass maintenance once it owns the gate.

Bounded size cleanup now amortizes checkpoint/index I/O across 10,000 trace
groups per batch, matching the existing per-signal 10,000-record limit, rather
than repeatedly checkpointing the whole live database for 1,000 trace groups.
