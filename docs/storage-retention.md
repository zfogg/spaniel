# Storage reclamation and retention

The size guard measures live allocated DuckDB blocks plus the WAL, excluding
blocks already marked free for reuse. Session and table sizes are uncompressed
live payload estimates; shared indexes, metadata, free blocks and deleted tuples
are not assigned to individual sessions. The storage breakdown also reports the
physical main-file and WAL sizes separately.

With automatic pruning enabled, the guard starts at 80% of the configured cap
and targets 70%. It first checkpoints and reclaims deleted tuples. DuckDB's
bundled version does not compact indexed row groups during checkpoint, and
`VACUUM` does not reclaim deleted rows. For unconstrained telemetry tables,
maintenance journals the original nonunique indexes and temporarily drops them
so native checkpoint vacuum can merge deleted row groups. Sparse groups that
cannot merge use a committed unindexed staging copy followed by an atomic name,
default and index swap. Creating indexes only after the copy commits avoids
serializing the whole indexed replacement into a large logical WAL. Constrained
metadata tables retain their schema-preserving transactional rebuild.
The staged copy uses an all-rows `LIMIT 100 PERCENT` source to select ordinary
serial CTAS. This avoids a DuckDB 1.1.3 parallel final-batch path that can serialize
full row groups into the WAL instead of persisting their block references.

The durable journal is written in the same transaction as the index drops.
Migration 0041 stores stable index definitions and an inline active flag in a
small unindexed journal table. Definitions are initialized before storage budgets
are measured; ordinary passes change the flag, avoiding JSON insert/delete churn
in the indexed `meta` table. A pending definition cannot be overwritten by retry.
Startup repairs unfinished index work and removes only its abandoned staging
copy before migrations, reads or appenders start. Failure/cancellation restores
indexes using the reserved maintenance connection. Appender connection leases remain
reserved throughout maintenance. Transaction ownership outlives the optional work
deadline, while every SQL statement retains that deadline; explicit rollback
keeps a canceled transaction from releasing its connection to queued readers
during recovery or checkpointing. Queued dashboard reads cannot prevent
ingestion from resuming when appenders reopen.

Automatic maintenance has an eight-second work budget, including waiting for the
appender lock and draining readers. A completed pass is followed by the normal
five-second guard delay; overdue checks cannot chain pauses together. Automatic
checks with no age, count or size work do not enter exclusive maintenance.
Required index recovery and checkpoint cleanup finish before appenders reopen,
including after the optional work deadline expires.
Size maintenance checkpoints in a separate pass before attempting a rebuild or
eviction. A rebuild pass replaces at most one dirty table and checkpoints its
committed WAL before resuming ingestion; an eviction pass removes one coherent
oldest prefix. The no-progress floor is judged after the whole affected-table
cycle, not after its first small table. A visited-table mask prevents fresh
metadata updates from starving the larger tables still pending in that cycle.

The bundled engine normally starts implicit checkpoints at 16 MiB of WAL, even
inside a transaction commit that does not accept a cancellation context. The
connector defers that native fallback to 1 TB. The five-second collector guard
instead checkpoints WAL from 8 MiB using the same reserved pool/appender pause
and work budget. This checkpoint scheduling runs with automatic pruning disabled
and with no size cap; at high water the pruning pass owns the checkpoint so WAL
traffic cannot starve eviction. Durable WAL commits and shutdown checkpoints
remain enabled. WAL bytes still count toward the hard cap.
The engine's setting is documented under
[checkpoint_threshold](https://duckdb.org/docs/stable/configuration/overview).

Only if the reclaimed allocation still exceeds the target does retention evict
live telemetry. Each pass selects a small proportional batch from one global
oldest-first queue of whole traces, independent logs and metric points. Logs
correlated with retained traces leave with those traces. Trace findings, events
and links leave with their owning traces. Session age/count policies continue to
protect the active and baseline session records; the size cap can evict old
telemetry within those sessions.

If deletion and reclamation do not reduce allocation, retention remembers the
unreclaimable floor and backs off for five minutes. A work-budget timeout backs
off for thirty seconds. Repeated guard passes cannot keep erasing data to satisfy
an impossible target. Missing the optional 70% target alone does not mark a
below-cap database full. Ingestion pauses at the actual hard cap or on a genuine
storage failure, including an appender-restoration failure. Raising the target
above that floor or successfully reducing allocation permits size eviction again.
If rebuilding preexisting telemetry tombstones makes no allocation progress,
retention reports that floor before deleting additional surviving telemetry.

Migration 0040 removes three composite span indexes unused by the bundled
DuckDB's scan planner and a duplicate trace-ID index. The single-column trace,
parent, session and start-time indexes remain. This frees index allocation
without deleting telemetry or changing query results.

`spaniel compact` uses the same reclamation path without deleting live data.
DuckDB can retain free blocks for subsequent writes, so the physical file need
not shrink to the live allocation. Compaction results report physical file bytes;
the usage statistics report live allocated blocks plus WAL.
Reading the storage breakdown does not checkpoint or pause ingestion.

Run `make test-retention` for reclamation, ordering, unreachable-cap and SQL-pool
isolation regressions. `make test` remains the full backend validation target.

DuckDB documents its reclamation limits in
[Reclaiming Space](https://duckdb.org/docs/stable/operations_manual/footprint_of_duckdb/reclaiming_space).

The [2026-10-21 verification](collector-retention-verification-2026-10-21.md)
records the real-database retention, exporter-latency and crash-replay checks.
