# Physical retention without a telemetry proxy

The Stonkpile incident showed that automatic pruning itself was rejecting
telemetry. The size guard started at 95% of the configured physical file+WAL
cap, and PrunePhysical unconditionally set storage full. It then exported the
entire database to Parquet and imported it again, repeating a whole-file copy
after each fixed, at-most-32K-group eviction. Large mixed-signal stores needed
many copies. The saved production snapshot reproduced an export out-of-memory
failure at the private 2 GiB limit; a native-copy prototype with the old batch
sizing still needed sixteen copies after the initial rewrite (about 127 seconds).

The repair keeps the configured physical cap and 70% low-water target:

- Begin proactive work at 80%, leaving 20% headroom instead of 5%.
- Copy schema/data directly through DuckDB, without intermediate Parquet for
  the built-in schema. Recreate explicit indexes once, under the original
  catalog name: this DuckDB version cannot rebind catalog-qualified GORM index
  dependencies. Views, constraints and literal defaults survive. Custom
  sequence schemas retain the portable export/import path because native
  catalog rebinding fails for their defaults.
- One completed physical rewrite per automatic pass, followed by the existing
  five-second ingestion/read window. Shared process-local phase state prevents
  repeating fragmentation-only copies before every eviction. Restart safely
  begins with fragmentation reclamation again.
- Size the oldest coherent prefix to the remaining physical deficit and
  calibrate subsequent batches from actual bytes freed. Each selection is
  bounded to half the groups and at most 1,048,576 groups. This avoids repeatedly
  copying a store where old tiny metrics occupy less space than newer big logs.
- Leave admission open during successful below-cap phases. The actual hard cap
  and genuine recovery errors still gate ingestion, and hard-cap recovery stays
  latched until physical low water. A successful partial proactive phase is not
  storage full. Failed copies retry reclamation before another live eviction.

Whole-file replacement still serializes appenders/readers and completes native
work once isolated; a single copy is not preempted at an arbitrary deadline.
This avoids known driver corruption on interrupted checkpoint/rebuild work.
The change shortens and separates phases; it does not claim zero pause time
for an arbitrarily large database. Explicit manual pruning still runs through
to its requested physical target. Oldest-first trace grouping, dependencies,
metadata preservation, atomic candidate validation/sync/swap, and interrupted
rename recovery remain intact. No model, schema or generator input changes.

## Verification

The ordinary direct-ingestion regression uses real HTTP OTLP receivers and the
acknowledgment pipeline against a file database while the real guard replaces
that file. All three signals must receive 200 within five seconds, ingestion
must stay unpaused between proactive phases, newest accepted spans must survive,
and stat(main)+stat(WAL) must reach low water. No collector/proxy is involved.
Native-copy tests cover qualified indexes, views, defaults, constraints and
absence of the intermediate export; existing tests cover custom sequences,
concurrent readers, shared handles, restart and interrupted swap recovery.

The explicit snapshot qualification used the stopped production backup
`.local/pi-retention-before-20261021.duckdb` (SHA-256
`424f03eb52f0e83e85dcc17992fd0642f4955dee67ba80a56267203c2f38dc86`), copied
to disposable storage under the original basename. The initial 811,348,596-byte
database reached 362,033,152 bytes. After two independent random-log refills
past the 500 MiB hard cap, recovery reached 356,003,840 and 264,253,440 bytes,
respectively, and reopened admission. The persistent guard policy is retained
through every phase; the test no longer recreates it and accidentally forgets
the recovery latch. Assertions use physical bytes, not live blocks. Synthetic
data and destructive qualification operate only on the disposable copy.

Commands: `make test-retention RETENTION_TEST_RUN=Test` (complete storage and
command suites), `make test-ingest-admission` (race-enabled), and the explicit
snapshot probe with SPANIEL_REPRO_DB and SPANIEL_REPRO_GUARD=1.
`GOFLAGS=-race make test-retention` also passed the focused physical, native-copy,
maintenance, guard, reclamation and direct-ingestion regressions.

## Pi deployment

Deployed code `bdcca63970b9deb812e58a91a086ffa684432062` as
`stonkpile-spaniel:retention-bdcca63` (image
`sha256:02ddc162fd6bfeb98ea19b9fa0ecab3f14dbd599eabda52ae6b99203ac358a02`),
reporting `v0.2.2-312-gbdcca63`. The ARM64 backend was cross-built through
`make build-server` using the Debian-compatible cross compiler; the native
Pi toolchain required newer glibc than the runtime image. The existing live
UI was retained and the served index, JS and CSS hashes match its captured
assets. The unchanged four-CPU limit and original database volume remain.

At 21:53:18 UTC the live guard restored 342.26171875 MiB, below its 350 MiB
low-water target with the unchanged 500 MiB cap. The optional concurrent
age/count pass logged its bounded-maintenance deferral; it did not prevent
physical recovery. Sustained backlog delivery was slow and generated receiver
context timeouts during staged validation, so a one-shot HTTP health check
alone is not sufficient delivery evidence.

At 22:04:31 UTC, after fleet ports 4345/4346 returned to direct Spaniel
reception, trace/log/metric HTTP acknowledgments were 200 in 535/1253/1211 ms.
Trace `63b18527a1304c71a910843fcc0cf153`, its correlated log and gauge
`stonkpile.telemetry.direct_pruning_probe=1` were independently read through
MCP in session `59359e7b-d44a-489c-9180-1c359d7b8ffd`. Ingestion health was
healthy, storage_full=false, zero recorded drops, physical main+WAL
370,001,144 bytes. The temporary collector was disconnected from fleet ingress
and retained solely to drain accepted backlog before removal.

A subsequent live oldest-prefix pass at 22:09:48 UTC deleted 94,932 telemetry
groups and reduced physical main+WAL from 373,273,611 to 339,226,624 bytes
(323.51171875 MiB), below low water. The guard reopened admission. On this
I/O-bound Pi, physical replacement still causes exporter timeouts during the
exclusive file swap; this repair does not establish uninterrupted ingestion.

At 22:13 UTC the external collector and its init container were removed.
Backlog recovery had resumed, but competed with fleet ingestion on an
I/O-bound Pi; the remaining roughly 3.25 MB of logs and 15.98 MB of traces
were preserved offline in `stonkpile_stonkpile_otel_queue`, with a separate
consistent pre-drain copy at
`/home/alarm/.local/state/spaniel/build-bdcca63/queue-before-offline-drain`.
No queue volume was deleted. Recovery state/configuration and final collector
metrics are recorded alongside that copy. This is preserved historical data,
not an active collector or a dependency of fleet OTLP.

The live guard again restored 328.51171875 MiB at 22:11:30 UTC and
332.01171875 MiB at 22:13:33 UTC. A direct probe overlapping maintenance at
22:12:37 UTC timed out for all three signals; earlier successful direct probes
must not be used to claim uninterrupted telemetry.
