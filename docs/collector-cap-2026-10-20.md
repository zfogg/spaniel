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
releases it before a 100 ms ingestion window. Successful deletions schedule the
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
