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
