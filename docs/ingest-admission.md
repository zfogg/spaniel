# Canceling queued ingestion

The collector limits concurrent ingestion requests to 64. An exporter that
timed out while waiting for a slot previously stayed blocked until another
request finished, then entered storage with its canceled context. Under a burst,
expired work could keep occupying the queue after clients had already left.

Admission now checks cancellation before queuing, selects between capacity and
the context deadline, and checks cancellation again after acquiring a slot.
Canceled waiters release their pending count without releasing another
request's slot. Trace, log and metric ingestion return the context error before
touching storage. Accepted requests keep the existing durability behavior.

`make test-ingest-admission` uses the platform's normal DuckDB toolchain and
race detector. It checks all three signals, cancellation before admission and
while the queue is full, slot accounting, tracing gaps, and HTTP/DB semantic
lint rules. No storage schema or generator input changes.
