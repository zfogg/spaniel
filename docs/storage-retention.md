# Storage reclamation and retention

The size guard measures live allocated DuckDB blocks plus the WAL, excluding
blocks already marked free for reuse. Session and table sizes are uncompressed
live payload estimates; shared indexes, metadata, free blocks and deleted tuples
are not assigned to individual sessions. The storage breakdown also reports the
physical main-file and WAL sizes separately.

With automatic pruning enabled, the guard starts at 80% of the configured cap
and targets 70%. It first checkpoints and reclaims deleted tuples. DuckDB's
bundled version does not compact indexed row groups during checkpoint, and
`VACUUM` does not reclaim deleted rows. Maintenance therefore copies live rows
from affected tables and recreates their original schemas and secondary indexes
inside a transaction, with appenders closed and the SQL pool reserved. Appenders
reopen against the replacement tables when maintenance finishes. A failed table
rebuild rolls back that table's replacement.

Only if the reclaimed allocation still exceeds the target does retention evict
live telemetry. Each pass selects a small proportional batch from one global
oldest-first queue of whole traces, independent logs and metric points. Logs
correlated with retained traces leave with those traces. Trace findings, events
and links leave with their owning traces. Session age/count policies continue to
protect the active and baseline session records; the size cap can evict old
telemetry within those sessions.

If deletion and reclamation do not reduce allocation, retention returns an error
and remembers the unreclaimable floor. Repeated guard passes cannot keep erasing
data to satisfy an impossible cap. The guard pauses ingestion on failure, rather
than emptying the database. Raising the target above that floor or successfully
reducing the allocation allows size retention to proceed again.

`spaniel compact` uses the same reclamation path without deleting live data.
DuckDB can retain free blocks for subsequent writes, so the physical file need
not shrink to the live allocation. Compaction results report physical file bytes;
the usage statistics report live allocated blocks plus WAL.

Run `make test-retention` for reclamation, ordering, unreachable-cap and SQL-pool
isolation regressions. `make test` remains the full backend validation target.

DuckDB documents its reclamation limits in
[Reclaiming Space](https://duckdb.org/docs/stable/operations_manual/footprint_of_duckdb/reclaiming_space).
