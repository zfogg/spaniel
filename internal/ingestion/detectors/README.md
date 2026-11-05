# Detector implementation boundaries

Trace-local detectors implement `detectors.Detector` and are registered in `detectors.Registry`. Ingestion owns debouncing, bounded execution, trace loading, persistence, and notifications. `SupportsTrace` preserves the existing partial-trace policy; new detectors must make an explicit decision about partial evidence. The inline sampling path calls N+1 directly and must remain consistent with its post-ingest implementation.

Parallel work can add separate implementation and test files, coordinating registry changes at integration:

- #147 retry storms: trace-local attempt grouping and failure evidence; coordinate overlap with #150.
- #149 timeouts: trace-local timeout and cancellation evidence; coordinate with error-chain behavior.
- #150 fan-out: trace-local parent/dependency grouping; coordinate overlap with #147, chatty HTTP, and N+1.
- #148 pool saturation: acquire/wait spans and metric evidence require an evaluation boundary beyond the trace-only interface; define metric freshness and windows with the storage/metrics integration.
- #151 redelivery: bounded cross-trace message correlation requires its own lifecycle and privacy-preserving identity; do not force it into the trace-only interface.
- #152 cardinality growth: evaluate the metric catalog and admission history separately, respecting session and retention boundaries; coordinate metric data access with #148.

Each issue owns its evidence, deduplication, late-data behavior, and tests. Shared schema, API, UI, and registry changes should be integrated deliberately to avoid conflicting parallel edits. No cross-trace or metric detector interface is prescribed until its data and lifecycle requirements are established.

## Messaging redelivery

`messaging_redelivery` reports suspected poison-message loops on consumer `process` spans. It accepts `messaging.operation.name` (or the older `messaging.operation`), `messaging.system`, `messaging.destination.name`, `messaging.message.id`, and numeric `messaging.message.delivery_attempt`. Three distinct failed spans with the same message identity, or a failed span with delivery attempt 3 or higher, produce a finding. A span must have OpenTelemetry error status; attempts without a message ID can only produce the explicit-attempt finding. Batch spans whose `messaging.batch.message_count` is not one are excluded. The finding shows the destination, attempt count (or observed failures), an example failed span, and up to eight failed trace/span references for navigating correlated telemetry. Message IDs and payloads are never included in findings.

Correlation is limited to the same session, consumer service, messaging system and destination, and a ten-minute event-time window. Evidence is held only in memory, with a ten-minute idle expiry, at most 4096 identities and 64 spans per identity. Evidence is lost on restart, but a database-local keyed digest keeps finding identity stable across restarts and expiry. The key is stored in the database; it protects against exposing message IDs in findings, not against a reader with database access. Capacity, ingestion sampling, missing spans, old arrivals after expiry, and missing instrumentation can hide loops. Arrival order within the retained window does not matter and retransmitted spans are deduplicated. An eventual success does not erase earlier failures. Identity reuse or collision within the same scope cannot be distinguished from redelivery, and separate loops for a reused ID share a finding; producers should provide unique message IDs. Attempt numbers are treated as instrumentation-provided evidence, not independently verified delivery counts. Destination names are shown as supplied by instrumentation, so avoid putting sensitive data in them.
