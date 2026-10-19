# Pi5 collector crash and checkpoint ownership

The October 18 investigation found the `stonkpile-spaniel:retention-fix`
container without a live process, despite Docker reporting it as running.
Its final application log, dated October 9 at 04:21 UTC, was a native abort:

```text
terminate called after throwing an instance of 'duckdb::InternalException'
Invalid node type for GetAllocatorIdx: 0.
```

The deployed Go binary linked `github.com/marcboeker/go-duckdb v1.8.5`.
This is a DuckDB ART index allocator failure, not an OTLP connectivity error.
The missing collector caused Stonkpile's subsequent broken-pipe/export errors.
Restarting the collector restored ingestion. The retained logs contain no
native stack trace or failing SQL, so they do not identify the exact operation
that triggered the abort. An upstream report with the same native exception
also describes persisted index failures, including with newer engines:
<https://github.com/duckdb/duckdb/issues/25360>. It is supporting context, not
proof that Spaniel hit that report's ALTER TABLE trigger.

## Confirmed checkpoint gap and repair

Spaniel already closes every appender under `Batcher.Maintenance` before its
explicit CHECKPOINT and VACUUM operations. DuckDB's automatic checkpoint on
commit bypassed that gate. Flushing one table could therefore checkpoint while
the other tables' native appenders remained alive. In-memory tests cannot
exercise this path.

The writer connector now sets `debug_skip_checkpoint_on_commit=true` before
opening the database. The batcher's existing background loop checks the WAL
every five seconds and checkpoints at 16 MiB through the same maintenance gate
used by retention. It closes appenders, checkpoints, and reopens them before
accepting more appends. Commits remain durable in the WAL between checkpoints;
the threshold is a trigger, not a hard size cap during a burst. Checkpoint
errors are logged and retried on the next check. Maintenance cannot run after
the batcher is closed.

`TestCheckpointOwnsWALLifecycle` lowers the engine threshold to 1 KiB, writes
all three signals, verifies that commits leave the WAL intact, invokes guarded
maintenance, and verifies all rows after reopening the file. With automatic
checkpointing restored, the WAL assertion must fail. The concurrency regression
runs ingestion alongside retention/checkpoints and verifies durable row counts.

This removes a verified checkpoint ownership defect and mitigates the native
failure class. It does not reproduce the historical native abort or repair
arbitrary pre-existing index corruption. Keep a stopped-volume backup before
deployment and retain the previous image for rollback.
