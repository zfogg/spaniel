# Dashboard read performance and visibility

Verified on the local Ratio, Spaniel and Stonkpile collectors on 2026-10-20.

Dashboard panels, annotations, editor catalogs and metric charts disable their
query observers while the browser document is hidden. Leaving the dashboard
unmounts its panel observers. Returning to a visible tab marks cached reads stale
and refreshes the mounted views once. In-flight panel/chart fetches consume the
abort signal. Dashboard variable overrides and the selected metric operation
survive metadata refreshes.

Metric events refresh only the selected matching stream; unrelated metric events
can update the catalog without reloading that chart. Existing requests finish
instead of being repeatedly canceled by invalidations.

The browser reports visibility on its existing WebSocket. Hidden clients receive
alerts, notification updates and heartbeats, while UI telemetry broadcasts are
suppressed. With no visible clients, the ingestion publisher skips derived stats
queries. The periodic UI broadcaster uses ingestion's in-memory throughput
counters instead of scanning database counts. Ingestion, retention and alert
evaluation continue independently of viewers. Older clients that do not send
visibility messages remain compatible and count as visible; reload existing tabs
to activate the new behavior.

Metric chart reads omit unused raw resource/attribute blobs and reuse decoded
series identities, histogram bounds and scope metadata within each response.
The raw storage/MCP inspection path retains those blobs. JSON responses negotiate
gzip and use the existing fast JSON encoder. No observations, buckets, exemplars,
scope attributes or trace overlays are downsampled or removed.

## Measured results

Five reads per series used the same historical Ratio session and fixed cutoff
before and after deployment. Canonical SHA-256 hashes of the complete decoded
JSON matched on every read, including trace overlays.

| Series | Points | Before wire bytes | After gzip bytes | Median before/after |
| --- | ---: | ---: | ---: | ---: |
| Maker routine executions | 10,744 | 1,295,901 | 85,473 | 219 / 219 ms |
| Maker DB duration p95 | 8,673 | 4,159,003 | 402,614 | 343 / 344 ms |
| Worker routine executions | 4,929 | 601,561 | 36,102 | 125 / 125 ms |

Transfer size fell 90–94%; local end-to-end latency was effectively unchanged.
The reduction in hidden/duplicate requests is verified independently of those
single-request timings. Fourteen production-asset browser checks passed,
including no hidden panel reads, one resume refresh, preserved variables and
operation, and no selected-series refresh for unrelated streams. All 53 frontend
unit suites passed (322 tests, two skipped); lint and formatting passed. Native
API, storage, live publisher, WebSocket and CLI packages passed race tests.

Under 48 chart reads with eight concurrent readers, six ingestion batches each
retained 32 spans, events and links, with ingestion responses at 0–63 ms on the
isolated candidate. After rollout, Ratio ingestion batches completed in 78–94 ms.

## Deployment and preservation

Image `spaniel:visible-dashboards-20261020`, identity
`sha256:4bb04ba0e715b9b0d718166caf785a696d075e24bdb2799fcf898747dd1acd39`,
was installed on all three existing collectors. Each database was backed up after
graceful shutdown. Named volumes, ports, command arguments, environment, previous
active sessions and retention policies were preserved. Stonkpile's existing
`auto_prune: false` setting was retained.

All three passed health, OTLP trace/log/metric ingestion and MCP persistence
readback. The earlier 20,000-trace/80,000-span retention fixture remained intact
after restart and the guard interval; drop counters were zero and storage was not
full. Local proof JSON and consistent database backups are under Ratio's ignored
`.local/retention-incident/` directory. Runtime data and backups are not committed.
