# Autoprune recovery hysteresis

The Stonkpile collector was configured with auto_prune=true and a 500 MiB cap.
Live logs showed hard-cap maintenance stepping from 545 MiB down through 500 MiB,
then reopening ingestion at 497.86 MiB despite its 350 MiB reclamation target.
The UI was reporting a real storage-pressure admission pause, not disk exhaustion:
the host still had about 23 GiB free.

The guard used the high-water threshold on every pass rather than retaining an
active low-water cycle. It also discarded its emergency recovery mode as soon
as one phase crossed below the hard cap. That reopened exporters and reduced
maintenance back to its short budget, while a minute-long proactive cooldown
left too little headroom for continuous incoming telemetry. A later WAL-only
checkpoint could independently clear the paused flag.

The policy now latches proactive pruning at 95% until <=70%. If the hard cap is
reached, it separately latches recovery admission and the larger per-phase budget
until the same target is reached. Intermediate below-cap/high-water results and
WAL checkpoints cannot reopen admission early. Proactive phases retain a
five-second read/ingest window rather than a minute per table. Existing bounded
single-table maintenance, oldest-first trace-coherent eviction, reclamation/floor
safety, disabled-autoprune preserve mode, and cap settings remain intact.
Updating the policy resets its latches, so disabling or changing limits remains
an explicit live operator control. A completed cycle logs its measured low-water
size. This changes neither the cap nor the target and does not reset the DB.

Three deterministic regressions failed before the fix: phased recovery at
101 -> 99 -> 94 -> 71 -> 70, proactive pruning at 95 -> 90 -> 80 -> 70, and a
WAL checkpoint during incomplete emergency recovery. They verify admission,
budget selection, completion, idle hysteresis, and switching to preserve mode.
Validation and deployment evidence will be appended after live verification.

Live-load follow-up: the first rollout continued pruning below the trigger, but
incoming writes consumed much of each physical reclamation gain. The guard now
also latches recovery after committed live-row eviction whose measured allocation
remains above target. Checkpoint-only/tombstone-only proactive phases remain
writable. Once live data is evicted, the collector holds retryable admission and
the larger budget through physical reclamation to low water. A fourth regression
fails against the first rollout and verifies 95 -> logical deletion at 96 ->
physical reclamation at 90 -> completion at 70 without reopening prematurely.

Validation: all four new regressions were demonstrated failing against their
respective prior implementation, then passed with the fix. The focused storage
guard/WAL tests also passed with the race detector. `make test-retention` and
the complete `make test` suite passed; the latter includes generated-contract
verification. Tests ran through Make in Linux Docker on the Windows workstation
because its native MSYS2 UCRT compiler was unavailable. Qualification used the
committed module files (gotel v0.7.4), excluding a concurrent dependency update.

Deployment: Pi's `stonkpile-spaniel` was replaced with image
`stonkpile-spaniel:autoprune-d39e270` at 2026-10-23 04:57:50 UTC. Its image digest
is `sha256:01602624240d107fea39d43951113449c43e1e5b78fc4b363c51cb8c54463bd7`;
the binary and MCP both report d39e270. The original volume, ports and four-CPU
limit were preserved. Effective settings remain auto_prune=true,
max_db_size_mb=500, retention_days=7 and max_sessions=50. A consistent stopped
database backup is under
`/home/alarm/.local/state/spaniel/backups/autoprune-d39e270-20261023/`.
The earlier image remains tagged `stonkpile-spaniel:rollback-autoprune-20261023`.

Physical-file correction: the operator clarified that the cap includes reusable
blocks in the main file, plus WAL. On Pi the main file was 624,701,440 bytes while
the old API reported only 391,643,136 used bytes. The prior latch fix therefore
did not satisfy the disk-footprint contract. Recovery now measures `FileSize`
(`os.Stat(main).Size()+os.Stat(WAL).Size()`), as do stats and preserve-mode limits.
Only the guard owns automatic size recovery; hourly retention handles age/count.

The physical loop first rewrites live contents into a clean database, avoiding
unnecessary deletion for existing holes. If still above target it deletes an
oldest coherent prefix (10% of groups, capped at 32,768), rewrites and measures
again. It stops at <=70% of the configured cap, or fails closed if there is no
evictable telemetry or no measured physical progress. Admission remains paused
through recovery. This replaces the expensive one-table rounds for automatic
size enforcement; existing explicit logical reclamation primitives remain.

All maintenance is serialized. Native appenders close and all SQL leases drain
before the serving pool closes. A private export/import rebuild preserves all
live schema/data, including non-telemetry configuration, sequences and indexes.
It uses the original base filename to preserve catalog-qualified index bindings
in this DuckDB version. The candidate is checkpointed, closed, reopened under
the restricted serving configuration and fsynced before replacement. The old
file remains recoverable across the two renames; startup restores it if interrupted
between renames. Stable GORM handles point through a submission gate to the new
pool, and appenders are restored before that gate opens. Private maintenance
I/O does not relax the serving pool's external-access restrictions. Temporary
export and rewrite space is needed during compaction; completed copies remove
their maintenance artifacts and old file.

New physical-file regressions verify repeated batch convergence using stat+WAL,
oldest-prefix survivors, post-swap append/query/restart persistence, hole-only
reclamation without live loss, custom tables, sequences/defaults/views/unique
constraints, concurrent readers under the race detector, and interrupted rename
recovery. The guard regression also verifies that tiny live-block usage cannot
bypass a physical high-water trigger.
