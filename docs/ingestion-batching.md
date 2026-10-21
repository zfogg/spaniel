# Shared ingestion batches

OTLP traces, logs and metrics share one bounded ingestion coordinator. It
collects exports for at most one second from the oldest queued export, then
processes them through the existing DuckDB columnar appenders and flushes the
group. Continuous arrivals do not reset the deadline. The collection window
does not include processing time or time waiting behind an executing batch.

Groups flush early at 4,096 signal records, 4 MiB of protobuf payload, or the
request limit. An export larger than a group target runs alone; exports are
not split at admission. The existing 4,096-row per-table appender threshold
and 200 ms storage backstop still bound writes during large exports.

There is a Go loop calling `AppendRow`, but no SQL INSERT per telemetry record.
Spans, events, links, logs and metric points use the five pinned appenders.
Lower-volume metadata operations, such as metric-series registration, still
use typed GORM Gen operations.

## Admission and acknowledgments

Queued and executing exports together are limited to 256 requests and 64 MiB
of protobuf payload. This is a payload accounting limit, not an exact heap
limit: decoded pdata, HTTP bodies and deferred notifications have overhead.
The existing HTTP body and gRPC message limits also apply before admission.
Admission overflow returns retryable HTTP 503 or gRPC RESOURCE_EXHAUSTED.
Shutdown rejection returns HTTP 503 or gRPC UNAVAILABLE. HTTP responses supply
a one-second Retry-After; gRPC queue exhaustion includes RetryInfo so exporters
can distinguish recoverable pressure from a permanent message-size failure,
following the [OTLP retry contract](https://opentelemetry.io/docs/specs/otlp/#failures).

One coordinator owns processing; exporters waiting for its flush do not hold
a separate processing semaphore.
Cancellation removes work immediately until its processing callback starts,
including work already selected into a group behind a slower export. Once a
callback starts, it retains ownership of pdata until completion so a canceled
exporter cannot reuse records while storage is reading them. Session identity
is captured at admission, so a session switch during collection does not
reassign queued telemetry.

An export receives success only after its shared flush succeeds. Background
and threshold failures remain observable at the acknowledgment barrier, even
if another explicit storage flush has already observed the error. Because
appenders can invalidate earlier buffered rows after a failure, any processing
write failure rejects the entire group conservatively. Tables and exports are
not one atomic transaction: retries can duplicate partially persisted data.
The queue is in memory; there is no early acknowledgment or durable spool.

Existing tail-based sampling still acknowledges admission into its separate
trace buffer before its later sampling decision. That preexisting sampling
contract is unchanged. Accepted tail-sampled records publish their live
updates after their own storage flush.

## Visibility and lifecycle

Foreground span/log events, linting, detector scheduling and accepted counters
are deferred until the shared flush succeeds. Derived live-state notifications
are coalesced to one per successful group. Ingestion tracing includes the shared
flush before ending request ingestion spans. Self telemetry retains its quiet
path without recursive instrumentation.

Shutdown flushes self telemetry while ingestion is available, then rejects new
work, immediately drains accepted groups, and runs
retention and closes storage. Storage keeps its existing exclusive maintenance
and reserved-connection rules; batching does not bypass them.

The existing queue depth/capacity instruments now describe the bounded shared
queue. `spaniel.ingest.queue.bytes` includes queued and executing protobuf
payloads. `spaniel.ingest.batch.exports` and `spaniel.ingest.batch.bytes` measure
shared group size; `spaniel.storage.flush.rows` measures actual appender flushes.

## Verification

Run `make test-ingest-batching` for focused race coverage and
`make test-extensive` for the complete backend/race/frontend/browser suite.
The focused suite covers coalescing, timer/row/byte triggers, continuous traffic,
capacity limits, queued/selected cancellation, shutdown, shared failures,
background failure visibility, mixed-signal persistence across reopening, and
maintenance/reader contention.

Live qualification compares isolated baseline/candidate collectors, checks
exact spans/events/links/logs/metric counts, exercises retryable overload and
sustained traffic alongside dashboard reads, drains pending exports on shutdown,
rechecks persisted records after restart, and verifies browser updates without
a manual reload. Measurements are workload-specific, not latency guarantees.

## Qualification on 2026-10-21

A shared-workstation run compared the deployed retention baseline with an
isolated candidate using the same workload and exact storage readbacks:

| Workload | Baseline maximum response | Candidate maximum response |
| --- | ---: | ---: |
| 100 concurrent one-span exports | 1.925 s | 1.159 s |
| 90 concurrent mixed-signal exports | 4.699 s | 1.141 s |
| Eight concurrent 1,024-span exports | 16.917 s | 1.340 s |
| Four exports with 1.1 MB attributes | 0.723 s | 1.207 s |

Each image retained exactly 8,326 spans, events and links, 30 logs and 30 metric
points. The candidate's measured workload CPU was 4.56 seconds versus 8.47
seconds for the baseline; post-workload cgroup memory was 184.8 MiB versus
231.4 MiB. These are single-run observations on a busy workstation. Collection
adds latency to small/low-volume or large-attribute exports; size limits do not
promise that every export completes within one second.

Additional qualification exercised 500 concurrent exports with admission
rejections and successful retries, 144 exports alongside 72 dashboard reads,
and shutdown with 80 pending exports. The shutdown check exposed final self
metrics retrying against the already-closed queue. Self exporters now stop
before queue closure, and a real self-monitoring regression checks their final
metrics survive reopening. The corrected live collector acknowledged all 80
pending exports and exited normally (code 0) in 6.63 seconds. Reopening retained
8,470 spans/events/links, 690 logs and 30 fixture metric points across all phases.
Browser inspection verified a uniquely named trace appeared live without
manual reload, followed by 20 concurrent exports adding 100 spans: the open
filtered dashboard advanced from 1 trace to 101 and exact readback matched. Queue/flush tests include actual appender failure and maintenance
reopening, in addition to deterministic coordinator failure injection.

Representative database-size regression: `make test-ingest-large-db` seeds one
million historical spans and at least 500 MiB of checkpointed live storage,
then checks 100 concurrent three-span exports alongside five dashboard reads
and a guarded checkpoint against a 10-second acknowledgment budget. The test
starts maintenance after rows are appended, so it cannot finish during the
collection window and miss the write/read boundary. It reopens storage and checks both
historical and newly accepted span counts. The routine suite runs the same
regression with 10,000 spans and 16 MiB. This isolates database size and reader
contention; it does not simulate a full production retention schedule or prove
that a busy host will always meet the deadline.

Live size investigation (before the batching deployment): Ratio reported about
498 MiB of live blocks plus WAL against its 500 MiB policy, while the main file
had about 652 MiB allocated. Its owner removed the largest sessions using the
supported API; live usage fell to about 294 MiB and a fresh, persisted one-span
export acknowledged in 96 ms. This is an observation across changing workloads,
not proof that database size alone caused the previous export timeouts.

The complete Windows routine suite and generated-contract checks passed. An
initial full Windows race run encountered a native heap-pointer failure while
MCP created a session. Identical single-worker MCP controls subsequently
passed three race runs on unchanged main and three on the candidate; the
original native failure did not reproduce in those controls.

The complete Linux backend and CLI race suite passed (including ingestion,
receiver, storage, MCP and self-monitoring shutdown). Routine Windows tests
and Linux race tests together cover the backend confidence checks; the
interrupted redundant Linux routine run is not counted as a successful run.

The committed frontend production build and formatting check passed. The
clean-install lint command could not start because the existing ESLint config
imports `typescript-eslint`, which is absent from package.json and the lockfile.
This tooling issue is recorded separately; batching changes no frontend or
frontend dependency files.

Representative-size result: 1,000,000 historical spans, 530,579,456 live bytes
(506 MiB) and 1,018,179,584 allocated bytes. The 100 three-span exports, five
dashboard reads and a checkpoint synchronized after append completed in
6.8117 seconds under the race detector. Closing and reopening retained exactly
1,000,000 historical spans and all 300 new spans. Fixture setup and reopening
made the complete explicit qualification run 416 seconds.

Final native failure regressions also passed: a buffered UNIQUE violation
reported only while maintenance closes its appender remains visible at the
export barrier, and a native reopen failure cannot be acknowledged away. Once
the failure has been observed, healthy restored tables continue writing.

The complete one-worker Playwright attempt finished with 137 passed and 32
failed. All 244 frontend source/contract files matched the unchanged committed
275fd7c baseline; these mocked browser tests run without the Go ingestion
server. Existing failures include duplicate initial dashboard reads and stale
search/source/navigation expectations. They are recorded as baseline failures,
not a successful end-to-end suite. The batching commit contains no frontend
changes. Actual collector load/readback and live browser-update qualification
are described above.
